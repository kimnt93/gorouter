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

func TestAntigravitySendUsesCloudCodeEnvelopeAndConvertsResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1internal:generateContent" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer google-token" {
			t.Errorf("authorization = %q", got)
		}
		var envelope map[string]any
		if err := json.NewDecoder(r.Body).Decode(&envelope); err != nil {
			t.Fatal(err)
		}
		if envelope["model"] != "gemini-3.1-pro-preview" || envelope["project"] != "project-1" {
			t.Errorf("envelope = %#v", envelope)
		}
		request := envelope["request"].(map[string]any)
		generation := request["generationConfig"].(map[string]any)
		thinking := generation["thinkingConfig"].(map[string]any)
		if thinking["thinkingLevel"] != "medium" {
			t.Errorf("thinking config = %#v", thinking)
		}
		if len(request["tools"].([]any)) != 1 {
			t.Errorf("tools = %#v", request["tools"])
		}
		_, _ = io.WriteString(w, `{"response":{"candidates":[{"content":{"parts":[{"text":"hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":1}}}`)
	}))
	defer server.Close()

	request := []byte(`{"messages":[{"role":"system","content":"be helpful"},{"role":"user","content":"hi"}],"reasoning":{"effort":"medium"},"tools":[{"type":"function","function":{"name":"lookup","description":"lookup","parameters":{"type":"object"}}}]}`)
	result, err := (&AntigravityAdapter{HTTP: server.Client()}).Send(context.Background(), &entities.CredentialRuntime{
		BaseURL:     server.URL,
		OAuthAccess: "google-token",
		OAuthMeta:   entities.OAuthMetadata{ProjectID: "project-1"},
	}, "gemini-3.1-pro-preview", request)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	var response Response
	if err := json.NewDecoder(result.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Choices[0].Message.Content != "hello" || response.Usage.PromptTokens != 3 {
		t.Fatalf("converted response = %+v", response)
	}
}

func TestAntigravityStreamConvertsTextAndToolCalls(t *testing.T) {
	upstream := io.NopCloser(strings.NewReader("data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hi\"},{\"functionCall\":{\"id\":\"call-1\",\"name\":\"lookup\",\"args\":{\"q\":\"x\"}}}]}}]}}\n\n"))
	body, err := io.ReadAll(antigravityStream(upstream, "gemini-3.1-pro-preview"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{`"content":"hi"`, `"name":"lookup"`, `"finish_reason":"tool_calls"`, "data: [DONE]"} {
		if !strings.Contains(text, want) {
			t.Fatalf("stream omitted %q: %s", want, text)
		}
	}
}
