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

func codexTestEvents() string {
	return "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-1\",\"model\":\"gpt-5.5\"}}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello \"}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"world\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-1\",\"model\":\"gpt-5.5\",\"status\":\"completed\",\"usage\":{\"input_tokens\":8,\"output_tokens\":2,\"input_tokens_details\":{\"cached_tokens\":3}}}}\n\n"
}

func TestCodexAdapterTranslatesRequestAndNonStreamingResponse(t *testing.T) {
	var upstream map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/codex/responses" || r.Header.Get("Authorization") != "Bearer oauth-access" || r.Header.Get("chatgpt-account-id") != "acct-1" || r.Header.Get("originator") != "codex_cli_rs" {
			t.Fatalf("request path=%s headers=%v", r.URL.Path, r.Header)
		}
		if err := json.NewDecoder(r.Body).Decode(&upstream); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, codexTestEvents())
	}))
	defer server.Close()
	effort := &Reasoning{Effort: "high", Summary: "auto"}
	body, _ := json.Marshal(ChatRequest{Model: "cx/gpt-5.5", Messages: []Message{{Role: "system", Content: json.RawMessage(`"be terse"`)}, {Role: "user", Content: json.RawMessage(`"hi"`)}}, Reasoning: effort})
	adapter := &CodexAdapter{HTTP: server.Client()}
	result, err := adapter.Send(context.Background(), &entities.CredentialRuntime{Kind: entities.KindOAuth, BaseURL: server.URL, OAuthAccess: "oauth-access", OAuthAccount: "acct-1"}, "gpt-5.5", body)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	responseBody, _ := io.ReadAll(result.Body)
	var response Response
	if err := json.Unmarshal(responseBody, &response); err != nil {
		t.Fatal(err)
	}
	if response.Model != "gpt-5.5" || response.Choices[0].Message.Content != "hello world" || response.Usage.PromptTokens != 5 || response.Usage.CacheReadTokens != 3 {
		t.Fatalf("response = %+v", response)
	}
	if upstream["model"] != "gpt-5.5" || upstream["instructions"] != "be terse" || upstream["stream"] != true || upstream["store"] != false {
		t.Fatalf("upstream body = %#v", upstream)
	}
	reasoning, _ := upstream["reasoning"].(map[string]any)
	if reasoning["effort"] != "high" || strings.Contains(upstream["model"].(string), "high") {
		t.Fatalf("reasoning/model = %#v / %v", reasoning, upstream["model"])
	}
}

func TestCodexAdapterStreamingAndModelDiscovery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/codex/responses":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, codexTestEvents())
		case "/codex/models":
			if r.URL.Query().Get("client_version") != codexClientVersion || r.Header.Get("Accept") != "application/json" || r.Header.Get("Openai-Beta") != "responses=experimental" {
				t.Fatalf("model discovery query/headers = %s %s", r.URL.RawQuery, r.Header)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]any{{"slug": "gpt-5.5", "context_window": 200000}, {"slug": "hidden", "visibility": "hide"}, {"id": "gpt-5.4-mini", "supported_in_api": true}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	adapter := &CodexAdapter{HTTP: server.Client()}
	runtime := &entities.CredentialRuntime{Kind: entities.KindOAuth, BaseURL: server.URL, OAuthAccess: "token"}
	body, _ := json.Marshal(ChatRequest{Model: "cx/gpt-5.5", Stream: true, Messages: []Message{{Role: "user", Content: json.RawMessage(`"hi"`)}}})
	result, err := adapter.Send(context.Background(), runtime, "gpt-5.5", body)
	if err != nil {
		t.Fatal(err)
	}
	stream, _ := io.ReadAll(result.Body)
	_ = result.Body.Close()
	if !strings.Contains(string(stream), `"content":"hello "`) || !strings.Contains(string(stream), `"prompt_tokens":5`) || !strings.Contains(string(stream), `"cache_read_tokens":3`) || !strings.Contains(string(stream), "data: [DONE]") {
		t.Fatalf("stream = %s", stream)
	}
	models, err := adapter.DiscoverModels(context.Background(), runtime)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "gpt-5.4-mini" || models[1].ID != "gpt-5.5" || models[1].ContextLength != 200000 {
		t.Fatalf("models = %+v", models)
	}
}

func TestCodexAdapterRefreshesAuthorizationHeaderAfter401(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			if r.Header.Get("Authorization") != "Bearer old-token" {
				t.Fatalf("first authorization = %q", r.Header.Get("Authorization"))
			}
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Authorization") != "Bearer new-token" {
			t.Fatalf("refreshed authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, codexTestEvents())
	}))
	defer server.Close()

	runtime := &entities.CredentialRuntime{Kind: entities.KindOAuth, BaseURL: server.URL, OAuthAccess: "old-token"}
	body, _ := json.Marshal(ChatRequest{Model: "cx/gpt-5.5", Messages: []Message{{Role: "user", Content: json.RawMessage(`"hi"`)}}})
	adapter := &CodexAdapter{HTTP: server.Client(), Refresh: func(_ context.Context, cr *entities.CredentialRuntime) error {
		cr.OAuthAccess = "new-token"
		return nil
	}}
	result, err := adapter.Send(context.Background(), runtime, "gpt-5.5", body)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	if calls != 2 {
		t.Fatalf("request count = %d", calls)
	}
}

func TestCodexModelDiscoveryNormalizesOpenAIStyleCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/codex/models" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"gpt-5.6-sol","context_length":272000,"max_input_tokens":872000,"max_output_tokens":128000,"supported_endpoints":["responses"],"name":"cx/GPT 5.6 Sol"}]}`)
	}))
	defer server.Close()

	models, err := (&CodexAdapter{HTTP: server.Client()}).DiscoverModels(context.Background(), &entities.CredentialRuntime{
		BaseURL:     server.URL + "/api/v1",
		Kind:        entities.KindOAuth,
		OAuthAccess: "oauth-access",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "gpt-5.6-sol" || models[0].Object != "model" || models[0].OwnedBy != "codex" || models[0].ContextLength != 272000 || models[0].MaxInputTokens != 872000 || models[0].MaxOutputTokens != 128000 || models[0].Name != "cx/GPT 5.6 Sol" {
		t.Fatalf("models = %+v", models)
	}
}

func TestCodexModelDiscoveryUsesProviderReportedCapabilities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"models":[{"slug":"future-model","contextWindow":272000,"maxContextWindow":872000,"maxInputTokens":800000,"maxOutputTokens":128000,"displayName":"Future Model","description":"Provider description","inputModalities":["text","image"],"outputModalities":["text"],"defaultReasoningLevel":"high","supportedReasoningLevels":[{"effort":"low","description":"Fast"},{"effort":"high","description":"Deep"}]}]}`)
	}))
	defer server.Close()
	models, err := (&CodexAdapter{HTTP: server.Client()}).DiscoverModels(context.Background(), &entities.CredentialRuntime{BaseURL: server.URL})
	if err != nil || len(models) != 1 {
		t.Fatalf("models=%+v err=%v", models, err)
	}
	model := models[0]
	if model.Name != "Future Model" || model.Description != "Provider description" || model.ContextLength != 272000 || model.MaxContextWindow != 872000 || model.MaxInputTokens != 800000 || model.DefaultReasoningLevel != "high" || len(model.SupportedReasoningLevels) != 2 || len(model.InputModalities) != 2 {
		t.Fatalf("model = %+v", model)
	}
}

func TestCodexModelDiscoveryFailsClosedWhenCapabilitiesAreAbsent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"models":[{"slug":"unknown-future-model","contextWindow":100000}]}`)
	}))
	defer server.Close()
	models, err := (&CodexAdapter{HTTP: server.Client()}).DiscoverModels(context.Background(), &entities.CredentialRuntime{BaseURL: server.URL})
	if err != nil || len(models) != 1 {
		t.Fatalf("models=%+v err=%v", models, err)
	}
	if strings.Join(models[0].InputModalities, ",") != "text" || len(models[0].SupportedReasoningLevels) != 0 || models[0].SupportsOriginalImage {
		t.Fatalf("unexpected inferred capabilities: %+v", models[0])
	}
}

func TestToCodexRequestTranslatesToolHistoryAndChoice(t *testing.T) {
	choice := json.RawMessage(`{"type":"function","function":{"name":"lookup"}}`)
	request := ChatRequest{
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"find it"`)},
			{Role: "assistant", ToolCalls: []ToolCall{{ID: "call-1", Type: "function", Function: ToolFunction{Name: "lookup", Arguments: `{"q":"go"}`}}}},
			{Role: "tool", ToolCallID: "call-1", Content: json.RawMessage(`"found"`)},
			{Role: "tool", ToolCallID: "orphan", Content: json.RawMessage(`"ignored"`)},
			{Role: "assistant", ToolCalls: []ToolCall{{ID: "call-2", Type: "function", Function: ToolFunction{Name: "next"}}}},
		},
		ToolChoice: choice,
	}
	got := toCodexRequest(&request, "gpt-5.5")
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Input      []map[string]any `json:"input"`
		ToolChoice map[string]any   `json:"tool_choice"`
	}
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatal(err)
	}
	if body.ToolChoice["type"] != "function" || body.ToolChoice["name"] != "lookup" {
		t.Fatalf("tool choice = %#v", body.ToolChoice)
	}
	if len(body.Input) != 5 {
		t.Fatalf("input = %#v", body.Input)
	}
	if body.Input[1]["type"] != "function_call" || body.Input[1]["call_id"] != "call-1" || body.Input[1]["arguments"] != `{"q":"go"}` {
		t.Fatalf("function call = %#v", body.Input[1])
	}
	if body.Input[2]["type"] != "function_call_output" || body.Input[2]["output"] != "found" {
		t.Fatalf("function output = %#v", body.Input[2])
	}
	if body.Input[3]["call_id"] != "call-2" || body.Input[4]["call_id"] != "call-2" || body.Input[4]["output"] != "" {
		t.Fatalf("repaired function history = %#v", body.Input[3:])
	}
}

func TestToCodexRequestPreservesImageInputs(t *testing.T) {
	request := &ChatRequest{Messages: []Message{{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"inspect"},{"type":"image_url","image_url":{"url":"data:image/png;base64,c3ludGhldGlj"}}]`)}}}
	payload, err := json.Marshal(toCodexRequest(request, "gpt-5.6-luna"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Input []struct {
			Content []struct {
				Type     string         `json:"type"`
				Text     string         `json:"text"`
				ImageURL map[string]any `json:"image_url"`
			} `json:"content"`
		} `json:"input"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Input) != 1 || len(decoded.Input[0].Content) != 2 || decoded.Input[0].Content[0].Text != "inspect" || decoded.Input[0].Content[1].Type != "input_image" || decoded.Input[0].Content[1].ImageURL["url"] != "data:image/png;base64,c3ludGhldGlj" {
		t.Fatalf("Codex image payload = %s", payload)
	}
}

func TestToCodexRequestUsesBackendSafeDefaults(t *testing.T) {
	request := &ChatRequest{
		Model:     "cx/gpt-5.4",
		Messages:  []Message{{Role: "user", Content: json.RawMessage(`"hello"`)}},
		MaxTokens: int64Ptr(128),
	}
	payload, err := json.Marshal(toCodexRequest(request, "gpt-5.4"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "max_output_tokens") || strings.Contains(string(payload), "max_tokens") {
		t.Fatalf("Codex payload contains rejected token limit: %s", payload)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["instructions"] != codexChatInstructions || decoded["store"] != false || decoded["stream"] != true {
		t.Fatalf("Codex defaults = %#v", decoded)
	}
}

func TestToCodexRequestPrefersExplicitConversationCacheKey(t *testing.T) {
	request := &ChatRequest{ConversationID: "conversation-123", Messages: []Message{{Role: "developer", Content: json.RawMessage(`"stable"`)}, {Role: "user", Content: json.RawMessage(`"turn"`)}}}
	if got := toCodexRequest(request, "gpt-5.5").PromptCacheKey; got != "conversation-123" {
		t.Fatalf("prompt cache key=%q", got)
	}
}

func TestToCodexRequestUsesStablePromptCacheKey(t *testing.T) {
	first := &ChatRequest{Messages: []Message{{Role: "developer", Content: json.RawMessage(`"stable coding instructions"`)}, {Role: "user", Content: json.RawMessage(`"first"`)}}}
	second := &ChatRequest{Messages: []Message{{Role: "developer", Content: json.RawMessage(`"stable coding instructions"`)}, {Role: "user", Content: json.RawMessage(`"second"`)}}}
	firstKey := toCodexRequest(first, "gpt-5.5").PromptCacheKey
	secondKey := toCodexRequest(second, "gpt-5.5").PromptCacheKey
	if firstKey == "" || firstKey != secondKey {
		t.Fatalf("unstable prompt cache keys: %q %q", firstKey, secondKey)
	}
}

func codexToolEvents() string {
	return "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-tools\"}}\n\n" +
		"data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"fc-1\",\"type\":\"function_call\",\"call_id\":\"call-1\",\"name\":\"lookup\",\"arguments\":\"\"}}\n\n" +
		"data: {\"type\":\"response.function_call_arguments.delta\",\"item_id\":\"fc-1\",\"output_index\":0,\"delta\":\"{\\\"q\\\":\"}\n\n" +
		"data: {\"type\":\"response.function_call_arguments.delta\",\"item_id\":\"fc-1\",\"output_index\":0,\"delta\":\"\\\"go\\\"}\"}\n\n" +
		"data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"fc-1\",\"type\":\"function_call\",\"call_id\":\"call-1\",\"name\":\"lookup\",\"arguments\":\"{\\\"q\\\":\\\"go\\\"}\"}}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-tools\",\"status\":\"completed\",\"usage\":{\"input_tokens\":4,\"output_tokens\":3},\"output\":[{\"type\":\"function_call\",\"call_id\":\"call-1\",\"name\":\"lookup\",\"arguments\":\"{\\\"q\\\":\\\"go\\\"}\"}]}}\n\n"
}

func TestCodexToolCallStreamingTranslation(t *testing.T) {
	var output strings.Builder
	if err := transformCodexStream(strings.NewReader(codexToolEvents()), &output, "gpt-5.5"); err != nil {
		t.Fatal(err)
	}
	stream := output.String()
	if !strings.Contains(stream, `"index":0,"id":"call-1","type":"function","function":{"name":"lookup"}`) {
		t.Fatalf("missing tool header: %s", stream)
	}
	if !strings.Contains(stream, `"arguments":"{\"q\":"`) || !strings.Contains(stream, `"arguments":"\"go\"}"`) {
		t.Fatalf("missing argument deltas: %s", stream)
	}
	if strings.Count(stream, `"id":"call-1"`) != 1 {
		t.Fatalf("tool header duplicated: %s", stream)
	}
	if !strings.Contains(stream, `"finish_reason":"tool_calls"`) || !strings.Contains(stream, `data: [DONE]`) {
		t.Fatalf("missing tool completion: %s", stream)
	}
}

func TestCodexToolCallNonStreamingCompletedSnapshotFallback(t *testing.T) {
	events := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-fallback\",\"usage\":{\"input_tokens\":2,\"output_tokens\":1},\"output\":[{\"type\":\"function_call\",\"call_id\":\"call-fallback\",\"name\":\"shell\",\"arguments\":\"{\\\"cmd\\\":\\\"pwd\\\"}\"}]}}\n\n"
	response, err := collectCodexResponse(strings.NewReader(events), "gpt-5.5")
	if err != nil {
		t.Fatal(err)
	}
	choice := response.Choices[0]
	if choice.FinishReason != "tool_calls" || len(choice.Message.ToolCalls) != 1 {
		t.Fatalf("response = %+v", response)
	}
	call := choice.Message.ToolCalls[0]
	if call.ID != "call-fallback" || call.Function.Name != "shell" || call.Function.Arguments != `{"cmd":"pwd"}` {
		t.Fatalf("tool call = %+v", call)
	}
}

func TestCodexSendUsesConversationIdentityForBodyAndSessionHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("session_id"); got != "conversation-123" {
			t.Fatalf("session_id=%q", got)
		}
		var request codexRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.PromptCacheKey != "conversation-123" {
			t.Fatalf("prompt_cache_key=%q", request.PromptCacheKey)
		}
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"response-1\",\"usage\":{}}}\n\n")
	}))
	defer server.Close()

	adapter := &CodexAdapter{HTTP: server.Client()}
	result, err := adapter.Send(context.Background(), &entities.CredentialRuntime{BaseURL: server.URL, OAuthAccess: "synthetic"}, "gpt-5.5", []byte(`{"model":"public","conversation_id":"conversation-123","messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	result.Body.Close()
}

func TestCodexStreamingRequiresCompletedTerminalEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-1\"}}\n\n"+
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n")
	}))
	defer server.Close()
	body, _ := json.Marshal(ChatRequest{Model: "cx/gpt-5.5", Stream: true, Messages: []Message{{Role: "user", Content: json.RawMessage(`"hi"`)}}})
	result, err := (&CodexAdapter{HTTP: server.Client()}).Send(context.Background(), &entities.CredentialRuntime{BaseURL: server.URL, OAuthAccess: "token"}, "gpt-5.5", body)
	if err != nil {
		t.Fatal(err)
	}
	_, readErr := io.ReadAll(result.Body)
	result.Body.Close()
	if readErr == nil {
		t.Fatal("incomplete Codex stream was accepted as successful")
	}
}

func TestCodexNonStreamingRequiresCompletedTerminalEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n")
	}))
	defer server.Close()
	body, _ := json.Marshal(ChatRequest{Model: "cx/gpt-5.5", Messages: []Message{{Role: "user", Content: json.RawMessage(`"hi"`)}}})
	if _, err := (&CodexAdapter{HTTP: server.Client()}).Send(context.Background(), &entities.CredentialRuntime{BaseURL: server.URL, OAuthAccess: "token"}, "gpt-5.5", body); err == nil {
		t.Fatal("incomplete non-streaming Codex response was accepted")
	}
}
