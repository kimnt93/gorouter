package promptcache

import (
	"testing"
	"time"

	"github.com/kimnt93/gorouter/pkg/chat"
	"github.com/kimnt93/gorouter/pkg/config"
)

func TestCanonicalKeyAndScopes(t *testing.T) {
	a := []byte(`{"model":"m","messages":[{"role":"user","content":"secret prompt"}]}`)
	b := []byte(" { \"messages\" : [ { \"content\" : \"secret prompt\", \"role\":\"user\" } ], \"model\":\"m\" }")
	if BuildKey("key-a", "m", a) != BuildKey("key-a", "m", b) {
		t.Fatal("semantically identical JSON must have the same cache key")
	}
	key := BuildKey("key-a", "m", a)
	if key == BuildKey("key-b", "m", a) || key == BuildKey("key-a", "other", a) {
		t.Fatal("scope and model must participate in key")
	}
	if len(key) != 64 {
		t.Fatalf("key is not a SHA-256 hex digest: %q", key)
	}
}

func TestMemoryIsolationTTLAndSize(t *testing.T) {
	m := NewMemory(Config{Scope: chat.ScopeTenant, TTL: 20 * time.Millisecond, MaxEntryBytes: 4})
	defer m.Close()
	body := []byte(`{"model":"m"}`)
	if !m.Store("a", "tenant-a", "m", body, &chat.CacheEntry{Body: []byte("1234")}) {
		t.Fatal("valid entry rejected")
	}
	if _, ok := m.Lookup("b", "tenant-a", "m", body); !ok {
		t.Fatal("tenant-scoped entry should share within tenant")
	}
	if _, ok := m.Lookup("b", "tenant-b", "m", body); ok {
		t.Fatal("cross-tenant cache leak")
	}
	if m.Store("a", "tenant-a", "m", []byte("other"), &chat.CacheEntry{Body: []byte("12345")}) {
		t.Fatal("oversized entry accepted")
	}
	time.Sleep(30 * time.Millisecond)
	if _, ok := m.Lookup("a", "tenant-a", "m", body); ok {
		t.Fatal("expired entry returned")
	}
}

func TestFactoryDisabledAndProductionFallback(t *testing.T) {
	c, client, err := New(config.CacheConfig{Enabled: false}, "")
	if err != nil || client != nil {
		t.Fatalf("disabled cache wiring failed: client=%v err=%v", client, err)
	}
	defer c.Close()
	if c.Store("k", "t", "m", nil, &chat.CacheEntry{}) {
		t.Fatal("disabled cache stored an entry")
	}
	if _, _, err := New(config.CacheConfig{Enabled: true, AllowMemory: false}, ""); err == nil {
		t.Fatal("production cache silently fell back to memory")
	}
}
