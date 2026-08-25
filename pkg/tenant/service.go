package tenant

import (
	"context"
	"errors"
	"strings"

	"github.com/kimnt93/gorouter/pkg/entities"
)

var ErrNameRequired = errors.New("tenant name is required")

type Repository interface {
	List(ctx context.Context) ([]entities.Tenant, error)
	Create(ctx context.Context, name string) (*entities.Tenant, error)
	EnsureDefault(ctx context.Context) error
}

type Service struct{ repo Repository }

func NewService(repo Repository) *Service { return &Service{repo: repo} }

func (s *Service) List(ctx context.Context) ([]entities.Tenant, error) { return s.repo.List(ctx) }

func (s *Service) Create(ctx context.Context, name string) (*entities.Tenant, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrNameRequired
	}
	return s.repo.Create(ctx, name)
}

func (s *Service) EnsureDefault(ctx context.Context) error { return s.repo.EnsureDefault(ctx) }
