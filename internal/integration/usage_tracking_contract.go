package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

type TrackingRepository interface {
	IdentityUsageRepository
	entities.UsageActivityRepository
	entities.WorkloadUsageAggregateRepository
}

// RunUsageTrackingContract is shared by SQLite, PostgreSQL, and ClickHouse.
// Synthetic records remain isolated by a random application namespace.
func RunUsageTrackingContract(t *testing.T, repo TrackingRepository) {
	t.Helper()
	ctx := context.Background()
	ns := entities.NewID("tracking")
	start := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 7)
	binding := entities.WorkloadBinding{Application: ns, WorkspaceID: "ws", AgentID: "a"}
	events := []entities.UsageEvent{}
	for i := 0; i < 4; i++ {
		event := entities.UsageEvent{ID: entities.NewID("usage"), TS: start.Add(time.Hour + 123*time.Millisecond), AccountingTS: start.Add(time.Hour + 123*time.Millisecond), ActorType: entities.ActorUser, UserID: "u1", Username: "synthetic-user", OrganizationID: "org1", Application: ns, WorkspaceID: "ws", AgentID: "a", ConversationID: "session-a", RunID: "run-a", ParentRunID: "parent-a", LogicalRequestID: "request-a", TraceID: "trace-a", ProviderAttemptID: entities.NewID("attempt"), Provider: "mock", CredentialID: "cred", Model: "m", ApiKeyID: "key", CostUSD: 1, InputCostUSD: 0.25, OutputCostUSD: 0.25, CacheReadCostUSD: 0.25, CacheWriteCostUSD: 0.25, PromptTokens: 10, CompletionTokens: 2, CacheReadTokens: 3, CacheWriteTokens: 4, Priced: true, StatusCode: 200}
		switch i {
		case 1:
			event.AgentID = "b"
			event.RunID = "run-b"
			event.TraceID = "trace-b"
			event.LogicalRequestID = "request-b"
			event.ParentRunID = "parent-b"
			event.ConversationID = "session-b"
		case 2:
			event.UserID = "u2"
			event.OrganizationID = "org2"
		case 3:
			event.Environment = "other"
			event.OrganizationID = "" // same user, private namespace
		}
		events = append(events, event)
	}
	// Legacy unbound record has no trace metadata and no current key dependency.
	legacy := events[0]
	legacy.ID = entities.NewID("usage")
	legacy.AgentID = ""
	legacy.TraceID = ""
	legacy.RunID = ""
	legacy.ParentRunID = ""
	legacy.ConversationID = ""
	legacy.LogicalRequestID = ""
	events = append(events, legacy)
	if err := repo.InsertBatch(ctx, events); err != nil {
		t.Fatal(err)
	}
	base := entities.UsageQuery{Visibility: entities.UsageVisibility{PrincipalType: entities.PrincipalMaster}, Application: ns, Since: &start, Until: &end}
	tests := []struct {
		name  string
		edit  func(*entities.UsageQuery)
		count int64
	}{
		{"default_all", func(*entities.UsageQuery) {}, 5},
		{"agent_multiple", func(q *entities.UsageQuery) { q.AgentID = "a,b" }, 4},
		{"users_multiple", func(q *entities.UsageQuery) { q.UserID = "u1,u2" }, 5},
		{"requests_multiple", func(q *entities.UsageQuery) { q.LogicalRequestID = "request-a,request-b" }, 4},
		{"runs_multiple", func(q *entities.UsageQuery) { q.RunID = "run-a,run-b" }, 4},
		{"parents_multiple", func(q *entities.UsageQuery) { q.ParentRunID = "parent-a,parent-b" }, 4},
		{"traces_multiple", func(q *entities.UsageQuery) { q.TraceID = "trace-a,trace-b" }, 4},
		{"sessions_multiple", func(q *entities.UsageQuery) { q.ConversationID = "session-a,session-b" }, 4},
		{"and_not_or", func(q *entities.UsageQuery) { q.AgentID = "a"; q.TraceID = "trace-b" }, 0},
		{"agent_batch_intersection", func(q *entities.UsageQuery) { q.AgentID = "a"; q.AgentIDs = []string{"b"} }, 0},
		{"no_agent_filter_regression", func(q *entities.UsageQuery) { q.AgentIDs = nil }, 5},
		{"personal_self", func(q *entities.UsageQuery) {
			q.Visibility = entities.UsageVisibility{PrincipalType: entities.PrincipalUser, UserID: "u1"}
		}, 4},
		{"forged_user_filter", func(q *entities.UsageQuery) {
			q.Visibility = entities.UsageVisibility{PrincipalType: entities.PrincipalUser, UserID: "u1"}
			q.UserID = "u2"
		}, 0},
		{"org_admin", func(q *entities.UsageQuery) {
			q.Visibility = entities.UsageVisibility{PrincipalType: entities.PrincipalUser, UserID: "u1", OrganizationID: "org1", OrganizationWide: true}
		}, 3},
		{"org_member_private_excluded", func(q *entities.UsageQuery) {
			q.Visibility = entities.UsageVisibility{PrincipalType: entities.PrincipalUser, UserID: "u1", OrganizationID: "org1"}
		}, 3},
		{"master_context", func(q *entities.UsageQuery) { q.Visibility.OrganizationID = "org1" }, 3},
		{"binding_empty_env_exact", func(q *entities.UsageQuery) {
			q.Visibility = entities.UsageVisibility{PrincipalType: entities.PrincipalUser, UserID: "u1", OrganizationID: "org1", Workload: &binding}
		}, 1},
		{"invalid_visibility", func(q *entities.UsageQuery) { q.Visibility = entities.UsageVisibility{} }, 0},
		{"millisecond_end_exclusive", func(q *entities.UsageQuery) { v := start.Add(time.Hour + 123*time.Millisecond); q.Until = &v }, 0},
		{"submillisecond_until", func(q *entities.UsageQuery) {
			v := start.Add(time.Hour + 123*time.Millisecond + time.Microsecond)
			q.Until = &v
		}, 5},
		{"exclusive_end", func(q *entities.UsageQuery) { v := start.Add(time.Hour); q.Until = &v }, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q := base
			tc.edit(&q)
			sum, err := repo.SummaryUsage(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			if sum.Requests != tc.count || sum.CostUSD != float64(tc.count) {
				t.Fatalf("summary=%+v want %d", sum, tc.count)
			}
			weekly, err := repo.WorkloadUsageAggregate(ctx, q)
			if err != nil || weekly.Requests != tc.count {
				t.Fatalf("weekly=%+v err=%v", weekly, err)
			}
			buckets, err := repo.ActivityUsage(ctx, q, "hour")
			if err != nil {
				t.Fatal(err)
			}
			var requests int64
			for _, b := range buckets {
				requests += b.Requests
			}
			if requests != tc.count {
				t.Fatalf("activity=%d want %d", requests, tc.count)
			}
			q.Limit = 1
			var found int64
			for {
				page, err := repo.QueryUsage(ctx, q)
				if err != nil {
					t.Fatal(err)
				}
				found += int64(len(page.Data))
				if found > 5 {
					t.Fatal("cursor failed to advance")
				}
				if page.NextCursor == "" {
					break
				}
				q.Cursor = page.NextCursor
			}
			if found != tc.count {
				t.Fatalf("pages=%d want %d", found, tc.count)
			}
		})
	}
	detail, err := repo.UsageDetail(ctx, events[0].ID, base.Visibility)
	if err != nil || detail.TraceID != "trace-a" || detail.ParentRunID != "parent-a" || detail.LogicalRequestID != "request-a" {
		t.Fatalf("detail=%+v err=%v", detail, err)
	}
	_, err = repo.UsageDetail(ctx, events[2].ID, entities.UsageVisibility{PrincipalType: entities.PrincipalUser, UserID: "u1"})
	if !errors.Is(err, entities.ErrNotFound) {
		t.Fatalf("foreign detail not concealed: %v", err)
	}
	_, err = repo.UsageDetail(ctx, events[3].ID, entities.UsageVisibility{PrincipalType: entities.PrincipalUser, UserID: "u1", OrganizationID: "org1", Workload: &binding})
	if !errors.Is(err, entities.ErrNotFound) {
		t.Fatalf("foreign workload detail not concealed: %v", err)
	}
}
