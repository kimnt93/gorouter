package pricing

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/redis/go-redis/v9"
)

type resolverRepo struct {
	manual  map[string]entities.Price
	catalog []entities.CatalogPrice
}

func (r *resolverRepo) ListPrices(context.Context) (map[string]entities.Price, error) {
	return r.manual, nil
}
func (r *resolverRepo) ListCatalogPrices(context.Context) ([]entities.CatalogPrice, error) {
	return r.catalog, nil
}

func TestResolverManualPrecedenceAndUpstreamFallback(t *testing.T) {
	repo := &resolverRepo{
		manual: map[string]entities.Price{"alias": {InputPerM: 9}},
		catalog: []entities.CatalogPrice{
			{Model: "alias", Price: entities.Price{InputPerM: 2}},
			{Model: "upstream/model", Price: entities.Price{InputPerM: 3}},
		},
	}
	r := NewResolver(repo)
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if price, ok := r.Resolve("alias", "upstream/model"); !ok || price.InputPerM != 9 {
		t.Fatalf("manual resolution = %+v, %v", price, ok)
	}
	if price, ok := r.Resolve("other-alias", "upstream/model"); !ok || price.InputPerM != 3 {
		t.Fatalf("upstream fallback = %+v, %v", price, ok)
	}
	if price, ok := r.Resolve("missing", "also-missing"); !ok || price != (entities.Price{}) {
		t.Fatalf("missing model should resolve as free: %+v, %v", price, ok)
	}
}

func TestResolverManualMutationsAreImmediate(t *testing.T) {
	r := NewResolver(&resolverRepo{})
	r.SetManual("model", entities.Price{InputPerM: 7})
	if price, ok := r.Resolve("model", ""); !ok || price.InputPerM != 7 {
		t.Fatalf("set manual = %+v, %v", price, ok)
	}
	r.DeleteManual("model")
	if price, ok := r.Resolve("model", ""); !ok || price != (entities.Price{}) {
		t.Fatalf("deleted manual price should fall back to free: %+v, %v", price, ok)
	}
}

func TestResolverMapsPublicProviderIDsToOriginalCatalogModels(t *testing.T) {
	repo := &resolverRepo{catalog: []entities.CatalogPrice{
		{Model: "openai/gpt-5.6-luna", CacheSupported: true, Price: entities.Price{InputPerM: .2, CachedInputPerM: .02}},
		{Model: "deepseek/deepseek-v4-flash", CacheSupported: true, Price: entities.Price{InputPerM: .0679}},
	}}
	r := NewResolver(repo)
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		model, upstream string
		want            float64
	}{
		{"cx/gpt-5.6-luna", "gpt-5.6-luna", .2},
		{"blend/luna", "gpt-5.6-luna", .2},
		{"ocz/deepseek-v4-flash", "deepseek-v4-flash", .0679},
	} {
		price, ok := r.Resolve(test.model, test.upstream)
		if !ok || price.InputPerM != test.want {
			t.Fatalf("Resolve(%q,%q) = %+v,%v", test.model, test.upstream, price, ok)
		}
	}
	catalog, ok := r.Catalog("cx/gpt-5.6-luna", "gpt-5.6-luna")
	if !ok || catalog.Model != "openai/gpt-5.6-luna" || !catalog.CacheSupported {
		t.Fatalf("catalog resolution = %+v,%v", catalog, ok)
	}
}

func TestResolverEstimatesPreserveSelectedSourceCacheCapability(t *testing.T) {
	repo := &resolverRepo{
		manual:  map[string]entities.Price{"alias": {InputPerM: 2, OutputPerM: 4, CachedInputPerM: 0.2}},
		catalog: []entities.CatalogPrice{{Model: "alias", CacheSupported: false, Price: entities.Price{InputPerM: 9}}},
	}
	r := NewResolver(repo)
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	estimate := r.Estimates("alias", "", 1_000_000, 0)
	if estimate.WithoutCache.USD != 2 || estimate.WithCache.USD != 0.2 {
		t.Fatalf("manual cache estimate = %+v", estimate)
	}
}

func TestResolverConcurrentRefreshAndResolve(t *testing.T) {
	repo := &resolverRepo{manual: map[string]entities.Price{"model": {InputPerM: 1}}}
	r := NewResolver(repo)
	if err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for n := 0; n < 10; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_, _ = r.Resolve("model", "")
			}
		}()
	}
	for i := 0; i < 20; i++ {
		if err := r.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
}

type lockedResolverRepo struct {
	mu     sync.RWMutex
	manual map[string]entities.Price
}

func (r *lockedResolverRepo) ListPrices(context.Context) (map[string]entities.Price, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]entities.Price, len(r.manual))
	for model, price := range r.manual {
		out[model] = price
	}
	return out, nil
}
func (*lockedResolverRepo) ListCatalogPrices(context.Context) ([]entities.CatalogPrice, error) {
	return nil, nil
}

func TestRedisInvalidationRefreshesPriceCacheAcrossReplicas(t *testing.T) {
	server := miniredis.RunT(t)
	clientA := redis.NewClient(&redis.Options{Addr: server.Addr()})
	clientB := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = clientA.Close(); _ = clientB.Close() })
	repo := &lockedResolverRepo{manual: map[string]entities.Price{"model": {InputPerM: 1}}}
	first, second := NewResolver(repo), NewResolver(repo)
	if err := first.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := second.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := first.EnableRedisInvalidation(ctx, clientA); err != nil {
		t.Fatal(err)
	}
	if err := second.EnableRedisInvalidation(ctx, clientB); err != nil {
		t.Fatal(err)
	}
	repo.mu.Lock()
	repo.manual["model"] = entities.Price{InputPerM: 9}
	repo.mu.Unlock()
	first.SetManual("model", entities.Price{InputPerM: 9})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if price, _ := second.Resolve("model", ""); price.InputPerM == 9 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("second replica did not refresh its price snapshot")
}

type catalogRepo struct {
	resolverRepo
	synced chan struct{}
}

func (r *catalogRepo) ReplaceCatalogPrices(_ context.Context, _ string, prices []entities.CatalogPrice) error {
	r.catalog = prices
	select {
	case r.synced <- struct{}{}:
	default:
	}
	return nil
}

type catalogImporterFunc func(context.Context) ([]entities.CatalogPrice, error)

func (f catalogImporterFunc) ImportCatalog(ctx context.Context) ([]entities.CatalogPrice, error) {
	return f(ctx)
}

func TestCatalogStartSyncsImmediately(t *testing.T) {
	repo := &catalogRepo{synced: make(chan struct{}, 1)}
	service := NewCatalogService(repo, catalogImporterFunc(func(context.Context) ([]entities.CatalogPrice, error) {
		return []entities.CatalogPrice{{Model: "model", Price: entities.Price{InputPerM: 1}}}, nil
	}), "openrouter", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.Start(ctx, time.Hour, nil)
	select {
	case <-repo.synced:
	case <-time.After(time.Second):
		t.Fatal("initial catalog sync did not run immediately")
	}
}

func TestCatalogSyncRejectsEmptyResponse(t *testing.T) {
	repo := &catalogRepo{synced: make(chan struct{}, 1)}
	service := NewCatalogService(repo, catalogImporterFunc(func(context.Context) ([]entities.CatalogPrice, error) {
		return nil, nil
	}), "openrouter", nil)
	if err := service.Sync(context.Background()); err == nil {
		t.Fatal("empty upstream response cleared the catalog")
	}
	select {
	case <-repo.synced:
		t.Fatal("empty catalog was persisted")
	default:
	}
}
