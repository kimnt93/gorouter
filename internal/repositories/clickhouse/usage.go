package clickhouse

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

func jsonUnmarshal(v string, out any) error { return json.Unmarshal([]byte(v), out) }

type UsageRepo struct{ s *Store }

func NewUsageRepo(s *Store) *UsageRepo { return &UsageRepo{s} }
func (r *UsageRepo) SpendForKeySince(ctx context.Context, id string, since time.Time) (float64, error) {
	var v float64
	e := r.s.Conn.QueryRow(ctx, `SELECT sum(cost_usd) FROM usage_events WHERE api_key_id=? AND ts>=?`, id, since.UTC()).Scan(&v)
	return v, e
}
func (r *UsageRepo) InsertBatch(ctx context.Context, events []entities.UsageEvent) error {
	if len(events) == 0 {
		return nil
	}
	b, e := r.s.Conn.PrepareBatch(ctx, `INSERT INTO usage_events (event_id,ts,tenant_id,api_key_id,credential_id,model,upstream_model,prompt_tokens,completion_tokens,cache_read_tokens,cache_write_tokens,cost_usd,priced,cache_hit,status_code,duration_ms,error,actor_type,user_id,username,organization_id)`)
	if e != nil {
		return e
	}
	for _, v := range events {
		if v.ID == "" {
			v.ID = entities.NewID("usage")
		}
		if v.TS.IsZero() {
			v.TS = time.Now().UTC()
		} else {
			v.TS = v.TS.UTC()
		}
		if v.ActorType == "" {
			v.ActorType, v.Username, v.OrganizationID = entities.ActorLegacy, entities.ActorLegacy, v.TenantID
		}
		if e = b.Append(v.ID, v.TS, v.TenantID, v.ApiKeyID, v.CredentialID, v.Model, v.UpstreamModel, v.PromptTokens, v.CompletionTokens, v.CacheReadTokens, v.CacheWriteTokens, v.CostUSD, v.Priced, v.CacheHit, int32(v.StatusCode), v.DurationMS, v.Error, v.ActorType, v.UserID, v.Username, v.OrganizationID); e != nil {
			return e
		}
	}
	return b.Send()
}
func (r *UsageRepo) Summary(ctx context.Context, since time.Time) (*entities.UsageSummary, error) {
	return r.summary(ctx, "", since)
}
func (r *UsageRepo) SummaryForTenant(ctx context.Context, tenant string, since time.Time) (*entities.UsageSummary, error) {
	return r.summary(ctx, tenant, since)
}
func (r *UsageRepo) summary(ctx context.Context, tenant string, since time.Time) (*entities.UsageSummary, error) {
	s := &entities.UsageSummary{ByModel: map[string]entities.ModelU{}, ByKey: map[string]entities.KeyU{}}
	where := "ts>=?"
	args := []any{since.UTC()}
	if tenant != "" {
		where = "tenant_id=? AND ts>=?"
		args = []any{tenant, since.UTC()}
	}
	e := r.s.Conn.QueryRow(ctx, `SELECT count(),sum(cost_usd),sum(prompt_tokens),sum(completion_tokens),sum(cache_read_tokens),sum(cache_write_tokens),countIf(cache_hit),countIf(NOT priced) FROM usage_events WHERE `+where, args...).Scan(&s.Requests, &s.CostUSD, &s.PromptTok, &s.CompletionTo, &s.CacheReadTok, &s.CacheWriteTok, &s.CacheHits, &s.Unpriced)
	if e != nil {
		return nil, e
	}
	rows, e := r.s.Conn.Query(ctx, `SELECT model,count(),sum(cost_usd),sum(prompt_tokens),sum(completion_tokens) FROM usage_events WHERE `+where+` GROUP BY model ORDER BY sum(cost_usd) DESC LIMIT 50`, args...)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var k string
		var v entities.ModelU
		if e = rows.Scan(&k, &v.Requests, &v.CostUSD, &v.InTok, &v.OutTok); e != nil {
			rows.Close()
			return nil, e
		}
		s.ByModel[k] = v
	}
	rows.Close()
	rows, e = r.s.Conn.Query(ctx, `SELECT api_key_id,count(),sum(cost_usd) FROM usage_events WHERE `+where+` GROUP BY api_key_id ORDER BY sum(cost_usd) DESC LIMIT 100`, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var v entities.KeyU
		if e = rows.Scan(&k, &v.Requests, &v.CostUSD); e != nil {
			return nil, e
		}
		s.ByKey[k] = v
	}
	return s, rows.Err()
}
func (r *UsageRepo) Recent(ctx context.Context, limit int) ([]entities.RecentEvent, error) {
	return r.recent(ctx, "", limit)
}
func (r *UsageRepo) RecentForTenant(ctx context.Context, tenant string, limit int) ([]entities.RecentEvent, error) {
	return r.recent(ctx, tenant, limit)
}
func (r *UsageRepo) recent(ctx context.Context, tenant string, limit int) ([]entities.RecentEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT event_id,ts,tenant_id,api_key_id,credential_id,model,upstream_model,prompt_tokens,completion_tokens,cache_read_tokens,cache_write_tokens,cost_usd,priced,cache_hit,status_code,duration_ms,error,actor_type,user_id,username,organization_id FROM usage_events`
	args := []any{}
	if tenant != "" {
		q += ` WHERE tenant_id=?`
		args = append(args, tenant)
	}
	q += ` ORDER BY ts DESC,event_id DESC LIMIT ?`
	args = append(args, limit)
	rows, e := r.s.Conn.Query(ctx, q, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []entities.RecentEvent{}
	for rows.Next() {
		var v entities.RecentEvent
		var statusCode int32
		if e = rows.Scan(&v.ID, &v.TS, &v.TenantID, &v.KeyID, &v.CredentialID, &v.Model, &v.UpstreamModel, &v.PromptTokens, &v.CompletionTokens, &v.CacheReadTokens, &v.CacheWriteTokens, &v.CostUSD, &v.Priced, &v.CacheHit, &statusCode, &v.DurationMS, &v.Error, &v.ActorType, &v.UserID, &v.Username, &v.OrganizationID); e != nil {
			return nil, e
		}
		v.StatusCode = int(statusCode)
		out = append(out, v)
	}
	return out, rows.Err()
}

func usageWhere(query entities.UsageQuery, includeCursor bool) ([]string, []any) {
	clauses := []string{"1=1"}
	args := []any{}
	if query.Visibility.PrincipalType != entities.PrincipalMaster {
		if query.Visibility.OrganizationWide {
			clauses, args = append(clauses, "organization_id=?"), append(args, query.Visibility.OrganizationID)
		} else {
			clauses, args = append(clauses, "user_id=?"), append(args, query.Visibility.UserID)
		}
	}
	for _, filter := range []struct{ column, value string }{{"organization_id", query.OrganizationID}, {"user_id", query.UserID}, {"model", query.Model}, {"api_key_id", query.APIKeyID}} {
		if filter.value != "" {
			clauses, args = append(clauses, filter.column+"=?"), append(args, filter.value)
		}
	}
	if query.StatusCode != nil {
		clauses, args = append(clauses, "status_code=?"), append(args, int32(*query.StatusCode))
	}
	if query.Since != nil {
		clauses, args = append(clauses, "ts>=?"), append(args, query.Since.UTC())
	}
	if query.Until != nil {
		clauses, args = append(clauses, "ts<=?"), append(args, query.Until.UTC())
	}
	if includeCursor {
		cursor := clickhouseAuditCursorDecode(query.Cursor)
		if !cursor.TS.IsZero() {
			clauses, args = append(clauses, "(ts,event_id)<(?,?)"), append(args, cursor.TS, cursor.ID)
		}
	}
	return clauses, args
}

func (r *UsageRepo) QueryUsage(ctx context.Context, query entities.UsageQuery) (*entities.UsagePage, error) {
	clauses, args := usageWhere(query, true)
	limit := boundedConfigLimit(query.Limit)
	args = append(args, limit+1)
	rows, err := r.s.Conn.Query(ctx, `SELECT event_id,ts,tenant_id,api_key_id,credential_id,model,upstream_model,prompt_tokens,completion_tokens,cache_read_tokens,cache_write_tokens,cost_usd,priced,cache_hit,status_code,duration_ms,error,actor_type,user_id,username,organization_id FROM usage_events WHERE `+strings.Join(clauses, " AND ")+` ORDER BY ts DESC,event_id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	page := &entities.UsagePage{Data: make([]entities.RecentEvent, 0, limit)}
	for rows.Next() {
		var event entities.RecentEvent
		var status int32
		if err := rows.Scan(&event.ID, &event.TS, &event.TenantID, &event.KeyID, &event.CredentialID, &event.Model, &event.UpstreamModel, &event.PromptTokens, &event.CompletionTokens, &event.CacheReadTokens, &event.CacheWriteTokens, &event.CostUSD, &event.Priced, &event.CacheHit, &status, &event.DurationMS, &event.Error, &event.ActorType, &event.UserID, &event.Username, &event.OrganizationID); err != nil {
			return nil, err
		}
		event.StatusCode = int(status)
		page.Data = append(page.Data, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(page.Data) > limit {
		last := page.Data[limit-1]
		page.NextCursor = clickhouseAuditCursorEncode(entities.AuditEvent{ID: last.ID, TS: last.TS})
		page.Data = page.Data[:limit]
	}
	return page, nil
}

func (r *UsageRepo) SummaryUsage(ctx context.Context, query entities.UsageQuery) (*entities.UsageSummary, error) {
	clauses, args := usageWhere(query, false)
	where := strings.Join(clauses, " AND ")
	summary := &entities.UsageSummary{ByModel: map[string]entities.ModelU{}, ByKey: map[string]entities.KeyU{}}
	if err := r.s.Conn.QueryRow(ctx, `SELECT count(),sum(cost_usd),sum(prompt_tokens),sum(completion_tokens),sum(cache_read_tokens),sum(cache_write_tokens),countIf(cache_hit),countIf(NOT priced) FROM usage_events WHERE `+where, args...).Scan(&summary.Requests, &summary.CostUSD, &summary.PromptTok, &summary.CompletionTo, &summary.CacheReadTok, &summary.CacheWriteTok, &summary.CacheHits, &summary.Unpriced); err != nil {
		return nil, err
	}
	rows, err := r.s.Conn.Query(ctx, `SELECT model,count(),sum(cost_usd),sum(prompt_tokens),sum(completion_tokens) FROM usage_events WHERE `+where+` GROUP BY model`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name string
		var value entities.ModelU
		if err = rows.Scan(&name, &value.Requests, &value.CostUSD, &value.InTok, &value.OutTok); err != nil {
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
	rows, err = r.s.Conn.Query(ctx, `SELECT api_key_id,count(),sum(cost_usd) FROM usage_events WHERE `+where+` GROUP BY api_key_id`, args...)
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
	bucketFunction := "toStartOfDay"
	switch groupBy {
	case "hour":
		bucketFunction = "toStartOfHour"
	case "week":
		bucketFunction = "toStartOfWeek"
	case "day":
	default:
		groupBy = "day"
	}
	clauses, args := usageWhere(query, false)
	rows, err := r.s.Conn.Query(ctx, `SELECT `+bucketFunction+`(ts) AS bucket,count(),sum(prompt_tokens),sum(completion_tokens),sum(cache_read_tokens),sum(cache_write_tokens),sum(cost_usd) FROM usage_events WHERE `+strings.Join(clauses, " AND ")+` GROUP BY bucket ORDER BY bucket`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]entities.UsageActivityBucket, 0)
	for rows.Next() {
		var bucket entities.UsageActivityBucket
		if err := rows.Scan(&bucket.Start, &bucket.Requests, &bucket.PromptTokens, &bucket.CompletionTokens, &bucket.CacheReadTokens, &bucket.CacheWriteTokens, &bucket.CostUSD); err != nil {
			return nil, err
		}
		out = append(out, bucket)
	}
	return out, rows.Err()
}
