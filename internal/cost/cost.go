package cost

type Prices struct {
	InputPerM       float64 `json:"input_per_m"`
	OutputPerM      float64 `json:"output_per_m"`
	CachedInputPerM float64 `json:"cached_input_per_m"`
	CacheWritePerM  float64 `json:"cache_write_per_m"`
}

type Usage struct {
	PromptTokens     int64
	CompletionTokens int64
	CacheReadTokens  int64
	CacheWriteTokens int64
}

func Compute(p *Prices, u Usage) float64 {
	if p == nil {
		return 0
	}
	return (float64(u.PromptTokens)*p.InputPerM +
		float64(u.CompletionTokens)*p.OutputPerM +
		float64(u.CacheReadTokens)*p.CachedInputPerM +
		float64(u.CacheWriteTokens)*p.CacheWritePerM) / 1e6
}
