package promptcache

import (
	"context"
	"encoding/json"

	"time"

	"github.com/redis/go-redis/v9"

	"github.com/kimnt93/gorouter/pkg/chat"
)

const redisKeyPrefix = "nr:pcache:"
const redisStatsPrefix = "nr:pstats:"

// Redis is a shared, multi-node prompt cache. Entries live under a scope key so
// tenants never read each other's cached responses; stats counters are atomic.
type Redis struct {
	rdb  *redis.Client
	cfg  Config
	stop chan struct{}
}

func NewRedis(rdb *redis.Client, cfg Config) *Redis {
	if cfg.TTL <= 0 {
		cfg.TTL = time.Hour
	}
	if cfg.MaxEntryBytes <= 0 {
		cfg.MaxEntryBytes = 1 << 20
	}
	return &Redis{rdb: rdb, cfg: cfg, stop: make(chan struct{})}
}

func (r *Redis) Close() { close(r.stop) }

type storedEntry struct {
	Status      int    `json:"status"`
	ContentType string `json:"content_type"`
	Body        []byte `json:"body"`
	Stream      bool   `json:"stream"`
	PromptTok   int64  `json:"prompt_tok"`
	Completion  int64  `json:"completion"`
}

func (r *Redis) Lookup(apiKeyID, tenantID, model string, body []byte) (*chat.CacheEntry, bool) {
	key := BuildKey(ScopeID(r.cfg.Scope, apiKeyID, tenantID), model, body)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	raw, err := r.rdb.Get(ctx, redisKeyPrefix+key).Bytes()
	if err != nil {
		r.bump(ctx, "misses")
		return nil, false
	}
	var se storedEntry
	if err := json.Unmarshal(raw, &se); err != nil {
		r.bump(ctx, "misses")
		return nil, false
	}
	r.bump(ctx, "hits")
	return &chat.CacheEntry{
		Status: se.Status, ContentType: se.ContentType, Body: se.Body, Stream: se.Stream,
		PromptTok: se.PromptTok, Completion: se.Completion,
	}, true
}

func (r *Redis) Store(apiKeyID, tenantID, model string, body []byte, e *chat.CacheEntry) bool {
	if e == nil || len(e.Body) > r.cfg.MaxEntryBytes {
		return false
	}
	raw, err := json.Marshal(storedEntry{
		Status: e.Status, ContentType: e.ContentType, Body: e.Body, Stream: e.Stream,
		PromptTok: e.PromptTok, Completion: e.Completion,
	})
	if err != nil {
		return false
	}
	key := BuildKey(ScopeID(r.cfg.Scope, apiKeyID, tenantID), model, body)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := r.rdb.Set(ctx, redisKeyPrefix+key, raw, r.cfg.TTL).Err(); err != nil {
		return false
	}
	r.bump(ctx, "stores")
	return true
}

func (r *Redis) Flush() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var cursor uint64
	for {
		keys, next, err := r.rdb.Scan(ctx, cursor, redisKeyPrefix+"*", 500).Result()
		if err != nil || len(keys) == 0 && next == 0 {
			break
		}
		if len(keys) > 0 {
			_ = r.rdb.Del(ctx, keys...).Err()
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	for _, s := range []string{"hits", "misses", "stores"} {
		_ = r.rdb.Del(ctx, redisStatsPrefix+s).Err()
	}
}

type statLine struct {
	Hits      uint64 `json:"hits"`
	Misses    uint64 `json:"misses"`
	Stores    uint64 `json:"stores"`
	Evictions uint64 `json:"evictions"`
}

func (r *Redis) Stats() chat.CacheStats {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var st statLine
	get := func(name string) int64 {
		v, err := r.rdb.Get(ctx, redisStatsPrefix+name).Int64()
		if err != nil {
			return 0
		}
		return v
	}
	st.Hits = uint64(get("hits"))
	st.Misses = uint64(get("misses"))
	st.Stores = uint64(get("stores"))
	var ratio float64
	if total := st.Hits + st.Misses; total > 0 {
		ratio = float64(st.Hits) / float64(total)
	}
	return chat.CacheStats{Hits: st.Hits, Misses: st.Misses, Stores: st.Stores, Evictions: 0, HitRatio: ratio, Entries: -1, Bytes: -1}
}

func (r *Redis) bump(ctx context.Context, name string) {
	pipe := r.rdb.Pipeline()
	pipe.Incr(ctx, redisStatsPrefix+name)
	pipe.Expire(ctx, redisStatsPrefix+name, 7*24*time.Hour)
	_, _ = pipe.Exec(ctx)
}

var _ chat.PromptCache = (*Redis)(nil)
var _ chat.PromptCache = (*Memory)(nil)
