// Package codexversion resolves the latest stable official Codex CLI version
// used for the ChatGPT Codex model catalog compatibility gate.
package codexversion

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// Baseline must support the model/request protocol implemented by GoRouter.
	Baseline   = "0.156.0"
	releaseURL = "https://api.github.com/repos/openai/codex/releases/latest"
	cacheKey   = "codex:official-stable-version:v1"
)

var stableTag = regexp.MustCompile(`^rust-v([0-9]+\.[0-9]+\.[0-9]+)$`)

type Cache interface {
	GetSnapshot(context.Context, string) ([]byte, bool, error)
	SetSnapshot(context.Context, string, []byte, time.Duration) error
}
type Resolver struct {
	HTTP      *http.Client
	Cache     Cache
	TTL       time.Duration
	URL       string
	mu        sync.Mutex
	current   string
	checked   time.Time
	resolving bool
	wait      chan struct{}
}
type snapshot struct {
	Version   string    `json:"version"`
	CheckedAt time.Time `json:"checked_at"`
}

func New(client *http.Client, cache Cache, ttl time.Duration) *Resolver {
	if ttl <= 0 {
		ttl = 6 * time.Hour
	}
	return &Resolver{HTTP: client, Cache: cache, TTL: ttl, current: Baseline}
}

// Resolve returns a safe baseline on all external failures. Release lookup is a
// bounded availability optimization; it never broadens authorization by itself.
func (r *Resolver) Resolve(ctx context.Context) string {
	for {
		r.mu.Lock()
		now := time.Now()
		if r.current == "" {
			r.current = Baseline
		}
		if now.Before(r.checked.Add(r.TTL)) {
			value := r.current
			r.mu.Unlock()
			return value
		}
		if r.resolving {
			wait := r.wait
			value := r.current
			r.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return value
			}
		}
		if r.Cache != nil {
			if b, ok, err := r.Cache.GetSnapshot(ctx, cacheKey); err == nil && ok && len(b) < 4096 {
				var s snapshot
				if json.Unmarshal(b, &s) == nil && valid(s.Version) && !s.CheckedAt.IsZero() && now.Before(s.CheckedAt.Add(r.TTL)) {
					r.current = maxVersion(Baseline, s.Version)
					r.checked = s.CheckedAt
					value := r.current
					r.mu.Unlock()
					return value
				}
			}
		}
		r.resolving = true
		r.wait = make(chan struct{})
		wait := r.wait
		r.mu.Unlock()
		resolved := r.fetch(ctx)
		checked := time.Now().UTC()
		r.mu.Lock()
		if resolved != "" && compatible(resolved) {
			r.current = maxVersion(Baseline, resolved)
		}
		r.checked = checked
		value := r.current
		r.resolving = false
		close(wait)
		r.wait = nil
		r.mu.Unlock()
		if r.Cache != nil {
			b, _ := json.Marshal(snapshot{Version: value, CheckedAt: checked})
			_ = r.Cache.SetSnapshot(ctx, cacheKey, b, r.TTL)
		}
		return value
	}
}
func (r *Resolver) fetch(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	endpoint := r.URL
	if endpoint == "" {
		endpoint = releaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "gorouter-codex-catalog")
	client := r.HTTP
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	response, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ""
	}
	var release struct {
		Tag, HTML         string
		Draft, Prerelease bool
	}
	var wire struct {
		Tag        string `json:"tag_name"`
		HTML       string `json:"html_url"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&wire) != nil {
		return ""
	}
	release.Tag, release.HTML, release.Draft, release.Prerelease = wire.Tag, wire.HTML, wire.Draft, wire.Prerelease
	match := stableTag.FindStringSubmatch(release.Tag)
	if len(match) != 2 || release.Draft || release.Prerelease {
		return ""
	}
	if r.URL == "" && release.HTML != "https://github.com/openai/codex/releases/tag/"+release.Tag {
		return ""
	}
	return match[1]
}
func compatible(value string) bool {
	if !valid(value) {
		return false
	}
	return strings.Split(value, ".")[0] == strings.Split(Baseline, ".")[0]
}
func valid(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		if _, err := strconv.ParseUint(p, 10, 64); err != nil {
			return false
		}
	}
	return true
}
func maxVersion(a, b string) string {
	if compare(b, a) > 0 {
		return b
	}
	return a
}
func compare(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		x, _ := strconv.ParseUint(pa[i], 10, 64)
		y, _ := strconv.ParseUint(pb[i], 10, 64)
		if x > y {
			return 1
		}
		if x < y {
			return -1
		}
	}
	return 0
}

// Start checks at startup and on the requested interval. Resolve remains safe
// for request paths and coalesces with this loop when they overlap.
func (r *Resolver) Start(ctx context.Context, interval time.Duration, report func(string)) {
	if interval <= 0 {
		interval = 12 * time.Hour
	}
	go func() {
		run := func() {
			version := r.Resolve(ctx)
			if report != nil {
				report(version)
			}
		}
		run()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}
