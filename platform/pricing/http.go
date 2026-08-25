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

	"github.com/kimnt93/gorouter/pkg/entities"
)

const (
	SourceOpenRouter = "openrouter"
	SourceLiteLLM    = "litellm"
)

// HTTPImporter reads public model-price documents without coupling request
// serving to an external catalog. It implements entities.PriceImporter and is
// intended to be invoked by pkg/pricing.Service on a background schedule.
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
	ID      string            `json:"id"`
	Pricing openRouterPricing `json:"pricing"`
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

func (i *HTTPImporter) Import(ctx context.Context) (map[string]entities.Price, error) {
	if strings.TrimSpace(i.URL) == "" {
		return nil, errors.New("price import URL is required")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, i.URL, nil)
	if err != nil {
		return nil, err
	}
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
	decoder := json.NewDecoder(io.LimitReader(response.Body, 32<<20))
	switch strings.ToLower(i.Source) {
	case SourceOpenRouter:
		var document openRouterDocument
		if err := decoder.Decode(&document); err != nil {
			return nil, fmt.Errorf("decode OpenRouter prices: %w", err)
		}
		prices := make(map[string]entities.Price, len(document.Data))
		for _, model := range document.Data {
			if model.ID == "" {
				continue
			}
			read := model.Pricing.CacheRead
			if read == 0 {
				read = model.Pricing.LegacyRead
			}
			write := model.Pricing.CacheWrite
			if write == 0 {
				write = model.Pricing.LegacyWrite
			}
			prices[model.ID] = perMillion(model.Pricing.Prompt, model.Pricing.Completion, read, write)
		}
		return prices, nil
	case SourceLiteLLM:
		var document map[string]liteLLMPrice
		if err := decoder.Decode(&document); err != nil {
			return nil, fmt.Errorf("decode LiteLLM prices: %w", err)
		}
		prices := make(map[string]entities.Price, len(document))
		for model, price := range document {
			if model != "" {
				prices[model] = perMillion(price.Input, price.Output, price.CacheRead, price.CacheCreate)
			}
		}
		return prices, nil
	default:
		return nil, fmt.Errorf("unsupported price source %q", i.Source)
	}
}

func perMillion(input, output, cacheRead, cacheWrite decimal) entities.Price {
	return entities.Price{InputPerM: float64(input) * 1e6, OutputPerM: float64(output) * 1e6,
		CachedInputPerM: float64(cacheRead) * 1e6, CacheWritePerM: float64(cacheWrite) * 1e6}
}

var _ entities.PriceImporter = (*HTTPImporter)(nil)
