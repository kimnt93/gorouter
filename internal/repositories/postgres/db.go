package postgres

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kimnt93/gorouter/pkg/entities"
)

type DB struct {
	Pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *DB { return &DB{Pool: pool} }

func NewID(prefix string) string {
	return entities.NewID(prefix)
}
