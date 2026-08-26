package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/kimnt93/gorouter/pkg/entities"
)

type TenantRepo struct{ db *DB }

func NewTenantRepo(db *DB) *TenantRepo { return &TenantRepo{db: db} }

func (r *TenantRepo) List(ctx context.Context) ([]entities.Tenant, error) {
	rows, err := r.db.Pool.Query(ctx, `SELECT id,name,created_at FROM tenants ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []entities.Tenant
	for rows.Next() {
		var t entities.Tenant
		if err := rows.Scan(&t.ID, &t.Name, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *TenantRepo) Create(ctx context.Context, name string) (*entities.Tenant, error) {
	t := &entities.Tenant{ID: NewID("tenant"), Name: name}
	err := r.db.Pool.QueryRow(ctx, `INSERT INTO tenants (id,name) VALUES ($1,$2) RETURNING created_at`, t.ID, t.Name).Scan(&t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (r *TenantRepo) EnsureDefault(ctx context.Context) error {
	_, err := r.db.Pool.Exec(ctx, `INSERT INTO tenants (id,name) VALUES ('tenant_default','default') ON CONFLICT DO NOTHING`)
	return err
}

type CredentialRepo struct{ db *DB }

type oauthBlob struct {
	Access   string                 `json:"access"`
	Refresh  string                 `json:"refresh"`
	IDToken  string                 `json:"id_token,omitempty"`
	Account  string                 `json:"account,omitempty"`
	Metadata entities.OAuthMetadata `json:"metadata,omitempty"`
}

func NewCredentialRepo(db *DB) *CredentialRepo { return &CredentialRepo{db: db} }

const credColumns = `id,name,provider,kind,base_url,key_preview,coalesce(api_key_enc,''::bytea),coalesce(oauth_blob_enc,''::bytea),status,owner_tenant_id,created_at`

func scanCredential(row pgx.Row) (*entities.Credential, error) {
	var c entities.Credential
	var keyEnc, oauthEnc []byte
	err := row.Scan(&c.ID, &c.Name, &c.Provider, &c.Kind, &c.BaseURL, &c.KeyPreview,
		&keyEnc, &oauthEnc, &c.Status, &c.OwnerTenantID, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	c.SetSecrets(keyEnc, oauthEnc)
	return &c, nil
}

func wrapDecrypt(id string, err error) error {
	return fmt.Errorf("decrypt credential %s: %w", id, err)
}

func (r *CredentialRepo) Create(ctx context.Context, in entities.CredentialInput, box entities.SecretBox) (*entities.Credential, error) {
	c := &entities.Credential{
		ID: NewID("cred"), Name: in.Name, Provider: in.Provider, Kind: in.Kind,
		BaseURL: in.BaseURL, Status: "active", OwnerTenantID: in.OwnerTenant,
	}
	var apiKeyEnc, oauthEnc []byte
	if in.APIKey != "" {
		b, err := box.Seal([]byte(in.APIKey))
		if err != nil {
			return nil, err
		}
		apiKeyEnc = b
		c.KeyPreview = preview(in.APIKey)
	}
	if in.OAuthAccess != "" || in.OAuthRefresh != "" {
		blob, _ := json.Marshal(oauthBlob{Access: in.OAuthAccess, Refresh: in.OAuthRefresh, IDToken: in.OAuthIDToken, Account: in.OAuthAccount, Metadata: in.OAuthMeta})
		b, err := box.Seal(blob)
		if err != nil {
			return nil, err
		}
		oauthEnc = b
		c.KeyPreview = preview(in.OAuthAccess)
	}
	_, err := r.db.Pool.Exec(ctx, `INSERT INTO credentials
		(id,name,provider,kind,base_url,key_preview,api_key_enc,oauth_blob_enc,status,owner_tenant_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		c.ID, c.Name, c.Provider, c.Kind, c.BaseURL, c.KeyPreview, apiKeyEnc, oauthEnc, c.Status, c.OwnerTenantID)
	if err != nil {
		return nil, err
	}
	c.SetSecrets(apiKeyEnc, oauthEnc)
	return c, nil
}

func (r *CredentialRepo) Update(ctx context.Context, box entities.SecretBox, id string, in entities.CredentialUpdate) (*entities.Credential, error) {
	existing, err := scanCredential(r.db.Pool.QueryRow(ctx, `SELECT `+credColumns+` FROM credentials WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, entities.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	apiKeyEnc, oauthEnc := existing.APIKeySealed(), existing.OAuthSealed()
	keyPreview := existing.KeyPreview
	switch existing.Kind {
	case entities.KindAPIKey:
		if in.APIKey != "" {
			apiKeyEnc, err = box.Seal([]byte(in.APIKey))
			if err != nil {
				return nil, err
			}
			keyPreview = preview(in.APIKey)
		}
	case entities.KindOAuth:
		if in.OAuthAccess != "" || in.OAuthRefresh != "" {
			if in.OAuthRefresh == "" {
				return nil, errors.New("oauth_refresh is required when rotating OAuth tokens")
			}
			blob, marshalErr := json.Marshal(oauthBlob{Access: in.OAuthAccess, Refresh: in.OAuthRefresh})
			if marshalErr != nil {
				return nil, marshalErr
			}
			oauthEnc, err = box.Seal(blob)
			if err != nil {
				return nil, err
			}
			keyPreview = preview(in.OAuthAccess)
		}
	}
	row := r.db.Pool.QueryRow(ctx, `UPDATE credentials SET name=$1,base_url=$2,status=$3,owner_tenant_id=$4,
		key_preview=$5,api_key_enc=$6,oauth_blob_enc=$7,updated_at=now() WHERE id=$8 RETURNING `+credColumns,
		in.Name, in.BaseURL, in.Status, in.OwnerTenant, keyPreview, apiKeyEnc, oauthEnc, id)
	updated, err := scanCredential(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, entities.ErrNotFound
	}
	return updated, err
}

func (r *CredentialRepo) List(ctx context.Context) ([]entities.Credential, error) {
	rows, err := r.db.Pool.Query(ctx, `SELECT `+credColumns+` FROM credentials ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []entities.Credential
	for rows.Next() {
		var c entities.Credential
		var keyEnc, oauthEnc []byte
		if err := rows.Scan(&c.ID, &c.Name, &c.Provider, &c.Kind, &c.BaseURL, &c.KeyPreview, &keyEnc, &oauthEnc, &c.Status, &c.OwnerTenantID, &c.CreatedAt); err != nil {
			return nil, err
		}
		c.SetSecrets(keyEnc, oauthEnc)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *CredentialRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.db.Pool.Exec(ctx, `DELETE FROM credentials WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return entities.ErrNotFound
	}
	return nil
}

func (r *CredentialRepo) Runtime(ctx context.Context, box entities.SecretBox, id string) (*entities.CredentialRuntime, error) {
	row := r.db.Pool.QueryRow(ctx, `SELECT `+credColumns+` FROM credentials WHERE id=$1`, id)
	c, err := scanCredential(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, entities.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rt := &entities.CredentialRuntime{ID: c.ID, Provider: c.Provider, Kind: c.Kind, BaseURL: c.BaseURL}
	switch c.Kind {
	case entities.KindAPIKey:
		plain, kerr := box.Open(c.APIKeySealed())
		if kerr != nil {
			return nil, wrapDecrypt(id, kerr)
		}
		rt.APIKey = string(plain)
	case entities.KindOAuth:
		plain, oerr := box.Open(c.OAuthSealed())
		if oerr != nil {
			return nil, wrapDecrypt(id, oerr)
		}
		var blob oauthBlob
		if jerr := json.Unmarshal(plain, &blob); jerr != nil {
			return nil, wrapDecrypt(id, jerr)
		}
		rt.OAuthAccess = blob.Access
		rt.OAuthRefreh = blob.Refresh
		rt.OAuthIDToken = blob.IDToken
		rt.OAuthAccount = blob.Account
		rt.OAuthMeta = blob.Metadata
	default:
		return nil, errors.New("unknown credential kind " + c.Kind)
	}
	return rt, nil
}

func (r *CredentialRepo) UpdateOAuthTokens(ctx context.Context, box entities.SecretBox, id, access, refresh string) error {
	var blob oauthBlob
	var current []byte
	err := r.db.Pool.QueryRow(ctx, `SELECT coalesce(oauth_blob_enc,''::bytea) FROM credentials WHERE id=$1`, id).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return entities.ErrNotFound
	}
	if err != nil {
		return err
	}
	if len(current) > 0 {
		plain, openErr := box.Open(current)
		if openErr != nil {
			return wrapDecrypt(id, openErr)
		}
		if unmarshalErr := json.Unmarshal(plain, &blob); unmarshalErr != nil {
			return wrapDecrypt(id, unmarshalErr)
		}
	}
	blob.Access = access
	blob.Refresh = refresh
	encoded, _ := json.Marshal(blob)
	sealed, err := box.Seal(encoded)
	if err != nil {
		return err
	}
	tag, err := r.db.Pool.Exec(ctx, `UPDATE credentials SET oauth_blob_enc=$1, updated_at=now() WHERE id=$2`, sealed, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return entities.ErrNotFound
	}
	return nil
}

func (r *CredentialRepo) RoutesForModel(ctx context.Context, model string) ([]entities.RouteCandidate, error) {
	rows, err := r.db.Pool.Query(ctx, `
		SELECT mr.credential_id, mr.priority, mr.weight, c.owner_tenant_id
		FROM model_routes mr JOIN credentials c ON c.id = mr.credential_id
		WHERE mr.model=$1 AND mr.enabled AND c.status='active'
		ORDER BY mr.priority DESC, mr.credential_id`, model)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []entities.RouteCandidate
	for rows.Next() {
		var rc entities.RouteCandidate
		var w int
		if err := rows.Scan(&rc.CredentialID, &rc.Priority, &w, &rc.OwnerTenant); err != nil {
			return nil, err
		}
		rc.Weight = w
		out = append(out, rc)
	}
	return out, rows.Err()
}

func preview(secret string) string {
	if len(secret) <= 8 {
		return "…"
	}
	return secret[:6] + "…" + secret[len(secret)-4:]
}

type ApiKeyRepo struct{ db *DB }

func NewApiKeyRepo(db *DB) *ApiKeyRepo { return &ApiKeyRepo{db: db} }

func HashSecret(plain string) string {
	h := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(h[:])
}

func GenerateSecret() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return "nr-" + hex.EncodeToString(b)
}

const keyColumns = `k.id,k.tenant_id,k.name,k.key_hash,k.key_prefix,k.models,k.scopes,k.quota_usd,k.quota_period,k.rpm,k.enabled,k.created_at`

func scanApiKey(row pgx.Row) (*entities.ApiKey, error) {
	var k entities.ApiKey
	var modelsJSON, scopesJSON []byte
	err := row.Scan(&k.ID, &k.TenantID, &k.Name, &k.SecretHash, &k.SecretPrefix, &modelsJSON, &scopesJSON, &k.QuotaUSD, &k.QuotaPeriod, &k.RPM, &k.Enabled, &k.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, entities.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	k.Models = decodeJSONStrings(modelsJSON)
	k.Scopes = decodeJSONStrings(scopesJSON)
	k.MonthlyQuotaUSD = nil
	return &k, nil
}

func decodeJSONStrings(b []byte) []string {
	var out []string
	if json.Unmarshal(b, &out) == nil && out != nil {
		return out
	}
	return []string{}
}

func (r *ApiKeyRepo) Create(ctx context.Context, tenantID, name string, models, scopes []string, quota *float64, rpm *int) (*entities.ApiKey, error) {
	period := entities.QuotaPeriodNone
	if quota != nil {
		period = entities.QuotaPeriodWeek
	}
	return r.CreateWithQuota(ctx, tenantID, name, models, scopes, quota, period, rpm)
}

func (r *ApiKeyRepo) CreateWithQuota(ctx context.Context, tenantID, name string, models, scopes []string, quota *float64, period string, rpm *int) (*entities.ApiKey, error) {
	plain := GenerateSecret()
	k := &entities.ApiKey{
		ID: NewID("key"), TenantID: tenantID, Name: name,
		SecretHash: HashSecret(plain), SecretPrefix: plain[:11],
		Models: models, Scopes: scopes, QuotaUSD: quota, QuotaPeriod: period, RPM: rpm, Enabled: true,
		Plaintext: plain,
	}
	modelsJSON, _ := json.Marshal(orEmpty(models))
	scopesJSON, _ := json.Marshal(orEmpty(scopes))
	_, err := r.db.Pool.Exec(ctx, `INSERT INTO api_keys
		(id,tenant_id,name,key_hash,key_prefix,models,scopes,quota_usd,quota_period,rpm,enabled)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,TRUE)`,
		k.ID, k.TenantID, k.Name, k.SecretHash, k.SecretPrefix, modelsJSON, scopesJSON, quota, period, rpm)
	if err != nil {
		return nil, err
	}
	return k, nil
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (r *ApiKeyRepo) GetBySecret(ctx context.Context, hash string) (*entities.ApiKey, error) {
	row := r.db.Pool.QueryRow(ctx, `SELECT `+keyColumns+` FROM api_keys k WHERE k.key_hash=$1`, hash)
	return scanApiKey(row)
}

func (r *ApiKeyRepo) GetByID(ctx context.Context, id string) (*entities.ApiKey, error) {
	row := r.db.Pool.QueryRow(ctx, `SELECT `+keyColumns+` FROM api_keys k WHERE k.id=$1`, id)
	return scanApiKey(row)
}

func (r *ApiKeyRepo) GetByIDForTenant(ctx context.Context, tenantID, id string) (*entities.ApiKey, error) {
	row := r.db.Pool.QueryRow(ctx, `SELECT `+keyColumns+` FROM api_keys k WHERE k.id=$1 AND k.tenant_id=$2`, id, tenantID)
	return scanApiKey(row)
}

func (r *ApiKeyRepo) List(ctx context.Context) ([]entities.ApiKey, error) {
	return r.list(ctx, "", false)
}

func (r *ApiKeyRepo) ListByTenant(ctx context.Context, tenantID string) ([]entities.ApiKey, error) {
	return r.list(ctx, tenantID, true)
}

func (r *ApiKeyRepo) list(ctx context.Context, tenantID string, scoped bool) ([]entities.ApiKey, error) {
	query := `SELECT ` + keyColumns + `,t.name FROM api_keys k JOIN tenants t ON t.id=k.tenant_id`
	var args []any
	if scoped {
		query += ` WHERE k.tenant_id=$1`
		args = append(args, tenantID)
	}
	query += ` ORDER BY k.created_at DESC`
	rows, err := r.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []entities.ApiKey
	for rows.Next() {
		var k entities.ApiKey
		var modelsJSON, scopesJSON []byte
		if err := rows.Scan(&k.ID, &k.TenantID, &k.Name, &k.SecretHash, &k.SecretPrefix, &modelsJSON, &scopesJSON, &k.QuotaUSD, &k.QuotaPeriod, &k.RPM, &k.Enabled, &k.CreatedAt, &k.TenantName); err != nil {
			return nil, err
		}
		k.Models = decodeJSONStrings(modelsJSON)
		k.Scopes = decodeJSONStrings(scopesJSON)
		out = append(out, k)
	}
	return out, rows.Err()
}

func (r *ApiKeyRepo) Patch(ctx context.Context, id string, enabled *bool, models *[]string, scopes *[]string, quota **float64, rpm **int) error {
	period := legacyPatchPeriod(quota)
	return r.patch(ctx, "", id, false, enabled, models, scopes, quota, period, rpm)
}

func (r *ApiKeyRepo) PatchForTenant(ctx context.Context, tenantID, id string, enabled *bool, models *[]string, scopes *[]string, quota **float64, rpm **int) error {
	period := legacyPatchPeriod(quota)
	return r.patch(ctx, tenantID, id, true, enabled, models, scopes, quota, period, rpm)
}

func legacyPatchPeriod(quota **float64) *string {
	if quota == nil {
		return nil
	}
	period := entities.QuotaPeriodNone
	if *quota != nil {
		period = entities.QuotaPeriodWeek
	}
	return &period
}

func (r *ApiKeyRepo) PatchQuota(ctx context.Context, id string, enabled *bool, models *[]string, scopes *[]string, quota **float64, period *string, rpm **int) error {
	return r.patch(ctx, "", id, false, enabled, models, scopes, quota, period, rpm)
}

func (r *ApiKeyRepo) PatchQuotaForTenant(ctx context.Context, tenantID, id string, enabled *bool, models *[]string, scopes *[]string, quota **float64, period *string, rpm **int) error {
	return r.patch(ctx, tenantID, id, true, enabled, models, scopes, quota, period, rpm)
}

func (r *ApiKeyRepo) patch(ctx context.Context, tenantID, id string, scoped bool, enabled *bool, models *[]string, scopes *[]string, quota **float64, period *string, rpm **int) error {
	var k *entities.ApiKey
	var err error
	if scoped {
		k, err = r.GetByIDForTenant(ctx, tenantID, id)
	} else {
		k, err = r.GetByID(ctx, id)
	}
	if err != nil {
		return err
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
			inferred := entities.QuotaPeriodNone
			if *quota != nil {
				inferred = entities.QuotaPeriodWeek
			}
			period = &inferred
		}
	}
	if period != nil {
		k.QuotaPeriod = *period
		if k.QuotaPeriod == entities.QuotaPeriodNone {
			k.QuotaUSD = nil
		}
	}
	if rpm != nil {
		k.RPM = *rpm
	}
	modelsJSON, _ := json.Marshal(k.Models)
	scopesJSON, _ := json.Marshal(k.Scopes)
	query := `UPDATE api_keys SET enabled=$1, models=$2, scopes=$3, quota_usd=$4, quota_period=$5, rpm=$6 WHERE id=$7`
	args := []any{k.Enabled, modelsJSON, scopesJSON, k.QuotaUSD, k.QuotaPeriod, k.RPM, id}
	if scoped {
		query += ` AND tenant_id=$8`
		args = append(args, tenantID)
	}
	tag, err := r.db.Pool.Exec(ctx, query, args...)
	if err == nil && tag.RowsAffected() == 0 {
		return entities.ErrNotFound
	}
	return err
}

func (r *ApiKeyRepo) Delete(ctx context.Context, id string) error {
	return r.delete(ctx, "", id, false)
}

func (r *ApiKeyRepo) DeleteForTenant(ctx context.Context, tenantID, id string) error {
	return r.delete(ctx, tenantID, id, true)
}

func (r *ApiKeyRepo) delete(ctx context.Context, tenantID, id string, scoped bool) error {
	query := `DELETE FROM api_keys WHERE id=$1`
	args := []any{id}
	if scoped {
		query += ` AND tenant_id=$2`
		args = append(args, tenantID)
	}
	tag, err := r.db.Pool.Exec(ctx, query, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return entities.ErrNotFound
	}
	return nil
}
