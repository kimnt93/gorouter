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
