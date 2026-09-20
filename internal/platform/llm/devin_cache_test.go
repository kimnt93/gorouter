package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/kimnt93/gorouter/internal/platform/modeldiscovery"
	"github.com/kimnt93/gorouter/internal/platform/refreshlock"
	"github.com/redis/go-redis/v9"
)

func TestDevinWarmDiscoveryAndChatReuseCatalog(t *testing.T) {
	a := devinTestAdapter(t, "normal")
	a.CatalogCache = modeldiscovery.NewMemorySnapshots(8)
	cr := devinTestCredential()
	cr.ID = "own"
	first, err := a.DiscoverModels(context.Background(), cr)
	if err != nil || len(first) != 2 {
		t.Fatal(err)
	}
	// Fresh catalog contains future-model; use an executable that refuses models
	// list to prove warm list AND chat skip that subprocess, not just public API.
	a.Binary = mockDevinBinary(t, "cached-only")
	next, err := a.DiscoverModels(context.Background(), cr)
	if err != nil || len(next) != 2 {
		t.Fatal("warm list did not use snapshot")
	}
	first[0].SupportedReasoningLevels[0].Effort = "mutated"
	if next[0].SupportedReasoningLevels[0].Effort == "mutated" {
		t.Fatal("snapshot mutation")
	}
	r, err := a.Send(context.Background(), cr, "future-model", []byte(`{"messages":[{"role":"user","content":"synthetic"}],"reasoning":{"effort":"high"}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatalf("warm chat status=%d", r.StatusCode)
	}
	if _, err := io.Copy(io.Discard, r.Body); err != nil {
		t.Fatal(err)
	}
	if _, err := a.RefreshModels(context.Background(), cr); err == nil {
		t.Fatal("refresh silently used snapshot")
	}
	if _, err := a.DiscoverModels(context.Background(), cr); err == nil {
		t.Fatal("failed refresh kept invalid catalog")
	}
}
func TestDevinCatalogRotationAndCredentialIsolation(t *testing.T) {
	a := devinTestAdapter(t, "normal")
	a.CatalogCache = modeldiscovery.NewMemorySnapshots(8)
	cr := devinTestCredential()
	cr.ID = "one"
	if _, err := a.DiscoverModels(context.Background(), cr); err != nil {
		t.Fatal(err)
	}
	keyBefore := devinCatalogKey(cr)
	cr.APIKey = "apk_user_invalid"
	if devinCatalogKey(cr) == keyBefore {
		t.Fatal("rotation did not alter cache identity")
	}
	if _, err := a.DiscoverModels(context.Background(), cr); devinStatus(err) != 401 {
		t.Fatal("rotation reused accepted credentials")
	}
	cr.APIKey = "apk_user_synthetic"
	cr.ID = "two"
	a.Binary = mockDevinBinary(t, "cached-only")
	if _, err := a.DiscoverModels(context.Background(), cr); err == nil {
		t.Fatal("cross-credential shared catalog")
	}
	if strings.Contains(keyBefore, "synthetic") {
		t.Fatal("key material exposed")
	}
}
func TestDevinWarmChatStillAuthenticatesAndInvalidates(t *testing.T) {
	a := devinTestAdapter(t, "normal")
	a.CatalogCache = modeldiscovery.NewMemorySnapshots(8)
	cr := devinTestCredential()
	cr.ID = "one"
	if _, err := a.DiscoverModels(context.Background(), cr); err != nil {
		t.Fatal(err)
	}
	a.Binary = mockDevinBinary(t, "auth-revoked")
	r, err := a.Send(context.Background(), cr, "future-model", []byte(`{"messages":[{"role":"user","content":"synthetic"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 401 {
		t.Fatal("warm metadata bypassed authentication")
	}
	if _, ok, _ := a.CatalogCache.GetSnapshot(context.Background(), devinCatalogKey(cr)); ok {
		t.Fatal("revoked cache not invalidated")
	}
}

// Atomic request-level counters avoid relying on wall-clock speed in CI.
type observedSnapshots struct {
	DevinCatalogCache
	writes atomic.Int32
}

func (c *observedSnapshots) SetSnapshot(ctx context.Context, k string, b []byte, ttl time.Duration) error {
	c.writes.Add(1)
	return c.DevinCatalogCache.SetSnapshot(ctx, k, b, ttl)
}
func TestDevinCatalogSharedRedisAcrossReplicas(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()
	cache := &observedSnapshots{DevinCatalogCache: modeldiscovery.NewSnapshots(client)}
	lock, _ := refreshlock.NewRedis(client, time.Minute)
	a, b := devinTestAdapter(t, "catalog-slow"), devinTestAdapter(t, "catalog-slow")
	for _, adapter := range []*DevinCLIAdapter{a, b} {
		adapter.CatalogCache = cache
		adapter.CatalogLocker = lock
	}
	cr := devinTestCredential()
	cr.ID = "shared"
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		adapter := a
		if i%2 == 0 {
			adapter = b
		}
		wg.Go(func() {
			m, err := adapter.DiscoverModels(context.Background(), cr)
			if err != nil || len(m) != 2 {
				t.Errorf("discovery err=%v", err)
			}
		})
	}
	wg.Wait()
	if cache.writes.Load() != 1 {
		t.Fatalf("refresh stampede: writes=%d", cache.writes.Load())
	}
	b.Binary = mockDevinBinary(t, "cached-only")
	if _, err := b.DiscoverModels(context.Background(), cr); err != nil {
		t.Fatal("replica missed shared cache")
	}
	server.FastForward(6 * time.Minute)
	if _, err := b.DiscoverModels(context.Background(), cr); err == nil {
		t.Fatal("TTL did not expire")
	}
}
func TestDevinCatalogCanceledWaiter(t *testing.T) {
	a := devinTestAdapter(t, "catalog-slow")
	a.CatalogCache = modeldiscovery.NewMemorySnapshots(8)
	cr := devinTestCredential()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := a.DiscoverModels(ctx, cr)
	if devinStatus(err) != 504 {
		t.Fatal("cancellation ignored")
	}
	// The process-group timeout and workspace cleanup happen asynchronously after
	// the caller exits. Wait without racing tests' temporary-directory removal.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(a.slots) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("canceled discovery leaked process capacity")
}

type brokenSnapshots struct{}

func (brokenSnapshots) GetSnapshot(context.Context, string) ([]byte, bool, error) {
	return nil, false, errors.New("offline")
}
func (brokenSnapshots) SetSnapshot(context.Context, string, []byte, time.Duration) error {
	return errors.New("offline")
}
func (brokenSnapshots) DeleteSnapshot(context.Context, string) error { return errors.New("offline") }
func TestDevinCacheOutageUsesFreshDiscovery(t *testing.T) {
	a := devinTestAdapter(t, "normal")
	a.CatalogCache = brokenSnapshots{}
	m, err := a.DiscoverModels(context.Background(), devinTestCredential())
	if err != nil || len(m) != 2 {
		t.Fatal(err)
	}
}
func TestDevinServiceKeyIsNotAcceptedAsInference(t *testing.T) {
	err := validateDevinKey("apk_synthetic_service")
	if devinStatus(err) != 400 || !strings.Contains(err.Error(), "Cloud sessions") {
		t.Fatal("service key guidance missing")
	}
	err = devinCredentialError("apk_user_synthetic", devinFailure(401, "safe"))
	if !strings.Contains(err.Error(), "legacy") || strings.Contains(err.Error(), "synthetic") {
		t.Fatal("legacy-key guidance missing or secret leaked")
	}
}

func BenchmarkDevinCatalogWarm(b *testing.B) {
	// Same adapter path; warm metadata only. No real provider or inference call.
	cache := modeldiscovery.NewMemorySnapshots(8)
	a := &DevinCLIAdapter{CatalogCache: cache}
	cr := devinTestCredential()
	cr.ID = "benchmark"
	models, _ := normalizeDevinCatalog(devinCatalog{Families: []devinFamily{{Slug: "future-model", Label: "Future Model", Variants: []devinVariant{{UID: "future-high", Label: "Future Model High"}}}}})
	body, _ := json.Marshal(devinCatalogSnapshot{FetchedAt: time.Now(), Models: models})
	_ = cache.SetSnapshot(context.Background(), devinCatalogKey(cr), body, time.Hour)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		m, err := a.DiscoverModels(context.Background(), cr)
		if err != nil || len(m) != 1 {
			b.Fatal(fmt.Sprint(err))
		}
	}
}

func TestDevinCatalogRealRedisMetadataAcrossNodes(t *testing.T) {
	address := os.Getenv("TEST_REDIS_URL")
	if address == "" {
		t.Skip("TEST_REDIS_URL unset")
	}
	options, err := redis.ParseURL(address)
	if err != nil {
		t.Fatal("invalid test Redis URL")
	}
	client := redis.NewClient(options)
	defer client.Close()
	if client.Ping(context.Background()).Err() != nil {
		t.Fatal("Redis unavailable")
	}
	a, b := devinTestAdapter(t, "normal"), devinTestAdapter(t, "cached-only")
	for _, adapter := range []*DevinCLIAdapter{a, b} {
		adapter.CatalogCache = modeldiscovery.NewSnapshots(client)
		adapter.CatalogLocker, _ = refreshlock.NewRedis(client, time.Minute)
	}
	cr := devinTestCredential()
	cr.ID = fmt.Sprintf("synthetic-%d", time.Now().UnixNano())
	defer a.invalidateCatalog(context.Background(), cr)
	if _, err := a.DiscoverModels(context.Background(), cr); err != nil {
		t.Fatal(err)
	}
	if m, err := b.DiscoverModels(context.Background(), cr); err != nil || len(m) != 2 {
		t.Fatal("cross-node snapshot not reused")
	}
}

func TestDevinRefreshAfterWarmCatalog(t *testing.T) {
	a := devinTestAdapter(t, "normal")
	cache := &observedSnapshots{DevinCatalogCache: modeldiscovery.NewMemorySnapshots(8)}
	a.CatalogCache = cache
	cr := devinTestCredential()
	if _, err := a.DiscoverModels(context.Background(), cr); err != nil {
		t.Fatal(err)
	}
	if _, err := a.RefreshModels(context.Background(), cr); err != nil {
		t.Fatal(err)
	}
	if cache.writes.Load() != 2 {
		t.Fatal("explicit refresh reused older metadata")
	}
}
