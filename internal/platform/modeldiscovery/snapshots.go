package modeldiscovery

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Snapshots holds bounded non-secret provider catalog mappings. Keys supplied by
// adapters are credential/revision digests, never raw API keys or user input.
type Snapshots struct{ client redis.UniversalClient }

func NewSnapshots(client redis.UniversalClient) *Snapshots { return &Snapshots{client: client} }
func snapshotKey(key string) string                        { return "gorouter:provider-catalog:v1:" + key }
func (s *Snapshots) GetSnapshot(ctx context.Context, key string) ([]byte, bool, error) {
	b, err := s.client.Get(ctx, snapshotKey(key)).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if len(b) > maxCatalogBytes {
		return nil, false, nil
	}
	return b, true, nil
}
func (s *Snapshots) SetSnapshot(ctx context.Context, key string, b []byte, ttl time.Duration) error {
	if len(b) > maxCatalogBytes {
		return nil
	}
	return s.client.Set(ctx, snapshotKey(key), b, ttl).Err()
}
func (s *Snapshots) DeleteSnapshot(ctx context.Context, key string) error {
	return s.client.Del(ctx, snapshotKey(key)).Err()
}

type memorySnapshot struct {
	data    []byte
	expires time.Time
}

// MemorySnapshots is only for explicit local mode. Production uses Redis or
// fresh upstream discovery on a cache outage, never an additional local copy.
type MemorySnapshots struct {
	mu      sync.Mutex
	entries map[string]memorySnapshot
	max     int
}

func NewMemorySnapshots(max int) *MemorySnapshots {
	if max < 1 {
		max = 128
	}
	return &MemorySnapshots{entries: map[string]memorySnapshot{}, max: max}
}
func (m *MemorySnapshots) GetSnapshot(ctx context.Context, key string) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.entries[key]
	if !ok || !time.Now().Before(v.expires) {
		delete(m.entries, key)
		return nil, false, nil
	}
	return append([]byte(nil), v.data...), true, nil
}
func (m *MemorySnapshots) SetSnapshot(ctx context.Context, key string, b []byte, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(b) > maxCatalogBytes {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.entries) >= m.max {
		for k := range m.entries {
			delete(m.entries, k)
			break
		}
	}
	m.entries[key] = memorySnapshot{append([]byte(nil), b...), time.Now().Add(ttl)}
	return nil
}
func (m *MemorySnapshots) DeleteSnapshot(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, key)
	return ctx.Err()
}
