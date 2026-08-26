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
	ErrInvalidQuota   = errors.New("quota must be non-negative")
	ErrInvalidPeriod  = errors.New("quota period must be none or week")
	ErrQuotaRequired  = errors.New("quota is required for a weekly limit")
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

// quotaRepository is implemented by stores supporting configurable quota
// windows. Keeping it separate lets older repository implementations continue
// to work with the legacy monthly quota methods.
type quotaRepository interface {
	CreateWithQuota(ctx context.Context, tenantID, name string, models, scopes []string, quota *float64, period string, rpm *int) (*entities.ApiKey, error)
	PatchQuota(ctx context.Context, id string, enabled *bool, models *[]string, scopes *[]string, quota **float64, period *string, rpm **int) error
	PatchQuotaForTenant(ctx context.Context, tenantID, id string, enabled *bool, models *[]string, scopes *[]string, quota **float64, period *string, rpm **int) error
}

type Service struct {
	repo   Repository
	hashFn func(string) string
	genFn  func() string
	cache  *tokenCache
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
	QuotaUSD        *float64
	QuotaPeriod     string
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
	quota, period, err := normalizeQuota(in.QuotaUSD, in.MonthlyQuotaUSD, in.QuotaPeriod)
	if err != nil {
		return nil, err
	}
	if err := validateLimits(quota, in.RPM); err != nil {
		return nil, err
	}
	if repo, ok := s.repo.(quotaRepository); ok {
		key, err := repo.CreateWithQuota(ctx, in.TenantID, in.Name, in.Models, in.Scopes, quota, period, in.RPM)
		if err == nil && s.cache != nil {
			s.cache.put(ctx, key)
		}
		return key, err
	}
	key, err := s.repo.Create(ctx, in.TenantID, in.Name, in.Models, in.Scopes, quota, in.RPM)
	if err == nil && s.cache != nil {
		s.cache.put(ctx, key)
	}
	return key, err
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
	hash := s.hash(secret)
	if s.cache != nil {
		if key, ok := s.cache.get(ctx, hash); ok {
			return key, nil
		}
	}
	key, err := s.repo.GetBySecret(ctx, hash)
	if err == nil && s.cache != nil {
		s.cache.put(ctx, key)
	}
	return key, err
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
	err := s.repo.Patch(ctx, id, enabled, models, scopes, quota, rpm)
	if err == nil && s.cache != nil {
		s.cache.invalidate(ctx, id, "")
	}
	return err
}

func (s *Service) PatchForTenant(ctx context.Context, tenantID, id string, enabled *bool, models *[]string, scopes *[]string, quota **float64, rpm **int) error {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return ErrTenantRequired
	}
	if err := validatePatch(models, scopes, quota, rpm); err != nil {
		return err
	}
	err := s.repo.PatchForTenant(ctx, tenantID, id, enabled, models, scopes, quota, rpm)
	if err == nil && s.cache != nil {
		s.cache.invalidate(ctx, id, "")
	}
	return err
}

func (s *Service) PatchQuota(ctx context.Context, id string, enabled *bool, models *[]string, scopes *[]string, quota **float64, period *string, rpm **int) error {
	if err := validateQuotaPatch(models, scopes, quota, period, rpm); err != nil {
		return err
	}
	if repo, ok := s.repo.(quotaRepository); ok {
		err := repo.PatchQuota(ctx, id, enabled, models, scopes, quota, period, rpm)
		if err == nil && s.cache != nil {
			s.cache.invalidate(ctx, id, "")
		}
		return err
	}
	return s.repo.Patch(ctx, id, enabled, models, scopes, quota, rpm)
}

func (s *Service) PatchQuotaForTenant(ctx context.Context, tenantID, id string, enabled *bool, models *[]string, scopes *[]string, quota **float64, period *string, rpm **int) error {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return ErrTenantRequired
	}
	if err := validateQuotaPatch(models, scopes, quota, period, rpm); err != nil {
		return err
	}
	if repo, ok := s.repo.(quotaRepository); ok {
		err := repo.PatchQuotaForTenant(ctx, tenantID, id, enabled, models, scopes, quota, period, rpm)
		if err == nil && s.cache != nil {
			s.cache.invalidate(ctx, id, "")
		}
		return err
	}
	return s.repo.PatchForTenant(ctx, tenantID, id, enabled, models, scopes, quota, rpm)
}

func (s *Service) Delete(ctx context.Context, id string) error {
	err := s.repo.Delete(ctx, id)
	if err == nil && s.cache != nil {
		s.cache.invalidate(ctx, id, "")
	}
	return err
}

func (s *Service) DeleteForTenant(ctx context.Context, tenantID, id string) error {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return ErrTenantRequired
	}
	err := s.repo.DeleteForTenant(ctx, tenantID, id)
	if err == nil && s.cache != nil {
		s.cache.invalidate(ctx, id, "")
	}
	return err
}

func validatePatch(models *[]string, scopes *[]string, quota **float64, rpm **int) error {
	return validateQuotaPatch(models, scopes, quota, nil, rpm)
}

func validateQuotaPatch(models *[]string, scopes *[]string, quota **float64, period *string, rpm **int) error {
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
	if period != nil {
		normalized, err := normalizePeriod(*period, quotaValue != nil)
		if err != nil {
			return err
		}
		*period = normalized
	}
	return validateLimits(quotaValue, rpmValue)
}

func normalizeQuota(quota, legacyMonthly *float64, period string) (*float64, string, error) {
	if quota == nil {
		quota = legacyMonthly
		if quota != nil && strings.TrimSpace(period) == "" {
			period = entities.QuotaPeriodWeek
		}
	}
	normalized, err := normalizePeriod(period, quota != nil)
	if err != nil {
		return nil, "", err
	}
	if normalized == entities.QuotaPeriodNone {
		quota = nil
	} else if quota == nil {
		return nil, "", ErrQuotaRequired
	}
	return quota, normalized, nil
}

func normalizePeriod(period string, hasQuota bool) (string, error) {
	period = strings.ToLower(strings.TrimSpace(period))
	if period == "" {
		if hasQuota {
			return entities.QuotaPeriodWeek, nil
		}
		return entities.QuotaPeriodNone, nil
	}
	switch period {
	case entities.QuotaPeriodNone, entities.QuotaPeriodWeek:
		return period, nil
	default:
		return "", ErrInvalidPeriod
	}
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
