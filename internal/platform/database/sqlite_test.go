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
