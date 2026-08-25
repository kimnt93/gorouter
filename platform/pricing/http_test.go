package pricing

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPImporterOpenRouterAndLiteLLM(t *testing.T) {
	tests := []struct {
		name   string
		source string
		body   string
		model  string
	}{
		{"OpenRouter", SourceOpenRouter, `{"data":[{"id":"open/model","pricing":{"prompt":"0.000001","completion":"0.000002","input_cache_read":"0.0000001","input_cache_write":"0.0000002"}}]}`, "open/model"},
		{"LiteLLM", SourceLiteLLM, `{"lite-model":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"cache_read_input_token_cost":0.0000001,"cache_creation_input_token_cost":0.0000002}}`, "lite-model"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			importer := &HTTPImporter{Client: server.Client(), URL: server.URL, Source: test.source}
			prices, err := importer.Import(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			price, ok := prices[test.model]
			if !ok || price.InputPerM != 1 || price.OutputPerM != 2 || math.Abs(price.CachedInputPerM-0.1) > 1e-12 || math.Abs(price.CacheWritePerM-0.2) > 1e-12 {
				t.Fatalf("imported price=%+v present=%v", price, ok)
			}
		})
	}
}
