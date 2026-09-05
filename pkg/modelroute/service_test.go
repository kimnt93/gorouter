package modelroute

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/kimnt93/gorouter/pkg/chat"
	"github.com/kimnt93/gorouter/pkg/entities"
)

type modelRepositoryStub struct{ model entities.ModelDef }

type priceCacheStub struct {
	setModel     string
	setPrice     entities.Price
	deletedModel string
}

func (c *priceCacheStub) SetManual(model string, price entities.Price) {
	c.setModel, c.setPrice = model, price
}
func (c *priceCacheStub) DeleteManual(model string) { c.deletedModel = model }

func (r *modelRepositoryStub) Upsert(_ context.Context, model entities.ModelDef) error {
	r.model = model
	return nil
}
func (*modelRepositoryStub) Delete(context.Context, string) error                   { return nil }
func (*modelRepositoryStub) List(context.Context) ([]entities.ModelDef, error)      { return nil, nil }
func (*modelRepositoryStub) SetPrice(context.Context, string, entities.Price) error { return nil }
func (*modelRepositoryStub) DeletePrice(context.Context, string) error              { return nil }
func (*modelRepositoryStub) ListPrices(context.Context) (map[string]entities.Price, error) {
	return nil, nil
}

func TestUpsertValidatesModelAndRoutes(t *testing.T) {
	repo := &modelRepositoryStub{}
	service := NewService(repo)
	if err := service.Upsert(context.Background(), entities.ModelDef{Name: " model ", Enabled: true,
		Routes: []entities.ModelRoute{{CredentialID: " credential ", Weight: 1, Enabled: true}}}); err != nil {
		t.Fatal(err)
	}
	if repo.model.Name != "model" || repo.model.Strategy != chat.StrategyPriority || repo.model.Routes[0].CredentialID != "credential" {
		t.Fatalf("model was not normalized: %+v", repo.model)
	}
	if err := service.Upsert(context.Background(), entities.ModelDef{Name: "cache-model", Strategy: chat.StrategyCacheAffinity, Routes: []entities.ModelRoute{{CredentialID: "credential", UpstreamModel: "upstream", Weight: 1}}}); err != nil {
		t.Fatalf("cache affinity strategy rejected: %v", err)
	}
	if err := service.Upsert(context.Background(), entities.ModelDef{Name: "model", Strategy: "random"}); !errors.Is(err, ErrModelStrategy) {
		t.Fatalf("invalid strategy error=%v", err)
	}
	duplicate := []entities.ModelRoute{{CredentialID: "c", UpstreamModel: "same", Weight: 1}, {CredentialID: "c", UpstreamModel: "same", Weight: 1}}
	if err := service.Upsert(context.Background(), entities.ModelDef{Name: "model", Routes: duplicate}); !errors.Is(err, ErrCredentialRoute) {
		t.Fatalf("duplicate route error=%v", err)
	}
	if err := service.Upsert(context.Background(), entities.ModelDef{Name: "blend", Routes: []entities.ModelRoute{{CredentialID: "c", UpstreamModel: "first", Weight: 1}, {CredentialID: "c", UpstreamModel: "second", Weight: 1}}}); err != nil {
		t.Fatalf("same credential with distinct upstream models: %v", err)
	}
}

func TestSetPriceRejectsInvalidNumbers(t *testing.T) {
	service := NewService(&modelRepositoryStub{})
	for _, price := range []entities.Price{{InputPerM: -1}, {OutputPerM: math.NaN()}, {CacheWritePerM: math.Inf(1)}} {
		if err := service.SetPrice(context.Background(), "model", price); !errors.Is(err, ErrInvalidPrice) {
			t.Fatalf("price %+v error=%v", price, err)
		}
	}
}

func TestPriceWritesUpdateConfiguredCache(t *testing.T) {
	service := NewService(&modelRepositoryStub{})
	cache := &priceCacheStub{}
	service.SetPriceCache(cache)
	price := entities.Price{InputPerM: 2}
	if err := service.SetPrice(context.Background(), "model", price); err != nil {
		t.Fatal(err)
	}
	if cache.setModel != "model" || cache.setPrice != price {
		t.Fatalf("cache set = %q %+v", cache.setModel, cache.setPrice)
	}
	if err := service.DeletePrice(context.Background(), "model"); err != nil {
		t.Fatal(err)
	}
	if cache.deletedModel != "model" {
		t.Fatalf("cache delete = %q", cache.deletedModel)
	}
}

func TestUpsertBlendCreatesStaticAutoAlias(t *testing.T) {
	repo := &recordingModelRepository{}
	service := NewService(repo)
	blend := entities.ModelDef{Name: "my-blend", Strategy: chat.StrategyPriority, Enabled: true, Routes: []entities.ModelRoute{{CredentialID: "cred-a", UpstreamModel: "model-a", Weight: 1, Enabled: true}, {CredentialID: "cred-b", UpstreamModel: "model-b", Weight: 1, Enabled: true}}}
	if err := service.Upsert(context.Background(), blend); err != nil {
		t.Fatal(err)
	}
	if len(repo.models) != 2 {
		t.Fatalf("upserts=%+v", repo.models)
	}
	auto := repo.models[1]
	if auto.Name != "my-blend/auto" || auto.UpstreamModel != "auto" || auto.Strategy != chat.StrategyRoundRobin || len(auto.Routes) != 2 {
		t.Fatalf("auto=%+v", auto)
	}
}

type recordingModelRepository struct {
	modelRepositoryStub
	models []entities.ModelDef
}

func (r *recordingModelRepository) Upsert(_ context.Context, model entities.ModelDef) error {
	r.models = append(r.models, model)
	return nil
}
