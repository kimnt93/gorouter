package modelroute

import (
	"context"
	"errors"
	"math"
	"strings"

	"github.com/kimnt93/gorouter/pkg/chat"
	"github.com/kimnt93/gorouter/pkg/entities"
)

var (
	ErrModelName       = errors.New("model name is required")
	ErrModelStrategy   = errors.New("model strategy must be priority or round_robin")
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
	if m.Strategy != chat.StrategyPriority && m.Strategy != chat.StrategyRoundRobin {
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
	return s.repo.Upsert(ctx, m)
}

func (s *Service) Delete(ctx context.Context, name string) error { return s.repo.Delete(ctx, name) }

func (s *Service) List(ctx context.Context) ([]entities.ModelDef, error) { return s.repo.List(ctx) }

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
