package handlers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/kimnt93/gorouter/internal/platform/llm"
	"github.com/kimnt93/gorouter/pkg/apikey"
	"github.com/kimnt93/gorouter/pkg/chat"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/modelroute"
)

func TestMessagesRequestTranslatesSystemToolsAndToolResults(t *testing.T) {
	var input MessagesRequest
	body := `{
		"model":"model-a","max_tokens":512,
		"system":[{"type":"text","text":"be concise","cache_control":{"type":"ephemeral","ttl":"1h"}}],
		"messages":[
			{"role":"user","content":[{"type":"text","text":"hello"}]},
			{"role":"assistant","content":[{"type":"tool_use","id":"tool-1","name":"lookup","input":{"q":"go"}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-1","content":"found"}]}
		],
		"tools":[{"name":"lookup","description":"search","input_schema":{"type":"object"},"cache_control":{"type":"ephemeral"}}],
		"tool_choice":{"type":"tool","name":"lookup"}
	}`
	if err := json.Unmarshal([]byte(body), &input); err != nil {
		t.Fatal(err)
	}
	req, err := input.chatRequest()
	if err != nil {
		t.Fatal(err)
	}
	if req.Model != "model-a" || req.MaxCompletionTokens == nil || *req.MaxCompletionTokens != 512 || len(req.Messages) != 4 || len(req.Tools) != 1 {
		t.Fatalf("request = %+v", req)
	}
	if req.Messages[0].Role != "developer" || string(req.Messages[0].Content) != `"be concise"` || req.Messages[2].ToolCalls[0].Function.Arguments != `{"q":"go"}` || req.Messages[3].Role != "tool" {
		t.Fatalf("messages = %+v", req.Messages)
	}
	if req.Messages[0].CacheControl == nil || req.Messages[0].CacheControl.TTL != "1h" || req.Tools[0].CacheControl == nil {
		t.Fatalf("cache control was not preserved: messages=%+v tools=%+v", req.Messages, req.Tools)
	}
	if !strings.Contains(string(req.ToolChoice), `"name":"lookup"`) {
		t.Fatalf("tool choice = %s", req.ToolChoice)
	}
}

func TestMessagesResponseUsesAnthropicEnvelope(t *testing.T) {
	response := llm.Response{ID: "chat-1", Model: "upstream", Choices: []llm.Choice{{FinishReason: "tool_calls", Message: &llm.ResponseMessage{Role: "assistant", Content: "checking", ToolCalls: []llm.ToolCall{{ID: "tool-1", Type: "function", Function: llm.ToolFunction{Name: "lookup", Arguments: `{"q":"go"}`}}}}}}, Usage: llm.Usage{PromptTokens: 10, CompletionTokens: 3, CacheReadTokens: 4, CacheWriteTokens: 2}}
	out := messagesResponse(response, "model-a")
	if out.Type != "message" || out.Role != "assistant" || out.Model != "model-a" || out.StopReason == nil || *out.StopReason != "tool_use" || len(out.Content) != 2 || out.Content[1].Type != "tool_use" || out.Usage.CacheReadInputTokens != 4 {
		t.Fatalf("response = %+v", out)
	}
}

func TestMessagesStreamEmitsAnthropicLifecycle(t *testing.T) {
	key := &entities.ApiKey{ID: "key-1", TenantID: "tenant-1", Models: []string{"model-a"}, Scopes: []string{entities.ScopeChat}, Enabled: true}
	runtime := &entities.CredentialRuntime{ID: "cred-a", Provider: entities.ProviderOpenAICompatible, Kind: entities.KindAPIKey}
	gateway := &Gateway{
		Keys:   apikey.NewService(gatewayKeyRepo{key}, func(string) string { return "" }, func() string { return "" }),
		Creds:  credential.NewService(gatewayCredRepo{routes: []entities.RouteCandidate{{CredentialID: "cred-a"}}, runtimes: map[string]*entities.CredentialRuntime{"cred-a": runtime}}, nil),
		Models: modelroute.NewService(gatewayModelRepo{model: entities.ModelDef{Name: "model-a", UpstreamModel: "upstream-a", Strategy: chat.StrategyPriority, Enabled: true}}),
		OpenAI: gatewayStreamUpstream{}, Selector: &chat.Selector{}, Health: chat.NewHealth(),
	}
	app := fiber.New()
	app.Post("/v1/messages", func(c fiber.Ctx) error {
		c.Locals(localSession, &entities.Session{Role: entities.RoleAPIKey, KeyID: key.ID, TenantID: key.TenantID, Scopes: []string{entities.ScopeChat}})
		return gateway.Messages(c)
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"model-a","max_tokens":100,"messages":[{"role":"user","content":"hello"}],"stream":true}`))
	request.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
	response, err := app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	bodyBytes, _ := io.ReadAll(response.Body)
	stream := string(bodyBytes)
	want := []string{"event: message_start", "event: content_block_start", "event: content_block_delta", "event: content_block_stop", "event: message_delta", "event: message_stop"}
	position := -1
	for _, marker := range want {
		next := strings.Index(stream[position+1:], marker)
		if next < 0 {
			t.Fatalf("missing %q in stream: %s", marker, stream)
		}
		position += next + 1
	}
	if response.StatusCode != http.StatusOK || !strings.Contains(stream, `"text":"stream works"`) {
		t.Fatalf("status=%d stream=%s", response.StatusCode, stream)
	}
}

func TestMessagesStreamEmitsToolUseDeltas(t *testing.T) {
	emitter := newMessagesStreamEmitter("model-a")
	var output bytes.Buffer
	writer := bufio.NewWriter(&output)
	if err := emitter.Created(writer); err != nil {
		t.Fatal(err)
	}
	toolIndex := 0
	firstCall := llm.ToolCall{Index: &toolIndex, ID: "tool-1", Type: "function", Function: llm.ToolFunction{Name: "lookup", Arguments: `{"q":`}}
	chunk, _ := json.Marshal(llm.Chunk{Choices: []llm.ChunkChoice{{Delta: llm.Delta{ToolCalls: []llm.ToolCall{firstCall}}}}})
	if err := emitter.ChatChunk(writer, chunk); err != nil {
		t.Fatal(err)
	}
	secondCall := llm.ToolCall{Index: &toolIndex, Function: llm.ToolFunction{Arguments: `"go"}`}}
	chunk, _ = json.Marshal(llm.Chunk{Choices: []llm.ChunkChoice{{Delta: llm.Delta{ToolCalls: []llm.ToolCall{secondCall}}, FinishReason: "tool_calls"}}})
	if err := emitter.ChatChunk(writer, chunk); err != nil {
		t.Fatal(err)
	}
	if err := emitter.Completed(writer, llm.Usage{CompletionTokens: 3}); err != nil {
		t.Fatal(err)
	}
	stream := output.String()
	for _, want := range []string{`"type":"tool_use"`, `"type":"input_json_delta"`, `"partial_json":"{\"q\":"`, `"stop_reason":"tool_use"`} {
		if !strings.Contains(stream, want) {
			t.Fatalf("missing %s in %s", want, stream)
		}
	}
}
