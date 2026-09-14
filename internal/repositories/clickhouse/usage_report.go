package clickhouse

import (
	"context"
	"github.com/kimnt93/gorouter/internal/repositories/usagereport"
	"github.com/kimnt93/gorouter/pkg/entities"
	"strings"
)

func (r *UsageRepo) ReportUsage(ctx context.Context, q entities.UsageReportQuery) ([]entities.UsageReportCell, error) {
	clauses, args := usageWhere(q.UsageQuery, false)
	where := strings.Join(clauses, " AND ")
	statement, err := usagereport.SQL("clickhouse", where, q)
	if err != nil {
		return nil, err
	}
	rows, err := r.s.Conn.Query(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return usagereport.Read(rows)
}
