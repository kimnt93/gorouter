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

type Service struct{ repo Repository }

func NewService(repo Repository) *Service { return &Service{repo: repo} }

func (s *Service) Upsert(ctx context.Context, m entities.ModelDef) error {
	m.Name = strings.TrimSpace(m.Name)
	m.UpstreamModel = strings.TrimSpace(m.UpstreamModel)
	if m.Name == "" {
		return ErrModelName
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
		if m.Routes[i].CredentialID == "" || m.Routes[i].Weight <= 0 {
			return ErrCredentialRoute
		}
		if _, exists := seen[m.Routes[i].CredentialID]; exists {
			return ErrCredentialRoute
		}
		seen[m.Routes[i].CredentialID] = struct{}{}
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
	return s.repo.SetPrice(ctx, model, p)
}

func (s *Service) DeletePrice(ctx context.Context, model string) error {
	return s.repo.DeletePrice(ctx, model)
}

func (s *Service) Prices(ctx context.Context) (map[string]entities.Price, error) {
	return s.repo.ListPrices(ctx)
}
