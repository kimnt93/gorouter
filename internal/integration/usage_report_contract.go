package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/usage"
)

type reportRepository interface {
	usage.Repository
	entities.UsageReportRepository
	entities.PrincipalUsageRepository
}

// RunUsageReportContract executes identical populated queries against all stores.
func RunUsageReportContract(t *testing.T, repo reportRepository) {
	t.Helper()
	ctx := context.Background()
	ns := entities.NewID("report")
	from := time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 4)
	events := []entities.UsageEvent{}
	for i := 0; i < 5; i++ {
		e := entities.UsageEvent{ID: entities.NewID("usage"), TS: from.Add(time.Duration(i+1) * 24 * time.Hour), AccountingTS: from.Add(time.Duration(i) * 24 * time.Hour), ActorType: entities.ActorUser, UserID: ns, OrganizationID: "org", Application: ns, AgentID: "alpha", Model: "alias", ApiKeyID: "key", Provider: "mock", ConversationID: "conversation", TraceID: "trace", PromptTokens: 100, CompletionTokens: 20, CacheReadTokens: 40, CacheWriteTokens: 5, CostUSD: .02, InputCostUSD: .01, OutputCostUSD: .005, CacheReadCostUSD: .004, CacheWriteCostUSD: .001, Priced: true, UsageMeasurement: "provider_reported_or_adapter_normalized", StatusCode: 200}
		switch i {
		case 1:
			e.AgentID = "beta"
			e.Model = "other"
		case 2:
			e.OrganizationID = ""
			e.AgentID = ""
			e.UsageMeasurement = "unknown"
		case 3:
			e.CacheHit = true
			e.CostUSD = 0
			e.InputCostUSD = 0
			e.OutputCostUSD = 0
			e.CacheReadCostUSD = 0
			e.CacheWriteCostUSD = 0
		case 4:
			e.UserID = "foreign"
			e.AccountingTS = from
		}
		events = append(events, e)
	}
	if err := repo.InsertBatch(ctx, events); err != nil {
		t.Fatal(err)
	}
	svc := usage.NewService(repo, 16, nil)
	defer svc.Close()
	base := entities.UsageReportQuery{UsageQuery: entities.UsageQuery{Visibility: entities.UsageVisibility{PrincipalType: entities.PrincipalUser, UserID: ns}, Application: ns, Since: &from, Until: &to, TimeBasis: "accounting"}, GroupBy: "agent", SeriesBy: "model", Bucket: "day", WeekStart: time.Sunday}
	r, err := svc.Report(ctx, base, entities.UsageReportScope{Kind: "all_owned", UserID: ns})
	if err != nil {
		t.Fatal(err)
	}
	if r.Totals.Requests != 4 || r.Totals.TotalTokens != 495 || r.Totals.ResponseCacheHits != 1 || r.Totals.MeasurementUnknownRequests != 1 || r.Totals.CostUSD != .06 {
		t.Fatalf("totals=%+v", r.Totals)
	}
	if len(r.Groups) != 3 || len(r.Series) != 4 || r.Coverage.State != "unknown" || r.Freshness.State != "stored_only" {
		t.Fatalf("report=%+v", r)
	}
	var groupSum, seriesSum entities.UsageReportTotals
	for _, g := range r.Groups {
		groupSum.Add(g.Totals)
	}
	for _, b := range r.Series {
		seriesSum.Add(b.Totals)
	}
	if groupSum.Requests != r.Totals.Requests || seriesSum.TotalTokens != r.Totals.TotalTokens {
		t.Fatal("non-reconciling report")
	}
	cases := []struct {
		name  string
		edit  func(*entities.UsageReportQuery)
		count int64
	}{
		{"one_agent", func(q *entities.UsageReportQuery) { q.AgentID = "alpha" }, 2},
		{"two_agents", func(q *entities.UsageReportQuery) { q.AgentID = "alpha,beta" }, 3},
		{"session", func(q *entities.UsageReportQuery) { q.ConversationID = "conversation"; q.AgentID = "beta" }, 1},
		{"foreign", func(q *entities.UsageReportQuery) { q.UserID = "foreign" }, 0},
		{"unknown", func(q *entities.UsageReportQuery) { q.AgentID = "missing" }, 0},
		{"personal", func(q *entities.UsageReportQuery) { q.PersonalOnly = true }, 1},
		{"org", func(q *entities.UsageReportQuery) {
			q.Visibility.OrganizationID = "org"
			q.Visibility.OrganizationWide = true
		}, 4},
		{"completion", func(q *entities.UsageReportQuery) { q.TimeBasis = "completion" }, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := base
			tc.edit(&q)
			r, err := svc.Report(ctx, q, entities.UsageReportScope{})
			if err != nil {
				t.Fatal(err)
			}
			if r.Totals.Requests != tc.count {
				t.Fatalf("requests=%d want %d", r.Totals.Requests, tc.count)
			}
			q.TotalsOnly = true
			s, err := repo.SummaryUsage(ctx, q.UsageQuery)
			if err != nil || s.Requests != tc.count || len(s.ByKey) != 0 || len(s.ByModel) != 0 {
				t.Fatalf("totals-only=%+v err=%v", s, err)
			}
		})
	}
	for _, bucket := range []string{"hour", "day", "week", "month"} {
		for _, week := range []time.Weekday{time.Sunday, time.Monday, time.Saturday} {
			q := base
			q.Bucket = bucket
			q.WeekStart = week
			r, err := svc.Report(ctx, q, entities.UsageReportScope{})
			if err != nil {
				t.Fatal(err)
			}
			var n int64
			for _, b := range r.Series {
				n += b.Totals.Requests
				if b.Start.Before(from) || b.End.After(to) {
					t.Fatal("unclipped bounds")
				}
			}
			if n != 4 {
				t.Fatal("calendar bucket mismatch")
			}
		}
	}

	t.Run("combined_series_without_groups", func(t *testing.T) {
		q := base
		q.GroupBy = ""
		q.SeriesBy = ""
		r, err := svc.Report(ctx, q, entities.UsageReportScope{})
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Groups) != 0 || r.Totals.Requests != 4 || len(r.Series) != 4 {
			t.Fatalf("combined report=%+v", r)
		}
	})
	t.Run("month_edges", func(t *testing.T) {
		q := base
		q.GroupBy = ""
		q.SeriesBy = ""
		q.Bucket = "month"
		r, err := svc.Report(ctx, q, entities.UsageReportScope{})
		if err != nil {
			t.Fatal(err)
		}
		boundary := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
		if len(r.Series) != 2 || !r.Series[0].End.Equal(boundary) || !r.Series[1].Start.Equal(boundary) || r.Series[0].Totals.Requests != 2 || r.Series[1].Totals.Requests != 2 {
			t.Fatal("calendar months were not partitioned at UTC month edge")
		}
	})
	t.Run("week_edges", func(t *testing.T) {
		q := base
		q.GroupBy = ""
		q.SeriesBy = ""
		q.Bucket = "week"
		q.WeekStart = time.Monday
		r, err := svc.Report(ctx, q, entities.UsageReportScope{})
		if err != nil {
			t.Fatal(err)
		}
		boundary := from.AddDate(0, 0, 1)
		if len(r.Series) != 2 || !r.Series[0].End.Equal(boundary) || r.Series[0].Totals.Requests != 1 || r.Series[1].Totals.Requests != 3 {
			t.Fatal("configured week edge mismatch")
		}
	})
	q := base
	q.Bucket = "invalid"
	if _, err = svc.Report(ctx, q, entities.UsageReportScope{}); !errors.Is(err, entities.ErrUsageReportLimit) {
		t.Fatalf("unbounded invalid bucket: %v", err)
	}
	q = base
	wide := from.AddDate(1, 0, 0)
	q.Until = &wide
	q.Bucket = "hour"
	if _, err = svc.Report(ctx, q, entities.UsageReportScope{}); !errors.Is(err, entities.ErrUsageReportLimit) {
		t.Fatal("unbounded buckets")
	}
}
