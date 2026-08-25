package quota

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func testRedis(t *testing.T) *redis.Client {
	t.Helper()
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL is not set")
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	c := redis.NewClient(opts)
	if err := c.Ping(context.Background()).Err(); err != nil {
		c.Close()
		t.Skipf("test Redis unavailable: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestRedisAtomicReserveSettleReleaseAndRPM(t *testing.T) {
	rdb := testRedis(t)
	q, err := NewRedis(rdb, PolicyStrict)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().UTC()
	keyID := fmt.Sprintf("test-%d", now.UnixNano())
	t.Cleanup(func() {
		_ = rdb.Del(ctx, quotaKey(keyID, now.Format("2006-01"))).Err()
	})

	var accepted atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := q.Reserve(ctx, keyID, 1, 0, 0.1, now); err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, ErrExceeded) {
				t.Errorf("reserve: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := accepted.Load(); got != 10 {
		t.Fatalf("atomic quota accepted %d requests, want 10", got)
	}

	settleKey := keyID + "-settle"
	r, err := q.Reserve(ctx, settleKey, 1, 0, 0.8, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Settle(ctx, r, 0.2); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Reserve(ctx, settleKey, 1, 0, 0.8, now); err != nil {
		t.Fatalf("unused reservation was not released on settle: %v", err)
	}
	if err := q.Settle(ctx, r, 0.2); !errors.Is(err, ErrClosed) {
		t.Fatalf("duplicate settlement must be rejected, got %v", err)
	}

	releaseKey := keyID + "-release"
	released, err := q.Reserve(ctx, releaseKey, 1, 0, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Release(ctx, released); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Reserve(ctx, releaseKey, 1, 0, 1, now); err != nil {
		t.Fatalf("released amount remained reserved: %v", err)
	}

	rpmKey := keyID + "-rpm"
	for i := 0; i < 3; i++ {
		ok, err := q.AllowRPM(ctx, rpmKey, 2, now)
		if err != nil {
			t.Fatal(err)
		}
		if ok != (i < 2) {
			t.Fatalf("request %d allowed=%v", i+1, ok)
		}
	}
}

func TestRedisOutagePolicies(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 10 * time.Millisecond, ReadTimeout: 10 * time.Millisecond})
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	strict, _ := NewRedis(client, PolicyStrict)
	if _, err := strict.Reserve(ctx, "k", 1, 0, 0.1, time.Now()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("strict mode must fail closed, got %v", err)
	}
	open, _ := NewRedis(client, PolicyOpen)
	r, err := open.Reserve(ctx, "k", 1, 0, 0.1, time.Now())
	if err != nil || !r.Bypassed {
		t.Fatalf("open mode must explicitly bypass: reservation=%+v err=%v", r, err)
	}
}
