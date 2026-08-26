package promptcache

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/kimnt93/gorouter/pkg/chat"
)

func TestRedisSharedCacheAndIsolation(t *testing.T) {
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL is not set")
	}
	optsA, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	optsB, _ := redis.ParseURL(url)
	a := redis.NewClient(optsA)
	b := redis.NewClient(optsB)
	defer a.Close()
	defer b.Close()
	if err := a.Ping(context.Background()).Err(); err != nil {
		t.Skipf("test Redis unavailable: %v", err)
	}
	one := NewRedis(a, Config{Scope: chat.ScopeKey, TTL: time.Minute, MaxEntryBytes: 4})
	two := NewRedis(b, Config{Scope: chat.ScopeKey, TTL: time.Minute, MaxEntryBytes: 4})
	prompt := []byte(fmt.Sprintf(`{"model":"m","messages":[{"content":%q}]}`, time.Now().String()))
	cacheKey := redisKeyPrefix + BuildKey(ScopeID(chat.ScopeKey, "key-a", "tenant-a"), "m", prompt)
	t.Cleanup(func() { _ = a.Del(context.Background(), cacheKey).Err() })
	if !one.Store("key-a", "tenant-a", "m", prompt, &chat.CacheEntry{Status: 200, Body: []byte("1234")}) {
		t.Fatal("store failed")
	}
	if got, ok := two.Lookup("key-a", "tenant-a", "m", prompt); !ok || string(got.Body) != "1234" {
		t.Fatal("second router instance did not share Redis cache")
	}
	if _, ok := two.Lookup("key-b", "tenant-a", "m", prompt); ok {
		t.Fatal("cross-key leak")
	}
	if one.Store("key-a", "tenant-a", "m", []byte("large"), &chat.CacheEntry{Body: []byte("12345")}) {
		t.Fatal("Redis accepted oversized entry")
	}
	tenantOne := NewRedis(a, Config{Scope: chat.ScopeTenant, TTL: time.Minute, MaxEntryBytes: 16})
	tenantTwo := NewRedis(b, Config{Scope: chat.ScopeTenant, TTL: time.Minute, MaxEntryBytes: 16})
	tenantPrompt := append(prompt, []byte("-tenant")...)
	if !tenantOne.Store("key-a", "tenant-a", "m", tenantPrompt, &chat.CacheEntry{Status: 200, Body: []byte("tenant")}) {
		t.Fatal("tenant-scoped store failed")
	}
	if _, ok := tenantTwo.Lookup("key-b", "tenant-a", "m", tenantPrompt); !ok {
		t.Fatal("tenant-scoped entry did not share within tenant")
	}
	if _, ok := tenantTwo.Lookup("key-c", "tenant-b", "m", tenantPrompt); ok {
		t.Fatal("tenant-scoped entry leaked cross-tenant")
	}
	one.Flush()
	if _, ok := two.Lookup("key-a", "tenant-a", "m", prompt); ok {
		t.Fatal("cache flush left an entry behind")
	}
}
