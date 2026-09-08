package modelroute

import (
	"context"
	"errors"
	"math"
	"regexp"
	"strings"

	"github.com/kimnt93/gorouter/pkg/chat"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/provider"
)

var (
	ErrModelName       = errors.New("model name must contain only letters, numbers, underscores, and hyphens")
	ErrModelStrategy   = errors.New("model strategy must be priority, round_robin, or cache_affinity")
	ErrCredentialRoute = errors.New("model routes require unique credential IDs and positive weights")
	ErrInvalidPrice    = errors.New("model prices must be finite and non-negative")
)

type Repository interface {
	Upsert(ctx context.Context, m entities.ModelDef) error
	Delete(ctx context.Context, name string) error
	List(ctx context.Context) ([]entities.ModelDef, error)
	SetPrice(ctx context.Context, model string, p entities.Price) error
	DeletePrice(ctx context.Context, model string) error
	ListPrices(ctx context.Context) (map[string]entities.Price, error)
}

type PriceCache interface {
	SetManual(model string, price entities.Price)
	DeleteManual(model string)
}

type Service struct {
	repo       Repository
	priceCache PriceCache
}

var blendNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func NewService(repo Repository) *Service { return &Service{repo: repo} }

func (s *Service) SetPriceCache(cache PriceCache) { s.priceCache = cache }

func (s *Service) Upsert(ctx context.Context, m entities.ModelDef) error {
	m.Name = strings.TrimSpace(m.Name)
	m.UpstreamModel = strings.TrimSpace(m.UpstreamModel)
	if m.Name == "" {
		return ErrModelName
	}
	if m.UpstreamModel == "" {
		m.UpstreamModel = m.Name
	}
	if m.Strategy == "" {
		m.Strategy = chat.StrategyPriority
	}
	if m.Strategy != chat.StrategyPriority && m.Strategy != chat.StrategyRoundRobin && m.Strategy != chat.StrategyCacheAffinity {
		return ErrModelStrategy
	}
	seen := make(map[string]struct{}, len(m.Routes))
	for i := range m.Routes {
		m.Routes[i].CredentialID = strings.TrimSpace(m.Routes[i].CredentialID)
		m.Routes[i].UpstreamModel = strings.TrimSpace(m.Routes[i].UpstreamModel)
		if m.Routes[i].UpstreamModel == "" {
			m.Routes[i].UpstreamModel = m.UpstreamModel
		}
		if m.Routes[i].CredentialID == "" || m.Routes[i].UpstreamModel == "" || m.Routes[i].Weight <= 0 {
			return ErrCredentialRoute
		}
		routeKey := m.Routes[i].CredentialID + "\x00" + m.Routes[i].UpstreamModel
		if _, exists := seen[routeKey]; exists {
			return ErrCredentialRoute
		}
		seen[routeKey] = struct{}{}
	}
	if err := s.repo.Upsert(ctx, m); err != nil {
		return err
	}

	return nil
}

func (s *Service) Delete(ctx context.Context, name string) error { return s.repo.Delete(ctx, name) }

// List excludes legacy per-model auto aliases on every backend. Provider auto
// routes are generated once per provider by catalog reconciliation.
func (s *Service) List(ctx context.Context) ([]entities.ModelDef, error) {
	models, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]entities.ModelDef, 0, len(models))
	for _, model := range models {
		if model.UpstreamModel == "auto" && strings.HasSuffix(model.Name, "/auto") && !providerAutoName(model.Name) {
			continue
		}
		out = append(out, model)
	}
	return out, nil
}

func providerAutoName(name string) bool {
	for _, definition := range provider.Catalog() {
		canonical := provider.PublicModelID(definition.ID, "auto")
		if name == canonical {
			return true
		}
		if organization, rest, ok := strings.Cut(name, "/"); ok && organization != "" && rest == canonical {
			return true
		}
	}
	return false
}

func (s *Service) SetPrice(ctx context.Context, model string, p entities.Price) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return ErrModelName
	}
	for _, value := range []float64{p.InputPerM, p.OutputPerM, p.CachedInputPerM, p.CacheWritePerM} {
		if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return ErrInvalidPrice
		}
	}
	if err := s.repo.SetPrice(ctx, model, p); err != nil {
		return err
	}
	if s.priceCache != nil {
		s.priceCache.SetManual(model, p)
	}
	return nil
}

func (s *Service) DeletePrice(ctx context.Context, model string) error {
	if err := s.repo.DeletePrice(ctx, model); err != nil {
		return err
	}
	if s.priceCache != nil {
		s.priceCache.DeleteManual(model)
	}
	return nil
}

func (s *Service) Prices(ctx context.Context) (map[string]entities.Price, error) {
	return s.repo.ListPrices(ctx)
}

func ValidateBlendName(name string) error {
	if !blendNamePattern.MatchString(strings.TrimSpace(name)) {
		return ErrModelName
	}
	return nil
}

func isNamespacedModelName(name string) bool {
	parts := strings.Split(name, "/")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if !blendNamePattern.MatchString(part) {
			return false
		}
	}
	return true
}
