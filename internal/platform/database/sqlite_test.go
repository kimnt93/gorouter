package database

import (
	"context"
	"os"
	"testing"
)

func TestSQLiteMigratesAndPersists(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/nested/gorouter.db"
	db, err := ConnectSQLite(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.ExecContext(ctx, `INSERT INTO config_records(entity,key,payload) VALUES('test','one','{}')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	db, err = ConnectSQLite(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.DB.QueryRowContext(ctx, `SELECT count(*) FROM config_records WHERE entity='test'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
}

func TestSQLiteUsageConversationMigration(t *testing.T) {
	ctx := context.Background()
	db, err := ConnectSQLite(ctx, t.TempDir()+"/gorouter.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.ExecContext(ctx, `INSERT INTO usage_events(id,ts,payload,conversation_enc,content_truncated) VALUES(?,?,?,?,?)`, "usage-1", "2026-09-04T00:00:00Z", []byte(`{}`), []byte("encrypted"), true); err != nil {
		t.Fatal(err)
	}
	var encrypted []byte
	var truncated bool
	if err := db.DB.QueryRowContext(ctx, `SELECT conversation_enc,content_truncated FROM usage_events WHERE id=?`, "usage-1").Scan(&encrypted, &truncated); err != nil || string(encrypted) != "encrypted" || !truncated {
		t.Fatalf("conversation=%q truncated=%v err=%v", encrypted, truncated, err)
	}
}

// Upgrade real v0.2.1 SQLite shape without rewriting money/content or losing
// subsecond ordering. Migrate is rerunnable against the resulting file/WAL.
func TestSQLiteAccountingReadUpgrade(t *testing.T) {
	ctx := context.Background()
	db, err := ConnectSQLite(ctx, t.TempDir()+"/upgrade.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.DB.ExecContext(ctx, `CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"001_primary_store.sql", "002_usage_conversation_encryption.sql", "003_workload_usage_accounting.sql"} {
		raw, err := sqliteMigrations.ReadFile("sqlite/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.DB.ExecContext(ctx, string(raw)); err != nil {
			t.Fatal(err)
		}
		if _, err = db.DB.ExecContext(ctx, `INSERT INTO schema_migrations VALUES(?,?)`, i+1, "2026-09-01T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	for i, ts := range []string{"2026-09-01T00:00:00Z", "2026-09-01T00:00:00.000000001Z", "2026-09-01T00:00:00.1Z"} {
		if _, err = db.DB.ExecContext(ctx, `INSERT INTO usage_events(id,ts,payload,conversation_enc) VALUES(?,?,?,?)`, i, ts, `{"user_id":"u","agent_id":"a","prompt_tokens":10,"completion_tokens":2,"cache_read_tokens":3,"cache_write_tokens":4,"cost_usd":1,"accounting_ts":"0001-01-01T00:00:00Z"}`, []byte("synthetic ciphertext")); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var requests, tokens int
	var cost float64
	if err = db.DB.QueryRowContext(ctx, `SELECT count(*),sum(prompt_tokens+completion_tokens+cache_read_tokens+cache_write_tokens),sum(cost_usd) FROM usage_events`).Scan(&requests, &tokens, &cost); err != nil || requests != 3 || tokens != 57 || cost != 3 {
		t.Fatalf("reconciliation=%d,%d,%v err=%v", requests, tokens, cost, err)
	}
	var first, last string
	if err = db.DB.QueryRowContext(ctx, `SELECT min(event_time),max(accounting_time) FROM usage_events`).Scan(&first, &last); err != nil || first != "2026-09-01T00:00:00.000000000Z" || last != "2026-09-01T00:00:00.100000000Z" {
		t.Fatalf("bounds=%s,%s err=%v", first, last, err)
	}
	var count int
	if err = db.DB.QueryRowContext(ctx, `SELECT count(*) FROM usage_events WHERE event_time>=? AND event_time<?`, "2026-09-01T00:00:00.000000000Z", "2026-09-01T00:00:00.000000001Z").Scan(&count); err != nil || count != 1 {
		t.Fatalf("nanosecond boundary count=%d err=%v", count, err)
	}
	if err = db.DB.QueryRowContext(ctx, `SELECT count(*) FROM usage_events WHERE conversation_enc=?`, []byte("synthetic ciphertext")).Scan(&count); err != nil || count != 3 {
		t.Fatal("content changed during upgrade")
	}
}
