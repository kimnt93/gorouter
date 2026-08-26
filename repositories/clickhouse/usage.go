package clickhouse

import (
	"context"
	"encoding/json"
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
	b, e := r.s.Conn.PrepareBatch(ctx, `INSERT INTO usage_events`)
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
		if e = b.Append(v.ID, v.TS, v.TenantID, v.ApiKeyID, v.CredentialID, v.Model, v.UpstreamModel, v.PromptTokens, v.CompletionTokens, v.CacheReadTokens, v.CacheWriteTokens, v.CostUSD, v.Priced, v.CacheHit, int32(v.StatusCode), v.DurationMS, v.Error); e != nil {
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
	e := r.s.Conn.QueryRow(ctx, `SELECT count(),sum(cost_usd),sum(prompt_tokens),sum(completion_tokens),sum(cache_read_tokens),countIf(cache_hit),countIf(NOT priced) FROM usage_events WHERE `+where, args...).Scan(&s.Requests, &s.CostUSD, &s.PromptTok, &s.CompletionTo, &s.CacheReadTok, &s.CacheHits, &s.Unpriced)
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
	q := `SELECT event_id,ts,tenant_id,api_key_id,credential_id,model,upstream_model,prompt_tokens,completion_tokens,cache_read_tokens,cache_write_tokens,cost_usd,priced,cache_hit,status_code,duration_ms,error FROM usage_events`
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
		if e = rows.Scan(&v.ID, &v.TS, &v.TenantID, &v.KeyID, &v.CredentialID, &v.Model, &v.UpstreamModel, &v.PromptTokens, &v.CompletionTokens, &v.CacheReadTokens, &v.CacheWriteTokens, &v.CostUSD, &v.Priced, &v.CacheHit, &statusCode, &v.DurationMS, &v.Error); e != nil {
			return nil, e
		}
		v.StatusCode = int(statusCode)
		out = append(out, v)
	}
	return out, rows.Err()
}
