package database

import (
	"io/fs"
	"regexp"
	"testing"
)

func TestMigrationsDoNotGenerateIDsOrTimes(t *testing.T) {
	forbidden := regexp.MustCompile(`(?i)\b(bigserial|serial|smallserial)\b|default\s+(now\s*\(|current_timestamp)|\b(now|now64|gen_random_uuid|uuid_generate_v[0-9]+)\s*\(`)
	for _, source := range []struct {
		name, glob string
		files      fs.FS
	}{{"postgres", "migrations/*.sql", migrationsFS}, {"clickhouse", "clickhouse/*.sql", clickhouseMigrations}} {
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
