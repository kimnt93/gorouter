package postgres

import (
	"context"
	"os"
	"testing"

	contract "github.com/kimnt93/gorouter/internal/integration"
	"github.com/kimnt93/gorouter/internal/platform/database"
)

func TestIdentityBackendContract(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := database.Connect(context.Background(), url)
	if err != nil {
		t.Skipf("test PostgreSQL unavailable: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	store := New(db.Pool)
	contract.RunIdentityBackendContract(t, contract.IdentityBackend{Identity: NewIdentityRepo(store), Keys: NewApiKeyRepo(store), Usage: NewUsageRepo(store), Audit: NewAuditRepo(store)})
}
