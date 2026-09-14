package usage

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

var updateReportFixture = flag.Bool("update-report-fixture", false, "regenerate synthetic report contract fixture")

func TestReportContractFixture(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 2)
	totals := entities.UsageReportTotals{Requests: 2, InputTokens: 100, OutputTokens: 20, CacheReadTokens: 40, TotalTokens: 160, CostUSD: .02, InputCostUSD: .01, OutputCostUSD: .005, CacheReadCostUSD: .005}
	report := entities.UsageReport{CapabilityVersion: "gorouter-usage-report-v1", Scope: entities.UsageReportScope{Kind: "personal", UserID: "usr_fixture"}, Range: entities.UsageReportRange{From: from, To: to, TimeBasis: "accounting", Timezone: "UTC", WeekStartsOn: "sunday"}, AsOf: to, Freshness: entities.UsageReportFreshness{State: "stored_only"}, Coverage: entities.UsageReportCoverage{State: "unknown"}, Totals: totals, Groups: []entities.UsageReportGroup{{Dimension: "agent", ID: "alpha", Totals: totals}}, Series: []entities.UsageReportSeries{{Start: from, End: from.AddDate(0, 0, 1), GroupID: "alpha", Totals: totals}}}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	path := filepath.Join("..", "..", "docs", "accounting-v0.2.2", "report.fixture.json")
	if *updateReportFixture {
		if err = os.WriteFile(path, raw, 0644); err != nil {
			t.Fatal(err)
		}
	}
	existing, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(existing) {
		t.Fatal("report fixture drift: go test ./pkg/usage -run TestReportContractFixture -update-report-fixture")
	}
}
