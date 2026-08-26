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
			prompt_tokens,completion_tokens,cache_read_tokens,cache_write_tokens,cost_usd,input_cost_usd,output_cost_usd,cache_read_cost_usd,cache_write_cost_usd,priced,cache_hit,status_code,duration_ms,error,
			actor_type,user_id,username,organization_id,request_body,response_body,content_truncated)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28)`,
			ev.ID, ev.TS, ev.TenantID, ev.ApiKeyID, ev.CredentialID, ev.Model, ev.UpstreamModel,
			ev.PromptTokens, ev.CompletionTokens, ev.CacheReadTokens, ev.CacheWriteTokens,
			ev.CostUSD, ev.InputCostUSD, ev.OutputCostUSD, ev.CacheReadCostUSD, ev.CacheWriteCostUSD, ev.Priced, ev.CacheHit, ev.StatusCode, ev.DurationMS, ev.Error,
			ev.ActorType, ev.UserID, ev.Username, ev.OrganizationID, ev.RequestBody, ev.ResponseBody, ev.ContentTruncated)
	}
	return r.db.Pool.SendBatch(ctx, b).Close()
}

func (r *UsageRepo) SummaryForTenant(ctx context.Context, tenantID string, since time.Time) (*entities.UsageSummary, error) {
	s := &entities.UsageSummary{ByModel: map[string]entities.ModelU{}, ByKey: map[string]entities.KeyU{}}
	err := r.db.Pool.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(SUM(cost_usd),0), COALESCE(SUM(prompt_tokens),0),
		       COALESCE(SUM(completion_tokens),0), COALESCE(SUM(cache_read_tokens),0), COALESCE(SUM(cache_write_tokens),0),
		       COALESCE(SUM(CASE WHEN cache_hit THEN 1 ELSE 0 END),0),
		       COALESCE(SUM(CASE WHEN NOT priced THEN 1 ELSE 0 END),0)
		FROM usage_events WHERE tenant_id=$1 AND ts >= $2`, tenantID, since).
		Scan(&s.Requests, &s.CostUSD, &s.PromptTok, &s.CompletionTo, &s.CacheReadTok, &s.CacheWriteTok, &s.CacheHits, &s.Unpriced)
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
		($5='' OR organization_id=ANY(string_to_array($5,','))) AND ($6='' OR user_id=ANY(string_to_array($6,','))) AND
		($7='' OR model=$7) AND ($8='' OR api_key_id=ANY(string_to_array($8,','))) AND (NOT $9 OR status_code=$10) AND
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

func (r *UsageRepo) UsageDetail(ctx context.Context, id string, visibility entities.UsageVisibility) (*entities.UsageDetail, error) {
	master := visibility.PrincipalType == entities.PrincipalMaster
	organizationWide := visibility.OrganizationWide && visibility.OrganizationID != ""
	var event entities.UsageDetail
	err := r.db.Pool.QueryRow(ctx, `SELECT COALESCE(event_id,'legacy_' || seq::text),ts,tenant_id,api_key_id,credential_id,model,upstream_model,
		prompt_tokens,completion_tokens,cache_read_tokens,cache_write_tokens,cost_usd,priced,cache_hit,status_code,duration_ms,error,
		actor_type,user_id,username,organization_id,request_body,response_body,content_truncated
		FROM usage_events WHERE COALESCE(event_id,'legacy_' || seq::text)=$1 AND
		($2 OR ($3 AND organization_id=$4) OR (NOT $3 AND user_id=$5))`, id, master, organizationWide, visibility.OrganizationID, visibility.UserID).Scan(
		&event.ID, &event.TS, &event.TenantID, &event.KeyID, &event.CredentialID, &event.Model, &event.UpstreamModel,
		&event.PromptTokens, &event.CompletionTokens, &event.CacheReadTokens, &event.CacheWriteTokens, &event.CostUSD, &event.Priced,
		&event.CacheHit, &event.StatusCode, &event.DurationMS, &event.Error, &event.ActorType, &event.UserID, &event.Username, &event.OrganizationID,
		&event.RequestBody, &event.ResponseBody, &event.ContentTruncated)
	if err == pgx.ErrNoRows {
		return nil, entities.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &event, nil
}

func (r *UsageRepo) SummaryUsage(ctx context.Context, query entities.UsageQuery) (*entities.UsageSummary, error) {
	summary := &entities.UsageSummary{ByModel: map[string]entities.ModelU{}, ByKey: map[string]entities.KeyU{}}
	filter, args := postgresUsageFilter(query)
	if err := r.db.Pool.QueryRow(ctx, `SELECT count(*),coalesce(sum(cost_usd),0),coalesce(sum(input_cost_usd),0),coalesce(sum(output_cost_usd),0),coalesce(sum(cache_read_cost_usd),0),coalesce(sum(cache_write_cost_usd),0),coalesce(sum(prompt_tokens),0),coalesce(sum(completion_tokens),0),coalesce(sum(cache_read_tokens),0),coalesce(sum(cache_write_tokens),0),count(*) FILTER (WHERE cache_hit),count(*) FILTER (WHERE NOT priced) FROM usage_events WHERE `+filter, args...).Scan(&summary.Requests, &summary.CostUSD, &summary.InputCostUSD, &summary.OutputCostUSD, &summary.CacheReadCostUSD, &summary.CacheWriteCostUSD, &summary.PromptTok, &summary.CompletionTo, &summary.CacheReadTok, &summary.CacheWriteTok, &summary.CacheHits, &summary.Unpriced); err != nil {
		return nil, err
	}
	rows, err := r.db.Pool.Query(ctx, `SELECT model,count(*),coalesce(sum(cost_usd),0),coalesce(sum(prompt_tokens),0),coalesce(sum(completion_tokens),0),coalesce(sum(cache_read_tokens),0),coalesce(sum(cache_write_tokens),0) FROM usage_events WHERE `+filter+` GROUP BY model`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name string
		var value entities.ModelU
		if err = rows.Scan(&name, &value.Requests, &value.CostUSD, &value.InTok, &value.OutTok, &value.CacheReadTok, &value.CacheWriteTok); err != nil {
			rows.Close()
			return nil, err
		}
		summary.ByModel[name] = value
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	rows, err = r.db.Pool.Query(ctx, `SELECT api_key_id,count(*),coalesce(sum(cost_usd),0) FROM usage_events WHERE `+filter+` GROUP BY api_key_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var value entities.KeyU
		if err = rows.Scan(&name, &value.Requests, &value.CostUSD); err != nil {
			return nil, err
		}
		summary.ByKey[name] = value
	}
	return summary, rows.Err()
}

func (r *UsageRepo) ActivityUsage(ctx context.Context, query entities.UsageQuery, groupBy string) ([]entities.UsageActivityBucket, error) {
	if groupBy != "hour" && groupBy != "day" && groupBy != "week" {
		groupBy = "day"
	}
	filter, args := postgresUsageFilter(query)
	rows, err := r.db.Pool.Query(ctx, `SELECT date_trunc('`+groupBy+`',ts) AS bucket,user_id,coalesce(nullif(username,''),nullif(user_id,''),'Legacy') AS user_label,count(*),coalesce(sum(prompt_tokens),0),coalesce(sum(completion_tokens),0),coalesce(sum(cache_read_tokens),0),coalesce(sum(cache_write_tokens),0),coalesce(sum(cost_usd),0),coalesce(sum(input_cost_usd),0),coalesce(sum(output_cost_usd),0),coalesce(sum(cache_read_cost_usd),0),coalesce(sum(cache_write_cost_usd),0) FROM usage_events WHERE `+filter+` GROUP BY bucket,user_id,user_label ORDER BY bucket,user_label`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]entities.UsageActivityBucket, 0)
	for rows.Next() {
		var bucket entities.UsageActivityBucket
		if err := rows.Scan(&bucket.Start, &bucket.UserID, &bucket.Username, &bucket.Requests, &bucket.PromptTokens, &bucket.CompletionTokens, &bucket.CacheReadTokens, &bucket.CacheWriteTokens, &bucket.CostUSD, &bucket.InputCostUSD, &bucket.OutputCostUSD, &bucket.CacheReadCostUSD, &bucket.CacheWriteCostUSD); err != nil {
			return nil, err
		}
		out = append(out, bucket)
	}
	return out, rows.Err()
}

func postgresUsageFilter(query entities.UsageQuery) (string, []any) {
	var since, until time.Time
	if query.Since != nil {
		since = query.Since.UTC()
	}
	if query.Until != nil {
		until = query.Until.UTC()
	}
	master := query.Visibility.PrincipalType == entities.PrincipalMaster
	organizationWide := query.Visibility.OrganizationWide && query.Visibility.OrganizationID != ""
	status := 0
	hasStatus := query.StatusCode != nil
	if hasStatus {
		status = *query.StatusCode
	}
	return `($1 OR ($2 AND organization_id=$3) OR (NOT $2 AND user_id=$4)) AND ($5='' OR organization_id=ANY(string_to_array($5,','))) AND ($6='' OR user_id=ANY(string_to_array($6,','))) AND ($7='' OR model=$7) AND ($8='' OR api_key_id=ANY(string_to_array($8,','))) AND (NOT $9 OR status_code=$10) AND ($11::timestamptz='0001-01-01 00:00:00+00' OR ts >= $11) AND ($12::timestamptz='0001-01-01 00:00:00+00' OR ts <= $12)`, []any{master, organizationWide, query.Visibility.OrganizationID, query.Visibility.UserID, query.OrganizationID, query.UserID, query.Model, query.APIKeyID, hasStatus, status, since, until}
}
