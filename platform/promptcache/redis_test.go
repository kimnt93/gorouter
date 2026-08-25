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
	if !one.Store("key-a", "tenant-a", "m", prompt, &chat.CacheEntry{Status: 200, Body: []byte("1234")}) {
		t.Fatal("store failed")
	}
	defer one.Flush()
	if got, ok := two.Lookup("key-a", "tenant-a", "m", prompt); !ok || string(got.Body) != "1234" {
		t.Fatal("second router instance did not share Redis cache")
	}
	if _, ok := two.Lookup("key-b", "tenant-a", "m", prompt); ok {
		t.Fatal("cross-key leak")
	}
	if one.Store("key-a", "tenant-a", "m", []byte("large"), &chat.CacheEntry{Body: []byte("12345")}) {
		t.Fatal("Redis accepted oversized entry")
	}
}
