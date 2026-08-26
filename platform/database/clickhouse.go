package database

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

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
	entries, err := fs.Glob(clickhouseMigrations, "clickhouse/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(entries)
	for _, name := range entries {
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
	}
	return nil
}
