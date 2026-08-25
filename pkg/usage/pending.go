package usage

import "sync"

type Pending struct {
	mu sync.Mutex
	m  map[string]float64
}

func NewPending() *Pending { return &Pending{m: map[string]float64{}} }

func (p *Pending) Add(keyID string, v float64) {
	p.mu.Lock()
	p.m[keyID] += v
	p.mu.Unlock()
}

func (p *Pending) Sub(keyID string, v float64) {
	p.mu.Lock()
	p.m[keyID] -= v
	if p.m[keyID] <= 1e-12 {
		delete(p.m, keyID)
	}
	p.mu.Unlock()
}

func (p *Pending) Load(keyID string) float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.m[keyID]
}
