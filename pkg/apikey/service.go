package apikey

import (
	"context"
	"errors"
	"math"
	"strings"

	"github.com/kimnt93/gorouter/pkg/entities"
)

var (
	ErrTenantRequired = errors.New("tenant ID is required")
	ErrNameRequired   = errors.New("API key name is required")
	ErrInvalidModel   = errors.New("model names must not be empty")
	ErrInvalidScope   = errors.New("invalid API key scope")
	ErrInvalidQuota   = errors.New("monthly quota must be non-negative")
	ErrInvalidRPM     = errors.New("RPM limit must be greater than zero")
)

type Repository interface {
	Create(ctx context.Context, tenantID, name string, models, scopes []string, quota *float64, rpm *int) (*entities.ApiKey, error)
	GetBySecret(ctx context.Context, secretHash string) (*entities.ApiKey, error)
	GetByID(ctx context.Context, id string) (*entities.ApiKey, error)
	GetByIDForTenant(ctx context.Context, tenantID, id string) (*entities.ApiKey, error)
	List(ctx context.Context) ([]entities.ApiKey, error)
	ListByTenant(ctx context.Context, tenantID string) ([]entities.ApiKey, error)
	Patch(ctx context.Context, id string, enabled *bool, models *[]string, scopes *[]string, quota **float64, rpm **int) error
	PatchForTenant(ctx context.Context, tenantID, id string, enabled *bool, models *[]string, scopes *[]string, quota **float64, rpm **int) error
	Delete(ctx context.Context, id string) error
	DeleteForTenant(ctx context.Context, tenantID, id string) error
}

type Service struct {
	repo   Repository
	hashFn func(string) string
	genFn  func() string
}

func NewService(repo Repository, hashFn func(string) string, genFn func() string) *Service {
	return &Service{repo: repo, hashFn: hashFn, genFn: genFn}
}

func (s *Service) hash(secret string) string { return s.hashFn(secret) }

type CreateInput struct {
	TenantID        string
	Name            string
	Models          []string
	Scopes          []string
	MonthlyQuotaUSD *float64
	RPM             *int
}

func (s *Service) Create(ctx context.Context, in CreateInput) (*entities.ApiKey, error) {
	var err error
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.Name = strings.TrimSpace(in.Name)
	if in.TenantID == "" {
		return nil, ErrTenantRequired
	}
	if in.Name == "" {
		return nil, ErrNameRequired
	}
	if in.Models, err = normalizeModels(in.Models); err != nil {
		return nil, err
	}
	if in.Scopes, err = normalizeScopes(in.Scopes); err != nil {
		return nil, err
	}
	if err := validateLimits(in.MonthlyQuotaUSD, in.RPM); err != nil {
		return nil, err
	}
	return s.repo.Create(ctx, in.TenantID, in.Name, in.Models, in.Scopes, in.MonthlyQuotaUSD, in.RPM)
}

func (s *Service) CreateForTenant(ctx context.Context, tenantID string, in CreateInput) (*entities.ApiKey, error) {
	in.TenantID = tenantID
	return s.Create(ctx, in)
}

func (s *Service) List(ctx context.Context) ([]entities.ApiKey, error) { return s.repo.List(ctx) }

func (s *Service) ListByTenant(ctx context.Context, tenantID string) ([]entities.ApiKey, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil, ErrTenantRequired
	}
	return s.repo.ListByTenant(ctx, tenantID)
}

func (s *Service) GetBySecret(ctx context.Context, secret string) (*entities.ApiKey, error) {
	return s.repo.GetBySecret(ctx, s.hash(secret))
}

func (s *Service) GetByID(ctx context.Context, id string) (*entities.ApiKey, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *Service) GetByIDForTenant(ctx context.Context, tenantID, id string) (*entities.ApiKey, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil, ErrTenantRequired
	}
	return s.repo.GetByIDForTenant(ctx, tenantID, id)
}

func (s *Service) Patch(ctx context.Context, id string, enabled *bool, models *[]string, scopes *[]string, quota **float64, rpm **int) error {
	if err := validatePatch(models, scopes, quota, rpm); err != nil {
		return err
	}
	return s.repo.Patch(ctx, id, enabled, models, scopes, quota, rpm)
}

func (s *Service) PatchForTenant(ctx context.Context, tenantID, id string, enabled *bool, models *[]string, scopes *[]string, quota **float64, rpm **int) error {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return ErrTenantRequired
	}
	if err := validatePatch(models, scopes, quota, rpm); err != nil {
		return err
	}
	return s.repo.PatchForTenant(ctx, tenantID, id, enabled, models, scopes, quota, rpm)
}

func (s *Service) Delete(ctx context.Context, id string) error { return s.repo.Delete(ctx, id) }

func (s *Service) DeleteForTenant(ctx context.Context, tenantID, id string) error {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return ErrTenantRequired
	}
	return s.repo.DeleteForTenant(ctx, tenantID, id)
}

func validatePatch(models *[]string, scopes *[]string, quota **float64, rpm **int) error {
	var err error
	if models != nil {
		if *models, err = normalizeModels(*models); err != nil {
			return err
		}
	}
	if scopes != nil {
		if *scopes, err = normalizeScopes(*scopes); err != nil {
			return err
		}
	}
	var quotaValue *float64
	if quota != nil {
		quotaValue = *quota
	}
	var rpmValue *int
	if rpm != nil {
		rpmValue = *rpm
	}
	return validateLimits(quotaValue, rpmValue)
}

func normalizeModels(values []string) ([]string, error) {
	return normalize(values, func(string) bool { return true }, ErrInvalidModel)
}

func normalizeScopes(values []string) ([]string, error) {
	return normalize(values, entities.ValidScope, ErrInvalidScope)
}

func normalize(values []string, valid func(string) bool, invalidErr error) ([]string, error) {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || !valid(value) {
			return nil, invalidErr
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out, nil
}

func validateLimits(quota *float64, rpm *int) error {
	if quota != nil && (*quota < 0 || math.IsNaN(*quota) || math.IsInf(*quota, 0)) {
		return ErrInvalidQuota
	}
	if rpm != nil && *rpm <= 0 {
		return ErrInvalidRPM
	}
	return nil
}
