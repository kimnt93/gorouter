package refreshlock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrUnavailable = errors.New("distributed refresh lock unavailable")

// Redis ensures only one replica performs a bounded external catalog refresh.
// Contention is a successful skip; Redis errors are returned to the scheduler.
type Redis struct {
	client redis.UniversalClient
	ttl    time.Duration
}

func NewRedis(client redis.UniversalClient, ttl time.Duration) (*Redis, error) {
	if client == nil || ttl <= 0 {
		return nil, ErrUnavailable
	}
	return &Redis{client: client, ttl: ttl}, nil
}

func (l *Redis) WithLock(ctx context.Context, key string, fn func() error) (bool, error) {
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return false, err
	}
	token := hex.EncodeToString(tokenBytes)
	lockKey := "gorouter:refresh-lock:" + key
	acquired, err := l.client.SetNX(ctx, lockKey, token, l.ttl).Result()
	if err != nil {
		return false, ErrUnavailable
	}
	if !acquired {
		return false, nil
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = l.client.Eval(releaseCtx, `if redis.call("get",KEYS[1]) == ARGV[1] then return redis.call("del",KEYS[1]) else return 0 end`, []string{lockKey}, token).Err()
	}()
	return true, fn()
}
