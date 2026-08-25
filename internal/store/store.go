package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kimnt93/gorouter/internal/cost"
	"github.com/kimnt93/gorouter/internal/cryptoseal"
	"github.com/kimnt93/gorouter/internal/llm"
)

var ErrNotFound = errors.New("not found")

type Tenant struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type Credential struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Provider      string    `json:"provider"`
	Kind          string    `json:"kind"`
	BaseURL       string    `json:"base_url"`
	Status        string    `json:"status"`
	OwnerTenantID *string   `json:"owner_tenant_id"`
	CreatedAt     time.Time `json:"created_at"`
	KeyPreview    string    `json:"key_preview,omitempty"`

	apiKeySealed []byte
	oauthSealed  []byte
}

type CredentialInput struct {
	Name         string
	Provider     string
	Kind         string
	BaseURL      string
	APIKey       string
	OAuthAccess  string
	OAuthRefresh string
	OwnerTenant  *string
}

func (d *DB) CreateCredential(ctx context.Context, s *cryptoseal.Sealer, in CredentialInput) (*Credential, error) {
	c := &Credential{
		ID: NewID("cred"), Name: in.Name, Provider: in.Provider, Kind: in.Kind,
		BaseURL: in.BaseURL, Status: "active", OwnerTenantID: in.OwnerTenant,
	}
	var apiKeyEnc, oauthEnc []byte
	if in.APIKey != "" {
		b, err := s.Seal([]byte(in.APIKey))
		if err != nil {
			return nil, err
		}
		apiKeyEnc = b
		c.KeyPreview = preview(in.APIKey)
	}
	if in.OAuthAccess != "" || in.OAuthRefresh != "" {
		blob, _ := json.Marshal(map[string]string{"access": in.OAuthAccess, "refresh": in.OAuthRefresh})
		b, err := s.Seal(blob)
		if err != nil {
			return nil, err
		}
		oauthEnc = b
		c.KeyPreview = preview(in.OAuthAccess)
	}
	_, err := d.Pool.Exec(ctx, `INSERT INTO credentials
		(id,name,provider,kind,base_url,key_preview,api_key_enc,oauth_blob_enc,status,owner_tenant_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		c.ID, c.Name, c.Provider, c.Kind, c.BaseURL, c.KeyPreview, apiKeyEnc, oauthEnc, c.Status, c.OwnerTenantID)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func preview(secret string) string {
	if len(secret) <= 8 {
		return "…"
	}
	return secret[:6] + "…" + secret[len(secret)-4:]
}

func scanCredential(row pgx.Row) (*Credential, error) {
	var c Credential
	err := row.Scan(&c.ID, &c.Name, &c.Provider, &c.Kind, &c.BaseURL, &c.KeyPreview,
		&c.apiKeySealed, &c.oauthSealed, &c.Status, &c.OwnerTenantID, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

const credColumns = `id,name,provider,kind,base_url,key_preview,coalesce(api_key_enc,''::bytea),coalesce(oauth_blob_enc,''::bytea),status,owner_tenant_id,created_at`

func (d *DB) ListCredentials(ctx context.Context) ([]Credential, error) {
	rows, err := d.Pool.Query(ctx, `SELECT `+credColumns+` FROM credentials ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Credential
	for rows.Next() {
		c, err := scanCredential(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (d *DB) GetCredentialRuntime(ctx context.Context, s *cryptoseal.Sealer, id string) (*llm.CredentialRuntime, error) {
	row := d.Pool.QueryRow(ctx, `SELECT `+credColumns+` FROM credentials WHERE id=$1`, id)
	c, err := scanCredential(row)
	if err != nil {
		return nil, err
	}
	rt := &llm.CredentialRuntime{ID: c.ID, Provider: c.Provider, Kind: c.Kind, BaseURL: c.BaseURL}
	switch c.Kind {
	case llm.KindAPIKey:
		plain, err := s.Open(c.apiKeySealed)
		if err != nil {
			return nil, fmt.Errorf("decrypt credential %s: %w", id, err)
		}
		rt.APIKey = string(plain)
	case llm.KindOAuth:
		plain, err := s.Open(c.oauthSealed)
		if err != nil {
			return nil, fmt.Errorf("decrypt oauth credential %s: %w", id, err)
		}
		var blob map[string]string
		if err := json.Unmarshal(plain, &blob); err != nil {
			return nil, fmt.Errorf("decode oauth blob for %s: %w", id, err)
		}
		rt.OAuthAccess = blob["access"]
		rt.OAuthRefreh = blob["refresh"]
	default:
		return nil, fmt.Errorf("unknown kind %q", c.Kind)
	}
	return rt, nil
}

func (d *DB) UpdateOAuthTokens(ctx context.Context, s *cryptoseal.Sealer, id, access, refresh string) error {
	blob, _ := json.Marshal(map[string]string{"access": access, "refresh": refresh})
	sealed, err := s.Seal(blob)
	if err != nil {
		return err
	}
	_, err = d.Pool.Exec(ctx, `UPDATE credentials SET oauth_blob_enc=$1 WHERE id=$2`, sealed, id)
	return err
}

func (d *DB) DeleteCredential(ctx context.Context, id string) error {
	tag, err := d.Pool.Exec(ctx, `DELETE FROM credentials WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

type ApiKey struct {
	ID              string    `json:"id"`
	TenantID        string    `json:"tenant_id"`
	TenantName      string    `json:"tenant_name,omitempty"`
	Name            string    `json:"name"`
	KeyHash         string    `json:"-"`
	KeyPrefix       string    `json:"key_prefix"`
	Models          []string  `json:"models"`
	MonthlyQuotaUSD *float64  `json:"monthly_quota_usd"`
	RPM             *int      `json:"rpm"`
	Enabled         bool      `json:"enabled"`
	CreatedAt       time.Time `json:"created_at"`
	Plaintext       string    `json:"-"`
}

func hashKey(plain string) string {
	h := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(h[:])
}

func GenerateAPIKey() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return "nr-" + hex.EncodeToString(b)
}

func (d *DB) CreateApiKey(ctx context.Context, tenantID, name string, models []string, quota *float64, rpm *int) (*ApiKey, error) {
	plain := GenerateAPIKey()
	k := &ApiKey{
		ID: NewID("key"), TenantID: tenantID, Name: name,
		KeyHash: hashKey(plain), KeyPrefix: plain[:11],
		Models: models, MonthlyQuotaUSD: quota, RPM: rpm, Enabled: true,
		Plaintext: plain,
	}
	if k.Models == nil {
		k.Models = []string{}
	}
	modelsJSON, _ := json.Marshal(k.Models)
	_, err := d.Pool.Exec(ctx, `INSERT INTO api_keys
		(id,tenant_id,name,key_hash,key_prefix,models,monthly_quota_usd,rpm,enabled)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,TRUE)`,
		k.ID, k.TenantID, k.Name, k.KeyHash, k.KeyPrefix, modelsJSON, quota, rpm)
	if err != nil {
		return nil, err
	}
	return k, nil
}

const keyColumns = `k.id,k.tenant_id,k.name,k.key_hash,k.key_prefix,k.models,k.monthly_quota_usd,k.rpm,k.enabled,k.created_at`

func scanKey(row pgx.Row) (*ApiKey, error) {
	var k ApiKey
	var modelsJSON []byte
	err := row.Scan(&k.ID, &k.TenantID, &k.Name, &k.KeyHash, &k.KeyPrefix, &modelsJSON, &k.MonthlyQuotaUSD, &k.RPM, &k.Enabled, &k.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(modelsJSON, &k.Models) != nil {
		k.Models = []string{}
	}
	return &k, nil
}

func (d *DB) GetApiKeyByHash(ctx context.Context, plain string) (*ApiKey, error) {
	row := d.Pool.QueryRow(ctx, `SELECT `+keyColumns+` FROM api_keys k WHERE k.key_hash=$1`, hashKey(plain))
	return scanKey(row)
}

func (d *DB) GetApiKey(ctx context.Context, id string) (*ApiKey, error) {
	row := d.Pool.QueryRow(ctx, `SELECT `+keyColumns+` FROM api_keys k WHERE k.id=$1`, id)
	return scanKey(row)
}

func (d *DB) ListApiKeys(ctx context.Context) ([]ApiKey, error) {
	rows, err := d.Pool.Query(ctx, `SELECT `+keyColumns+`,t.name FROM api_keys k JOIN tenants t ON t.id=k.tenant_id ORDER BY k.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ApiKey
	for rows.Next() {
		var k ApiKey
		var modelsJSON []byte
		if err := rows.Scan(&k.ID, &k.TenantID, &k.Name, &k.KeyHash, &k.KeyPrefix, &modelsJSON, &k.MonthlyQuotaUSD, &k.RPM, &k.Enabled, &k.CreatedAt, &k.TenantName); err != nil {
			return nil, err
		}
		if json.Unmarshal(modelsJSON, &k.Models) != nil {
			k.Models = []string{}
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (d *DB) PatchApiKey(ctx context.Context, id string, enabled *bool, models *[]string, quota **float64, rpm **int) error {
	k, err := d.GetApiKey(ctx, id)
	if err != nil {
		return err
	}
	if enabled != nil {
		k.Enabled = *enabled
	}
	if models != nil {
		k.Models = *models
	}
	if quota != nil {
		k.MonthlyQuotaUSD = *quota
	}
	if rpm != nil {
		k.RPM = *rpm
	}
	modelsJSON, _ := json.Marshal(k.Models)
	_, err = d.Pool.Exec(ctx, `UPDATE api_keys SET enabled=$1, models=$2, monthly_quota_usd=$3, rpm=$4 WHERE id=$5`,
		k.Enabled, modelsJSON, k.MonthlyQuotaUSD, k.RPM, id)
	return err
}

func (d *DB) DeleteApiKey(ctx context.Context, id string) error {
	tag, err := d.Pool.Exec(ctx, `DELETE FROM api_keys WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

type ModelRoute struct {
	CredentialID string `json:"credential_id"`
	Priority     int    `json:"priority"`
	Weight       int    `json:"weight"`
	Enabled      bool   `json:"enabled"`
}

type ModelDef struct {
	Name          string       `json:"name"`
	Strategy      string       `json:"strategy"`
	UpstreamModel string       `json:"upstream_model"`
	Enabled       bool         `json:"enabled"`
	Routes        []ModelRoute `json:"routes"`
	Price         *cost.Prices `json:"price,omitempty"`
}

func (d *DB) UpsertModel(ctx context.Context, m ModelDef) error {
	up := m.UpstreamModel
	if up == "" {
		up = m.Name
	}
	strategy := m.Strategy
	if strategy == "" {
		strategy = "priority"
	}
	err := d.poolTx(ctx, func(q pgx.Tx) error {
		if _, err := q.Exec(ctx, `INSERT INTO models (name,strategy,upstream_model,enabled) VALUES ($1,$2,$3,$4)
			ON CONFLICT (name) DO UPDATE SET strategy=EXCLUDED.strategy, upstream_model=EXCLUDED.upstream_model, enabled=EXCLUDED.enabled`,
			m.Name, strategy, up, m.Enabled); err != nil {
			return err
		}
		if _, err := q.Exec(ctx, `DELETE FROM model_routes WHERE model=$1`, m.Name); err != nil {
			return err
		}
		for _, r := range m.Routes {
			w := r.Weight
			if w <= 0 {
				w = 1
			}
			if _, err := q.Exec(ctx, `INSERT INTO model_routes (model,credential_id,priority,weight,enabled) VALUES ($1,$2,$3,$4,$5)
				ON CONFLICT (model,credential_id) DO UPDATE SET priority=EXCLUDED.priority, weight=EXCLUDED.weight, enabled=EXCLUDED.enabled`,
				m.Name, r.CredentialID, r.Priority, w, r.Enabled); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

func (d *DB) poolTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

func (d *DB) DeleteModel(ctx context.Context, name string) error {
	tag, err := d.Pool.Exec(ctx, `DELETE FROM models WHERE name=$1`, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) RoutesForModel(ctx context.Context, model string) ([]routingCandidate, error) {
	rows, err := d.Pool.Query(ctx, `
		SELECT mr.credential_id, mr.priority, mr.weight, mr.enabled, c.status, c.owner_tenant_id
		FROM model_routes mr JOIN credentials c ON c.id = mr.credential_id
		WHERE mr.model=$1 AND mr.enabled AND c.status='active'
		ORDER BY mr.priority DESC, mr.credential_id`, model)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []routingCandidate
	for rows.Next() {
		var r routingCandidate
		if err := rows.Scan(&r.CredentialID, &r.Priority, &r.Weight, &r.RouteEnabled, &r.CredStatus, &r.OwnerTenant); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type routingCandidate struct {
	CredentialID string
	Priority     int
	Weight       int
	RouteEnabled bool
	CredStatus   string
	OwnerTenant  *string
}

func (d *DB) ListModels(ctx context.Context) ([]ModelDef, error) {
	rows, err := d.Pool.Query(ctx, `SELECT name,strategy,upstream_model,enabled FROM models ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ModelDef
	for rows.Next() {
		var m ModelDef
		if err := rows.Scan(&m.Name, &m.Strategy, &m.UpstreamModel, &m.Enabled); err != nil {
			return nil, err
		}
		m.Routes = []ModelRoute{}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	prices, err := d.ListPrices(ctx)
	if err != nil {
		return nil, err
	}
	routeRows, err := d.Pool.Query(ctx, `SELECT model,credential_id,priority,weight,enabled FROM model_routes ORDER BY priority DESC`)
	if err != nil {
		return nil, err
	}
	defer routeRows.Close()
	byModel := map[string][]ModelRoute{}
	for routeRows.Next() {
		var m, cid string
		var r ModelRoute
		if err := routeRows.Scan(&m, &cid, &r.Priority, &r.Weight, &r.Enabled); err != nil {
			return nil, err
		}
		r.CredentialID = cid
		byModel[m] = append(byModel[m], r)
	}
	for i := range out {
		out[i].Routes = byModel[out[i].Name]
		if p, ok := prices[out[i].Name]; ok {
			pp := p
			out[i].Price = &pp
		}
	}
	return out, nil
}

func (d *DB) SetPrice(ctx context.Context, model string, p cost.Prices) error {
	_, err := d.Pool.Exec(ctx, `INSERT INTO prices (model,input_per_m,output_per_m,cached_input_per_m,cache_write_per_m,updated_at)
		VALUES ($1,$2,$3,$4,$5,now())
		ON CONFLICT (model) DO UPDATE SET input_per_m=EXCLUDED.input_per_m, output_per_m=EXCLUDED.output_per_m,
		cached_input_per_m=EXCLUDED.cached_input_per_m, cache_write_per_m=EXCLUDED.cache_write_per_m, updated_at=now()`,
		model, p.InputPerM, p.OutputPerM, p.CachedInputPerM, p.CacheWritePerM)
	return err
}

func (d *DB) ListPrices(ctx context.Context) (map[string]cost.Prices, error) {
	rows, err := d.Pool.Query(ctx, `SELECT model,input_per_m,output_per_m,cached_input_per_m,cache_write_per_m FROM prices`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]cost.Prices{}
	for rows.Next() {
		var model string
		var p cost.Prices
		if err := rows.Scan(&model, &p.InputPerM, &p.OutputPerM, &p.CachedInputPerM, &p.CacheWritePerM); err != nil {
			return nil, err
		}
		out[model] = p
	}
	return out, rows.Err()
}

type UsageEvent struct {
	TS               time.Time `json:"ts"`
	TenantID         string    `json:"tenant_id"`
	ApiKeyID         string    `json:"api_key_id"`
	CredentialID     string    `json:"credential_id"`
	Model            string    `json:"model"`
	UpstreamModel    string    `json:"upstream_model"`
	PromptTokens     int64     `json:"prompt_tokens"`
	CompletionTokens int64     `json:"completion_tokens"`
	CacheReadTokens  int64     `json:"cache_read_tokens"`
	CacheWriteTokens int64     `json:"cache_write_tokens"`
	CostUSD          float64   `json:"cost_usd"`
	CacheHit         bool      `json:"cache_hit"`
	StatusCode       int       `json:"status_code"`
	DurationMS       int64     `json:"duration_ms"`
	Error            string    `json:"error"`
}

type UsageWriter struct {
	db   *DB
	ch   chan UsageEvent
	stop chan struct{}
	done chan struct{}
}

func NewUsageWriter(db *DB, buffer int) *UsageWriter {
	if buffer <= 0 {
		buffer = 1024
	}
	w := &UsageWriter{db: db, ch: make(chan UsageEvent, buffer), stop: make(chan struct{}), done: make(chan struct{})}
	go w.run()
	return w
}

func (w *UsageWriter) Submit(ev UsageEvent) {
	select {
	case w.ch <- ev:
	default:
	}
}

func (w *UsageWriter) Close() {
	close(w.stop)
	<-w.done
}

func (w *UsageWriter) run() {
	defer close(w.done)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	batch := make([]UsageEvent, 0, 256)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		b := &pgx.Batch{}
		for _, ev := range batch {
			b.Queue(`INSERT INTO usage_events (ts,tenant_id,api_key_id,credential_id,model,upstream_model,
				prompt_tokens,completion_tokens,cache_read_tokens,cache_write_tokens,cost_usd,cache_hit,status_code,duration_ms,error)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
				ev.TS, ev.TenantID, ev.ApiKeyID, ev.CredentialID, ev.Model, ev.UpstreamModel,
				ev.PromptTokens, ev.CompletionTokens, ev.CacheReadTokens, ev.CacheWriteTokens,
				ev.CostUSD, ev.CacheHit, ev.StatusCode, ev.DurationMS, ev.Error)
		}
		_ = w.db.Pool.SendBatch(ctx, b).Close()
		batch = batch[:0]
	}
	for {
		select {
		case ev := <-w.ch:
			batch = append(batch, ev)
			if len(batch) >= 256 {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-w.stop:
			for {
				select {
				case ev := <-w.ch:
					batch = append(batch, ev)
					continue
				default:
				}
				break
			}
			flush()
			return
		}
	}
}

func (d *DB) MonthSpendForKey(ctx context.Context, apiKeyID string) (float64, error) {
	var spent float64
	err := d.Pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(cost_usd),0) FROM usage_events
		WHERE api_key_id=$1 AND ts >= date_trunc('month', now())`, apiKeyID).Scan(&spent)
	return spent, err
}

type Summary struct {
	Requests     int64             `json:"requests"`
	CacheHits    int64             `json:"cache_hits"`
	CostUSD      float64           `json:"cost_usd"`
	PromptTok    int64             `json:"prompt_tokens"`
	CompletionTo int64             `json:"completion_tokens"`
	CacheReadTok int64             `json:"cache_read_tokens"`
	ByModel      map[string]ModelU `json:"by_model"`
	ByKey        map[string]KeyU   `json:"by_key"`
}

type ModelU struct {
	Requests int64   `json:"requests"`
	CostUSD  float64 `json:"cost_usd"`
	InTok    int64   `json:"in_tokens"`
	OutTok   int64   `json:"out_tokens"`
}

type KeyU struct {
	Requests int64   `json:"requests"`
	CostUSD  float64 `json:"cost_usd"`
}

func (d *DB) UsageSummary(ctx context.Context, since time.Time) (*Summary, error) {
	s := &Summary{ByModel: map[string]ModelU{}, ByKey: map[string]KeyU{}}
	var hits float64
	err := d.Pool.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(SUM(cost_usd),0), COALESCE(SUM(prompt_tokens),0),
		       COALESCE(SUM(completion_tokens),0), COALESCE(SUM(cache_read_tokens),0),
		       COALESCE(SUM(CASE WHEN cache_hit THEN 1 ELSE 0 END),0)
		FROM usage_events WHERE ts >= $1`, since).
		Scan(&s.Requests, &s.CostUSD, &s.PromptTok, &s.CompletionTo, &s.CacheReadTok, &hits)
	if err != nil {
		return nil, err
	}
	s.CacheHits = int64(hits)
	rows, err := d.Pool.Query(ctx, `
		SELECT model, COUNT(*), COALESCE(SUM(cost_usd),0), COALESCE(SUM(prompt_tokens),0), COALESCE(SUM(completion_tokens),0)
		FROM usage_events WHERE ts >= $1 GROUP BY model ORDER BY SUM(cost_usd) DESC LIMIT 50`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m string
		var u ModelU
		if err := rows.Scan(&m, &u.Requests, &u.CostUSD, &u.InTok, &u.OutTok); err != nil {
			return nil, err
		}
		s.ByModel[m] = u
	}
	krows, err := d.Pool.Query(ctx, `
		SELECT api_key_id, COUNT(*), COALESCE(SUM(cost_usd),0)
		FROM usage_events WHERE ts >= $1 GROUP BY api_key_id ORDER BY SUM(cost_usd) DESC LIMIT 100`, since)
	if err != nil {
		return nil, err
	}
	defer krows.Close()
	for krows.Next() {
		var kid string
		var u KeyU
		if err := krows.Scan(&kid, &u.Requests, &u.CostUSD); err != nil {
			return nil, err
		}
		s.ByKey[kid] = u
	}
	return s, nil
}

type RecentEvent struct {
	TS           time.Time `json:"ts"`
	TenantID     string    `json:"tenant_id"`
	KeyID        string    `json:"api_key_id"`
	CredentialID string    `json:"credential_id"`
	Model        string    `json:"model"`
	CostUSD      float64   `json:"cost_usd"`
	CacheHit     bool      `json:"cache_hit"`
	StatusCode   int       `json:"status_code"`
	DurationMS   int64     `json:"duration_ms"`
	Error        string    `json:"error"`
}

func (d *DB) RecentUsage(ctx context.Context, limit int) ([]RecentEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := d.Pool.Query(ctx, `
		SELECT ts,tenant_id,api_key_id,credential_id,model,cost_usd,cache_hit,status_code,duration_ms,error
		FROM usage_events ORDER BY seq DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecentEvent
	for rows.Next() {
		var e RecentEvent
		if err := rows.Scan(&e.TS, &e.TenantID, &e.KeyID, &e.CredentialID, &e.Model, &e.CostUSD, &e.CacheHit, &e.StatusCode, &e.DurationMS, &e.Error); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (d *DB) EnsureDefaultTenants(ctx context.Context) error {
	_, err := d.Pool.Exec(ctx, `INSERT INTO tenants (id,name) VALUES ('tenant_default','default') ON CONFLICT DO NOTHING`)
	return err
}

func (d *DB) ListTenants(ctx context.Context) ([]Tenant, error) {
	rows, err := d.Pool.Query(ctx, `SELECT id,name,created_at FROM tenants ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Tenant
	for rows.Next() {
		var t Tenant
		if err := rows.Scan(&t.ID, &t.Name, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (d *DB) CreateTenant(ctx context.Context, name string) (*Tenant, error) {
	t := &Tenant{ID: NewID("tenant"), Name: name}
	err := d.Pool.QueryRow(ctx, `INSERT INTO tenants (id,name) VALUES ($1,$2) RETURNING created_at`, t.ID, t.Name).Scan(&t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return t, nil
}
