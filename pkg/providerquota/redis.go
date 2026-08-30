package providerquota

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const quotaStatePrefix = "gorouter:provider-quota:"

type RedisState struct{ client redis.UniversalClient }

func NewRedisState(client redis.UniversalClient) *RedisState { return &RedisState{client: client} }

func (r *RedisState) Snapshot(ctx context.Context, id string) (Snapshot, bool, error) {
	raw, err := r.client.Get(ctx, quotaStatePrefix+"snapshot:"+id).Bytes()
	if err == redis.Nil {
		return Snapshot{}, false, nil
	}
	if err != nil {
		return Snapshot{}, false, err
	}
	var snapshot Snapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return Snapshot{}, false, err
	}
	return snapshot, true, nil
}

func (r *RedisState) PutSnapshot(ctx context.Context, snapshot Snapshot) error {
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, quotaStatePrefix+"snapshot:"+snapshot.CredentialID, raw, 0).Err()
}

func (r *RedisState) ExhaustedUntil(ctx context.Context, id string) (time.Time, error) {
	value, err := r.client.Get(ctx, quotaStatePrefix+"exhausted:"+id).Int64()
	if err == redis.Nil {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(value, 0), nil
}

func (r *RedisState) MarkExhausted(ctx context.Context, id string, until time.Time) error {
	ttl := time.Until(until)
	if ttl <= 0 {
		return nil
	}
	return r.client.Set(ctx, quotaStatePrefix+"exhausted:"+id, strconv.FormatInt(until.Unix(), 10), ttl).Err()
}

func (r *RedisState) ClearExhausted(ctx context.Context, id string) error {
	return r.client.Del(ctx, quotaStatePrefix+"exhausted:"+id).Err()
}

func (r *RedisState) ActiveCredential(ctx context.Context, provider string) (string, error) {
	value, err := r.client.Get(ctx, quotaStatePrefix+"active:"+provider).Result()
	if err == redis.Nil {
		return "", nil
	}
	return value, err
}

var markActiveScript = redis.NewScript(`
if redis.call("EXISTS", KEYS[1]) == 1 then
  return 0
end
redis.call("SET", KEYS[2], ARGV[1])
return 1
`)

// MarkActive atomically rejects stale successes for an account another replica
// has already exhausted. This prevents a slower in-flight request from moving
// the shared cursor backwards after failover selected a new account.
func (r *RedisState) MarkActive(ctx context.Context, provider, id string) (bool, error) {
	result, err := markActiveScript.Run(ctx, r.client, []string{
		quotaStatePrefix + "exhausted:" + id,
		quotaStatePrefix + "active:" + provider,
	}, id).Int64()
	return result == 1, err
}
