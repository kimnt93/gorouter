package local

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"sort"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

type UsageRepo struct{ s *Store }

func NewUsageRepo(store *Store) *UsageRepo { return &UsageRepo{s: store} }

func nullableConversation(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func (r *UsageRepo) InsertBatch(ctx context.Context, events []entities.UsageEvent) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := r.s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statement, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO usage_events(id,ts,payload,conversation_enc,content_truncated) VALUES(?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer statement.Close()
	for _, event := range events {
		if event.ID == "" {
			event.ID = entities.NewID("usage")
		}
		if event.TS.IsZero() {
			event.TS = time.Now().UTC()
		} else {
			event.TS = event.TS.UTC()
		}
		if event.ActorType == "" {
			event.ActorType, event.Username, event.OrganizationID = entities.ActorLegacy, entities.ActorLegacy, event.TenantID
		}
		if event.AccountingTS.IsZero() {
			event.AccountingTS = event.TS
		}
		event.AccountingTS = event.AccountingTS.UTC()
		if event.AccountingState == "" {
			event.AccountingState = "settled"
		}
		if event.UsageMeasurement == "" {
			event.UsageMeasurement = "unknown"
		}
		payload, err := json.Marshal(event)
		if err != nil {
			return err
		}
		if _, err := statement.ExecContext(ctx, event.ID, event.TS.Format(time.RFC3339Nano), payload, nullableConversation(event.ConversationEnc), event.ContentTruncated); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func usageMatches(event entities.UsageEvent, query entities.UsageQuery, cursor auditCursor) bool {
	if !query.Visibility.Allows(event.UserID, event.OrganizationID) {
		return false
	}
	for _, filter := range query.Filters() {
		var value string
		switch filter.Field {
		case "organization_id":
			value = event.OrganizationID
		case "user_id":
			value = event.UserID
		case "model":
			value = event.Model
		case "api_key_id":
			value = event.ApiKeyID
		case "provider":
			value = event.Provider
		case "credential_id":
			value = event.CredentialID
		case "application":
			value = event.Application
		case "environment":
			value = event.Environment
		case "workspace_id":
			value = event.WorkspaceID
		case "agent_id":
			value = event.AgentID
		case "conversation_id":
			value = event.ConversationID
		case "run_id":
			value = event.RunID
		case "parent_run_id":
			value = event.ParentRunID
		case "trace_id":
			value = event.TraceID
		case "logical_request_id":
			value = event.LogicalRequestID
		}
		if !slices.Contains(filter.Values, value) {
			return false
		}
	}
	if query.StatusCode != nil && event.StatusCode != *query.StatusCode || query.Since != nil && event.TS.Before(*query.Since) || query.Until != nil && !event.TS.Before(*query.Until) {
		return false
	}
	return cursor.TS.IsZero() || event.TS.Before(cursor.TS) || event.TS.Equal(cursor.TS) && event.ID < cursor.ID
}

func recent(event entities.UsageEvent) entities.RecentEvent {
	return entities.RecentEvent{ID: event.ID, TS: event.TS, TenantID: event.TenantID, KeyID: event.ApiKeyID, CredentialID: event.CredentialID, Provider: event.Provider, Model: event.Model, UpstreamModel: event.UpstreamModel, PromptTokens: event.PromptTokens, CompletionTokens: event.CompletionTokens, CacheReadTokens: event.CacheReadTokens, CacheWriteTokens: event.CacheWriteTokens, CostUSD: event.CostUSD, Priced: event.Priced, CacheHit: event.CacheHit, StatusCode: event.StatusCode, DurationMS: event.DurationMS, Error: event.Error, ActorType: event.ActorType, UserID: event.UserID, Username: event.Username, OrganizationID: event.OrganizationID, Application: event.Application, Environment: event.Environment, WorkspaceID: event.WorkspaceID, AgentID: event.AgentID, ConversationID: event.ConversationID, RunID: event.RunID, ParentRunID: event.ParentRunID, TraceID: event.TraceID, LogicalRequestID: event.LogicalRequestID, ProviderAttemptID: event.ProviderAttemptID, AccountingTS: event.AccountingTS, UsageMeasurement: event.UsageMeasurement, AccountingState: event.AccountingState}
}

func (r *UsageRepo) Recent(ctx context.Context, limit int) ([]entities.RecentEvent, error) {
	return r.recentFor(ctx, "", limit)
}
func (r *UsageRepo) RecentForTenant(ctx context.Context, tenant string, limit int) ([]entities.RecentEvent, error) {
	return r.recentFor(ctx, tenant, limit)
}
func (r *UsageRepo) recentFor(ctx context.Context, tenant string, limit int) ([]entities.RecentEvent, error) {
	page, err := r.QueryUsage(ctx, entities.UsageQuery{Visibility: entities.UsageVisibility{PrincipalType: entities.PrincipalMaster}, OrganizationID: tenant, Limit: limit})
	if err != nil {
		return nil, err
	}
	return page.Data, nil
}
func (r *UsageRepo) Summary(ctx context.Context, since time.Time) (*entities.UsageSummary, error) {
	return r.SummaryUsage(ctx, entities.UsageQuery{Visibility: entities.UsageVisibility{PrincipalType: entities.PrincipalMaster}, Since: &since})
}
func (r *UsageRepo) SummaryForTenant(ctx context.Context, tenant string, since time.Time) (*entities.UsageSummary, error) {
	return r.SummaryUsage(ctx, entities.UsageQuery{Visibility: entities.UsageVisibility{PrincipalType: entities.PrincipalMaster}, OrganizationID: tenant, Since: &since})
}

func (r *UsageRepo) HealthUsage(ctx context.Context, query entities.UsageQuery) ([]entities.UsageHealthMetric, error) {
	events, err := r.selectedEvents(ctx, query)
	if err != nil {
		return nil, err
	}
	type accumulator struct {
		metric       entities.UsageHealthMetric
		durations    []int64
		cache, total int64
	}
	groups := map[string]*accumulator{}
	for _, e := range events {
		if !usageMatches(e, query, auditCursor{}) {
			continue
		}
		for _, d := range []struct{ name, id string }{{"provider", e.Provider}, {"model", e.Model}, {"credential", e.CredentialID}} {
			if d.id == "" {
				continue
			}
			key := d.name + "\x00" + d.id
			a := groups[key]
			if a == nil {
				a = &accumulator{metric: entities.UsageHealthMetric{Dimension: d.name, ID: d.id}}
				groups[key] = a
			}
			a.metric.Requests++
			if e.StatusCode >= 200 && e.StatusCode < 400 {
				a.metric.Successes++
			} else if e.StatusCode >= 400 && e.StatusCode < 500 && e.StatusCode != 402 && e.StatusCode != 429 {
				a.metric.ClientErrors++
			} else if e.StatusCode >= 500 || e.StatusCode == 402 || e.StatusCode == 429 {
				a.metric.ProviderErrors++
			}
			a.durations = append(a.durations, e.DurationMS)
			a.cache += e.CacheReadTokens
			a.total += e.PromptTokens + e.CacheReadTokens
		}
	}
	out := make([]entities.UsageHealthMetric, 0, len(groups))
	for _, a := range groups {
		sort.Slice(a.durations, func(i, j int) bool { return a.durations[i] < a.durations[j] })
		var total int64
		for _, v := range a.durations {
			total += v
		}
		if len(a.durations) > 0 {
			a.metric.AverageMS = float64(total) / float64(len(a.durations))
			a.metric.P95MS = float64(a.durations[int(math.Ceil(.95*float64(len(a.durations))))-1])
		}
		attempts := a.metric.Successes + a.metric.ProviderErrors
		if attempts > 0 {
			a.metric.SuccessRate = float64(a.metric.Successes) / float64(attempts)
		}
		if a.total > 0 {
			a.metric.CacheReadRate = float64(a.cache) / float64(a.total)
		}
		out = append(out, a.metric)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Requests > out[j].Requests })
	return out, nil
}
func (r *UsageRepo) ActivityUsage(ctx context.Context, query entities.UsageQuery, groupBy string) ([]entities.UsageActivityBucket, error) {
	events, err := r.selectedEvents(ctx, query)
	if err != nil {
		return nil, err
	}
	groups := map[string]*entities.UsageActivityBucket{}
	for _, e := range events {
		if !usageMatches(e, query, auditCursor{}) {
			continue
		}
		start := e.TS.UTC()
		switch groupBy {
		case "hour":
			start = start.Truncate(time.Hour)
		case "week":
			start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -int(start.Weekday()))
		default:
			start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
		}
		key := start.Format(time.RFC3339Nano) + "\x00" + e.UserID
		b := groups[key]
		if b == nil {
			label := e.Username
			if label == "" {
				if e.UserID != "" {
					label = e.UserID
				} else {
					label = "Legacy"
				}
			}
			b = &entities.UsageActivityBucket{Start: start, UserID: e.UserID, Username: label}
			groups[key] = b
		}
		b.Requests++
		b.PromptTokens += e.PromptTokens
		b.CompletionTokens += e.CompletionTokens
		b.CacheReadTokens += e.CacheReadTokens
		b.CacheWriteTokens += e.CacheWriteTokens
		b.CostUSD += e.CostUSD
		b.InputCostUSD += e.InputCostUSD
		b.OutputCostUSD += e.OutputCostUSD
		b.CacheReadCostUSD += e.CacheReadCostUSD
		b.CacheWriteCostUSD += e.CacheWriteCostUSD
	}
	out := make([]entities.UsageActivityBucket, 0, len(groups))
	for _, b := range groups {
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Start.Before(out[j].Start) || out[i].Start.Equal(out[j].Start) && out[i].Username < out[j].Username
	})
	return out, nil
}

func (r *UsageRepo) UsageDetail(ctx context.Context, id string, visibility entities.UsageVisibility) (*entities.UsageDetail, error) {
	where, args := localUsageFilter(entities.UsageQuery{Visibility: visibility})
	args = append(args, id)
	var payload, encrypted []byte
	var truncated bool
	err := r.s.DB.QueryRowContext(ctx, "SELECT payload,conversation_enc,content_truncated FROM usage_events WHERE "+where+" AND id=?", args...).Scan(&payload, &encrypted, &truncated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, entities.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var event entities.UsageEvent
	if err = json.Unmarshal(payload, &event); err != nil {
		return nil, err
	}
	return &entities.UsageDetail{RecentEvent: recent(event), ContentTruncated: truncated, ConversationEncrypted: encrypted}, nil
}
