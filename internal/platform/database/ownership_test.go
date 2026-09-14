package database

import (
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"
)

func TestMigrationsDoNotGenerateIDsOrTimes(t *testing.T) {
	forbidden := regexp.MustCompile(`(?i)\b(bigserial|serial|smallserial)\b|default\s+(now\s*\(|current_timestamp)|\b(now|now64|gen_random_uuid|uuid_generate_v[0-9]+)\s*\(`)
	for _, source := range []struct {
		name, glob string
		files      fs.FS
	}{{"postgres", "migrations/*.sql", migrationsFS}, {"clickhouse", "clickhouse/*.sql", clickhouseMigrations}, {"sqlite", "sqlite/*.sql", sqliteMigrations}} {
		names, err := fs.Glob(source.files, source.glob)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range names {
			body, err := fs.ReadFile(source.files, name)
			if err != nil {
				t.Fatal(err)
			}
			if match := forbidden.Find(body); match != nil {
				t.Errorf("%s migration %s delegates identity/time generation to database: %q", source.name, name, match)
			}
		}
	}
}

// 0023 was historically used twice. New versions must not repeat that mistake;
// 0028 explicitly repairs installations where the workload migration was skipped.
func TestNewMigrationVersionsAreUnique(t *testing.T) {
	for _, source := range []struct {
		glob  string
		files fs.FS
	}{{"migrations/*.sql", migrationsFS}, {"clickhouse/*.sql", clickhouseMigrations}, {"sqlite/*.sql", sqliteMigrations}} {
		names, err := fs.Glob(source.files, source.glob)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]string{}
		for _, name := range names {
			base := path.Base(name)
			version := strings.SplitN(base, "_", 2)[0]
			if previous, exists := seen[version]; exists && !(version == "0023" && source.glob == "migrations/*.sql") {
				t.Errorf("duplicate migration version: %s and %s", previous, name)
			}
			seen[version] = name
		}
	}
}
