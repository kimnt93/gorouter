package clickhouse

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrMutationLockUnavailable = errors.New("clickhouse configuration mutation lock unavailable")

type RedisMutationLocker struct {
	client redis.UniversalClient
	ttl    time.Duration
}

func NewRedisMutationLocker(client redis.UniversalClient, ttl time.Duration) (*RedisMutationLocker, error) {
	if client == nil || ttl <= 0 {
		return nil, ErrMutationLockUnavailable
	}
	return &RedisMutationLocker{client: client, ttl: ttl}, nil
}

func (l *RedisMutationLocker) WithLock(ctx context.Context, key string, fn func() error) error {
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return err
	}
	token := hex.EncodeToString(tokenBytes)
	lockKey := "gorouter:config-lock:" + key
	ok, err := l.client.SetNX(ctx, lockKey, token, l.ttl).Result()
	if err != nil || !ok {
		return ErrMutationLockUnavailable
	}
	defer l.client.Eval(ctx, `if redis.call("get",KEYS[1]) == ARGV[1] then return redis.call("del",KEYS[1]) else return 0 end`, []string{lockKey}, token)
	return fn()
}

var _ MutationLocker = (*RedisMutationLocker)(nil)
