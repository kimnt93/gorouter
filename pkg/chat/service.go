package chat

import (
	"context"
	"sync"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
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

type PlanInput struct {
	Session     *entities.Session
	ModelName   string
	RawBody     []byte
	Price       *entities.Price
	QuotaUSD    *float64
	EstInTok    int64
	EstOutTok   int64
	CacheScope  string
	CacheEnable bool
}

// Plan resolves authorization, model routing candidates and quota for a chat call.
type Planner struct {
	Models     ModelLookup
	Creds      CredentialRouter
	UsageSpend SpendReader
	Selector   *Selector
	Health     *Health
}

type ModelLookup interface {
	List(ctx context.Context) ([]entities.ModelDef, error)
}

type CredentialRouter interface {
	RoutesForModel(ctx context.Context, model string) ([]entities.RouteCandidate, error)
	Runtime(ctx context.Context, id string) (*entities.CredentialRuntime, error)
}

type SpendReader interface {
	MonthSpendForKey(ctx context.Context, apiKeyID string) (float64, error)
}

var ErrModelNotFound = errorsNew("unknown model")
var ErrModelNotAllowed = errorsNew("model not allowed for this key")
var ErrNoCredentials = errorsNew("no healthy credential available")
var ErrQuotaExceeded = errorsNew("quota exceeded")

func errorsNew(s string) error { return &simpleError{s} }

type simpleError struct{ msg string }

func (e *simpleError) Error() string { return e.msg }

func (p *Planner) ResolveModel(ctx context.Context, name string) (*entities.ModelDef, error) {
	models, err := p.Models.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range models {
		if models[i].Name == name && models[i].Enabled {
			m := models[i]
			return &m, nil
		}
	}
	return nil, ErrModelNotFound
}

func (p *Planner) CheckAllowed(key *entities.ApiKey, modelName string) error {
	for _, m := range key.Models {
		if m == modelName {
			return nil
		}
	}
	return ErrModelNotAllowed
}

func (p *Planner) Candidates(ctx context.Context, sess *entities.Session, m *entities.ModelDef) ([]Candidate, error) {
	rows, err := p.Creds.RoutesForModel(ctx, m.Name)
	if err != nil {
		return nil, err
	}
	var cands []Candidate
	for _, rc := range rows {
		if rc.OwnerTenant != nil && sess.TenantID != "" && *rc.OwnerTenant != sess.TenantID {
			continue
		}
		if !p.Health.Available(rc.CredentialID) {
			continue
		}
		cands = append(cands, Candidate{ID: rc.CredentialID, Priority: rc.Priority, Weight: rc.Weight})
	}
	strategy := m.Strategy
	return p.Selector.Order(strategy, cands), nil
}

func (p *Planner) QuotaMessage(ctx context.Context, keyID string, quota *float64, price *entities.Price, estIn, estOut int64) string {
	if quota == nil || p.UsageSpend == nil {
		return ""
	}
	est := entities.ComputeCost(price, entities.TokenUsage{PromptTokens: estIn, CompletionTokens: estOut})
	spent, err := p.UsageSpend.MonthSpendForKey(ctx, keyID)
	if err != nil {
		return ""
	}
	if spent+est > *quota {
		return ErrQuotaExceeded.Error()
	}
	return ""
}
