package postgres

import (
	"context"
	"github.com/kimnt93/gorouter/pkg/entities"
	"os"
	"testing"
	"time"

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
	contract.RunUsageTrackingContract(t, NewUsageRepo(store))
	contract.RunOrganizationModelsContract(t, NewOrganizationModelRepo(store))
	contract.RunAliasUniquenessContract(t, NewOrganizationModelRepo(store))
	user := entities.User{ID: entities.NewID("usr"), Username: entities.NewID("person") + "@example.test", NormalizedUsername: entities.NewID("person") + "@example.test", Status: entities.StatusActive, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := NewIdentityRepo(store).CreateUser(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	contract.RunPrimaryKeyContract(t, NewApiKeyRepo(store), user.ID)

}
