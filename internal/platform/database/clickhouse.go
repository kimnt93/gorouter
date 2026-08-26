package database

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
)

//go:embed clickhouse/*.sql
var clickhouseMigrations embed.FS

type ClickHouse struct{ Conn ch.Conn }

func ConnectClickHouse(ctx context.Context, dsn string) (*ClickHouse, error) {
	opts, err := ch.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse ClickHouse configuration: %w", err)
	}
	conn, err := ch.Open(opts)
	if err != nil {
		return nil, err
	}
	if err := conn.Ping(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ping ClickHouse: %w", err)
	}
	return &ClickHouse{Conn: conn}, nil
}

func (c *ClickHouse) Close() { _ = c.Conn.Close() }

func (c *ClickHouse) Migrate(ctx context.Context) error {
	if err := c.Conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version UInt32, applied_at DateTime64(3,'UTC')) ENGINE=ReplacingMergeTree(applied_at) ORDER BY version`); err != nil {
		return err
	}
	entries, err := fs.Glob(clickhouseMigrations, "clickhouse/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(entries)
	for _, name := range entries {
		var version uint32
		if _, err := fmt.Sscanf(name, "clickhouse/%d_", &version); err != nil {
			continue
		}
		var applied uint64
		if err := c.Conn.QueryRow(ctx, `SELECT count() FROM schema_migrations WHERE version=?`, version).Scan(&applied); err != nil {
			return err
		}
		if applied > 0 {
			continue
		}
		b, err := clickhouseMigrations.ReadFile(name)
		if err != nil {
			return err
		}
		for _, statement := range strings.Split(string(b), ";") {
			if strings.TrimSpace(statement) == "" {
				continue
			}
			if err := c.Conn.Exec(ctx, statement); err != nil {
				return fmt.Errorf("migration %s: %w", name, err)
			}
		}
		if err := c.Conn.Exec(ctx, `INSERT INTO schema_migrations (version,applied_at) VALUES (?,?)`, version, time.Now().UTC()); err != nil {
			return err
		}
	}
	return nil
}
