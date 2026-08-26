package clickhouse

import (
	"context"
	"sort"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

type ApiKeyRepo struct{ s *Store }
type storedAPIKey struct {
	entities.ApiKey
	Hash string `json:"secret_hash"`
}

func NewApiKeyRepo(s *Store) *ApiKeyRepo { return &ApiKeyRepo{s} }
func (r *ApiKeyRepo) Create(ctx context.Context, tenant, name string, models, scopes []string, quota *float64, rpm *int) (*entities.ApiKey, error) {
	p := entities.QuotaPeriodNone
	if quota != nil {
		p = entities.QuotaPeriodWeek
	}
	return r.CreateWithQuota(ctx, tenant, name, models, scopes, quota, p, rpm)
}
func (r *ApiKeyRepo) CreateWithQuota(ctx context.Context, tenant, name string, models, scopes []string, quota *float64, period string, rpm *int) (*entities.ApiKey, error) {
	plain := GenerateSecret()
	k := &entities.ApiKey{ID: id("key"), TenantID: tenant, Name: name, SecretHash: HashSecret(plain), SecretPrefix: plain[:11], Models: models, Scopes: scopes, QuotaUSD: quota, QuotaPeriod: period, RPM: rpm, Enabled: true, CreatedAt: time.Now().UTC(), Plaintext: plain}
	return k, r.s.put(ctx, "api_key", k.ID, storedAPIKey{*k, k.SecretHash})
}
func (r *ApiKeyRepo) GetByID(ctx context.Context, id string) (*entities.ApiKey, error) {
	v, e := get[storedAPIKey](ctx, r.s, "api_key", id)
	if e != nil {
		return nil, e
	}
	v.SecretHash = v.Hash
	return &v.ApiKey, nil
}
func (r *ApiKeyRepo) GetByIDForTenant(ctx context.Context, tenant, id string) (*entities.ApiKey, error) {
	k, e := r.GetByID(ctx, id)
	if e == nil && k.TenantID != tenant {
		return nil, entities.ErrNotFound
	}
	return k, e
}
func (r *ApiKeyRepo) GetBySecret(ctx context.Context, hash string) (*entities.ApiKey, error) {
	keys, e := r.List(ctx)
	if e != nil {
		return nil, e
	}
	for i := range keys {
		if keys[i].SecretHash == hash {
			return &keys[i], nil
		}
	}
	return nil, entities.ErrNotFound
}
func (r *ApiKeyRepo) List(ctx context.Context) ([]entities.ApiKey, error) {
	stored, e := list[storedAPIKey](ctx, r.s, "api_key")
	if e != nil {
		return nil, e
	}
	keys := make([]entities.ApiKey, 0, len(stored))
	for _, v := range stored {
		v.SecretHash = v.Hash
		keys = append(keys, v.ApiKey)
	}
	tenants, _ := list[entities.Tenant](ctx, r.s, "tenant")
	names := map[string]string{}
	for _, t := range tenants {
		names[t.ID] = t.Name
	}
	for i := range keys {
		keys[i].TenantName = names[keys[i].TenantID]
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].CreatedAt.After(keys[j].CreatedAt) })
	return keys, nil
}
func (r *ApiKeyRepo) ListByTenant(ctx context.Context, tenant string) ([]entities.ApiKey, error) {
	all, e := r.List(ctx)
	out := []entities.ApiKey{}
	for _, k := range all {
		if k.TenantID == tenant {
			out = append(out, k)
		}
	}
	return out, e
}
func (r *ApiKeyRepo) Patch(ctx context.Context, id string, enabled *bool, models, scopes *[]string, quota **float64, rpm **int) error {
	return r.patch(ctx, "", id, enabled, models, scopes, quota, nil, rpm)
}
func (r *ApiKeyRepo) PatchForTenant(ctx context.Context, tenant, id string, enabled *bool, models, scopes *[]string, quota **float64, rpm **int) error {
	return r.patch(ctx, tenant, id, enabled, models, scopes, quota, nil, rpm)
}
func (r *ApiKeyRepo) PatchQuota(ctx context.Context, id string, enabled *bool, models, scopes *[]string, quota **float64, period *string, rpm **int) error {
	return r.patch(ctx, "", id, enabled, models, scopes, quota, period, rpm)
}
func (r *ApiKeyRepo) PatchQuotaForTenant(ctx context.Context, tenant, id string, enabled *bool, models, scopes *[]string, quota **float64, period *string, rpm **int) error {
	return r.patch(ctx, tenant, id, enabled, models, scopes, quota, period, rpm)
}
func (r *ApiKeyRepo) patch(ctx context.Context, tenant, id string, enabled *bool, models, scopes *[]string, quota **float64, period *string, rpm **int) error {
	k, e := r.GetByID(ctx, id)
	if e != nil {
		return e
	}
	if tenant != "" && k.TenantID != tenant {
		return entities.ErrNotFound
	}
	if enabled != nil {
		k.Enabled = *enabled
	}
	if models != nil {
		k.Models = *models
	}
	if scopes != nil {
		k.Scopes = *scopes
	}
	if quota != nil {
		k.QuotaUSD = *quota
		if period == nil {
			p := entities.QuotaPeriodNone
			if *quota != nil {
				p = entities.QuotaPeriodWeek
			}
			period = &p
		}
	}
	if period != nil {
		k.QuotaPeriod = *period
		if *period == entities.QuotaPeriodNone {
			k.QuotaUSD = nil
		}
	}
	if rpm != nil {
		k.RPM = *rpm
	}
	return r.s.put(ctx, "api_key", id, storedAPIKey{*k, k.SecretHash})
}
func (r *ApiKeyRepo) Delete(ctx context.Context, id string) error { return r.s.del(ctx, "api_key", id) }
func (r *ApiKeyRepo) DeleteForTenant(ctx context.Context, tenant, id string) error {
	if _, e := r.GetByIDForTenant(ctx, tenant, id); e != nil {
		return e
	}
	return r.Delete(ctx, id)
}
