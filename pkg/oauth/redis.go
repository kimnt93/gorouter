package oauth

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

const redisFlowPrefix = "gorouter:oauth-flow:"

type RedisFlowStore struct {
	client redis.UniversalClient
}

func NewRedisFlowStore(client redis.UniversalClient) *RedisFlowStore {
	return &RedisFlowStore{client: client}
}

type storedFlow struct {
	Provider       string            `json:"provider"`
	State          string            `json:"state"`
	Verifier       string            `json:"verifier"`
	RedirectURI    string            `json:"redirect_uri"`
	SessionBinding string            `json:"session_binding"`
	Expires        time.Time         `json:"expires"`
	FlowType       string            `json:"flow_type"`
	DeviceCode     string            `json:"device_code"`
	Interval       int               `json:"interval"`
	Extra          map[string]string `json:"extra"`
}

func encodeFlow(value flow) storedFlow {
	return storedFlow{Provider: value.provider, State: value.state, Verifier: value.verifier, RedirectURI: value.redirectURI, SessionBinding: value.sessionBinding, Expires: value.expires, FlowType: value.flowType, DeviceCode: value.deviceCode, Interval: value.interval, Extra: value.extra}
}

func decodeFlow(value storedFlow) flow {
	return flow{provider: value.Provider, state: value.State, verifier: value.Verifier, redirectURI: value.RedirectURI, sessionBinding: value.SessionBinding, expires: value.Expires, flowType: value.FlowType, deviceCode: value.DeviceCode, interval: value.Interval, extra: value.Extra}
}

func (s *RedisFlowStore) Put(ctx context.Context, id string, value flow, ttl time.Duration) error {
	encoded, err := json.Marshal(encodeFlow(value))
	if err != nil {
		return err
	}
	return s.client.Set(ctx, redisFlowPrefix+id, encoded, ttl).Err()
}

func (s *RedisFlowStore) Get(ctx context.Context, id string) (flow, bool, error) {
	raw, err := s.client.Get(ctx, redisFlowPrefix+id).Bytes()
	if err == redis.Nil {
		return flow{}, false, nil
	}
	if err != nil {
		return flow{}, false, err
	}
	var value storedFlow
	if err := json.Unmarshal(raw, &value); err != nil {
		return flow{}, false, err
	}
	return decodeFlow(value), true, nil
}

func (s *RedisFlowStore) Delete(ctx context.Context, id string) error {
	return s.client.Del(ctx, redisFlowPrefix+id).Err()
}

func (s *RedisFlowStore) Claim(ctx context.Context, id string, ttl time.Duration) (bool, error) {
	return s.client.SetNX(ctx, redisFlowPrefix+"claim:"+id, "1", ttl).Result()
}

func (s *RedisFlowStore) Release(ctx context.Context, id string) error {
	return s.client.Del(ctx, redisFlowPrefix+"claim:"+id).Err()
}
