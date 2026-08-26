package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/platform/database"
)

func TestTenantUsageQueriesAreIsolated(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pg, err := database.Connect(ctx, url)
	if err != nil {
		t.Skipf("test PostgreSQL unavailable: %v", err)
	}
	defer pg.Close()
	if err := pg.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	pool := pg.Pool
	for _, table := range []string{"tenants", "credentials", "api_keys", "models", "model_routes", "prices", "usage_events"} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil || !exists {
			t.Fatalf("migration table %s: exists=%v err=%v", table, exists, err)
		}
	}
	id := fmt.Sprintf("usage-isolation-%d", time.Now().UnixNano())
	keyA, keyB := id+"-a", id+"-b"
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM usage_events WHERE api_key_id IN ($1,$2)`, keyA, keyB)
	})
	now := time.Now().UTC()
	_, err = pool.Exec(ctx, `INSERT INTO usage_events
		(event_id,ts,tenant_id,api_key_id,model,cost_usd,priced,actor_type,user_id,username,organization_id) VALUES
		($1,$2,$3,$4,'model-a',1,true,'legacy','','legacy',$3),($5,$6,$7,$8,'model-b',2,true,'legacy','','legacy',$7)`,
		entities.NewID("usage"), now, id+"-tenant-a", keyA,
		entities.NewID("usage"), now, id+"-tenant-b", keyB)
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
	if len(recent) != 1 || recent[0].ID == "" || recent[0].TS.IsZero() || recent[0].TenantID != id+"-tenant-a" || recent[0].KeyID != keyA {
		t.Fatalf("tenant recent usage leaked: %+v", recent)
	}
}
