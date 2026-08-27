package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

type ModelRouteRepo struct{ db *DB }

func NewModelRouteRepo(db *DB) *ModelRouteRepo { return &ModelRouteRepo{db: db} }

func (r *ModelRouteRepo) Upsert(ctx context.Context, m entities.ModelDef) error {
	up := m.UpstreamModel
	if up == "" {
		up = m.Name
	}
	strategy := m.Strategy
	if strategy == "" {
		strategy = "priority"
	}
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	createdAt := time.Now().UTC()
	var metadata []byte
	if m.Metadata != nil {
		metadata, err = json.Marshal(m.Metadata)
		if err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO models (name,strategy,upstream_model,enabled,created_at,metadata) VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (name) DO UPDATE SET strategy=EXCLUDED.strategy, upstream_model=EXCLUDED.upstream_model, enabled=EXCLUDED.enabled, metadata=EXCLUDED.metadata`,
		m.Name, strategy, up, m.Enabled, createdAt, metadata); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM model_routes WHERE model=$1`, m.Name); err != nil {
		return err
	}
	for _, rt := range m.Routes {
		w := rt.Weight
		if w <= 0 {
			w = 1
		}
		if _, err := tx.Exec(ctx, `INSERT INTO model_routes (model,credential_id,priority,weight,enabled) VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (model,credential_id) DO UPDATE SET priority=EXCLUDED.priority, weight=EXCLUDED.weight, enabled=EXCLUDED.enabled`,
			m.Name, rt.CredentialID, rt.Priority, w, rt.Enabled); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *ModelRouteRepo) Delete(ctx context.Context, name string) error {
	tag, err := r.db.Pool.Exec(ctx, `DELETE FROM models WHERE name=$1`, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return entities.ErrNotFound
	}
	return nil
}

func (r *ModelRouteRepo) List(ctx context.Context) ([]entities.ModelDef, error) {
	rows, err := r.db.Pool.Query(ctx, `SELECT name,strategy,upstream_model,enabled,metadata FROM models ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []entities.ModelDef
	for rows.Next() {
		var m entities.ModelDef
		var metadata []byte
		if err := rows.Scan(&m.Name, &m.Strategy, &m.UpstreamModel, &m.Enabled, &metadata); err != nil {
			return nil, err
		}
		if len(metadata) != 0 {
			m.Metadata = &entities.ModelMetadata{}
			if err := json.Unmarshal(metadata, m.Metadata); err != nil {
				return nil, err
			}
		}
		m.Routes = []entities.ModelRoute{}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	prices, err := r.ListPrices(ctx)
	if err != nil {
		return nil, err
	}
	routeRows, err := r.db.Pool.Query(ctx, `SELECT model,credential_id,priority,weight,enabled FROM model_routes ORDER BY priority DESC`)
	if err != nil {
		return nil, err
	}
	defer routeRows.Close()
	byModel := map[string][]entities.ModelRoute{}
	for routeRows.Next() {
		var name string
		var rt entities.ModelRoute
		if err := routeRows.Scan(&name, &rt.CredentialID, &rt.Priority, &rt.Weight, &rt.Enabled); err != nil {
			return nil, err
		}
		byModel[name] = append(byModel[name], rt)
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

func (r *ModelRouteRepo) SetPrice(ctx context.Context, model string, p entities.Price) error {
	updatedAt := time.Now().UTC()
	_, err := r.db.Pool.Exec(ctx, `INSERT INTO prices (model,input_per_m,output_per_m,cached_input_per_m,cache_write_per_m,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (model) DO UPDATE SET input_per_m=EXCLUDED.input_per_m, output_per_m=EXCLUDED.output_per_m,
		cached_input_per_m=EXCLUDED.cached_input_per_m, cache_write_per_m=EXCLUDED.cache_write_per_m, updated_at=EXCLUDED.updated_at`,
		model, p.InputPerM, p.OutputPerM, p.CachedInputPerM, p.CacheWritePerM, updatedAt)
	return err
}

func (r *ModelRouteRepo) DeletePrice(ctx context.Context, model string) error {
	tag, err := r.db.Pool.Exec(ctx, `DELETE FROM prices WHERE model=$1`, model)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return entities.ErrNotFound
	}
	return nil
}

func (r *ModelRouteRepo) ListPrices(ctx context.Context) (map[string]entities.Price, error) {
	rows, err := r.db.Pool.Query(ctx, `SELECT model,input_per_m,output_per_m,cached_input_per_m,cache_write_per_m FROM prices`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]entities.Price{}
	for rows.Next() {
		var model string
		var p entities.Price
		if err := rows.Scan(&model, &p.InputPerM, &p.OutputPerM, &p.CachedInputPerM, &p.CacheWritePerM); err != nil {
			return nil, err
		}
		out[model] = p
	}
	return out, rows.Err()
}

func (r *ModelRouteRepo) ReplaceCatalogPrices(ctx context.Context, source string, prices []entities.CatalogPrice) error {
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	now := time.Now().UTC()
	for _, item := range prices {
		updatedAt := item.UpdatedAt.UTC()
		if item.UpdatedAt.IsZero() {
			updatedAt = now
		}
		_, err = tx.Exec(ctx, `INSERT INTO catalog_prices
			(model,name,provider,context_length,cache_supported,input_per_m,output_per_m,cached_input_per_m,cache_write_per_m,source,updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			ON CONFLICT (model) DO UPDATE SET name=EXCLUDED.name, provider=EXCLUDED.provider,
			context_length=EXCLUDED.context_length, cache_supported=EXCLUDED.cache_supported,
			input_per_m=EXCLUDED.input_per_m, output_per_m=EXCLUDED.output_per_m,
			cached_input_per_m=EXCLUDED.cached_input_per_m, cache_write_per_m=EXCLUDED.cache_write_per_m,
			source=EXCLUDED.source, updated_at=EXCLUDED.updated_at`, item.Model, item.Name, item.Provider,
			item.ContextLength, item.CacheSupported, item.Price.InputPerM, item.Price.OutputPerM,
			item.Price.CachedInputPerM, item.Price.CacheWritePerM, source, updatedAt)
		if err != nil {
			return err
		}
	}
	models := make([]string, 0, len(prices))
	for _, item := range prices {
		models = append(models, item.Model)
	}
	if len(models) == 0 {
		_, err = tx.Exec(ctx, `DELETE FROM catalog_prices WHERE source=$1`, source)
	} else {
		_, err = tx.Exec(ctx, `DELETE FROM catalog_prices WHERE source=$1 AND NOT (model = ANY($2))`, source, models)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *ModelRouteRepo) ListCatalogPrices(ctx context.Context) ([]entities.CatalogPrice, error) {
	rows, err := r.db.Pool.Query(ctx, `SELECT model,name,provider,context_length,cache_supported,
		input_per_m,output_per_m,cached_input_per_m,cache_write_per_m,source,updated_at
		FROM catalog_prices ORDER BY model`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []entities.CatalogPrice
	for rows.Next() {
		var item entities.CatalogPrice
		if err := rows.Scan(&item.Model, &item.Name, &item.Provider, &item.ContextLength, &item.CacheSupported,
			&item.Price.InputPerM, &item.Price.OutputPerM, &item.Price.CachedInputPerM,
			&item.Price.CacheWritePerM, &item.Source, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

type UsageRepo struct{ db *DB }

func NewUsageRepo(db *DB) *UsageRepo { return &UsageRepo{db: db} }

func (r *UsageRepo) SpendForKeySince(ctx context.Context, apiKeyID string, since time.Time) (float64, error) {
	var spent float64
	err := r.db.Pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(cost_usd),0) FROM usage_events
		WHERE api_key_id=$1 AND ts >= $2`, apiKeyID, since.UTC()).Scan(&spent)
	return spent, err
}

func (r *UsageRepo) Summary(ctx context.Context, since time.Time) (*entities.UsageSummary, error) {
	s := &entities.UsageSummary{ByModel: map[string]entities.ModelU{}, ByKey: map[string]entities.KeyU{}}
	err := r.db.Pool.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(SUM(cost_usd),0), COALESCE(SUM(prompt_tokens),0),
		       COALESCE(SUM(completion_tokens),0), COALESCE(SUM(cache_read_tokens),0), COALESCE(SUM(cache_write_tokens),0),
		       COALESCE(SUM(CASE WHEN cache_hit THEN 1 ELSE 0 END),0),
		       COALESCE(SUM(CASE WHEN NOT priced THEN 1 ELSE 0 END),0)
		FROM usage_events WHERE ts >= $1`, since).
		Scan(&s.Requests, &s.CostUSD, &s.PromptTok, &s.CompletionTo, &s.CacheReadTok, &s.CacheWriteTok, &s.CacheHits, &s.Unpriced)
	if err != nil {
		return nil, err
	}
	mrows, err := r.db.Pool.Query(ctx, `
		SELECT model, COUNT(*), COALESCE(SUM(cost_usd),0), COALESCE(SUM(prompt_tokens),0), COALESCE(SUM(completion_tokens),0)
		FROM usage_events WHERE ts >= $1 GROUP BY model ORDER BY SUM(cost_usd) DESC LIMIT 50`, since)
	if err != nil {
		return nil, err
	}
	defer mrows.Close()
	for mrows.Next() {
		var name string
		var u entities.ModelU
		if err := mrows.Scan(&name, &u.Requests, &u.CostUSD, &u.InTok, &u.OutTok); err != nil {
			return nil, err
		}
		s.ByModel[name] = u
	}
	krows, err := r.db.Pool.Query(ctx, `
		SELECT api_key_id, COUNT(*), COALESCE(SUM(cost_usd),0)
		FROM usage_events WHERE ts >= $1 GROUP BY api_key_id ORDER BY SUM(cost_usd) DESC LIMIT 100`, since)
	if err != nil {
		return nil, err
	}
	defer krows.Close()
	for krows.Next() {
		var kid string
		var u entities.KeyU
		if err := krows.Scan(&kid, &u.Requests, &u.CostUSD); err != nil {
			return nil, err
		}
		s.ByKey[kid] = u
	}
	return s, nil
}

func (r *UsageRepo) Recent(ctx context.Context, limit int) ([]entities.RecentEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.db.Pool.Query(ctx, `
		SELECT COALESCE(event_id,'legacy_' || seq::text),ts,tenant_id,api_key_id,credential_id,model,upstream_model,prompt_tokens,completion_tokens,
		       cache_read_tokens,cache_write_tokens,cost_usd,priced,cache_hit,status_code,duration_ms,error
		FROM usage_events ORDER BY ts DESC,event_id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []entities.RecentEvent
	for rows.Next() {
		var ev entities.RecentEvent
		if err := rows.Scan(&ev.ID, &ev.TS, &ev.TenantID, &ev.KeyID, &ev.CredentialID, &ev.Model, &ev.UpstreamModel,
			&ev.PromptTokens, &ev.CompletionTokens, &ev.CacheReadTokens, &ev.CacheWriteTokens,
			&ev.CostUSD, &ev.Priced, &ev.CacheHit, &ev.StatusCode, &ev.DurationMS, &ev.Error); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}
