package clickhouse

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/kimnt93/gorouter/internal/platform/database"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/seal"
)

func TestPrimaryStoreRoundTrip(t *testing.T) {
	dsn := os.Getenv("TEST_CLICKHOUSE_URL")
	if dsn == "" {
		t.Skip("TEST_CLICKHOUSE_URL is not set")
	}
	ctx := context.Background()
	db, err := database.ConnectClickHouse(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	s := New(db.Conn)
	suffix := time.Now().Format("150405.000000000")
	tenantRepo := NewTenantRepo(s)
	tenant, err := tenantRepo.Create(ctx, "integration-"+suffix)
	if err != nil {
		t.Fatal(err)
	}
	keyRepo := NewApiKeyRepo(s)
	limit := 3.5
	key, err := keyRepo.CreateWithQuota(ctx, tenant.ID, "key", []string{"model-" + suffix}, []string{"chat"}, &limit, entities.QuotaPeriodWeek, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = keyRepo.Delete(context.Background(), key.ID) })
	loaded, err := keyRepo.GetBySecret(ctx, key.SecretHash)
	if err != nil || loaded.ID != key.ID || loaded.QuotaPeriod != entities.QuotaPeriodWeek {
		t.Fatalf("API key round trip=%+v err=%v", loaded, err)
	}
	box, err := seal.New("clickhouse-integration-secret-key")
	if err != nil {
		t.Fatal(err)
	}
	credRepo := NewCredentialRepo(s)
	cred, err := credRepo.Create(ctx, entities.CredentialInput{Name: "credential", Provider: "openai-compatible", Kind: entities.KindAPIKey, BaseURL: "https://example.invalid/v1", APIKey: "encrypted-secret", OwnerTenant: &tenant.ID}, box)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = credRepo.Delete(context.Background(), cred.ID) })
	runtime, err := credRepo.Runtime(ctx, box, cred.ID)
	if err != nil || runtime.APIKey != "encrypted-secret" {
		t.Fatalf("credential runtime=%+v err=%v", runtime, err)
	}
	modelName := "model-" + suffix
	models := NewModelRouteRepo(s)
	err = models.Upsert(ctx, entities.ModelDef{Name: modelName, Enabled: true, Routes: []entities.ModelRoute{{CredentialID: cred.ID, Priority: 2, Weight: 1, Enabled: true}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = models.Delete(context.Background(), modelName) })
	routes, err := credRepo.RoutesForModel(ctx, modelName)
	if err != nil || len(routes) != 1 || routes[0].CredentialID != cred.ID {
		t.Fatalf("routes=%+v err=%v", routes, err)
	}
	if err = models.SetPrice(ctx, modelName, entities.Price{InputPerM: 1, OutputPerM: 2}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = models.DeletePrice(context.Background(), modelName) })
	prices, err := models.ListPrices(ctx)
	if err != nil || prices[modelName].OutputPerM != 2 {
		t.Fatalf("prices=%+v err=%v", prices, err)
	}
	usage := NewUsageRepo(s)
	event := entities.UsageEvent{TS: time.Now().UTC(), TenantID: tenant.ID, ApiKeyID: key.ID, CredentialID: cred.ID, Model: modelName, CostUSD: .25, Priced: true, StatusCode: 200}
	if err = usage.InsertBatch(ctx, []entities.UsageEvent{event}); err != nil {
		t.Fatal(err)
	}
	spent, err := usage.SpendForKeySince(ctx, key.ID, event.TS.Add(-time.Second))
	if err != nil || spent != .25 {
		t.Fatalf("spend=%v err=%v", spent, err)
	}
	recent, err := usage.RecentForTenant(ctx, tenant.ID, 10)
	if err != nil || len(recent) != 1 || recent[0].ID == "" || recent[0].TS.IsZero() {
		t.Fatalf("usage identity/time=%+v err=%v", recent, err)
	}
}
