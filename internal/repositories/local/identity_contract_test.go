package local

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/kimnt93/gorouter/internal/integration"
	"github.com/kimnt93/gorouter/internal/platform/database"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/seal"
)

func TestLocalIdentityBackendContract(t *testing.T) {
	ctx := context.Background()
	db, err := database.ConnectSQLite(ctx, t.TempDir()+"/gorouter.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	store := New(db.DB)
	integration.RunIdentityBackendContract(t, integration.IdentityBackend{
		Identity: NewIdentityRepo(store), Keys: NewApiKeyRepo(store),
		Usage: NewUsageRepo(store), Audit: NewAuditRepo(store),
	})
}

func TestLocalGlobalCredentialRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := database.ConnectSQLite(ctx, t.TempDir()+"/gorouter.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	box, err := seal.New("local-global-credential-test-key")
	if err != nil {
		t.Fatal(err)
	}
	repo := NewCredentialRepo(New(db.DB))
	created, err := repo.Create(ctx, entities.CredentialInput{
		Name: "global", Provider: "openai-compatible", Kind: entities.KindAPIKey,
		BaseURL: "https://example.invalid/v1", APIKey: "synthetic-secret",
	}, box)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := repo.Runtime(ctx, box, created.ID)
	if err != nil || runtime.APIKey != "synthetic-secret" {
		t.Fatalf("runtime=%+v err=%v", runtime, err)
	}
	credentials, err := repo.List(ctx)
	if err != nil || len(credentials) != 1 || credentials[0].OwnerUserID != "" || credentials[0].OwnerTenantID != nil {
		t.Fatalf("global credential ownership=%+v err=%v", credentials, err)
	}
}

func TestLocalUsagePersistsEncryptedConversationColumns(t *testing.T) {
	ctx := context.Background()
	db, err := database.ConnectSQLite(ctx, t.TempDir()+"/gorouter.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo := NewUsageRepo(New(db.DB))
	event := entities.UsageEvent{ID: "usage-conversation", TS: time.Now().UTC(), ActorType: entities.ActorMaster, ConversationEnc: []byte("ciphertext"), ContentTruncated: true}
	if err := repo.InsertBatch(ctx, []entities.UsageEvent{event}); err != nil {
		t.Fatal(err)
	}
	detail, err := repo.UsageDetail(ctx, event.ID, entities.UsageVisibility{PrincipalType: entities.PrincipalMaster})
	if err != nil || string(detail.ConversationEncrypted) != "ciphertext" || !detail.ContentTruncated {
		t.Fatalf("detail=%+v err=%v", detail, err)
	}
	var payload []byte
	if err := db.DB.QueryRowContext(ctx, `SELECT payload FROM usage_events WHERE id=?`, event.ID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(payload, []byte("ciphertext")) {
		t.Fatal("usage JSON payload contains encrypted conversation column")
	}
}

func TestLocalAgentUsageAggregateIsIsolatedAndHalfOpen(t *testing.T) {
	ctx := context.Background()
	db, err := database.ConnectSQLite(ctx, t.TempDir()+"/agent-usage.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo := NewUsageRepo(New(db.DB))
	start := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	events := []entities.UsageEvent{
		{ID: "usage-a", TS: start.Add(time.Hour), AccountingTS: start.Add(time.Hour), UserID: "user-1", Application: "xnobrain", WorkspaceID: "workspace-1", AgentID: "agent-1", CostUSD: 1, PromptTokens: 10, Priced: true},
		{ID: "usage-b", TS: start.Add(2 * time.Hour), AccountingTS: start.Add(2 * time.Hour), UserID: "user-1", Application: "xnobrain", WorkspaceID: "workspace-2", AgentID: "agent-1", CostUSD: 20, Priced: true},
		{ID: "usage-end", TS: start.AddDate(0, 0, 7), AccountingTS: start.AddDate(0, 0, 7), UserID: "user-1", Application: "xnobrain", WorkspaceID: "workspace-1", AgentID: "agent-1", CostUSD: 30, Priced: true},
	}
	if err = repo.InsertBatch(ctx, events); err != nil {
		t.Fatal(err)
	}
	end := start.AddDate(0, 0, 7)
	summary, err := repo.AgentUsageAggregate(ctx, entities.UsageQuery{Visibility: entities.UsageVisibility{PrincipalType: entities.PrincipalUser, UserID: "user-1"}, Since: &start, Until: &end, Application: "xnobrain", WorkspaceID: "workspace-1", AgentID: "agent-1"})
	if err != nil || summary.Requests != 1 || summary.CostUSD != 1 || summary.PromptTok != 10 {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
	if err = repo.InsertBatch(ctx, events[:1]); err != nil {
		t.Fatal(err)
	}
	summary, err = repo.AgentUsageAggregate(ctx, entities.UsageQuery{Visibility: entities.UsageVisibility{PrincipalType: entities.PrincipalUser, UserID: "user-1"}, Since: &start, Until: &end, Application: "xnobrain", WorkspaceID: "workspace-1", AgentID: "agent-1"})
	if err != nil || summary.Requests != 1 {
		t.Fatalf("idempotent summary=%+v err=%v", summary, err)
	}
}
