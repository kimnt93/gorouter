package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kimnt93/gorouter/pkg/entities"
)

func TestOpenAIModelDiscoveryNormalizesAndSorts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("unexpected request %s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"z","owned_by":"p"},{"id":"a"},{"id":"a"},{"id":""}]}`))
	}))
	defer server.Close()
	adapter := &OpenAIAdapter{HTTP: server.Client()}
	models, err := adapter.DiscoverModels(context.Background(), &entities.CredentialRuntime{BaseURL: server.URL + "/v1", APIKey: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "a" || models[1].ID != "z" {
		t.Fatalf("models = %+v", models)
	}
}

func TestOpenAIModelDiscoveryAcceptsFullChatEndpointAndOpenAILikeVariants(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/models" {
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-5.6-sol","owned_by":"codex","context_length":272000,"name":"cx/GPT 5.6 Sol"},{"model":"fallback-model","provider":"test"}]}`))
	}))
	defer server.Close()

	models, err := (&OpenAIAdapter{HTTP: server.Client()}).DiscoverModels(context.Background(), &entities.CredentialRuntime{
		BaseURL: server.URL + "/api/v1/chat/completions",
		APIKey:  "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "fallback-model" || models[1].ID != "gpt-5.6-sol" || models[1].ContextLength != 272000 || models[1].OwnedBy != "codex" {
		t.Fatalf("models = %+v", models)
	}
}

func TestOpenAIAdapterPreservesRequestFieldsAndNormalizesFullChatEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/chat/completions" || r.Method != http.MethodPost {
			t.Fatalf("unexpected upstream request %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		for _, expected := range []string{`"model":"gpt-5.6-sol"`, `"reasoning_effort":"high"`, `"response_format":{"type":"json_object"}`, `"include_usage":true`} {
			if !strings.Contains(string(body), expected) {
				t.Fatalf("upstream body %s does not contain %s", body, expected)
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	adapter := &OpenAIAdapter{HTTP: server.Client()}
	result, err := adapter.Send(context.Background(), &entities.CredentialRuntime{
		BaseURL: server.URL + "/api/v1/chat/completions",
		APIKey:  "secret",
	}, "gpt-5.6-sol", []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"stream":true,"reasoning_effort":"high","response_format":{"type":"json_object"}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
}

func TestAnthropicModelDiscoveryUsesNativeHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("x-api-key") != "secret" || r.Header.Get("anthropic-version") == "" {
			t.Fatalf("unexpected Anthropic request")
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-test"}]}`))
	}))
	defer server.Close()
	adapter := &AnthropicAdapter{HTTP: server.Client()}
	models, err := adapter.DiscoverModels(context.Background(), &entities.CredentialRuntime{BaseURL: server.URL, Kind: entities.KindAPIKey, APIKey: "secret"})
	if err != nil || len(models) != 1 || models[0].ID != "claude-test" {
		t.Fatalf("models=%+v err=%v", models, err)
	}
}
