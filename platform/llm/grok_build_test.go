package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kimnt93/gorouter/pkg/entities"
)

func TestGrokBuildUsesResponsesEndpointAndCLIIdentityHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/responses" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Errorf("authorization = %q", got)
		}
		if got := r.Header.Get("X-Grok-Client-Identifier"); got != "grok-shell" {
			t.Errorf("client identifier = %q", got)
		}
		if got := r.Header.Get("X-Grok-Model-Override"); got != "grok-4.1" {
			t.Errorf("model override = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["model"] != "grok-4.1" || body["stream"] != true {
			t.Errorf("responses payload = %#v", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n"+
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-1\",\"usage\":{\"input_tokens\":2,\"output_tokens\":1}}}\n\n")
	}))
	defer server.Close()

	request := []byte(`{"model":"ignored","messages":[{"role":"user","content":"hi"}]}`)
	result, err := (&GrokBuildAdapter{HTTP: server.Client()}).Send(context.Background(), &entities.CredentialRuntime{
		BaseURL:     server.URL,
		OAuthAccess: "access-token",
		OAuthMeta:   entities.OAuthMetadata{AccountID: "account", Email: "user@example.test"},
	}, "grok-4.1", request)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	var response Response
	if err := json.NewDecoder(result.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Model != "grok-4.1" || response.Choices[0].Message.Content != "hello" {
		t.Fatalf("converted response = %+v", response)
	}
}
