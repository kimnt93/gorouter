package local

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kimnt93/gorouter/internal/platform/database"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/usage"
)

func TestCanonicalLookupIncludesDisabledAndLegacyOrder(t *testing.T) {
	ctx := context.Background()
	db, err := database.ConnectSQLite(ctx, t.TempDir()+"/primary.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	store := New(db.DB)
	now := time.Now().UTC().Truncate(time.Second)
	for _, k := range []entities.ApiKey{{ID: "org", OwnerType: entities.OwnerUser, OwnerUserID: "u", ContextOrganizationID: "org", CreatedAt: now.Add(-time.Hour)}, {ID: "z", OwnerType: entities.OwnerUser, OwnerUserID: "u", CreatedAt: now.Add(time.Nanosecond)}, {ID: "a", OwnerType: entities.OwnerUser, OwnerUserID: "u", CreatedAt: now, Enabled: false}, {ID: "foreign", OwnerType: entities.OwnerUser, OwnerUserID: "other", CreatedAt: now.Add(-time.Hour)}} {
		if err = store.put(ctx, "api_key", k.ID, storedAPIKey{ApiKey: k}); err != nil {
			t.Fatal(err)
		}
	}
	k, err := NewApiKeyRepo(store).PrimaryForUser(ctx, "u")
	if err != nil || k.ID != "a" || k.Enabled {
		t.Fatalf("key=%+v err=%v", k, err)
	}
	rows, err := db.DB.QueryContext(ctx, `EXPLAIN QUERY PLAN SELECT sum(cost_usd) FROM usage_events WHERE user_id=? AND agent_id=? AND accounting_time>=?`, "u", "a", now.Format(usageTimeFormat))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	plan := ""
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan += detail
	}
	if !strings.Contains(plan, "usage_events_user_accounting_idx") {
		t.Fatalf("selective read plan=%s", plan)
	}
}

// This populated single-writer/file-WAL microbenchmark is not the handoff's
// 1m/10m-event, concurrent-ingestion performance gate.
func BenchmarkLocalAccountingReads(b *testing.B) {
	ctx := context.Background()
	db, err := database.ConnectSQLite(ctx, b.TempDir()+"/bench.db")
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		b.Fatal(err)
	}
	repo := NewUsageRepo(New(db.DB))
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, 0)
	events := make([]entities.UsageEvent, 10000)
	for i := range events {
		events[i] = entities.UsageEvent{ID: fmt.Sprintf("e%d", i), TS: from.Add(time.Duration(i%30) * 24 * time.Hour), ActorType: entities.ActorUser, UserID: fmt.Sprintf("u%d", i/100), AgentID: fmt.Sprintf("a%d", i%100), ConversationID: fmt.Sprintf("c%d", i%100), Model: "synthetic", PromptTokens: 100, CompletionTokens: 20, CacheReadTokens: 40, CacheWriteTokens: 5, CostUSD: .02, Priced: true, UsageMeasurement: "provider_reported_or_adapter_normalized"}
	}
	if err = repo.InsertBatch(ctx, events); err != nil {
		b.Fatal(err)
	}
	svc := usage.NewService(repo, 16, nil)
	defer svc.Close()
	base := entities.UsageQuery{Visibility: entities.UsageVisibility{PrincipalType: entities.PrincipalUser, UserID: "u0"}, Since: &from, Until: &to, TimeBasis: "accounting", TotalsOnly: true}
	b.Run("session_totals_10k", func(b *testing.B) {
		q := base
		q.AgentID = "a0"
		q.ConversationID = "c0"
		b.ReportAllocs()
		for b.Loop() {
			r, err := repo.SummaryUsage(ctx, q)
			if err != nil || r.Requests != 1 {
				b.Fatalf("populated totals: %v", err)
			}
		}
	})
	b.Run("100_agent_report_10k", func(b *testing.B) {
		q := entities.UsageReportQuery{UsageQuery: base, GroupBy: "agent", SeriesBy: "agent", Bucket: "day", WeekStart: time.Sunday}
		b.ReportAllocs()
		for b.Loop() {
			r, err := svc.Report(ctx, q, entities.UsageReportScope{Kind: "personal", UserID: "u0"})
			if err != nil || r.Totals.Requests != 100 {
				b.Fatalf("populated report: %v", err)
			}
		}
	})
}
