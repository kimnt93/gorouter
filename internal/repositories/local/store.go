// Package local implements the complete durable repository set for the
// single-process local runtime. SQLite owns durable records; process memory is
// used only for coordination that Redis owns in distributed deployments.
package local

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"

	"github.com/kimnt93/gorouter/pkg/entities"
)

type Store struct {
	DB *sql.DB
	mu sync.Mutex
}

func New(db *sql.DB) *Store { return &Store{DB: db} }

func id(prefix string) string { return entities.NewID(prefix) }
func HashSecret(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func GenerateSecret() string {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		panic(err)
	}
	return "nr-" + hex.EncodeToString(value)
}

func (s *Store) mutate(_ context.Context, _ string, fn func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn()
}

func (s *Store) put(ctx context.Context, entity, key string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO config_records(entity,key,payload) VALUES(?,?,?)
		ON CONFLICT(entity,key) DO UPDATE SET payload=excluded.payload`, entity, key, payload)
	return err
}

func (s *Store) del(ctx context.Context, entity, key string) error {
	result, err := s.DB.ExecContext(ctx, `DELETE FROM config_records WHERE entity=? AND key=?`, entity, key)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return entities.ErrNotFound
	}
	return nil
}

func (s *Store) raw(ctx context.Context, entity, key string) ([]byte, error) {
	var payload []byte
	if err := s.DB.QueryRowContext(ctx, `SELECT payload FROM config_records WHERE entity=? AND key=?`, entity, key).Scan(&payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, entities.ErrNotFound
		}
		return nil, err
	}
	return payload, nil
}

func get[T any](ctx context.Context, store *Store, entity, key string) (*T, error) {
	payload, err := store.raw(ctx, entity, key)
	if err != nil {
		return nil, err
	}
	var value T
	if err := json.Unmarshal(payload, &value); err != nil {
		return nil, err
	}
	return &value, nil
}

func list[T any](ctx context.Context, store *Store, entity string) ([]T, error) {
	rows, err := store.DB.QueryContext(ctx, `SELECT payload FROM config_records WHERE entity=?`, entity)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]T, 0)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var value T
		if err := json.Unmarshal(payload, &value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
