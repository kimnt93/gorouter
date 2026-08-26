package entities

import (
	"context"
	"time"
)

type Price struct {
	InputPerM       float64 `json:"input_per_m"`
	OutputPerM      float64 `json:"output_per_m"`
	CachedInputPerM float64 `json:"cached_input_per_m"`
	CacheWritePerM  float64 `json:"cache_write_per_m"`
}

// CatalogPrice deliberately stores only the fields needed for model selection
// and cost estimates. Provider catalog payloads are much larger and volatile.
type CatalogPrice struct {
	Model          string    `json:"model"`
	Name           string    `json:"name,omitempty"`
	Provider       string    `json:"provider,omitempty"`
	ContextLength  int       `json:"context_length,omitempty"`
	CacheSupported bool      `json:"cache_supported"`
	Price          Price     `json:"price"`
	Source         string    `json:"source"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type PriceEstimates struct {
	WithoutCache Cost `json:"without_cache"`
	WithCache    Cost `json:"with_cache"`
}

type TokenUsage struct {
	PromptTokens     int64
	CompletionTokens int64
	CacheReadTokens  int64
	CacheWriteTokens int64
}

// Cost preserves whether a price was available. USD=0 alone cannot distinguish
// genuinely free usage from usage that has not been priced yet.
type Cost struct {
	USD           float64 `json:"usd"`
	InputUSD      float64 `json:"input_usd"`
	OutputUSD     float64 `json:"output_usd"`
	CacheReadUSD  float64 `json:"cache_read_usd"`
	CacheWriteUSD float64 `json:"cache_write_usd"`
	Priced        bool    `json:"priced"`
}

func (u TokenUsage) TotalTokens() int64 {
	return u.PromptTokens + u.CompletionTokens
}

func CalculateCost(p *Price, u TokenUsage) Cost {
	if p == nil {
		return Cost{}
	}
	result := Cost{
		InputUSD:      float64(u.PromptTokens) * p.InputPerM / 1e6,
		OutputUSD:     float64(u.CompletionTokens) * p.OutputPerM / 1e6,
		CacheReadUSD:  float64(u.CacheReadTokens) * p.CachedInputPerM / 1e6,
		CacheWriteUSD: float64(u.CacheWriteTokens) * p.CacheWritePerM / 1e6,
		Priced:        true,
	}
	result.USD = result.InputUSD + result.OutputUSD + result.CacheReadUSD + result.CacheWriteUSD
	return result
}

// EstimateCosts derives both totals from rates at read time, so neither total
// needs to be persisted. Unsupported cache pricing falls back to regular input
// pricing rather than incorrectly presenting cache reads as free.
func EstimateCosts(p *Price, promptTokens, completionTokens int64, cacheSupported bool) PriceEstimates {
	without := CalculateCost(p, TokenUsage{PromptTokens: promptTokens, CompletionTokens: completionTokens})
	if p == nil {
		return PriceEstimates{}
	}
	cacheRate := p.CachedInputPerM
	if !cacheSupported {
		cacheRate = p.InputPerM
	}
	with := Cost{InputUSD: float64(promptTokens) * cacheRate / 1e6, OutputUSD: float64(completionTokens) * p.OutputPerM / 1e6, Priced: true}
	with.USD = with.InputUSD + with.OutputUSD
	return PriceEstimates{WithoutCache: without, WithCache: with}
}

// PriceImporter is implemented by non-blocking external price sources such as
// OpenRouter or LiteLLM. Serving requests must never depend on Import succeeding.
type PriceImporter interface {
	Import(ctx context.Context) (map[string]Price, error)
}

type CatalogImporter interface {
	ImportCatalog(ctx context.Context) ([]CatalogPrice, error)
}
