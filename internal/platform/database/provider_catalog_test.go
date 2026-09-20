package database

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/kimnt93/gorouter/pkg/provider"
)

// Stop future provider additions from shipping with an obsolete PostgreSQL
// CHECK constraint. SQLite and ClickHouse config records have no SQL provider
// allowlist; all three backends share the service-level catalog validation.
func TestLatestPostgresCredentialProviderConstraintMatchesCatalog(t *testing.T) {
	raw, err := migrationsFS.ReadFile("migrations/0034_devin_cloud_provider.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	match := regexp.MustCompile(`(?s)provider IN \((.*?)\)`).FindStringSubmatch(sql)
	if len(match) != 2 {
		t.Fatal("provider constraint is missing")
	}
	quoted := regexp.MustCompile(`'([^']+)'`).FindAllStringSubmatch(match[1], -1)
	got := make(map[string]bool, len(quoted))
	for _, value := range quoted {
		if got[value[1]] {
			t.Errorf("duplicate provider %q", value[1])
		}
		got[value[1]] = true
	}
	for _, definition := range provider.Catalog() {
		if !got[definition.ID] {
			t.Errorf("database rejects catalog provider %q", definition.ID)
		}
		delete(got, definition.ID)
	}
	for obsolete := range got {
		t.Errorf("database accepts provider outside the catalog: %q", obsolete)
	}
	if !strings.Contains(sql, "NOT VALID") {
		t.Error("upgrade should not revalidate all historical credentials")
	}
}
func TestSQLiteDevinCredentialNoProviderConstraint(t *testing.T) {
	ctx := context.Background()
	db, err := ConnectSQLite(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	_, err = db.DB.ExecContext(ctx, `INSERT INTO config_records(entity,key,payload) VALUES('credential','synthetic',?)`, `{"provider":"devin-desktop","kind":"api_key"}`)
	if err != nil {
		t.Fatal(err)
	}
}
