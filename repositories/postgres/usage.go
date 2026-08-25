package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kimnt93/gorouter/pkg/entities"
)

func (r *UsageRepo) InsertBatch(ctx context.Context, events []entities.UsageEvent) error {
	if len(events) == 0 {
		return nil
	}
	b := &pgx.Batch{}
	for _, ev := range events {
		b.Queue(`INSERT INTO usage_events (ts,tenant_id,api_key_id,credential_id,model,upstream_model,
			prompt_tokens,completion_tokens,cache_read_tokens,cache_write_tokens,cost_usd,priced,cache_hit,status_code,duration_ms,error)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
			ev.TS, ev.TenantID, ev.ApiKeyID, ev.CredentialID, ev.Model, ev.UpstreamModel,
			ev.PromptTokens, ev.CompletionTokens, ev.CacheReadTokens, ev.CacheWriteTokens,
			ev.CostUSD, ev.Priced, ev.CacheHit, ev.StatusCode, ev.DurationMS, ev.Error)
	}
	return r.db.Pool.SendBatch(ctx, b).Close()
}

func (r *UsageRepo) SummaryForTenant(ctx context.Context, tenantID string, since time.Time) (*entities.UsageSummary, error) {
	s := &entities.UsageSummary{ByModel: map[string]entities.ModelU{}, ByKey: map[string]entities.KeyU{}}
	err := r.db.Pool.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(SUM(cost_usd),0), COALESCE(SUM(prompt_tokens),0),
		       COALESCE(SUM(completion_tokens),0), COALESCE(SUM(cache_read_tokens),0),
		       COALESCE(SUM(CASE WHEN cache_hit THEN 1 ELSE 0 END),0),
		       COALESCE(SUM(CASE WHEN NOT priced THEN 1 ELSE 0 END),0)
		FROM usage_events WHERE tenant_id=$1 AND ts >= $2`, tenantID, since).
		Scan(&s.Requests, &s.CostUSD, &s.PromptTok, &s.CompletionTo, &s.CacheReadTok, &s.CacheHits, &s.Unpriced)
	if err != nil {
		return nil, err
	}
	mrows, err := r.db.Pool.Query(ctx, `
		SELECT model, COUNT(*), COALESCE(SUM(cost_usd),0), COALESCE(SUM(prompt_tokens),0), COALESCE(SUM(completion_tokens),0)
		FROM usage_events WHERE tenant_id=$1 AND ts >= $2 GROUP BY model ORDER BY SUM(cost_usd) DESC LIMIT 50`, tenantID, since)
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
	if err := mrows.Err(); err != nil {
		return nil, err
	}
	krows, err := r.db.Pool.Query(ctx, `
		SELECT api_key_id, COUNT(*), COALESCE(SUM(cost_usd),0)
		FROM usage_events WHERE tenant_id=$1 AND ts >= $2 GROUP BY api_key_id ORDER BY SUM(cost_usd) DESC LIMIT 100`, tenantID, since)
	if err != nil {
		return nil, err
	}
	defer krows.Close()
	for krows.Next() {
		var keyID string
		var u entities.KeyU
		if err := krows.Scan(&keyID, &u.Requests, &u.CostUSD); err != nil {
			return nil, err
		}
		s.ByKey[keyID] = u
	}
	return s, krows.Err()
}

func (r *UsageRepo) RecentForTenant(ctx context.Context, tenantID string, limit int) ([]entities.RecentEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.db.Pool.Query(ctx, `
		SELECT ts,tenant_id,api_key_id,credential_id,model,cost_usd,priced,cache_hit,status_code,duration_ms,error
		FROM usage_events WHERE tenant_id=$1 ORDER BY seq DESC LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []entities.RecentEvent
	for rows.Next() {
		var ev entities.RecentEvent
		if err := rows.Scan(&ev.TS, &ev.TenantID, &ev.KeyID, &ev.CredentialID, &ev.Model, &ev.CostUSD, &ev.Priced, &ev.CacheHit, &ev.StatusCode, &ev.DurationMS, &ev.Error); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}
