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
	if err := b.ClearExhausted(ctx, "cred-a"); err != nil {
		t.Fatal(err)
	}
	if shared, err := a.ExhaustedUntil(ctx, "cred-a"); err != nil || !shared.IsZero() {
		t.Fatalf("cleared exhaustion=%s err=%v", shared, err)
	}
	if accepted, err := a.MarkActive(ctx, "codex", "cred-a"); err != nil || !accepted {
		t.Fatalf("mark active accepted=%v err=%v", accepted, err)
	}
	if active, err := b.ActiveCredential(ctx, "codex"); err != nil || active != "cred-a" {
		t.Fatalf("active=%q err=%v", active, err)
	}
}

func TestDistributedCursorRejectsStaleSuccessAfterAccountExhaustion(t *testing.T) {
	server := miniredis.RunT(t)
	clientA := redis.NewClient(&redis.Options{Addr: server.Addr()})
	clientB := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = clientA.Close(); _ = clientB.Close() })
	stateA, stateB := NewRedisState(clientA), NewRedisState(clientB)
	first, second := New(nil, nil), New(nil, nil)
	first.SetStateCache(stateA)
	second.SetStateCache(stateB)
	for _, service := range []*Service{first, second} {
		service.snapshots["cred-a"] = Snapshot{CredentialID: "cred-a", Provider: "codex", Available: true, Windows: []Window{}}
		service.snapshots["cred-b"] = Snapshot{CredentialID: "cred-b", Provider: "codex", Available: true, Windows: []Window{}}
	}

	first.MarkInUse("cred-a")
	if got := second.ActiveCredential("codex"); got != "cred-a" {
		t.Fatalf("second replica cursor=%q want cred-a", got)
	}
	first.MarkExhausted("cred-a")
	second.MarkInUse("cred-b")
	if got := first.ActiveCredential("codex"); got != "cred-b" {
		t.Fatalf("first replica cursor=%q want cred-b", got)
	}
	second.MarkInUse("cred-a") // stale in-flight success from another replica
	if got := first.ActiveCredential("codex"); got != "cred-b" {
		t.Fatalf("stale success changed cursor=%q", got)
	}
	if second.Available("cred-a") {
		t.Fatal("second replica did not observe shared exhaustion")
	}
}

func TestRestoreSeedsMissingDistributedCursor(t *testing.T) {
	server := miniredis.RunT(t)
	clientA := redis.NewClient(&redis.Options{Addr: server.Addr()})
	clientB := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = clientA.Close(); _ = clientB.Close() })
	store := &quotaStore{loaded: []Snapshot{{CredentialID: "cred-c", Provider: "codex", InUse: true, Available: true, Windows: []Window{}}}}
	first := New(nil, nil)
	first.SetStore(store)
	first.SetStateCache(NewRedisState(clientA))
	if err := first.Restore(t.Context()); err != nil {
		t.Fatal(err)
	}
	second := New(nil, nil)
	second.SetStateCache(NewRedisState(clientB))
	if got := second.ActiveCredential("codex"); got != "cred-c" {
		t.Fatalf("restored distributed cursor=%q", got)
	}
}
