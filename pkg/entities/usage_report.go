package entities

import (
	"context"
	"errors"
	"time"
)

var ErrUsageReportLimit = errors.New("usage report exceeds bounded range or cardinality")

// UsageReportQuery is already authorized. Dimensions are validated by the
// service and again by the SQL builder; none is interpolated from HTTP input.
type UsageReportQuery struct {
	UsageQuery
	GroupBy   string
	SeriesBy  string
	Bucket    string
	WeekStart time.Weekday
}

type UsageReportTotals struct {
	Requests                   int64   `json:"requests"`
	InputTokens                int64   `json:"input_tokens"`
	OutputTokens               int64   `json:"output_tokens"`
	CacheReadTokens            int64   `json:"cache_read_tokens"`
	CacheWriteTokens           int64   `json:"cache_write_tokens"`
	TotalTokens                int64   `json:"total_tokens"`
	CostUSD                    float64 `json:"cost_usd"`
	InputCostUSD               float64 `json:"input_cost_usd"`
	OutputCostUSD              float64 `json:"output_cost_usd"`
	CacheReadCostUSD           float64 `json:"cache_read_cost_usd"`
	CacheWriteCostUSD          float64 `json:"cache_write_cost_usd"`
	ResponseCacheHits          int64   `json:"response_cache_hits"`
	MeasurementUnknownRequests int64   `json:"measurement_unknown_requests"`
	UnattributedRequests       int64   `json:"unattributed_requests"`
}

func (t *UsageReportTotals) Add(v UsageReportTotals) {
	t.Requests += v.Requests
	t.InputTokens += v.InputTokens
	t.OutputTokens += v.OutputTokens
	t.CacheReadTokens += v.CacheReadTokens
	t.CacheWriteTokens += v.CacheWriteTokens
	t.TotalTokens = t.InputTokens + t.OutputTokens + t.CacheReadTokens + t.CacheWriteTokens
	t.CostUSD += v.CostUSD
	t.InputCostUSD += v.InputCostUSD
	t.OutputCostUSD += v.OutputCostUSD
	t.CacheReadCostUSD += v.CacheReadCostUSD
	t.CacheWriteCostUSD += v.CacheWriteCostUSD
	t.ResponseCacheHits += v.ResponseCacheHits
	t.MeasurementUnknownRequests += v.MeasurementUnknownRequests
	t.UnattributedRequests += v.UnattributedRequests
}

type UsageReportCell struct {
	StartUnix int64
	GroupID   string
	SeriesID  string
	Totals    UsageReportTotals
}
type UsageReportRepository interface {
	ReportUsage(context.Context, UsageReportQuery) ([]UsageReportCell, error)
}
type UsageReportScope struct {
	Kind           string `json:"kind"`
	UserID         string `json:"user_id,omitempty"`
	OrganizationID string `json:"organization_id,omitempty"`
}
type UsageReportRange struct {
	From         time.Time `json:"from"`
	To           time.Time `json:"to"`
	TimeBasis    string    `json:"time_basis"`
	Timezone     string    `json:"timezone"`
	WeekStartsOn string    `json:"week_starts_on"`
}
type UsageReportFreshness struct {
	State string `json:"state"`
	// No revision is promised until there is a durable acceptance watermark.
	Revision string `json:"revision,omitempty"`
}
type UsageReportCoverage struct {
	State                      string `json:"state"`
	UnattributedRequests       int64  `json:"unattributed_requests"`
	MeasurementUnknownRequests int64  `json:"measurement_unknown_requests"`
}
type UsageReportGroup struct {
	Dimension    string            `json:"dimension"`
	ID           string            `json:"id"`
	Unattributed bool              `json:"unattributed"`
	Totals       UsageReportTotals `json:"totals"`
}
type UsageReportSeries struct {
	Start        time.Time         `json:"start"`
	End          time.Time         `json:"end"`
	GroupID      string            `json:"group_id"`
	Unattributed bool              `json:"unattributed"`
	Totals       UsageReportTotals `json:"totals"`
}
type UsageReport struct {
	CapabilityVersion string               `json:"capability_version"`
	Scope             UsageReportScope     `json:"scope"`
	Range             UsageReportRange     `json:"range"`
	AsOf              time.Time            `json:"as_of"`
	Freshness         UsageReportFreshness `json:"freshness"`
	Coverage          UsageReportCoverage  `json:"coverage"`
	Totals            UsageReportTotals    `json:"totals"`
	Groups            []UsageReportGroup   `json:"groups"`
	Series            []UsageReportSeries  `json:"series"`
	Truncated         bool                 `json:"truncated"`
}
