package providerquota

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestRedisStateSharesSnapshotExhaustionAndActiveAccount(t *testing.T) {
	server := miniredis.RunT(t)
	clientA := redis.NewClient(&redis.Options{Addr: server.Addr()})
	clientB := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = clientA.Close(); _ = clientB.Close() })
	a, b := NewRedisState(clientA), NewRedisState(clientB)
	ctx := context.Background()
	snapshot := Snapshot{CredentialID: "cred-a", Provider: "codex", Account: "account", Available: true, Windows: []Window{}}
	if err := a.PutSnapshot(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	if shared, ok, err := b.Snapshot(ctx, "cred-a"); err != nil || !ok || shared.Account != "account" {
		t.Fatalf("shared snapshot=%+v ok=%v err=%v", shared, ok, err)
	}
	until := time.Now().Add(time.Minute).Truncate(time.Second)
	if err := a.MarkExhausted(ctx, "cred-a", until); err != nil {
		t.Fatal(err)
	}
	if shared, err := b.ExhaustedUntil(ctx, "cred-a"); err != nil || !shared.Equal(until) {
		t.Fatalf("shared exhaustion=%s err=%v", shared, err)
	}
	if err := a.MarkActive(ctx, "codex", "cred-a"); err != nil {
		t.Fatal(err)
	}
	if active, err := b.ActiveCredential(ctx, "codex"); err != nil || active != "cred-a" {
		t.Fatalf("active=%q err=%v", active, err)
	}
}
