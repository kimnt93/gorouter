package postgres

import (
	"context"

	"github.com/kimnt93/gorouter/internal/repositories/usagereport"
	"github.com/kimnt93/gorouter/pkg/entities"
)

func (r *UsageRepo) ReportUsage(ctx context.Context, q entities.UsageReportQuery) ([]entities.UsageReportCell, error) {
	where, args := postgresUsageFilter(q.UsageQuery)
	statement, err := usagereport.SQL("postgres", where, q)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.Pool.Query(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return usagereport.Read(rows)
}
