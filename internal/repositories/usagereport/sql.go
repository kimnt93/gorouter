// Package usagereport builds the bounded, content-free aggregate projection.
// Each backend supplies its own independently authorized WHERE and parameters.
package usagereport

import (
	"fmt"
	"strings"

	"github.com/kimnt93/gorouter/pkg/entities"
)

func SQL(dialect, where string, q entities.UsageReportQuery) (string, error) {
	dim := func(value string) (string, bool) {
		switch value {
		case "":
			return "''", true
		case "agent":
			if dialect == "local" {
				return "agent_id", true
			}
			return "workload_agent_id", true
		case "user":
			return "user_id", true
		case "model":
			return "model", true
		}
		return "", false
	}
	group, ok := dim(q.GroupBy)
	if !ok {
		return "", entities.ErrUsageReportLimit
	}
	series, ok := dim(q.SeriesBy)
	if !ok {
		return "", entities.ErrUsageReportLimit
	}
	if q.WeekStart < 0 || q.WeekStart > 6 {
		return "", entities.ErrUsageReportLimit
	}
	if q.Bucket != "hour" && q.Bucket != "day" && q.Bucket != "week" && q.Bucket != "month" {
		return "", entities.ErrUsageReportLimit
	}
	col := "ts"
	if q.TimeBasis == "accounting" {
		col = "accounting_ts"
	}
	bucket := ""
	switch dialect {
	case "postgres":
		col = "(" + col + " AT TIME ZONE 'UTC')"
		bucket = "date_trunc('" + q.Bucket + "'," + col + ")"
		if q.Bucket == "week" {
			shift := (8 - int(q.WeekStart)) % 7
			bucket = fmt.Sprintf("date_trunc('week',%s + interval '%d days') - interval '%d days'", col, shift, shift)
		}
		bucket = "CAST(extract(epoch FROM (" + bucket + ")) AS bigint)"
	case "clickhouse":
		if q.TimeBasis == "accounting" {
			col = "coalesce(accounting_ts,ts)"
		}
		switch q.Bucket {
		case "hour":
			bucket = "toStartOfHour(" + col + ",'UTC')"
		case "day":
			bucket = "toStartOfDay(" + col + ",'UTC')"
		case "month":
			bucket = "toStartOfMonth(toTimeZone(" + col + ",'UTC'))"
		case "week":
			bucket = fmt.Sprintf("subtractDays(toStartOfDay(%s,'UTC'),modulo(toDayOfWeek(%s,0,'UTC') - %d + 7,7))", col, col, int(q.WeekStart))
		}
		bucket = "toInt64(toUnixTimestamp(" + bucket + "))"
	case "local":
		col = "event_time"
		if q.TimeBasis == "accounting" {
			col = "accounting_time"
		}
		format := "%Y-%m-%d 00:00:00"
		if q.Bucket == "hour" {
			format = "%Y-%m-%d %H:00:00"
		}
		if q.Bucket == "month" {
			format = "%Y-%m-01 00:00:00"
		}
		bucket = "strftime('" + format + "',substr(" + col + ",1,19))"
		if q.Bucket == "week" {
			bucket = fmt.Sprintf("datetime(%s, '-' || ((CAST(strftime('%%w',substr(%s,1,19)) AS INTEGER)-%d+7)%%7) || ' days')", bucket, col, int(q.WeekStart))
		}
		bucket = "CAST(strftime('%s'," + bucket + ") AS INTEGER)"
	default:
		return "", entities.ErrUsageReportLimit
	}
	count := "count(*)"
	if dialect == "clickhouse" {
		count = "toInt64(count())"
	}
	expr := []string{bucket + " AS bucket_start", group + " AS group_id", series + " AS series_id", count}
	// Router cache hits replay metadata, not new provider token consumption.
	for _, col := range []string{"prompt_tokens", "completion_tokens", "cache_read_tokens", "cache_write_tokens"} {
		expr = append(expr, "coalesce(sum(CASE WHEN cache_hit THEN 0 ELSE "+col+" END),0)")
	}
	for _, col := range []string{"cost_usd", "input_cost_usd", "output_cost_usd", "cache_read_cost_usd", "cache_write_cost_usd"} {
		expr = append(expr, "coalesce(sum("+col+"),0)")
	}
	for _, condition := range []string{"cache_hit", "NOT cache_hit AND usage_measurement NOT IN ('provider','measured','provider_reported_or_adapter_normalized')", "user_id=''"} {
		expression := "coalesce(sum(CASE WHEN " + condition + " THEN 1 ELSE 0 END),0)"
		if dialect == "clickhouse" {
			expression = "toInt64(" + expression + ")"
		}
		expr = append(expr, expression)
	}
	return "SELECT " + strings.Join(expr, ",") + " FROM usage_events WHERE " + where + " GROUP BY bucket_start,group_id,series_id ORDER BY bucket_start,group_id,series_id LIMIT 20001", nil
}

type Rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}

func Read(rows Rows) ([]entities.UsageReportCell, error) {
	out := []entities.UsageReportCell{}
	for rows.Next() {
		var c entities.UsageReportCell
		t := &c.Totals
		if err := rows.Scan(&c.StartUnix, &c.GroupID, &c.SeriesID, &t.Requests, &t.InputTokens, &t.OutputTokens, &t.CacheReadTokens, &t.CacheWriteTokens, &t.CostUSD, &t.InputCostUSD, &t.OutputCostUSD, &t.CacheReadCostUSD, &t.CacheWriteCostUSD, &t.ResponseCacheHits, &t.MeasurementUnknownRequests, &t.UnattributedRequests); err != nil {
			return nil, err
		}
		t.TotalTokens = t.InputTokens + t.OutputTokens + t.CacheReadTokens + t.CacheWriteTokens
		out = append(out, c)
		if len(out) > 20000 {
			return nil, entities.ErrUsageReportLimit
		}
	}
	return out, rows.Err()
}
