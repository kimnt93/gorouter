package cliversion

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

var semver = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

type Cache interface {
	GetSnapshot(context.Context, string) ([]byte, bool, error)
	SetSnapshot(context.Context, string, []byte, time.Duration) error
}
type NPM struct {
	HTTP                   *http.Client
	Cache                  Cache
	TTL                    time.Duration
	Package, Baseline, URL string
	mu                     sync.Mutex
	version                string
	checked                time.Time
}
type npmSnapshot struct {
	Version   string    `json:"version"`
	CheckedAt time.Time `json:"checked_at"`
}

func NewNPM(client *http.Client, cache Cache, ttl time.Duration, pkg, baseline string) *NPM {
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	return &NPM{HTTP: client, Cache: cache, TTL: ttl, Package: pkg, Baseline: baseline, version: baseline}
}
func (r *NPM) Resolve(ctx context.Context) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	if now.Before(r.checked.Add(r.TTL)) {
		return r.version
	}
	key := "npm-version:v1:" + r.Package
	if r.Cache != nil {
		if b, ok, err := r.Cache.GetSnapshot(ctx, key); err == nil && ok && len(b) < 4096 {
			var s npmSnapshot
			if json.Unmarshal(b, &s) == nil && semver.MatchString(s.Version) && now.Before(s.CheckedAt.Add(r.TTL)) {
				r.version = max(r.Baseline, s.Version)
				r.checked = s.CheckedAt
				return r.version
			}
		}
	}
	endpoint := r.URL
	if endpoint == "" {
		endpoint = "https://registry.npmjs.org/" + strings.ReplaceAll(r.Package, "/", "%2f") + "/latest"
	}
	requestCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err == nil {
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "gorouter-cli-version-check")
		client := r.HTTP
		if client == nil {
			client = &http.Client{Timeout: 8 * time.Second}
		}
		if response, e := client.Do(req); e == nil {
			defer response.Body.Close()
			if response.StatusCode == 200 {
				var wire struct {
					Version string `json:"version"`
				}
				if json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&wire) == nil && semver.MatchString(wire.Version) {
					r.version = max(r.Baseline, wire.Version)
				}
			}
		}
	}
	r.checked = now.UTC()
	if r.Cache != nil {
		b, _ := json.Marshal(npmSnapshot{r.version, r.checked})
		_ = r.Cache.SetSnapshot(ctx, key, b, r.TTL)
	}
	return r.version
}
func max(a, b string) string {
	pa, pb := parts(a), parts(b)
	for i := range pa {
		if pb[i] > pa[i] {
			return b
		}
		if pb[i] < pa[i] {
			return a
		}
	}
	return a
}
func parts(s string) [3]int {
	var p [3]int
	_, _ = fmt.Sscanf(s, "%d.%d.%d", &p[0], &p[1], &p[2])
	return p
}

// Start refreshes compatibility metadata independently of provider traffic.
func (r *NPM) Start(ctx context.Context, interval time.Duration, report func(string)) {
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
