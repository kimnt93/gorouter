package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/kimnt93/gorouter/internal/platform/database"
	"github.com/kimnt93/gorouter/pkg/entities"
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
		(event_id,ts,tenant_id,api_key_id,model,prompt_tokens,completion_tokens,cache_read_tokens,cache_write_tokens,cost_usd,input_cost_usd,output_cost_usd,cache_read_cost_usd,cache_write_cost_usd,priced,actor_type,user_id,username,organization_id) VALUES
		($1,$2,$3,$4,'model-a',10,5,4,3,1,.4,.3,.1,.2,true,'legacy','','legacy',$3),($5,$6,$7,$8,'model-b',20,8,6,4,2,.8,.6,.2,.4,true,'legacy','','legacy',$7)`,
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
	if summary.Requests != 1 || summary.CostUSD != 1 || summary.CacheReadTok != 4 || summary.CacheWriteTok != 3 || summary.ByModel["model-b"].Requests != 0 {
		t.Fatalf("tenant summary leaked another tenant: %+v", summary)
	}
	recent, err := repo.RecentForTenant(ctx, id+"-tenant-a", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 || recent[0].ID == "" || recent[0].TS.IsZero() || recent[0].TenantID != id+"-tenant-a" || recent[0].KeyID != keyA {
		t.Fatalf("tenant recent usage leaked: %+v", recent)
	}
	activity, err := repo.ActivityUsage(ctx, entities.UsageQuery{Visibility: entities.UsageVisibility{PrincipalType: entities.PrincipalMaster}, APIKeyID: keyA}, "hour")
	if err != nil {
		t.Fatal(err)
	}
	if len(activity) != 1 || activity[0].Requests != 1 || activity[0].PromptTokens != 10 || activity[0].CompletionTokens != 5 || activity[0].CacheReadTokens != 4 || activity[0].CacheWriteTokens != 3 || activity[0].CostUSD != 1 || activity[0].InputCostUSD != .4 || activity[0].OutputCostUSD != .3 || activity[0].CacheReadCostUSD != .1 || activity[0].CacheWriteCostUSD != .2 {
		t.Fatalf("activity aggregation=%+v", activity)
	}
}
