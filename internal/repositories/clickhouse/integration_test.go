package clickhouse

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/kimnt93/gorouter/internal/platform/database"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/providerquota"
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
	event := entities.UsageEvent{TS: time.Now().UTC(), TenantID: tenant.ID, ApiKeyID: key.ID, CredentialID: cred.ID, Model: modelName, PromptTokens: 10, CompletionTokens: 5, CacheReadTokens: 4, CacheWriteTokens: 3, CostUSD: .25, InputCostUSD: .1, OutputCostUSD: .08, CacheReadCostUSD: .03, CacheWriteCostUSD: .04, Priced: true, StatusCode: 200}
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
	activity, err := usage.ActivityUsage(ctx, entities.UsageQuery{Visibility: entities.UsageVisibility{PrincipalType: entities.PrincipalMaster}, APIKeyID: key.ID}, "hour")
	if err != nil || len(activity) != 1 || activity[0].Requests != 1 || activity[0].PromptTokens != 10 || activity[0].CompletionTokens != 5 || activity[0].CacheReadTokens != 4 || activity[0].CacheWriteTokens != 3 || activity[0].CostUSD != .25 || activity[0].InputCostUSD != .1 || activity[0].OutputCostUSD != .08 || activity[0].CacheReadCostUSD != .03 || activity[0].CacheWriteCostUSD != .04 {
		t.Fatalf("usage activity=%+v err=%v", activity, err)
	}
	quotaRepo := NewProviderQuotaRepo(s)
	quotaProvider := "integration-" + suffix
	quotaA := providerquota.Snapshot{CredentialID: cred.ID, Provider: quotaProvider, Account: "account-a", Available: true, Windows: []providerquota.Window{{Name: "Session", RemainingPercent: 75}}}
	quotaB := providerquota.Snapshot{CredentialID: "quota-" + suffix, Provider: quotaProvider, Account: "account-b", Available: true, Windows: []providerquota.Window{}}
	if err := quotaRepo.Save(ctx, quotaA); err != nil {
		t.Fatal(err)
	}
	if err := quotaRepo.Save(ctx, quotaB); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = s.del(context.Background(), "provider_quota", quotaA.CredentialID)
		_ = s.del(context.Background(), "provider_quota", quotaB.CredentialID)
	})
	if err := quotaRepo.SetInUse(ctx, quotaB.CredentialID, quotaProvider); err != nil {
		t.Fatal(err)
	}
	snapshots, err := quotaRepo.LoadAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	foundA, foundB := false, false
	for _, snapshot := range snapshots {
		switch snapshot.CredentialID {
		case quotaA.CredentialID:
			foundA = !snapshot.InUse && len(snapshot.Windows) == 1 && snapshot.Windows[0].RemainingPercent == 75
		case quotaB.CredentialID:
			foundB = snapshot.InUse
		}
	}
	if !foundA || !foundB {
		t.Fatalf("provider quota round trip failed: a=%v b=%v snapshots=%+v", foundA, foundB, snapshots)
	}
}
