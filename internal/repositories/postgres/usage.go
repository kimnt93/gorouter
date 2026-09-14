package postgres

import (
	"context"
	"fmt"
	"strings"
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
		if ev.AccountingTS.IsZero() {
			ev.AccountingTS = ev.TS
		}
		if ev.AccountingState == "" {
			ev.AccountingState = "settled"
		}
		if ev.UsageMeasurement == "" {
			ev.UsageMeasurement = "unknown"
		}
		b.Queue(`INSERT INTO usage_events (event_id,ts,tenant_id,api_key_id,credential_id,provider,model,upstream_model,
			prompt_tokens,completion_tokens,cache_read_tokens,cache_write_tokens,cost_usd,input_cost_usd,output_cost_usd,cache_read_cost_usd,cache_write_cost_usd,priced,cache_hit,status_code,duration_ms,error,
			actor_type,user_id,username,organization_id,conversation_enc,content_truncated,workload_application,workload_environment,workload_workspace_id,workload_agent_id,conversation_id,run_id,parent_run_id,trace_id,logical_request_id,provider_attempt_id,accounting_ts,usage_measurement,accounting_state)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35,$36,$37,$38,$39,$40,$41)
			ON CONFLICT (event_id) DO NOTHING`,
			ev.ID, ev.TS, ev.TenantID, ev.ApiKeyID, ev.CredentialID, ev.Provider, ev.Model, ev.UpstreamModel,
			ev.PromptTokens, ev.CompletionTokens, ev.CacheReadTokens, ev.CacheWriteTokens,
			ev.CostUSD, ev.InputCostUSD, ev.OutputCostUSD, ev.CacheReadCostUSD, ev.CacheWriteCostUSD, ev.Priced, ev.CacheHit, ev.StatusCode, ev.DurationMS, ev.Error,
			ev.ActorType, ev.UserID, ev.Username, ev.OrganizationID, nullableBytes(ev.ConversationEnc), ev.ContentTruncated,
			ev.Application, ev.Environment, ev.WorkspaceID, ev.AgentID, ev.ConversationID, ev.RunID, ev.ParentRunID, ev.TraceID, ev.LogicalRequestID, ev.ProviderAttemptID, ev.AccountingTS, ev.UsageMeasurement, ev.AccountingState)
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
	page, err := r.QueryUsage(ctx, entities.UsageQuery{Visibility: entities.UsageVisibility{PrincipalType: entities.PrincipalMaster}, OrganizationID: tenantID, Limit: limit})
	if err != nil {
		return nil, err
	}
	return page.Data, nil
}

func (r *UsageRepo) QueryUsage(ctx context.Context, query entities.UsageQuery) (*entities.UsagePage, error) {
	limit := boundedLimit(query.Limit)
	cursor := decodeAuditCursor(query.Cursor)
	filter, args := postgresUsageFilter(query)
	if !cursor.TS.IsZero() {
		n := len(args)
		filter += fmt.Sprintf(" AND (ts,COALESCE(event_id,'legacy_' || seq::text)) < ($%d,$%d)", n+1, n+2)
		args = append(args, cursor.TS, cursor.ID)
	}
	args = append(args, limit+1)
	rows, err := r.db.Pool.Query(ctx, `SELECT COALESCE(event_id,'legacy_' || seq::text),ts,tenant_id,api_key_id,credential_id,provider,model,upstream_model,
		prompt_tokens,completion_tokens,cache_read_tokens,cache_write_tokens,cost_usd,priced,cache_hit,status_code,duration_ms,error,
		actor_type,user_id,username,organization_id,workload_application,workload_environment,workload_workspace_id,workload_agent_id,conversation_id,run_id,parent_run_id,trace_id,logical_request_id,provider_attempt_id,accounting_ts,usage_measurement,accounting_state FROM usage_events WHERE `+filter+fmt.Sprintf(` ORDER BY ts DESC,event_id DESC LIMIT $%d`, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	page := &entities.UsagePage{Data: make([]entities.RecentEvent, 0, limit)}
	for rows.Next() {
		var event entities.RecentEvent
		if err := rows.Scan(&event.ID, &event.TS, &event.TenantID, &event.KeyID, &event.CredentialID, &event.Provider, &event.Model, &event.UpstreamModel,
			&event.PromptTokens, &event.CompletionTokens, &event.CacheReadTokens, &event.CacheWriteTokens, &event.CostUSD, &event.Priced,
			&event.CacheHit, &event.StatusCode, &event.DurationMS, &event.Error, &event.ActorType, &event.UserID, &event.Username, &event.OrganizationID, &event.Application, &event.Environment, &event.WorkspaceID, &event.AgentID, &event.ConversationID, &event.RunID, &event.ParentRunID, &event.TraceID, &event.LogicalRequestID, &event.ProviderAttemptID, &event.AccountingTS, &event.UsageMeasurement, &event.AccountingState); err != nil {
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
	summary := &entities.UsageSummary{ByModel: map[string]entities.ModelU{}, ByKey: map[string]entities.KeyU{}}
	filter, args := postgresUsageFilter(query)
	if err := r.db.Pool.QueryRow(ctx, `SELECT count(*),coalesce(sum(cost_usd),0),coalesce(sum(input_cost_usd),0),coalesce(sum(output_cost_usd),0),coalesce(sum(cache_read_cost_usd),0),coalesce(sum(cache_write_cost_usd),0),coalesce(sum(prompt_tokens),0),coalesce(sum(completion_tokens),0),coalesce(sum(cache_read_tokens),0),coalesce(sum(cache_write_tokens),0),count(*) FILTER (WHERE cache_hit),count(*) FILTER (WHERE NOT priced) FROM usage_events WHERE `+filter, args...).Scan(&summary.Requests, &summary.CostUSD, &summary.InputCostUSD, &summary.OutputCostUSD, &summary.CacheReadCostUSD, &summary.CacheWriteCostUSD, &summary.PromptTok, &summary.CompletionTo, &summary.CacheReadTok, &summary.CacheWriteTok, &summary.CacheHits, &summary.Unpriced); err != nil {
		return nil, err
	}
	if query.TotalsOnly {
		return summary, nil
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

func (r *UsageRepo) HealthUsage(ctx context.Context, query entities.UsageQuery) ([]entities.UsageHealthMetric, error) {
	filter, args := postgresUsageFilter(query)
	const selectMetrics = `count(*),count(*) FILTER (WHERE status_code>=200 AND status_code<400),count(*) FILTER (WHERE status_code>=400 AND status_code<500 AND status_code NOT IN (402,429)),count(*) FILTER (WHERE status_code>=500 OR status_code IN (402,429)),coalesce(avg(duration_ms),0),coalesce(percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms),0),coalesce(sum(cache_read_tokens)::double precision/nullif(sum(prompt_tokens+cache_read_tokens),0),0)`
	dimensions := []struct{ name, column, condition string }{
		{"provider", "provider", "provider<>''"}, {"model", "model", "model<>''"}, {"credential", "credential_id", "credential_id<>''"},
	}
	out := make([]entities.UsageHealthMetric, 0)
	for _, dimension := range dimensions {
		rows, err := r.db.Pool.Query(ctx, `SELECT `+dimension.column+`,`+selectMetrics+` FROM usage_events WHERE `+filter+` AND `+dimension.condition+` GROUP BY `+dimension.column+` ORDER BY count(*) DESC LIMIT 200`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			metric := entities.UsageHealthMetric{Dimension: dimension.name}
			if err := rows.Scan(&metric.ID, &metric.Requests, &metric.Successes, &metric.ClientErrors, &metric.ProviderErrors, &metric.AverageMS, &metric.P95MS, &metric.CacheReadRate); err != nil {
				rows.Close()
				return nil, err
			}
			if attempts := metric.Successes + metric.ProviderErrors; attempts > 0 {
				metric.SuccessRate = float64(metric.Successes) / float64(attempts)
			}
			out = append(out, metric)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

func postgresUsageFilter(query entities.UsageQuery) (string, []any) {
	clauses := []string{}
	args := []any{}
	add := func(column string, value any, op string) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf("%s %s $%d", column, op, len(args)))
	}
	v := query.Visibility
	switch {
	case v.PrincipalType == entities.PrincipalMaster:
		if v.OrganizationID != "" {
			add("organization_id", v.OrganizationID, "=")
		}
	case v.OrganizationWide && v.OrganizationID != "":
		add("organization_id", v.OrganizationID, "=")
	case !v.OrganizationWide && v.UserID != "":
		add("user_id", v.UserID, "=")
		if v.OrganizationID != "" || v.Workload != nil {
			add("organization_id", v.OrganizationID, "=")
		}
	default:
		clauses = append(clauses, "FALSE")
	}
	if query.PersonalOnly {
		add("organization_id", "", "=")
	}
	for _, filter := range query.Filters() {
		column := filter.Field
		switch column {
		case "application", "environment", "workspace_id", "agent_id":
			column = "workload_" + column
		}
		args = append(args, filter.Values)
		clauses = append(clauses, fmt.Sprintf("%s=ANY($%d::text[])", column, len(args)))
	}
	if query.StatusCode != nil {
		add("status_code", *query.StatusCode, "=")
	}
	timeColumn := "ts"
	if query.TimeBasis == "accounting" {
		timeColumn = "accounting_ts"
	}
	if query.Since != nil {
		add(timeColumn, query.Since.UTC(), ">=")
	}
	if query.Until != nil {
		add(timeColumn, query.Until.UTC(), "<")
	}
	if len(clauses) == 0 {
		clauses = append(clauses, "TRUE")
	}
	return strings.Join(clauses, " AND "), args
}

func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func (r *UsageRepo) UsageDetail(ctx context.Context, id string, visibility entities.UsageVisibility) (*entities.UsageDetail, error) {
	filter, args := postgresUsageFilter(entities.UsageQuery{Visibility: visibility})
	args = append(args, id)
	var event entities.UsageDetail
	var encrypted []byte
	err := r.db.Pool.QueryRow(ctx, `SELECT COALESCE(event_id,'legacy_' || seq::text),ts,tenant_id,api_key_id,credential_id,provider,model,upstream_model,
		prompt_tokens,completion_tokens,cache_read_tokens,cache_write_tokens,cost_usd,priced,cache_hit,status_code,duration_ms,error,
		actor_type,user_id,username,organization_id,workload_application,workload_environment,workload_workspace_id,workload_agent_id,conversation_id,run_id,parent_run_id,trace_id,logical_request_id,provider_attempt_id,accounting_ts,usage_measurement,accounting_state,COALESCE(conversation_enc,''::bytea),content_truncated
		FROM usage_events WHERE `+filter+fmt.Sprintf(` AND COALESCE(event_id,'legacy_' || seq::text)=$%d`, len(args)), args...).Scan(
		&event.ID, &event.TS, &event.TenantID, &event.KeyID, &event.CredentialID, &event.Provider, &event.Model, &event.UpstreamModel,
		&event.PromptTokens, &event.CompletionTokens, &event.CacheReadTokens, &event.CacheWriteTokens, &event.CostUSD, &event.Priced,
		&event.CacheHit, &event.StatusCode, &event.DurationMS, &event.Error, &event.ActorType, &event.UserID, &event.Username, &event.OrganizationID, &event.Application, &event.Environment, &event.WorkspaceID, &event.AgentID, &event.ConversationID, &event.RunID, &event.ParentRunID, &event.TraceID, &event.LogicalRequestID, &event.ProviderAttemptID, &event.AccountingTS, &event.UsageMeasurement, &event.AccountingState,
		&encrypted, &event.ContentTruncated)
	if err == pgx.ErrNoRows {
		return nil, entities.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	event.ConversationEncrypted = encrypted
	return &event, nil
}

func (r *UsageRepo) WorkloadUsageAggregate(ctx context.Context, query entities.UsageQuery) (*entities.UsageSummary, error) {
	filter, args := postgresUsageFilter(query)
	filter = strings.ReplaceAll(filter, "ts >=", "accounting_ts >=")
	filter = strings.ReplaceAll(filter, "ts <", "accounting_ts <")
	summary := &entities.UsageSummary{ByModel: map[string]entities.ModelU{}, ByKey: map[string]entities.KeyU{}}
	err := r.db.Pool.QueryRow(ctx, `SELECT count(*),coalesce(sum(cost_usd),0),coalesce(sum(input_cost_usd),0),coalesce(sum(output_cost_usd),0),coalesce(sum(cache_read_cost_usd),0),coalesce(sum(cache_write_cost_usd),0),coalesce(sum(prompt_tokens),0),coalesce(sum(completion_tokens),0),coalesce(sum(cache_read_tokens),0),coalesce(sum(cache_write_tokens),0),count(*) FILTER (WHERE cache_hit),count(*) FILTER (WHERE NOT priced) FROM usage_events WHERE `+filter, args...).Scan(&summary.Requests, &summary.CostUSD, &summary.InputCostUSD, &summary.OutputCostUSD, &summary.CacheReadCostUSD, &summary.CacheWriteCostUSD, &summary.PromptTok, &summary.CompletionTo, &summary.CacheReadTok, &summary.CacheWriteTok, &summary.CacheHits, &summary.Unpriced)
	return summary, err
}
