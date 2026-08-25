package promptcache

import (
	"context"
	"errors"

	"github.com/redis/go-redis/v9"

	"github.com/kimnt93/gorouter/pkg/chat"
	"github.com/kimnt93/gorouter/pkg/config"
)

func New(cfg config.CacheConfig, redisURL string) (chat.PromptCache, *redis.Client, error) {
	if !cfg.Enabled {
		return Noop{}, nil, nil
	}
	if redisURL == "" {
		if !cfg.AllowMemory {
			return nil, nil, errors.New("Redis connection settings are required when cache is enabled outside development")
		}
		return NewMemory(Config{TTL: cfg.TTL, Scope: cfg.Scope, MaxEntryBytes: cfg.MaxEntryBytes, MaxTotalBytes: cfg.MaxTotalBytes}), nil, nil
	}
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, nil, err
	}
	client := redis.NewClient(options)
	if err := client.Ping(context.Background()).Err(); err != nil {
		client.Close()
		return nil, nil, err
	}
	return NewRedis(client, Config{TTL: cfg.TTL, Scope: cfg.Scope, MaxEntryBytes: cfg.MaxEntryBytes}), client, nil
}
