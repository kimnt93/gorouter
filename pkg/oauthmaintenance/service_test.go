package oauthmaintenance

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/kimnt93/gorouter/internal/platform/refreshlock"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/redis/go-redis/v9"
)

type memoryStore struct {
	mu   sync.Mutex
	cr   entities.CredentialRuntime
	list []entities.Credential
}

func (m *memoryStore) List(context.Context) ([]entities.Credential, error) { return m.list, nil }
func (m *memoryStore) Runtime(_ context.Context, _ string) (*entities.CredentialRuntime, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.cr
	return &c, nil
}
func (m *memoryStore) rotate(access, refresh string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cr.OAuthAccess = access
	m.cr.OAuthRefreh = refresh
}
func TestConcurrentRefreshUsesRotatedToken(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()
	store := &memoryStore{cr: entities.CredentialRuntime{ID: "id", Provider: "codex", Kind: entities.KindOAuth, OAuthAccess: "old", OAuthRefreh: "old-refresh"}}
	var exchanges atomic.Int32
	makeService := func() *Service {
		lock, err := refreshlock.NewRedis(client, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		return &Service{Store: store, Locker: lock, Refreshers: map[string]Refresh{"codex": func(_ context.Context, cr *entities.CredentialRuntime) error {
			if cr.OAuthRefreh != "old-refresh" {
				t.Errorf("stale exchange")
			}
			exchanges.Add(1)
			time.Sleep(40 * time.Millisecond)
			store.rotate("new", "new-refresh")
			cr.OAuthAccess = "new"
			cr.OAuthRefreh = "new-refresh"
			return nil
		}}}
	}
	a, b := makeService(), makeService()
	cr1, _ := store.Runtime(context.Background(), "id")
	cr2, _ := store.Runtime(context.Background(), "id")
	errs := make(chan error, 2)
	go func() { errs <- a.Refresh(context.Background(), cr1, true) }()
	go func() { errs <- b.Refresh(context.Background(), cr2, true) }()
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if exchanges.Load() != 1 || cr1.OAuthAccess != "new" || cr2.OAuthRefreh != "new-refresh" {
		t.Fatalf("exchanges=%d, runtimes not synchronized", exchanges.Load())
	}
}
func TestRefreshFailsClosedOnRedisOutage(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()
	lock, _ := refreshlock.NewRedis(client, time.Minute)
	server.Close()
	store := &memoryStore{cr: entities.CredentialRuntime{ID: "id", Provider: "codex", Kind: entities.KindOAuth, OAuthRefreh: "r"}}
	called := false
	svc := &Service{Store: store, Locker: lock, Refreshers: map[string]Refresh{"codex": func(context.Context, *entities.CredentialRuntime) error { called = true; return nil }}}
	cr, _ := store.Runtime(context.Background(), "id")
	if err := svc.Refresh(context.Background(), cr, true); !errors.Is(err, refreshlock.ErrUnavailable) || called {
		t.Fatalf("err=%v, called=%v", err, called)
	}
}
func TestIdleRefresh(t *testing.T) {
	store := &memoryStore{cr: entities.CredentialRuntime{ID: "id", Provider: "codex", Kind: entities.KindOAuth, OAuthRefreh: "r"}, list: []entities.Credential{{ID: "id", Provider: "codex", Kind: entities.KindOAuth, Status: entities.StatusActive}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{}, 1)
	svc := &Service{Store: store, Refreshers: map[string]Refresh{"codex": func(_ context.Context, cr *entities.CredentialRuntime) error {
		store.rotate("new", "r2")
		cr.OAuthAccess = "new"
		cr.OAuthRefreh = "r2"
		done <- struct{}{}
		return nil
	}}}
	svc.Start(ctx, time.Millisecond, nil)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("idle credential not refreshed")
	}
}

func TestRefreshWithoutExpiryIsThrottledAndReloadsRuntime(t *testing.T) {
	store := &memoryStore{cr: entities.CredentialRuntime{ID: "id", Provider: "codex", Kind: entities.KindOAuth, OAuthAccess: "new", OAuthRefreh: "new-refresh", OAuthMeta: entities.OAuthMetadata{LastRefreshedAt: time.Now().UTC().Format(time.RFC3339Nano)}}}
	calls := 0
	svc := &Service{Store: store, Refreshers: map[string]Refresh{"codex": func(context.Context, *entities.CredentialRuntime) error { calls++; return nil }}}
	stale := &entities.CredentialRuntime{ID: "id", Provider: "codex", Kind: entities.KindOAuth, OAuthAccess: "new", OAuthRefreh: "new-refresh"}
	if err := svc.Refresh(context.Background(), stale, false); err != nil || calls != 0 || stale.OAuthMeta.LastRefreshedAt == "" {
		t.Fatalf("refresh=%v calls=%d runtime=%+v", err, calls, stale.OAuthMeta)
	}
}
