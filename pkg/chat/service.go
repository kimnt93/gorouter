package chat

import (
	"sync"
	"time"
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
	StrategyPriority   = "priority"
	StrategyRoundRobin = "round_robin"
	ScopeKey           = "key"
	ScopeTenant        = "tenant"
	ScopeGlobal        = "global"
)

type Candidate struct {
	ID       string
	Priority int
	Weight   int
}

type Selector struct {
	mu      sync.Mutex
	counter uint64
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
		s.mu.Lock()
		s.counter++
		start := int(s.counter-1) % n
		s.mu.Unlock()
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
}

func NewHealth() *Health {
	return &Health{failures: map[string]int{}, banned: map[string]time.Time{}}
}

func (h *Health) Available(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	until, ok := h.banned[id]
	return !ok || time.Now().After(until)
}

func (h *Health) Report(id string, ok bool) {
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
