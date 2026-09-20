package modeldiscovery

import (
	"context"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"testing"
	"time"
)

func TestSnapshotStoresIsolationExpiryAndCopying(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()
	type store interface {
		GetSnapshot(context.Context, string) ([]byte, bool, error)
		SetSnapshot(context.Context, string, []byte, time.Duration) error
		DeleteSnapshot(context.Context, string) error
	}
	for _, s := range []store{NewSnapshots(client), NewMemorySnapshots(2)} {
		ctx := context.Background()
		raw := []byte(`{"models":[]}`)
		if err := s.SetSnapshot(ctx, "a", raw, time.Hour); err != nil {
			t.Fatal(err)
		}
		raw[0] = 'X'
		got, ok, err := s.GetSnapshot(ctx, "a")
		if err != nil || !ok || got[0] != '{' {
			t.Fatal("write alias")
		}
		got[0] = 'X'
		got, _, _ = s.GetSnapshot(ctx, "a")
		if got[0] != '{' {
			t.Fatal("read alias")
		}
		if _, ok, _ := s.GetSnapshot(ctx, "b"); ok {
			t.Fatal("namespace leak")
		}
		_ = s.DeleteSnapshot(ctx, "a")
		if _, ok, _ := s.GetSnapshot(ctx, "a"); ok {
			t.Fatal("delete ignored")
		}
		_ = s.SetSnapshot(ctx, "a", []byte("x"), time.Nanosecond)
		time.Sleep(time.Millisecond)
		server.FastForward(time.Second)
		if _, ok, _ := s.GetSnapshot(ctx, "a"); ok {
			t.Fatal("TTL ignored")
		}
	}
}
