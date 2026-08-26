package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kimnt93/gorouter/pkg/entities"
)

func TestOpenCodeZenRoutesMuseToResponsesAndConvertsStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" || r.Header.Get("Authorization") != "Bearer zen-key" {
			t.Fatalf("request = %s authorization=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["model"] != "muse-spark-1.2" || body["stream"] != true || body["input"] == nil {
			t.Fatalf("responses body = %#v", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"connection healthy\"}\n\n"+
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-muse\",\"usage\":{\"input_tokens\":8,\"output_tokens\":2}}}\n\n")
	}))
	defer server.Close()

	result, err := (&OpenCodeZenAdapter{HTTP: server.Client()}).Send(context.Background(), &entities.CredentialRuntime{
		BaseURL: server.URL + "/v1", APIKey: "zen-key",
	}, "muse-spark-1.2", []byte(`{"model":"public","stream":true,"messages":[{"role":"user","content":"test"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	stream, _ := io.ReadAll(result.Body)
	if !bytesContainAll(stream, `"content":"connection healthy"`, `"prompt_tokens":8`, "data: [DONE]") {
		t.Fatalf("converted stream = %s", stream)
	}
}

func TestOpenCodeZenKeepsDeepSeekOnChatCompletions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("request path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chat-1","choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer server.Close()
	result, err := (&OpenCodeZenAdapter{HTTP: server.Client()}).Send(context.Background(), &entities.CredentialRuntime{BaseURL: server.URL + "/v1", APIKey: "zen-key"}, "deepseek-v4-flash", []byte(`{"messages":[{"role":"user","content":"test"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	result.Body.Close()
}

func bytesContainAll(value []byte, values ...string) bool {
	text := string(value)
	for _, item := range values {
		if !strings.Contains(text, item) {
			return false
		}
	}
	return true
}
