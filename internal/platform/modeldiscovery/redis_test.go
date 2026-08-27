package modeldiscovery

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/kimnt93/gorouter/pkg/credential"
)

func TestRedisCatalogIsCredentialScopedExpiresAndDeletes(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	cache := NewRedis(client)
	ctx := context.Background()

	if err := cache.Set(ctx, "one", []credential.ProviderModel{{ID: "model-one"}}, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := cache.Get(ctx, "two"); err != nil || ok {
		t.Fatalf("other credential cache: ok=%v err=%v", ok, err)
	}
	models, ok, err := cache.Get(ctx, "one")
	if err != nil || !ok || len(models) != 1 || models[0].ID != "model-one" {
		t.Fatalf("cached models=%+v ok=%v err=%v", models, ok, err)
	}
	server.FastForward(time.Minute)
	if _, ok, err := cache.Get(ctx, "one"); err != nil || ok {
		t.Fatalf("expired cache: ok=%v err=%v", ok, err)
	}
	if err := cache.Set(ctx, "one", []credential.ProviderModel{{ID: "model-one"}}, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := cache.Delete(ctx, "one"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := cache.Get(ctx, "one"); err != nil || ok {
		t.Fatalf("deleted cache: ok=%v err=%v", ok, err)
	}
}
