package chat

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type PromptCache interface {
	Lookup(scopeID, tenantID, model string, body []byte) (*CacheEntry, bool)
	Store(scopeID, tenantID, model string, body []byte, e *CacheEntry) bool
	Flush()
	Stats() CacheStats
	Close()
}

type CacheEntry struct {
	Status      int
	ContentType string
	Body        []byte
	Stream      bool
	PromptTok   int64
	Completion  int64
}

type CacheStats struct {
	Entries   int64   `json:"entries"`
	Bytes     int64   `json:"bytes"`
	Hits      uint64  `json:"hits"`
	Misses    uint64  `json:"misses"`
	Stores    uint64  `json:"stores"`
	Evictions uint64  `json:"evictions"`
	HitRatio  float64 `json:"hit_ratio"`
}

const (
	StrategyPriority      = "priority"
	StrategyRoundRobin    = "round_robin"
	StrategyCacheAffinity = "cache_affinity"
	ScopeKey              = "key"
	ScopeTenant           = "tenant"
	ScopeGlobal           = "global"
)

type Candidate struct {
	ID       string
	Priority int
	Weight   int
}

type Selector struct {
	mu      sync.Mutex
	counter uint64
	redis   redis.UniversalClient
}

// RouteAffinity keeps one explicit client session on the same provider
// credential. Every isolation dimension is hashed before it becomes a Redis
// key, so session/cache identifiers and tenant IDs are never stored verbatim.
type RouteAffinity struct {
	ScopeID  string
	TenantID string
	Model    string
	Value    string
}

const routeAffinityTTL = time.Hour

func (s *Selector) SetRedis(client redis.UniversalClient) { s.redis = client }

func (a RouteAffinity) redisKey() string {
	if strings.TrimSpace(a.Value) == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(a.ScopeID + "\x00" + a.TenantID + "\x00" + a.Model + "\x00" + a.Value))
	return fmt.Sprintf("gorouter:routing:affinity:%x", sum[:])
}

// OrderWithAffinity preserves round-robin distribution across independent
// sessions while pinning repeated calls from one explicit session to a single
// credential. Redis is an optimization boundary here: on an outage, routing
// safely continues with ordinary distributed/local round robin.
func (s *Selector) OrderWithAffinity(ctx context.Context, strategy string, in []Candidate, affinity RouteAffinity) []Candidate {
	if strategy == StrategyCacheAffinity {
		return orderByRendezvous(in, affinity)
	}
	if strategy != StrategyRoundRobin || s.redis == nil {
		return s.Order(strategy, in)
	}
	key := affinity.redisKey()
	if key == "" {
		return s.Order(strategy, in)
	}
	if candidateID, err := s.redis.Get(ctx, key).Result(); err == nil {
		if pinned, ok := pinCandidate(in, candidateID); ok {
			_ = s.redis.Expire(ctx, key, routeAffinityTTL).Err()
			return pinned
		}
		_ = s.redis.Del(ctx, key).Err()
	}
	ordered := s.Order(strategy, in)
	if len(ordered) == 0 {
		return ordered
	}
	stored, err := s.redis.SetNX(ctx, key, ordered[0].ID, routeAffinityTTL).Result()
	if err != nil || stored {
		return ordered
	}
	if candidateID, getErr := s.redis.Get(ctx, key).Result(); getErr == nil {
		if pinned, ok := pinCandidate(ordered, candidateID); ok {
			return pinned
		}
	}
	return ordered
}

// BindAffinity updates a session after failover so subsequent requests reuse
// the credential that actually succeeded.
func (s *Selector) BindAffinity(ctx context.Context, affinity RouteAffinity, candidateID string) {
	if s.redis == nil || candidateID == "" {
		return
	}
	if key := affinity.redisKey(); key != "" {
		_ = s.redis.Set(ctx, key, candidateID, routeAffinityTTL).Err()
	}
}

func orderByRendezvous(in []Candidate, affinity RouteAffinity) []Candidate {
	out := append([]Candidate(nil), in...)
	value := strings.TrimSpace(affinity.Value)
	if value == "" {
		// Without a reusable prefix, preserve explicit priority semantics rather
		// than collapsing unrelated one-turn requests onto one credential.
		for i := 1; i < len(out); i++ {
			for j := i; j > 0 && (out[j].Priority > out[j-1].Priority || out[j].Priority == out[j-1].Priority && out[j].ID < out[j-1].ID); j-- {
				out[j], out[j-1] = out[j-1], out[j]
			}
		}
		return out
	}
	seed := affinity.ScopeID + "\x00" + affinity.TenantID + "\x00" + affinity.Model + "\x00" + value
	for i := 1; i < len(out); i++ {
		for j := i; j > 0; j-- {
			left := sha256.Sum256([]byte(seed + "\x00" + out[j-1].ID))
			right := sha256.Sum256([]byte(seed + "\x00" + out[j].ID))
			if string(right[:]) <= string(left[:]) {
				break
			}
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func pinCandidate(in []Candidate, candidateID string) ([]Candidate, bool) {
	for index := range in {
		if in[index].ID != candidateID {
			continue
		}
		out := make([]Candidate, 0, len(in))
		out = append(out, in[index])
		out = append(out, in[:index]...)
		out = append(out, in[index+1:]...)
		return out, true
	}
	return nil, false
}

// Order returns candidates in attempt order for the configured strategy.
func (s *Selector) Order(strategy string, in []Candidate) []Candidate {
	out := make([]Candidate, len(in))
	copy(out, in)
	switch strategy {
	case StrategyRoundRobin:
		n := len(out)
		if n == 0 {
			return out
		}
		var count uint64
		if s.redis != nil {
			if value, err := s.redis.Incr(context.Background(), "gorouter:routing:round-robin").Uint64(); err == nil {
				count = value
			}
		}
		if count == 0 {
			s.mu.Lock()
			s.counter++
			count = s.counter
			s.mu.Unlock()
		}
		start := int(count-1) % n
		rotated := make([]Candidate, 0, n)
		rotated = append(rotated, out[start:]...)
		rotated = append(rotated, out[:start]...)
		return rotated
	default:
		for i := 1; i < len(out); i++ {
			for j := i; j > 0; j-- {
				if out[j].Priority > out[j-1].Priority ||
					(out[j].Priority == out[j-1].Priority && out[j].ID < out[j-1].ID) {
					out[j], out[j-1] = out[j-1], out[j]
				} else {
					break
				}
			}
		}
		return out
	}
}

type Health struct {
	mu       sync.Mutex
	failures map[string]int
	banned   map[string]time.Time
	redis    redis.UniversalClient
}

func (h *Health) SetRedis(client redis.UniversalClient) { h.redis = client }

func NewHealth() *Health {
	return &Health{failures: map[string]int{}, banned: map[string]time.Time{}}
}

func (h *Health) Available(id string) bool {
	if h.redis != nil {
		exists, err := h.redis.Exists(context.Background(), "gorouter:routing:banned:"+id).Result()
		if err == nil {
			return exists == 0
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	until, ok := h.banned[id]
	return !ok || time.Now().After(until)
}

func (h *Health) Report(id string, ok bool) {
	if h.redis != nil {
		ctx := context.Background()
		failureKey := "gorouter:routing:failures:" + id
		bannedKey := "gorouter:routing:banned:" + id
		if ok {
			_ = h.redis.Del(ctx, failureKey, bannedKey).Err()
			return
		}
		count, err := h.redis.Incr(ctx, failureKey).Result()
		if err == nil {
			_ = h.redis.Expire(ctx, failureKey, 2*time.Minute).Err()
			if count >= 3 {
				pipe := h.redis.TxPipeline()
				pipe.Set(ctx, bannedKey, "1", time.Minute)
				pipe.Del(ctx, failureKey)
				_, _ = pipe.Exec(ctx)
			}
			return
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if ok {
		delete(h.failures, id)
		delete(h.banned, id)
		return
	}
	h.failures[id]++
	if h.failures[id] >= 3 {
		h.banned[id] = time.Now().Add(60 * time.Second)
		delete(h.failures, id)
	}
}
