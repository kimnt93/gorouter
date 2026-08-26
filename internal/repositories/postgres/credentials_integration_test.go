package postgres

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/seal"
	"github.com/kimnt93/gorouter/platform/database"
)

func TestOAuthCredentialMetadataIsEncryptedAndPreservedAcrossRefresh(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	schema := fmt.Sprintf("oauth_credential_test_%d", time.Now().UnixNano())
	admin, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Skipf("test PostgreSQL unavailable: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		admin.Close(ctx)
		t.Fatal(err)
	}
	admin.Close(ctx)
	t.Cleanup(func() {
		cleanup, cleanupErr := pgx.Connect(context.Background(), databaseURL)
		if cleanupErr != nil {
			return
		}
		_, _ = cleanup.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		cleanup.Close(context.Background())
	})

	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	db, err := database.Connect(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	box, err := seal.New("oauth-integration-encryption-key")
	if err != nil {
		t.Fatal(err)
	}
	repo := NewCredentialRepo(New(db.Pool))
	created, err := repo.Create(ctx, entities.CredentialInput{
		Name:         "Claude subscription",
		Provider:     "claude",
		Kind:         entities.KindOAuth,
		OAuthAccess:  "initial-access-secret",
		OAuthRefresh: "initial-refresh-secret",
		OAuthIDToken: "identity-token-secret",
		OAuthAccount: "account-a",
		OAuthMeta: entities.OAuthMetadata{
			AccountID: "account-a", OrganizationID: "organization-a", DeviceID: "device-a",
		},
	}, box)
	if err != nil {
		t.Fatal(err)
	}

	var ciphertext []byte
	if err := db.Pool.QueryRow(ctx, `SELECT oauth_blob_enc FROM credentials WHERE id=$1`, created.ID).Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	for _, plaintext := range []string{"initial-access-secret", "initial-refresh-secret", "identity-token-secret", "account-a", "organization-a", "device-a"} {
		if bytes.Contains(ciphertext, []byte(plaintext)) {
			t.Fatalf("oauth_blob_enc contains plaintext %q", plaintext)
		}
	}

	before, err := repo.Runtime(ctx, box, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.OAuthAccess != "initial-access-secret" || before.OAuthRefreh != "initial-refresh-secret" || before.OAuthIDToken != "identity-token-secret" || before.OAuthAccount != "account-a" {
		t.Fatalf("initial OAuth runtime fields = %+v", before)
	}
	if before.OAuthMeta.AccountID != "account-a" || before.OAuthMeta.OrganizationID != "organization-a" || before.OAuthMeta.DeviceID != "device-a" {
		t.Fatalf("initial OAuth metadata = %+v", before.OAuthMeta)
	}

	if err := repo.UpdateOAuthTokens(ctx, box, created.ID, "refreshed-access-secret", "refreshed-refresh-secret"); err != nil {
		t.Fatal(err)
	}
	after, err := repo.Runtime(ctx, box, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.OAuthAccess != "refreshed-access-secret" || after.OAuthRefreh != "refreshed-refresh-secret" {
		t.Fatalf("refreshed OAuth tokens = access %q refresh %q", after.OAuthAccess, after.OAuthRefreh)
	}
	if after.OAuthIDToken != before.OAuthIDToken || after.OAuthAccount != before.OAuthAccount || after.OAuthMeta != before.OAuthMeta {
		t.Fatalf("refresh discarded OAuth identity metadata: before=%+v after=%+v", before, after)
	}
}
