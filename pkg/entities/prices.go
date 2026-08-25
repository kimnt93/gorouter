package entities

import "context"

type Price struct {
	InputPerM       float64 `json:"input_per_m"`
	OutputPerM      float64 `json:"output_per_m"`
	CachedInputPerM float64 `json:"cached_input_per_m"`
	CacheWritePerM  float64 `json:"cache_write_per_m"`
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
	USD    float64 `json:"usd"`
	Priced bool    `json:"priced"`
}

func (u TokenUsage) TotalTokens() int64 {
	return u.PromptTokens + u.CompletionTokens
}

func ComputeCost(p *Price, u TokenUsage) float64 {
	return CalculateCost(p, u).USD
}

func CalculateCost(p *Price, u TokenUsage) Cost {
	if p == nil {
		return Cost{}
	}
	return Cost{USD: (float64(u.PromptTokens)*p.InputPerM +
		float64(u.CompletionTokens)*p.OutputPerM +
		float64(u.CacheReadTokens)*p.CachedInputPerM +
		float64(u.CacheWriteTokens)*p.CacheWritePerM) / 1e6, Priced: true}
}

// PriceImporter is implemented by non-blocking external price sources such as
// OpenRouter or LiteLLM. Serving requests must never depend on Import succeeding.
type PriceImporter interface {
	Import(ctx context.Context) (map[string]Price, error)
}
