package modeldiscovery

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
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

func TestRedisCatalogVersionAndMultiNodeIsolation(t *testing.T) {
	server := miniredis.RunT(t)
	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()
	one, two := NewRedis(client), NewRedis(client)
	_ = client.Set(ctx, "gorouter:model-discovery:cred", `[{"id":"adaptive"}]`, time.Minute).Err()
	if _, ok, err := one.Get(ctx, "cred"); err != nil || ok {
		t.Fatal("reused old placeholder catalog")
	}
	if err := one.Set(ctx, "cred", []credential.ProviderModel{{ID: "family", SupportedReasoningLevels: []entities.ModelReasoningLevel{{Effort: "high"}}}}, time.Minute); err != nil {
		t.Fatal(err)
	}
	m, ok, err := two.Get(ctx, "cred")
	if err != nil || !ok || len(m) != 1 || len(m[0].SupportedReasoningLevels) != 1 {
		t.Fatal("cross-node family metadata lost")
	}
	if _, ok, err := two.Get(ctx, "other"); err != nil || ok {
		t.Fatal("cross-credential metadata leak")
	}
	if err := two.Delete(ctx, "cred"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := one.Get(ctx, "cred"); err != nil || ok {
		t.Fatal("cross-node invalidation failed")
	}
}
