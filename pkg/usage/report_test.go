package usage

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

type reportLimitRepo struct {
	Repository
	cells []entities.UsageReportCell
	calls int
}

func (r *reportLimitRepo) ReportUsage(context.Context, entities.UsageReportQuery) ([]entities.UsageReportCell, error) {
	r.calls++
	return r.cells, nil
}
func TestReportRejectsCardinalityWithoutTruncation(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 30)
	repo := &reportLimitRepo{}
	svc := &Service{repo: repo}
	for i := 0; i < 101; i++ {
		repo.cells = append(repo.cells, entities.UsageReportCell{StartUnix: from.Unix(), GroupID: fmt.Sprint(i), SeriesID: fmt.Sprint(i), Totals: entities.UsageReportTotals{Requests: 1}})
	}
	q := entities.UsageReportQuery{UsageQuery: entities.UsageQuery{Since: &from, Until: &to, TimeBasis: "accounting"}, GroupBy: "agent", SeriesBy: "agent", Bucket: "day"}
	if _, err := svc.Report(context.Background(), q, entities.UsageReportScope{}); !errors.Is(err, entities.ErrUsageReportLimit) {
		t.Fatalf("overflow=%v", err)
	}
	if repo.calls != 1 {
		t.Fatal("report fanout")
	}
	q.Bucket = "hour"
	repo.cells = repo.cells[:100]
	if _, err := svc.Report(context.Background(), q, entities.UsageReportScope{}); !errors.Is(err, entities.ErrUsageReportLimit) {
		t.Fatal("chart cell product not bounded")
	}
}
func TestBucketStartUsesUTCAndConfiguredWeek(t *testing.T) {
	at := time.Date(2026, 9, 7, 0, 30, 0, 0, time.FixedZone("+7", 7*3600))
	if got := BucketStart(at, "day", time.Sunday); got != time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("UTC day=%v", got)
	}
	if got := BucketStart(at, "week", time.Monday); got != time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("UTC week=%v", got)
	}
	if got := BucketEnd(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), "month"); got != time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("month=%v", got)
	}
}
