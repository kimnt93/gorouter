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
	if err := r.s.put(ctx, "api_key", k.ID, storedAPIKey{*k, k.SecretHash}); err != nil {
		return nil, err
	}
	if err := r.s.put(ctx, "api_key_hash", k.SecretHash, k.ID); err != nil {
		return nil, err
	}
	return k, nil
}
func (r *ApiKeyRepo) CreateOwned(ctx context.Context, input entities.ApiKey) (*entities.ApiKey, error) {
	if e := input.ValidateOwnerShape(); e != nil {
		return nil, e
	}
	plain := GenerateSecret()
	input.ID, input.SecretHash, input.SecretPrefix = id("key"), HashSecret(plain), plain[:11]
	input.Enabled, input.CreatedAt, input.Plaintext = true, time.Now().UTC(), plain
	input.TenantID = input.ContextOrganizationID
	e := r.s.mutate(ctx, "api_key_hash:"+input.SecretHash, func() error {
		if e := r.s.put(ctx, "api_key", input.ID, storedAPIKey{input, input.SecretHash}); e != nil {
			return e
		}
		return r.s.put(ctx, "api_key_hash", input.SecretHash, input.ID)
	})
	if e != nil {
		return nil, e
	}
	return &input, nil
}
func (r *ApiKeyRepo) Rotate(ctx context.Context, keyID string) (*entities.ApiKey, error) {
	var result *entities.ApiKey
	err := r.s.mutate(ctx, "api_key:"+keyID, func() error {
		key, e := r.GetByID(ctx, keyID)
		if e != nil {
			return e
		}
		oldHash := key.SecretHash
		plain := GenerateSecret()
		key.SecretHash, key.SecretPrefix, key.Plaintext = HashSecret(plain), plain[:11], plain
		if e = r.s.put(ctx, "api_key", keyID, storedAPIKey{*key, key.SecretHash}); e != nil {
			return e
		}
		if e = r.s.put(ctx, "api_key_hash", key.SecretHash, keyID); e != nil {
			return e
		}
		if e = r.s.del(ctx, "api_key_hash", oldHash); e != nil {
			return e
		}
		result = key
		return nil
	})
	return result, err
}
func (r *ApiKeyRepo) GetByID(ctx context.Context, id string) (*entities.ApiKey, error) {
	v, e := get[storedAPIKey](ctx, r.s, "api_key", id)
	if e != nil {
		return nil, e
	}
	v.SecretHash = v.Hash
	if v.OwnerType == "" {
		v.OwnerType, v.OwnerOrganizationID, v.ContextOrganizationID = entities.OwnerOrganization, v.TenantID, v.TenantID
	}
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
	idv, e := get[string](ctx, r.s, "api_key_hash", hash)
	if e != nil {
		return nil, e
	}
	return r.GetByID(ctx, *idv)
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
func (r *ApiKeyRepo) Delete(ctx context.Context, id string) error {
	k, e := r.GetByID(ctx, id)
	if e != nil {
		return e
	}
	if e = r.s.del(ctx, "api_key", id); e != nil {
		return e
	}
	return r.s.del(ctx, "api_key_hash", k.SecretHash)
}
func (r *ApiKeyRepo) DeleteForTenant(ctx context.Context, tenant, id string) error {
	if _, e := r.GetByIDForTenant(ctx, tenant, id); e != nil {
		return e
	}
	return r.Delete(ctx, id)
}
