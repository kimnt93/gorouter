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

var syncAccountRingScript = redis.NewScript(`
redis.call("DEL", KEYS[1])
if #ARGV > 0 then
  redis.call("RPUSH", KEYS[1], unpack(ARGV))
end
local checkpoint = redis.call("GET", KEYS[2])
local present = false
for _, id in ipairs(ARGV) do
  if id == checkpoint then
    present = true
    break
  end
end
if not present then
  if #ARGV == 0 then
    redis.call("DEL", KEYS[2])
  else
    redis.call("SET", KEYS[2], ARGV[1])
  end
end
return 1
`)

func (r *RedisState) SyncAccountRing(ctx context.Context, provider string, credentialIDs []string) error {
	args := make([]any, len(credentialIDs))
	for index, id := range credentialIDs {
		args[index] = id
	}
	return syncAccountRingScript.Run(ctx, r.client, []string{
		quotaStatePrefix + "ring:" + provider,
		quotaStatePrefix + "active:" + provider,
	}, args...).Err()
}

var alignAccountScript = redis.NewScript(`
local eligible = {}
for _, id in ipairs(ARGV) do
  eligible[id] = true
end
local current = redis.call("GET", KEYS[2])
if eligible[current] then
  return current
end
local ring = redis.call("LRANGE", KEYS[1], 0, -1)
if #ring == 0 then
  return ""
end
local start = 1
for index, id in ipairs(ring) do
  if id == current then
    start = index
    break
  end
end
for offset = 1, #ring do
  local nextIndex = ((start - 1 + offset) % #ring) + 1
  if eligible[ring[nextIndex]] then
    redis.call("SET", KEYS[2], ring[nextIndex])
    return ring[nextIndex]
  end
end
return ""
`)

func (r *RedisState) AlignAccount(ctx context.Context, provider string, eligible []string) error {
	args := make([]any, len(eligible))
	for index, id := range eligible {
		args[index] = id
	}
	return alignAccountScript.Run(ctx, r.client, []string{
		quotaStatePrefix + "ring:" + provider,
		quotaStatePrefix + "active:" + provider,
	}, args...).Err()
}

func (r *RedisState) AccountRing(ctx context.Context, provider string) ([]string, string, error) {
	pipe := r.client.Pipeline()
	ringCommand := pipe.LRange(ctx, quotaStatePrefix+"ring:"+provider, 0, -1)
	checkpointCommand := pipe.Get(ctx, quotaStatePrefix+"active:"+provider)
	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		return nil, "", err
	}
	checkpoint, checkpointErr := checkpointCommand.Result()
	if checkpointErr == redis.Nil {
		checkpoint = ""
	} else if checkpointErr != nil {
		return nil, "", checkpointErr
	}
	return ringCommand.Val(), checkpoint, nil
}

var advanceAccountScript = redis.NewScript(`
local current = redis.call("GET", KEYS[2])
if current ~= ARGV[1] then
  return 0
end
local eligible = {}
for index = 2, #ARGV do
  eligible[ARGV[index]] = true
end
local ring = redis.call("LRANGE", KEYS[1], 0, -1)
for index, id in ipairs(ring) do
  if id == ARGV[1] then
    for offset = 1, #ring do
      local nextIndex = ((index - 1 + offset) % #ring) + 1
      if eligible[ring[nextIndex]] then
        redis.call("SET", KEYS[2], ring[nextIndex])
        return 1
      end
    end
  end
end
return 0
`)

func (r *RedisState) AdvanceAccount(ctx context.Context, provider, credentialID string, eligible []string) error {
	args := make([]any, 0, 1+len(eligible))
	args = append(args, credentialID)
	for _, id := range eligible {
		args = append(args, id)
	}
	return advanceAccountScript.Run(ctx, r.client, []string{
		quotaStatePrefix + "ring:" + provider,
		quotaStatePrefix + "active:" + provider,
	}, args...).Err()
}
