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

func TestWindowUTCBoundaries(t *testing.T) {
	SetWeekStart(time.Sunday)
	now := time.Date(2026, time.August, 26, 16, 30, 0, 0, time.FixedZone("ICT", 7*60*60))
	tests := []struct {
		period, start, end, suffix string
	}{
		{"week", "2026-08-23T00:00:00Z", "2026-08-30T00:00:00Z", "2026-08-23"},
	}
	for _, tt := range tests {
		start, end, suffix, err := Window(tt.period, now)
		if err != nil {
			t.Fatal(err)
		}
		if start.Format(time.RFC3339) != tt.start || end.Format(time.RFC3339) != tt.end || suffix != tt.suffix {
			t.Fatalf("%s window = %s %s %s", tt.period, start, end, suffix)
		}
	}
}

func TestWindowConfigurableWeekStart(t *testing.T) {
	SetWeekStart(time.Monday)
	t.Cleanup(func(){ SetWeekStart(time.Sunday) })
	start,end,suffix,err:=Window("week",time.Date(2027,time.January,1,12,0,0,0,time.UTC))
	if err!=nil || start.Format("2006-01-02")!="2026-12-28" || end.Format("2006-01-02")!="2027-01-04" || suffix!="2026-12-28" { t.Fatalf("window = %s %s %s err=%v",start,end,suffix,err) }
}

func TestNoLimitBypassesRedis(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 10 * time.Millisecond})
	defer client.Close()
	q, _ := NewRedis(client, PolicyStrict)
	res, err := q.ReserveForPeriod(context.Background(), "key", 0, 0, 10, "none", time.Now())
	if err != nil || !res.Bypassed {
		t.Fatalf("no-limit reservation = %+v, %v", res, err)
	}
}
