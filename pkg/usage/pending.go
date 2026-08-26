package usage

import (
	"sync"
	"time"
)

type pendingEntry struct {
	ts   time.Time
	cost float64
}

type Pending struct {
	mu sync.Mutex
	m  map[string][]pendingEntry
}

func NewPending() *Pending { return &Pending{m: map[string][]pendingEntry{}} }

func (p *Pending) Add(key string, cost float64) {
	p.AddAt(key, time.Now().UTC(), cost)
}

func (p *Pending) AddAt(key string, ts time.Time, cost float64) {
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	p.mu.Lock()
	p.m[key] = append(p.m[key], pendingEntry{ts.UTC(), cost})
	p.mu.Unlock()
}

func (p *Pending) Sub(key string, cost float64) {
	p.SubAt(key, time.Time{}, cost)
}

func (p *Pending) SubAt(key string, ts time.Time, cost float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	items := p.m[key]
	for i, v := range items {
		if v.cost != cost || (!ts.IsZero() && !v.ts.Equal(ts.UTC())) {
			continue
		}
		items = append(items[:i], items[i+1:]...)
		if len(items) == 0 {
			delete(p.m, key)
		} else {
			p.m[key] = items
		}
		return
	}
}

func (p *Pending) Load(key string) float64 { return p.LoadSince(key, time.Time{}) }

func (p *Pending) LoadSince(key string, since time.Time) float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	var total float64
	for _, v := range p.m[key] {
		if since.IsZero() || !v.ts.Before(since.UTC()) {
			total += v.cost
		}
	}
	return total
}
