package pricing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

const (
	SourceOpenRouter         = "openrouter"
	SourceLiteLLM            = "litellm"
	DefaultOpenRouterCatalog = "https://openrouter.ai/api/frontend/v1/catalog/models"
)

// HTTPImporter fetches a public catalog. OpenRouter's frontend endpoint does
// not require an API key and no application credential is sent to it.
type HTTPImporter struct {
	Client *http.Client
	URL    string
	Source string
}

type decimal float64

func (d *decimal) UnmarshalJSON(body []byte) error {
	if string(body) == "null" || string(body) == `""` {
		*d = 0
		return nil
	}
	var text string
	if len(body) > 0 && body[0] == '"' {
		if err := json.Unmarshal(body, &text); err != nil {
			return err
		}
	} else {
		text = string(body)
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return errors.New("invalid per-token price")
	}
	*d = decimal(value)
	return nil
}

type openRouterDocument struct {
	Data []openRouterModel `json:"data"`
}

type openRouterModel struct {
	Slug          string             `json:"slug"`
	Name          string             `json:"name"`
	ContextLength int                `json:"context_length"`
	Endpoint      openRouterEndpoint `json:"endpoint"`
	ID            string             `json:"id"`      // /api/v1/models compatibility
	Pricing       openRouterPricing  `json:"pricing"` // /api/v1/models compatibility
}

type openRouterEndpoint struct {
	Variant          string            `json:"variant"`
	ModelVariantSlug string            `json:"model_variant_slug"`
	ProviderName     string            `json:"provider_name"`
	ContextLength    int               `json:"context_length"`
	Pricing          openRouterPricing `json:"pricing"`
}

type openRouterPricing struct {
	Prompt      decimal `json:"prompt"`
	Completion  decimal `json:"completion"`
	CacheRead   decimal `json:"input_cache_read"`
	CacheWrite  decimal `json:"input_cache_write"`
	LegacyRead  decimal `json:"cache_read"`
	LegacyWrite decimal `json:"cache_write"`
}

type liteLLMPrice struct {
	Input       decimal `json:"input_cost_per_token"`
	Output      decimal `json:"output_cost_per_token"`
	CacheRead   decimal `json:"cache_read_input_token_cost"`
	CacheCreate decimal `json:"cache_creation_input_token_cost"`
}

// ImportCatalog returns one compact record per canonical model. When the
// frontend response contains several variants, standard always wins. If no
// standard row exists, the first usable variant is retained.
func (i *HTTPImporter) ImportCatalog(ctx context.Context) ([]entities.CatalogPrice, error) {
	document, err := i.fetch(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	switch strings.ToLower(i.Source) {
	case SourceOpenRouter:
		var parsed openRouterDocument
		if err := json.Unmarshal(document, &parsed); err != nil {
			return nil, fmt.Errorf("decode OpenRouter prices: %w", err)
		}
		type selected struct {
			priority int
			entry    entities.CatalogPrice
		}
		byModel := make(map[string]selected, len(parsed.Data))
		for _, model := range parsed.Data {
			slug := strings.TrimSpace(model.Slug)
			pricing := model.Endpoint.Pricing
			provider := model.Endpoint.ProviderName
			contextLength := model.Endpoint.ContextLength
			variant := model.Endpoint.Variant
			if slug == "" {
				slug, pricing = strings.TrimSpace(model.ID), model.Pricing
			}
			if slug == "" {
				continue
			}
			priority := 1
			if variant == "standard" || model.Endpoint.ModelVariantSlug == slug {
				priority = 2
			}
			if old, exists := byModel[slug]; exists && old.priority >= priority {
				continue
			}
			read, write := pricing.CacheRead, pricing.CacheWrite
			if read == 0 {
				read = pricing.LegacyRead
			}
			if write == 0 {
				write = pricing.LegacyWrite
			}
			if contextLength == 0 {
				contextLength = model.ContextLength
			}
			byModel[slug] = selected{priority: priority, entry: entities.CatalogPrice{
				Model: slug, Name: model.Name, Provider: provider, ContextLength: contextLength,
				CacheSupported: read > 0 || write > 0,
				Price:          perMillion(pricing.Prompt, pricing.Completion, read, write),
				Source:         SourceOpenRouter, UpdatedAt: now,
			}}
		}
		out := make([]entities.CatalogPrice, 0, len(byModel))
		for _, item := range byModel {
			out = append(out, item.entry)
		}
		return out, nil
	case SourceLiteLLM:
		var parsed map[string]liteLLMPrice
		if err := json.Unmarshal(document, &parsed); err != nil {
			return nil, fmt.Errorf("decode LiteLLM prices: %w", err)
		}
		out := make([]entities.CatalogPrice, 0, len(parsed))
		for model, price := range parsed {
			if model != "" {
				out = append(out, entities.CatalogPrice{Model: model, Price: perMillion(price.Input, price.Output, price.CacheRead, price.CacheCreate), CacheSupported: price.CacheRead > 0 || price.CacheCreate > 0, Source: SourceLiteLLM, UpdatedAt: now})
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported price source %q", i.Source)
	}
}

// Import keeps the original rate-only interface available for existing users.
func (i *HTTPImporter) Import(ctx context.Context) (map[string]entities.Price, error) {
	entries, err := i.ImportCatalog(ctx)
	if err != nil {
		return nil, err
	}
	prices := make(map[string]entities.Price, len(entries))
	for _, entry := range entries {
		prices[entry.Model] = entry.Price
	}
	return prices, nil
}

func (i *HTTPImporter) fetch(ctx context.Context) ([]byte, error) {
	if strings.TrimSpace(i.URL) == "" {
		return nil, errors.New("price import URL is required")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, i.URL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	client := i.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch price catalog: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return nil, fmt.Errorf("price catalog returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("read price catalog: %w", err)
	}
	return body, nil
}

func perMillion(input, output, cacheRead, cacheWrite decimal) entities.Price {
	return entities.Price{InputPerM: float64(input) * 1e6, OutputPerM: float64(output) * 1e6,
		CachedInputPerM: float64(cacheRead) * 1e6, CacheWritePerM: float64(cacheWrite) * 1e6}
}

var (
	_ entities.PriceImporter   = (*HTTPImporter)(nil)
	_ entities.CatalogImporter = (*HTTPImporter)(nil)
)
