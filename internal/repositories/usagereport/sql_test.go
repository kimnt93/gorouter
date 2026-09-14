package usagereport

import (
	"github.com/kimnt93/gorouter/pkg/entities"
	"strings"
	"testing"
	"time"
)

func TestReportSQLBoundedAndContentFree(t *testing.T) {
	for _, backend := range []string{"local", "postgres", "clickhouse"} {
		for _, bucket := range []string{"hour", "day", "week", "month"} {
			q := entities.UsageReportQuery{GroupBy: "agent", SeriesBy: "user", Bucket: bucket, WeekStart: time.Sunday}
			q.TimeBasis = "accounting"
			sql, err := SQL(backend, "authorized_predicate", q)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(sql, "LIMIT 20001") || !strings.Contains(sql, "authorized_predicate") || strings.Contains(sql, "conversation_enc") || strings.Contains(sql, "payload") || strings.Contains(sql, "JOIN") {
				t.Fatalf("unbounded/content-bearing SQL: %s", sql)
			}
		}
	}
	q := entities.UsageReportQuery{GroupBy: "agent); DROP TABLE usage_events; --", Bucket: "day"}
	if _, err := SQL("postgres", "TRUE", q); err == nil {
		t.Fatal("unvalidated SQL dimension")
	}
}
