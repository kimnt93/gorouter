package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/kimnt93/gorouter/pkg/entities"
)

type priceCatalogStub struct {
	prices  map[string]entities.Price
	catalog []entities.CatalogPrice
}

func (s *priceCatalogStub) Resolve(model, upstreamModel string) (entities.Price, bool) {
	for _, name := range []string{model, upstreamModel} {
		if price, ok := s.prices[name]; ok {
			return price, true
		}
	}
	return entities.Price{}, false
}

func (s *priceCatalogStub) Estimates(model, upstreamModel string, promptTokens, completionTokens int64) entities.PriceEstimates {
	price, ok := s.Resolve(model, upstreamModel)
	if !ok {
		return entities.PriceEstimates{}
	}
	catalog, found := s.Catalog(model, upstreamModel)
	return entities.EstimateCosts(&price, promptTokens, completionTokens, found && catalog.CacheSupported)
}

func (s *priceCatalogStub) Catalog(model, upstreamModel string) (entities.CatalogPrice, bool) {
	for _, name := range []string{model, upstreamModel} {
		for _, item := range s.catalog {
			if item.Model == name {
				return item, true
			}
		}
	}
	return entities.CatalogPrice{}, false
}

func (s *priceCatalogStub) CatalogPrices() []entities.CatalogPrice {
	return append([]entities.CatalogPrice(nil), s.catalog...)
}

func TestPricingCatalogFiltersAndPaginates(t *testing.T) {
	updated := time.Date(2026, time.August, 26, 1, 2, 3, 0, time.UTC)
	catalog := &priceCatalogStub{catalog: []entities.CatalogPrice{
		{Model: "openai/gpt-mini", Name: "GPT Mini", Provider: "OpenAI", Source: "openrouter", UpdatedAt: updated},
		{Model: "anthropic/claude-haiku", Name: "Claude Haiku", Provider: "Anthropic", Source: "openrouter", UpdatedAt: updated},
		{Model: "openai/gpt-nano", Name: "GPT Nano", Provider: "OpenAI", Source: "openrouter", UpdatedAt: updated},
	}}
	app := fiber.New()
	app.Get("/catalog", (&Admin{Pricing: catalog}).PricingCatalog)

	response, err := app.Test(httptest.NewRequest("GET", "/catalog?q=OPENAI&offset=1&limit=1", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var body struct {
		Data   []entities.CatalogPrice `json:"data"`
		Total  int                     `json:"total"`
		Offset int                     `json:"offset"`
		Limit  int                     `json:"limit"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Total != 2 || body.Offset != 1 || body.Limit != 1 || len(body.Data) != 1 || body.Data[0].Model != "openai/gpt-nano" {
		t.Fatalf("unexpected catalog response: %+v", body)
	}
}

func TestPricingEstimateReturnsDerivedCacheCosts(t *testing.T) {
	price := entities.Price{InputPerM: 2, OutputPerM: 8, CachedInputPerM: 0.5, CacheWritePerM: 2.5}
	catalog := &priceCatalogStub{
		prices:  map[string]entities.Price{"openai/gpt-mini": price},
		catalog: []entities.CatalogPrice{{Model: "openai/gpt-mini", CacheSupported: true, Price: price}},
	}
	app := fiber.New()
	app.Get("/estimate", (&Admin{Pricing: catalog}).PricingEstimate)

	response, err := app.Test(httptest.NewRequest("GET", "/estimate?model=alias&upstream_model=openai%2Fgpt-mini&prompt_tokens=100000&completion_tokens=10000", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var body priceEstimateResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Model != "alias" || body.UpstreamModel != "openai/gpt-mini" || body.Price == nil || !body.CacheSupported {
		t.Fatalf("unexpected estimate metadata: %+v", body)
	}
	if body.Estimates.WithoutCache.USD != 0.28 || body.Estimates.WithCache.USD != 0.13 {
		t.Fatalf("unexpected derived estimates: %+v", body.Estimates)
	}
	if !body.Estimates.WithoutCache.Priced || !body.Estimates.WithCache.Priced {
		t.Fatalf("priced flags were not set: %+v", body.Estimates)
	}
}

func TestPricingEstimateWithoutCacheRateUsesRegularInputPrice(t *testing.T) {
	price := entities.Price{InputPerM: 2, OutputPerM: 8}
	catalog := &priceCatalogStub{prices: map[string]entities.Price{"plain-model": price}}
	app := fiber.New()
	app.Get("/estimate", (&Admin{Pricing: catalog}).PricingEstimate)

	response, err := app.Test(httptest.NewRequest("GET", "/estimate?model=plain-model&prompt_tokens=100000&completion_tokens=10000", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body priceEstimateResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.CacheSupported || body.Estimates.WithCache.USD != body.Estimates.WithoutCache.USD || body.Estimates.WithCache.USD != 0.28 {
		t.Fatalf("unexpected no-cache estimate: %+v", body)
	}
}

func TestPricingEndpointsValidateInputsAndAvailability(t *testing.T) {
	tests := []struct {
		name    string
		handler fiber.Handler
		target  string
		status  int
	}{
		{"catalog unavailable", (&Admin{}).PricingCatalog, "/catalog", fiber.StatusServiceUnavailable},
		{"estimate unavailable", (&Admin{}).PricingEstimate, "/estimate?model=x", fiber.StatusServiceUnavailable},
		{"model required", (&Admin{Pricing: &priceCatalogStub{}}).PricingEstimate, "/estimate", fiber.StatusBadRequest},
		{"negative prompt", (&Admin{Pricing: &priceCatalogStub{}}).PricingEstimate, "/estimate?model=x&prompt_tokens=-1", fiber.StatusBadRequest},
		{"invalid completion", (&Admin{Pricing: &priceCatalogStub{}}).PricingEstimate, "/estimate?model=x&completion_tokens=nope", fiber.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := fiber.New()
			app.Get("/catalog", test.handler)
			app.Get("/estimate", test.handler)
			response, err := app.Test(httptest.NewRequest("GET", test.target, nil))
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != test.status {
				t.Fatalf("status = %d, want %d", response.StatusCode, test.status)
			}
		})
	}
}
