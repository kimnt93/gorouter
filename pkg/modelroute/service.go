package modelroute

import (
	"context"

	"github.com/kimnt93/gorouter/pkg/entities"
)

type Repository interface {
	Upsert(ctx context.Context, m entities.ModelDef) error
	Delete(ctx context.Context, name string) error
	List(ctx context.Context) ([]entities.ModelDef, error)
	SetPrice(ctx context.Context, model string, p entities.Price) error
	ListPrices(ctx context.Context) (map[string]entities.Price, error)
}

type Service struct{ repo Repository }

func NewService(repo Repository) *Service { return &Service{repo: repo} }

func (s *Service) Upsert(ctx context.Context, m entities.ModelDef) error {
	return s.repo.Upsert(ctx, m)
}

func (s *Service) Delete(ctx context.Context, name string) error { return s.repo.Delete(ctx, name) }

func (s *Service) List(ctx context.Context) ([]entities.ModelDef, error) { return s.repo.List(ctx) }

func (s *Service) SetPrice(ctx context.Context, model string, p entities.Price) error {
	return s.repo.SetPrice(ctx, model, p)
}

func (s *Service) Prices(ctx context.Context) (map[string]entities.Price, error) {
	return s.repo.ListPrices(ctx)
}
