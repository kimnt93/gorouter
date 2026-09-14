package local

import (
	"context"

	"github.com/kimnt93/gorouter/internal/repositories/usagereport"
	"github.com/kimnt93/gorouter/pkg/entities"
)

func (r *UsageRepo) ReportUsage(ctx context.Context, q entities.UsageReportQuery) ([]entities.UsageReportCell, error) {
	where, args := localUsageFilter(q.UsageQuery)
	statement, err := usagereport.SQL("local", where, q)
	if err != nil {
		return nil, err
	}
	rows, err := r.s.DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return usagereport.Read(rows)
}
