package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTenantUsageQueriesAreIsolated(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("test PostgreSQL unavailable: %v", err)
	}
	id := fmt.Sprintf("usage-isolation-%d", time.Now().UnixNano())
	keyA, keyB := id+"-a", id+"-b"
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM usage_events WHERE api_key_id IN ($1,$2)`, keyA, keyB)
	})
	_, err = pool.Exec(ctx, `INSERT INTO usage_events
		(ts,tenant_id,api_key_id,model,cost_usd,priced) VALUES
		(now(),$1,$2,'model-a',1,true),(now(),$3,$4,'model-b',2,true)`, id+"-tenant-a", keyA, id+"-tenant-b", keyB)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewUsageRepo(&DB{Pool: pool})
	summary, err := repo.SummaryForTenant(ctx, id+"-tenant-a", time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if summary.Requests != 1 || summary.CostUSD != 1 || summary.ByModel["model-b"].Requests != 0 {
		t.Fatalf("tenant summary leaked another tenant: %+v", summary)
	}
	recent, err := repo.RecentForTenant(ctx, id+"-tenant-a", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 || recent[0].TenantID != id+"-tenant-a" || recent[0].KeyID != keyA {
		t.Fatalf("tenant recent usage leaked: %+v", recent)
	}
}
