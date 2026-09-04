package pricing

import (
	"context"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/redis/go-redis/v9"
)

const priceInvalidationChannel = "gorouter:pricing:invalidate"

type ResolverRepository interface {
	ListPrices(ctx context.Context) (map[string]entities.Price, error)
	ListCatalogPrices(ctx context.Context) ([]entities.CatalogPrice, error)
}

type resolvedSnapshot struct {
	manual         map[string]entities.Price
	catalog        map[string]entities.CatalogPrice
	catalogAliases map[string]string
}

var publicProviderCatalogPrefixes = map[string]string{
	"cx": "openai", "openai": "openai", "ocz": "", "ocg": "",
	"deepseek": "deepseek", "anthropic": "anthropic", "cc": "anthropic",
	"gemini": "google", "xai": "x-ai", "qwen": "qwen",
}

// catalogCandidates maps route-facing public IDs back to canonical catalog
// IDs. A route such as cx/gpt-5.6-luna deliberately keeps its stable public
// name, while pricing follows the original openai/gpt-5.6-luna model.
func catalogCandidates(model string) []string {
	model = strings.TrimSpace(strings.ToLower(model))
	if model == "" {
		return nil
	}
	values := []string{model}
	base := model
	if prefix, rest, ok := strings.Cut(model, "/"); ok {
		if catalogPrefix, known := publicProviderCatalogPrefixes[prefix]; known {
			base = rest
			if catalogPrefix != "" {
				values = append(values, catalogPrefix+"/"+rest)
			}
		}
	}
	family := ""
	switch {
	case strings.HasPrefix(base, "gpt-") || strings.HasPrefix(base, "o1") || strings.HasPrefix(base, "o3") || strings.HasPrefix(base, "o4"):
		family = "openai"
	case strings.HasPrefix(base, "deepseek-"):
		family = "deepseek"
	case strings.HasPrefix(base, "claude-"):
		family = "anthropic"
	case strings.HasPrefix(base, "gemini-"):
		family = "google"
	case strings.HasPrefix(base, "grok-"):
		family = "x-ai"
	case strings.HasPrefix(base, "qwen"):
		family = "qwen"
	}
	values = append(values, base)
	if family != "" {
		values = append(values, family+"/"+base)
	}
	seen := make(map[string]bool, len(values))
	out := values[:0]
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func normalizedModelKey(model string) string {
	model = strings.TrimSpace(strings.ToLower(model))
	parts := strings.FieldsFunc(model, func(char rune) bool {
		return !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9')
	})
	return strings.Join(parts, "-")
}

// catalogAliasIndex maps punctuation-only model-name variants to a catalog ID.
// Providers do not consistently represent version separators (for example,
// "4-8" versus "4.8"). Ambiguous normalized names are deliberately excluded
// so a fuzzy alias can never select an arbitrary vendor or model.
func catalogAliasIndex(catalog map[string]entities.CatalogPrice) map[string]string {
	aliases := make(map[string]string, len(catalog)*2)
	for model := range catalog {
		keys := []string{normalizedModelKey(model)}
		if _, base, ok := strings.Cut(model, "/"); ok {
			keys = append(keys, normalizedModelKey(base))
		}
		for _, key := range keys {
			if key == "" {
				continue
			}
			if existing, found := aliases[key]; found && existing != model {
				aliases[key] = ""
			} else if !found {
				aliases[key] = model
			}
		}
	}
	for key, model := range aliases {
		if model == "" {
			delete(aliases, key)
		}
	}
	return aliases
}

func catalogPrice(snapshot *resolvedSnapshot, model string) (entities.CatalogPrice, bool) {
	candidates := catalogCandidates(model)
	for _, candidate := range candidates {
		if item, ok := snapshot.catalog[candidate]; ok {
			return item, true
		}
	}
	for _, candidate := range candidates {
		if canonical := snapshot.catalogAliases[normalizedModelKey(candidate)]; canonical != "" {
			return snapshot.catalog[canonical], true
		}
	}
	return entities.CatalogPrice{}, false
}

// Resolver serves immutable snapshots with lock-free reads. Refresh builds a
// complete new snapshot before the atomic swap, so requests never see a partly
// synchronized catalog.
type Resolver struct {
	repo     ResolverRepository
	snapshot atomic.Pointer[resolvedSnapshot]
	redis    redis.UniversalClient
	redisMu  sync.RWMutex
}

func NewResolver(repo ResolverRepository) *Resolver {
	r := &Resolver{repo: repo}
	r.snapshot.Store(&resolvedSnapshot{manual: map[string]entities.Price{}, catalog: map[string]entities.CatalogPrice{}, catalogAliases: map[string]string{}})
	return r
}

// EnableRedisInvalidation keeps immutable in-process read snapshots coherent
// across replicas. PostgreSQL or ClickHouse remains the source of truth; Redis
// carries only an invalidation signal.
func (r *Resolver) EnableRedisInvalidation(ctx context.Context, client redis.UniversalClient) error {
	subscriber := client.Subscribe(ctx, priceInvalidationChannel)
	if _, err := subscriber.Receive(ctx); err != nil {
		_ = subscriber.Close()
		return err
	}
	r.redisMu.Lock()
	r.redis = client
	r.redisMu.Unlock()
	go func() {
		defer subscriber.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-subscriber.Channel():
				if !ok {
					return
				}
				_ = r.Refresh(context.Background())
			}
		}
	}()
	return nil
}

func (r *Resolver) NotifyChange(ctx context.Context) {
	r.redisMu.RLock()
	client := r.redis
	r.redisMu.RUnlock()
	if client != nil {
		_ = client.Publish(ctx, priceInvalidationChannel, "refresh").Err()
	}
}

func (r *Resolver) Refresh(ctx context.Context) error {
	manual, err := r.repo.ListPrices(ctx)
	if err != nil {
		return err
	}
	items, err := r.repo.ListCatalogPrices(ctx)
	if err != nil {
		return err
	}
	catalog := make(map[string]entities.CatalogPrice, len(items))
	for _, item := range items {
		model := strings.TrimSpace(strings.ToLower(item.Model))
		if model == "" {
			continue
		}
		item.Model = model
		catalog[model] = item
	}
	r.snapshot.Store(&resolvedSnapshot{manual: manual, catalog: catalog, catalogAliases: catalogAliasIndex(catalog)})
	return nil
}

// SetManual and DeleteManual keep dashboard price edits immediately visible
// without waiting for the next catalog refresh.
func (r *Resolver) SetManual(model string, price entities.Price) {
	for {
		old := r.snapshot.Load()
		manual := make(map[string]entities.Price, len(old.manual)+1)
		for name, current := range old.manual {
			manual[name] = current
		}
		manual[model] = price
		next := &resolvedSnapshot{manual: manual, catalog: old.catalog, catalogAliases: old.catalogAliases}
		if r.snapshot.CompareAndSwap(old, next) {
			r.NotifyChange(context.Background())
			return
		}
	}
}

func (r *Resolver) DeleteManual(model string) {
	for {
		old := r.snapshot.Load()
		manual := make(map[string]entities.Price, len(old.manual))
		for name, current := range old.manual {
			if name != model {
				manual[name] = current
			}
		}
		next := &resolvedSnapshot{manual: manual, catalog: old.catalog, catalogAliases: old.catalogAliases}
		if r.snapshot.CompareAndSwap(old, next) {
			r.NotifyChange(context.Background())
			return
		}
	}
}

// Resolve uses a manually configured alias price first, then its catalog
// price, and finally the upstream model catalog price.
func (r *Resolver) Resolve(model, upstreamModel string) (entities.Price, bool) {
	s := r.snapshot.Load()
	if price, ok := s.manual[model]; ok {
		return price, true
	}
	if item, ok := catalogPrice(s, model); ok {
		return item.Price, true
	}
	if price, ok := s.manual[upstreamModel]; ok {
		return price, true
	}
	if item, ok := catalogPrice(s, upstreamModel); ok {
		return item.Price, true
	}
	return entities.Price{}, true
}

func (r *Resolver) Catalog(model, upstreamModel string) (entities.CatalogPrice, bool) {
	s := r.snapshot.Load()
	if item, ok := catalogPrice(s, model); ok {
		return item, true
	}
	item, ok := catalogPrice(s, upstreamModel)
	return item, ok
}

func (r *Resolver) CatalogPrices() []entities.CatalogPrice {
	s := r.snapshot.Load()
	items := make([]entities.CatalogPrice, 0, len(s.catalog))
	for _, item := range s.catalog {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Model < items[j].Model })
	return items
}

func (r *Resolver) Estimates(model, upstreamModel string, promptTokens, completionTokens int64) entities.PriceEstimates {
	// Select rate and cache capability from one immutable snapshot. This keeps
	// estimates internally consistent if an hourly refresh swaps concurrently.
	s := r.snapshot.Load()
	if price, ok := s.manual[model]; ok {
		return entities.EstimateCosts(&price, promptTokens, completionTokens, price.CachedInputPerM > 0 || price.CacheWritePerM > 0)
	}
	if item, ok := catalogPrice(s, model); ok {
		return entities.EstimateCosts(&item.Price, promptTokens, completionTokens, item.CacheSupported)
	}
	if price, ok := s.manual[upstreamModel]; ok {
		return entities.EstimateCosts(&price, promptTokens, completionTokens, price.CachedInputPerM > 0 || price.CacheWritePerM > 0)
	}
	if item, ok := catalogPrice(s, upstreamModel); ok {
		return entities.EstimateCosts(&item.Price, promptTokens, completionTokens, item.CacheSupported)
	}
	free := entities.Price{}
	return entities.EstimateCosts(&free, promptTokens, completionTokens, true)
}
