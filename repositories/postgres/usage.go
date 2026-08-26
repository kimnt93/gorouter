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
		if ev.ID == "" {
			ev.ID = entities.NewID("usage")
		}
		if ev.TS.IsZero() {
			ev.TS = time.Now().UTC()
		} else {
			ev.TS = ev.TS.UTC()
		}
		if ev.ActorType == "" {
			ev.ActorType, ev.Username, ev.OrganizationID = entities.ActorLegacy, entities.ActorLegacy, ev.TenantID
		}
		b.Queue(`INSERT INTO usage_events (event_id,ts,tenant_id,api_key_id,credential_id,model,upstream_model,
			prompt_tokens,completion_tokens,cache_read_tokens,cache_write_tokens,cost_usd,priced,cache_hit,status_code,duration_ms,error,
			actor_type,user_id,username,organization_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)`,
			ev.ID, ev.TS, ev.TenantID, ev.ApiKeyID, ev.CredentialID, ev.Model, ev.UpstreamModel,
			ev.PromptTokens, ev.CompletionTokens, ev.CacheReadTokens, ev.CacheWriteTokens,
			ev.CostUSD, ev.Priced, ev.CacheHit, ev.StatusCode, ev.DurationMS, ev.Error,
			ev.ActorType, ev.UserID, ev.Username, ev.OrganizationID)
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
		SELECT COALESCE(event_id,'legacy_' || seq::text),ts,tenant_id,api_key_id,credential_id,model,upstream_model,prompt_tokens,completion_tokens,
		       cache_read_tokens,cache_write_tokens,cost_usd,priced,cache_hit,status_code,duration_ms,error,
		       actor_type,user_id,username,organization_id
		FROM usage_events WHERE tenant_id=$1 ORDER BY ts DESC,event_id DESC LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []entities.RecentEvent
	for rows.Next() {
		var ev entities.RecentEvent
		if err := rows.Scan(&ev.ID, &ev.TS, &ev.TenantID, &ev.KeyID, &ev.CredentialID, &ev.Model, &ev.UpstreamModel,
			&ev.PromptTokens, &ev.CompletionTokens, &ev.CacheReadTokens, &ev.CacheWriteTokens,
			&ev.CostUSD, &ev.Priced, &ev.CacheHit, &ev.StatusCode, &ev.DurationMS, &ev.Error,
			&ev.ActorType, &ev.UserID, &ev.Username, &ev.OrganizationID); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

func (r *UsageRepo) QueryUsage(ctx context.Context, query entities.UsageQuery) (*entities.UsagePage, error) {
	limit := boundedLimit(query.Limit)
	cursor := decodeAuditCursor(query.Cursor)
	var since, until time.Time
	if query.Since != nil {
		since = query.Since.UTC()
	}
	if query.Until != nil {
		until = query.Until.UTC()
	}
	master := query.Visibility.PrincipalType == entities.PrincipalMaster
	organizationWide := query.Visibility.OrganizationWide && query.Visibility.OrganizationID != ""
	var status int
	hasStatus := query.StatusCode != nil
	if hasStatus {
		status = *query.StatusCode
	}
	rows, err := r.db.Pool.Query(ctx, `SELECT COALESCE(event_id,'legacy_' || seq::text),ts,tenant_id,api_key_id,credential_id,model,upstream_model,
		prompt_tokens,completion_tokens,cache_read_tokens,cache_write_tokens,cost_usd,priced,cache_hit,status_code,duration_ms,error,
		actor_type,user_id,username,organization_id FROM usage_events WHERE
		($1 OR ($2 AND organization_id=$3) OR (NOT $2 AND user_id=$4)) AND
		($5='' OR organization_id=$5) AND ($6='' OR user_id=$6) AND
		($7='' OR model=$7) AND ($8='' OR api_key_id=$8) AND (NOT $9 OR status_code=$10) AND
		($11::timestamptz='0001-01-01 00:00:00+00' OR ts >= $11) AND
		($12::timestamptz='0001-01-01 00:00:00+00' OR ts <= $12) AND
		($13::timestamptz='0001-01-01 00:00:00+00' OR (ts,COALESCE(event_id,'legacy_' || seq::text)) < ($13,$14))
		ORDER BY ts DESC,event_id DESC LIMIT $15`, master, organizationWide, query.Visibility.OrganizationID, query.Visibility.UserID,
		query.OrganizationID, query.UserID, query.Model, query.APIKeyID, hasStatus, status, since, until, cursor.TS, cursor.ID, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	page := &entities.UsagePage{Data: make([]entities.RecentEvent, 0, limit)}
	for rows.Next() {
		var event entities.RecentEvent
		if err := rows.Scan(&event.ID, &event.TS, &event.TenantID, &event.KeyID, &event.CredentialID, &event.Model, &event.UpstreamModel,
			&event.PromptTokens, &event.CompletionTokens, &event.CacheReadTokens, &event.CacheWriteTokens, &event.CostUSD, &event.Priced,
			&event.CacheHit, &event.StatusCode, &event.DurationMS, &event.Error, &event.ActorType, &event.UserID, &event.Username, &event.OrganizationID); err != nil {
			return nil, err
		}
		page.Data = append(page.Data, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(page.Data) > limit {
		last := page.Data[limit-1]
		page.NextCursor = encodeAuditCursor(entities.AuditEvent{ID: last.ID, TS: last.TS})
		page.Data = page.Data[:limit]
	}
	return page, nil
}

func (r *UsageRepo) SummaryUsage(ctx context.Context, query entities.UsageQuery) (*entities.UsageSummary, error) {
	page, err := r.QueryUsage(ctx, query)
	if err != nil {
		return nil, err
	}
	summary := &entities.UsageSummary{ByModel: map[string]entities.ModelU{}, ByKey: map[string]entities.KeyU{}}
	for _, event := range page.Data {
		summary.Requests++
		summary.CostUSD += event.CostUSD
		summary.PromptTok += event.PromptTokens
		summary.CompletionTo += event.CompletionTokens
		summary.CacheReadTok += event.CacheReadTokens
		if event.CacheHit {
			summary.CacheHits++
		}
		if !event.Priced {
			summary.Unpriced++
		}
		model := summary.ByModel[event.Model]
		model.Requests++
		model.CostUSD += event.CostUSD
		model.InTok += event.PromptTokens
		model.OutTok += event.CompletionTokens
		summary.ByModel[event.Model] = model
		key := summary.ByKey[event.KeyID]
		key.Requests++
		key.CostUSD += event.CostUSD
		summary.ByKey[event.KeyID] = key
	}
	return summary, nil
}
