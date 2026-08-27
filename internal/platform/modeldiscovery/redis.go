package modeldiscovery

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/kimnt93/gorouter/pkg/credential"
)

const maxCatalogBytes = 16 << 20

type Redis struct {
	client redis.UniversalClient
}

func NewRedis(client redis.UniversalClient) *Redis { return &Redis{client: client} }

func key(credentialID string) string { return "gorouter:model-discovery:" + credentialID }

func (r *Redis) Get(ctx context.Context, credentialID string) ([]credential.ProviderModel, bool, error) {
	value, err := r.client.Get(ctx, key(credentialID)).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if len(value) > maxCatalogBytes {
		return nil, false, fmt.Errorf("cached model catalog exceeds size limit")
	}
	var models []credential.ProviderModel
	if err := json.Unmarshal(value, &models); err != nil {
		return nil, false, err
	}
	return models, true, nil
}

func (r *Redis) Set(ctx context.Context, credentialID string, models []credential.ProviderModel, ttl time.Duration) error {
	value, err := json.Marshal(models)
	if err != nil {
		return err
	}
	if len(value) > maxCatalogBytes {
		return fmt.Errorf("model catalog exceeds size limit")
	}
	return r.client.Set(ctx, key(credentialID), value, ttl).Err()
}

func (r *Redis) Delete(ctx context.Context, credentialID string) error {
	return r.client.Del(ctx, key(credentialID)).Err()
}
