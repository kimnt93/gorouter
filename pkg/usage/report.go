package usage

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

func BucketStart(at time.Time, bucket string, week time.Weekday) time.Time {
	u := at.UTC()
	day := time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
	switch bucket {
	case "hour":
		return u.Truncate(time.Hour)
	case "week":
		return day.AddDate(0, 0, -(int(day.Weekday())-int(week)+7)%7)
	case "month":
		return time.Date(u.Year(), u.Month(), 1, 0, 0, 0, 0, time.UTC)
	}
	return day
}
func BucketEnd(start time.Time, bucket string) time.Time {
	switch bucket {
	case "hour":
		return start.Add(time.Hour)
	case "week":
		return start.AddDate(0, 0, 7)
	case "month":
		return start.AddDate(0, 1, 0)
	}
	return start.AddDate(0, 0, 1)
}

func (s *Service) Report(ctx context.Context, q entities.UsageReportQuery, scope entities.UsageReportScope) (*entities.UsageReport, error) {
	validDim := func(d string) bool { return d == "" || d == "agent" || d == "model" || d == "user" }
	if !validDim(q.GroupBy) || !validDim(q.SeriesBy) || (q.Bucket != "hour" && q.Bucket != "day" && q.Bucket != "week" && q.Bucket != "month") || q.Since == nil || q.Until == nil || !q.Since.Before(*q.Until) || q.Until.Sub(*q.Since) > 366*5*24*time.Hour || (q.TimeBasis != "accounting" && q.TimeBasis != "completion") || q.WeekStart < 0 || q.WeekStart > 6 {
		return nil, entities.ErrUsageReportLimit
	}
	buckets := 0
	for at := BucketStart(*q.Since, q.Bucket, q.WeekStart); at.Before(*q.Until); at = BucketEnd(at, q.Bucket) {
		buckets++
		if buckets > 2000 {
			return nil, entities.ErrUsageReportLimit
		}
	}
	repo, ok := s.repo.(entities.UsageReportRepository)
	if !ok {
		return nil, errors.New("usage report unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// AsOf is query-start observation time, not a durable-acceptance watermark.
	asOf := time.Now().UTC()
	cells, err := repo.ReportUsage(ctx, q)
	if err != nil {
		return nil, err
	}
	out := &entities.UsageReport{CapabilityVersion: "gorouter-usage-report-v1", Scope: scope, Range: entities.UsageReportRange{From: q.Since.UTC(), To: q.Until.UTC(), TimeBasis: q.TimeBasis, Timezone: "UTC", WeekStartsOn: strings.ToLower(q.WeekStart.String())}, AsOf: asOf, Freshness: entities.UsageReportFreshness{State: "stored_only"}, Coverage: entities.UsageReportCoverage{State: "unknown"}, Groups: []entities.UsageReportGroup{}, Series: []entities.UsageReportSeries{}}
	groups := map[string]entities.UsageReportTotals{}
	type seriesKey struct {
		Start int64
		ID    string
	}
	series := map[seriesKey]entities.UsageReportTotals{}
	seriesIDs := map[string]bool{}
	for _, c := range cells {
		out.Totals.Add(c.Totals)
		if q.GroupBy != "" {
			v := groups[c.GroupID]
			v.Add(c.Totals)
			groups[c.GroupID] = v
		}
		k := seriesKey{c.StartUnix, c.SeriesID}
		v := series[k]
		v.Add(c.Totals)
		series[k] = v
		seriesIDs[c.SeriesID] = true
		if len(groups) > 100 || len(seriesIDs) > 100 || len(series) > 20000 || len(seriesIDs)*buckets > 20000 {
			return nil, entities.ErrUsageReportLimit
		}
	}
	for id, v := range groups {
		out.Groups = append(out.Groups, entities.UsageReportGroup{Dimension: q.GroupBy, ID: id, Unattributed: id == "", Totals: v})
	}
	sort.Slice(out.Groups, func(i, j int) bool { return out.Groups[i].ID < out.Groups[j].ID })
	for k, v := range series {
		start := time.Unix(k.Start, 0).UTC()
		end := BucketEnd(start, q.Bucket)
		if start.Before(*q.Since) {
			start = q.Since.UTC()
		}
		if end.After(*q.Until) {
			end = q.Until.UTC()
		}
		out.Series = append(out.Series, entities.UsageReportSeries{Start: start, End: end, GroupID: k.ID, Unattributed: q.SeriesBy != "" && k.ID == "", Totals: v})
	}
	sort.Slice(out.Series, func(i, j int) bool {
		a, b := out.Series[i], out.Series[j]
		return a.Start.Before(b.Start) || a.Start.Equal(b.Start) && a.GroupID < b.GroupID
	})
	out.Coverage.UnattributedRequests = out.Totals.UnattributedRequests
	out.Coverage.MeasurementUnknownRequests = out.Totals.MeasurementUnknownRequests
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	if len(encoded) > 16<<20 {
		return nil, entities.ErrUsageReportLimit
	}
	return out, nil
}

func (s *Service) SupportsReport() bool { _, ok := s.repo.(entities.UsageReportRepository); return ok }
