package apikey

import (
	"context"
	"encoding/json"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/redis/go-redis/v9"
)

const tokenCachePrefix = "gorouter:api-token:"

type tokenCache struct {
	rdb redis.UniversalClient
	ttl time.Duration
}

type cachedKey struct {
	Key  entities.ApiKey `json:"key"`
	Hash string          `json:"hash"`
}

func (s *Service) SetTokenCache(rdb redis.UniversalClient, ttl time.Duration) {
	if rdb != nil && ttl > 0 {
		s.cache = &tokenCache{rdb: rdb, ttl: ttl}
	}
}

func (c *tokenCache) get(ctx context.Context, hash string) (*entities.ApiKey, bool) {
	b, err := c.rdb.Get(ctx, tokenCachePrefix+"hash:"+hash).Bytes()
	if err != nil {
		return nil, false
	}
	var value cachedKey
	if json.Unmarshal(b, &value) != nil {
		return nil, false
	}
	pipe := c.rdb.Pipeline()
	pipe.Expire(ctx, tokenCachePrefix+"hash:"+hash, c.ttl)
	pipe.Expire(ctx, tokenCachePrefix+"id:"+value.Key.ID, c.ttl)
	_, _ = pipe.Exec(ctx)
	value.Key.SecretHash = hash
	return &value.Key, true
}

func (c *tokenCache) put(ctx context.Context, key *entities.ApiKey) {
	if key == nil || key.SecretHash == "" {
		return
	}
	b, err := json.Marshal(cachedKey{Key: *key, Hash: key.SecretHash})
	if err != nil {
		return
	}
	pipe := c.rdb.Pipeline()
	pipe.Set(ctx, tokenCachePrefix+"hash:"+key.SecretHash, b, c.ttl)
	pipe.Set(ctx, tokenCachePrefix+"id:"+key.ID, key.SecretHash, c.ttl)
	_, _ = pipe.Exec(ctx)
}

func (c *tokenCache) invalidate(ctx context.Context, id, hash string) {
	if hash == "" && id != "" {
		hash, _ = c.rdb.Get(ctx, tokenCachePrefix+"id:"+id).Result()
	}
	keys := []string{}
	if id != "" {
		keys = append(keys, tokenCachePrefix+"id:"+id)
	}
	if hash != "" {
		keys = append(keys, tokenCachePrefix+"hash:"+hash)
	}
	if len(keys) > 0 {
		_ = c.rdb.Del(ctx, keys...).Err()
	}
}
