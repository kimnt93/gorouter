package local

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

const usageTimeFormat = "2006-01-02T15:04:05.000000000Z"

func localUsageFilter(q entities.UsageQuery) (string, []any) {
	clauses, args := []string{"1=1"}, []any{}
	add := func(column, op string, value any) {
		clauses = append(clauses, column+op+"?")
		args = append(args, value)
	}
	v := q.Visibility
	switch {
	case v.PrincipalType == entities.PrincipalMaster:
		if v.OrganizationID != "" {
			add("organization_id", "=", v.OrganizationID)
		}
	case v.OrganizationWide && v.OrganizationID != "":
		add("organization_id", "=", v.OrganizationID)
	case !v.OrganizationWide && v.UserID != "":
		add("user_id", "=", v.UserID)
		if v.OrganizationID != "" || v.Workload != nil {
			add("organization_id", "=", v.OrganizationID)
		}
	default:
		clauses = append(clauses, "0=1")
	}
	if q.PersonalOnly {
		add("organization_id", "=", "")
	}
	for _, f := range q.Filters() {
		clauses = append(clauses, f.Field+" IN ("+strings.TrimSuffix(strings.Repeat("?,", len(f.Values)), ",")+")")
		for _, value := range f.Values {
			args = append(args, value)
		}
	}
	if q.StatusCode != nil {
		add("status_code", "=", *q.StatusCode)
	}
	column := "event_time"
	if q.TimeBasis == "accounting" {
		column = "accounting_time"
	}
	if q.Since != nil {
		add(column, ">=", q.Since.UTC().Format(usageTimeFormat))
	}
	if q.Until != nil {
		add(column, "<", q.Until.UTC().Format(usageTimeFormat))
	}
	return strings.Join(clauses, " AND "), args
}

// selectedEvents is only for legacy health/activity computations. SQL performs
// authorization and filtering first; encrypted content is never read here.
func (r *UsageRepo) selectedEvents(ctx context.Context, q entities.UsageQuery) ([]entities.UsageEvent, error) {
	where, args := localUsageFilter(q)
	rows, err := r.s.DB.QueryContext(ctx, "SELECT payload FROM usage_events WHERE "+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []entities.UsageEvent{}
	for rows.Next() {
		var raw []byte
		var event entities.UsageEvent
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &event); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

const localSummaryColumns = `count(*),coalesce(sum(cost_usd),0),coalesce(sum(input_cost_usd),0),coalesce(sum(output_cost_usd),0),coalesce(sum(cache_read_cost_usd),0),coalesce(sum(cache_write_cost_usd),0),coalesce(sum(prompt_tokens),0),coalesce(sum(completion_tokens),0),coalesce(sum(cache_read_tokens),0),coalesce(sum(cache_write_tokens),0),coalesce(sum(cache_hit),0),coalesce(sum(CASE WHEN priced=0 THEN 1 ELSE 0 END),0)`

func (r *UsageRepo) SummaryUsage(ctx context.Context, q entities.UsageQuery) (*entities.UsageSummary, error) {
	out := &entities.UsageSummary{ByModel: map[string]entities.ModelU{}, ByKey: map[string]entities.KeyU{}}
	where, args := localUsageFilter(q)
	err := r.s.DB.QueryRowContext(ctx, "SELECT "+localSummaryColumns+" FROM usage_events WHERE "+where, args...).Scan(&out.Requests, &out.CostUSD, &out.InputCostUSD, &out.OutputCostUSD, &out.CacheReadCostUSD, &out.CacheWriteCostUSD, &out.PromptTok, &out.CompletionTo, &out.CacheReadTok, &out.CacheWriteTok, &out.CacheHits, &out.Unpriced)
	if err != nil {
		return nil, err
	}
	if q.TotalsOnly {
		return out, nil
	}
	rows, err := r.s.DB.QueryContext(ctx, `SELECT model,count(*),sum(cost_usd),sum(prompt_tokens),sum(completion_tokens),sum(cache_read_tokens),sum(cache_write_tokens) FROM usage_events WHERE `+where+` GROUP BY model`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name string
		var v entities.ModelU
		if err = rows.Scan(&name, &v.Requests, &v.CostUSD, &v.InTok, &v.OutTok, &v.CacheReadTok, &v.CacheWriteTok); err != nil {
			rows.Close()
			return nil, err
		}
		out.ByModel[name] = v
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = r.s.DB.QueryContext(ctx, `SELECT api_key_id,count(*),sum(cost_usd) FROM usage_events WHERE `+where+` GROUP BY api_key_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var v entities.KeyU
		if err = rows.Scan(&name, &v.Requests, &v.CostUSD); err != nil {
			return nil, err
		}
		out.ByKey[name] = v
	}
	return out, rows.Err()
}

func (r *UsageRepo) WorkloadUsageAggregate(ctx context.Context, q entities.UsageQuery) (*entities.UsageSummary, error) {
	q.TimeBasis = "accounting"
	q.TotalsOnly = true
	return r.SummaryUsage(ctx, q)
}

func (r *UsageRepo) SpendForKeySince(ctx context.Context, id string, since time.Time) (float64, error) {
	q := entities.UsageQuery{Visibility: entities.UsageVisibility{PrincipalType: entities.PrincipalMaster}, APIKeyID: id, Since: &since, TotalsOnly: true}
	summary, err := r.SummaryUsage(ctx, q)
	if err != nil {
		return 0, err
	}
	return summary.CostUSD, nil
}

func (r *UsageRepo) QueryUsage(ctx context.Context, q entities.UsageQuery) (*entities.UsagePage, error) {
	where, args := localUsageFilter(q)
	cursor := decodeAuditCursor(q.Cursor)
	if !cursor.TS.IsZero() {
		where += " AND (event_time,id)<(?,?)"
		args = append(args, cursor.TS.UTC().Format(usageTimeFormat), cursor.ID)
	}
	limit := boundedConfigLimit(q.Limit)
	args = append(args, limit+1)
	rows, err := r.s.DB.QueryContext(ctx, "SELECT payload FROM usage_events WHERE "+where+" ORDER BY event_time DESC,id DESC LIMIT ?", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	page := &entities.UsagePage{Data: []entities.RecentEvent{}}
	for rows.Next() {
		var raw []byte
		var e entities.UsageEvent
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &e); err != nil {
			return nil, err
		}
		page.Data = append(page.Data, recent(e))
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(page.Data) > limit {
		last := page.Data[limit-1]
		page.NextCursor = encodeAuditCursor(entities.AuditEvent{ID: last.ID, TS: last.TS})
		page.Data = page.Data[:limit]
	}
	return page, nil
}
