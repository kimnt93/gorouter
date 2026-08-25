package routing

import (
	"sort"
	"sync"
	"time"
)

const (
	StrategyPriority   = "priority"
	StrategyRoundRobin = "round_robin"
)

type Candidate struct {
	ID        string
	Priority  int
	Weight    int
	Enabled   bool
	Unhealthy bool
}

type Selector struct {
	mu      sync.Mutex
	counter uint64
}

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
		for _, c := range out[start:] {
			rotated = append(rotated, c)
		}
		for _, c := range out[:start] {
			rotated = append(rotated, c)
		}
		return rotated
	default:
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].Priority != out[j].Priority {
				return out[i].Priority > out[j].Priority
			}
			return out[i].ID < out[j].ID
		})
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
