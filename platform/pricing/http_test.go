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

func TestOpenRouterFrontendCatalogSelectsStandardAndMetadata(t *testing.T) {
	body := `{"data":[
		{"slug":"vendor/model","name":"Model","context_length":100,"endpoint":{"variant":"batch","model_variant_slug":"vendor/model:batch","provider_name":"Batch Provider","context_length":200,"pricing":{"prompt":"0.0000001","completion":"0.0000002"}}},
		{"slug":"vendor/model","name":"Model","context_length":100,"endpoint":{"variant":"standard","model_variant_slug":"vendor/model","provider_name":"Standard Provider","context_length":300,"pricing":{"prompt":"0.000001","completion":"0.000002","input_cache_read":"0.0000001"}}},
		{"slug":"vendor/free-only","name":"Free","endpoint":{"variant":"free","model_variant_slug":"vendor/free-only:free","pricing":{"prompt":"0","completion":"0"}}}
	]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	entries, err := (&HTTPImporter{Client: server.Client(), URL: server.URL, Source: SourceOpenRouter}).ImportCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries=%d, want one record per slug", len(entries))
	}
	for _, entry := range entries {
		if entry.Model == "vendor/model" {
			if entry.Price.InputPerM != 1 || entry.Provider != "Standard Provider" || entry.ContextLength != 300 || !entry.CacheSupported {
				t.Fatalf("standard entry = %+v", entry)
			}
			return
		}
	}
	t.Fatal("standard model missing")
}

func TestHTTPImporterRejectsInvalidPriceAndResponse(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{"negative", http.StatusOK, `{"data":[{"slug":"m","endpoint":{"pricing":{"prompt":"-1"}}}]}`},
		{"too large", http.StatusOK, `{"data":[{"slug":"m","endpoint":{"pricing":{"prompt":"NaN"}}}]}`},
		{"http error", http.StatusBadGateway, `{}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			_, err := (&HTTPImporter{Client: server.Client(), URL: server.URL, Source: SourceOpenRouter}).ImportCatalog(context.Background())
			if err == nil {
				t.Fatal("invalid catalog accepted")
			}
		})
	}
}
