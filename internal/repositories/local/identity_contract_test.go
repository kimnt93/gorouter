package local

import (
	"context"
	"testing"

	"github.com/kimnt93/gorouter/internal/integration"
	"github.com/kimnt93/gorouter/internal/platform/database"
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
