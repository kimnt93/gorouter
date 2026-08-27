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

func TestResponsesRequestTranslatesCodexInputAndTools(t *testing.T) {
	var input ResponsesRequest
	body := `{
		"model":"model-a",
		"instructions":"be concise",
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},
			{"type":"function_call","call_id":"call-1","name":"lookup","arguments":"{\"q\":\"go\"}"},
			{"type":"function_call_output","call_id":"call-1","output":"found"}
		],
		"tools":[{"type":"function","name":"lookup","description":"search","parameters":{"type":"object"}}],
		"tool_choice":{"type":"function","name":"lookup"},
		"max_output_tokens":123
	}`
	if err := json.Unmarshal([]byte(body), &input); err != nil {
		t.Fatal(err)
	}
	request, err := input.chatRequest()
	if err != nil {
		t.Fatal(err)
	}
	if request.Model != "model-a" || len(request.Messages) != 4 || len(request.Tools) != 1 || request.MaxCompletionTokens == nil || *request.MaxCompletionTokens != 123 {
		t.Fatalf("request = %+v", request)
	}
	if request.Messages[0].Role != "developer" || string(request.Messages[1].Content) != `"hello"` {
		t.Fatalf("messages = %+v", request.Messages)
	}
	if request.Messages[2].ToolCalls[0].Function.Name != "lookup" || request.Messages[3].Role != "tool" || request.Messages[3].ToolCallID != "call-1" {
		t.Fatalf("tool messages = %+v", request.Messages)
	}
	if request.Tools[0].Function.Name != "lookup" || !strings.Contains(string(request.ToolChoice), `"name":"lookup"`) {
		t.Fatalf("tools=%+v choice=%s", request.Tools, request.ToolChoice)
	}
}

func TestResponsesResponseUsesOpenAICompatibleEnvelope(t *testing.T) {
	response := ResponsesResponse{
		ID: "resp-1", Object: "response", CreatedAt: 123, Model: "model-a", Status: "completed", OutputText: "hello",
		Output: []ResponsesOutput{
			{ID: "msg-1", Type: "message", Role: "assistant", Content: []ResponsesContent{
				{Type: "output_text", Text: "hello", Annotations: []any{}, Logprobs: []any{}},
			}},
		},
		Usage: ResponsesUsage{InputTokens: 2, OutputTokens: 1, TotalTokens: 3},
	}
	body, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"background":false`, `"error":null`, `"output_text":"hello"`, `"created_at":123`, `"annotations":[]`, `"logprobs":[]`, `"input_tokens_details"`, `"output_tokens_details"`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("missing %s in %s", want, body)
		}
	}
}

func TestResponsesStreamEmitsCodexItemLifecycle(t *testing.T) {
	key := &entities.ApiKey{ID: "key-1", TenantID: "tenant-1", Models: []string{"model-a"}, Scopes: []string{entities.ScopeChat}, Enabled: true}
	runtime := &entities.CredentialRuntime{ID: "cred-a", Provider: entities.ProviderOpenAICompatible, Kind: entities.KindAPIKey}
	gateway := &Gateway{
		Keys:   apikey.NewService(gatewayKeyRepo{key}, func(string) string { return "" }, func() string { return "" }),
		Creds:  credential.NewService(gatewayCredRepo{routes: []entities.RouteCandidate{{CredentialID: "cred-a"}}, runtimes: map[string]*entities.CredentialRuntime{"cred-a": runtime}}, nil),
		Models: modelroute.NewService(gatewayModelRepo{model: entities.ModelDef{Name: "model-a", UpstreamModel: "upstream-a", Strategy: chat.StrategyPriority, Enabled: true}}),
		OpenAI: gatewayStreamUpstream{}, Selector: &chat.Selector{}, Health: chat.NewHealth(),
	}
	app := fiber.New()
	app.Post("/v1/responses", func(c fiber.Ctx) error {
		c.Locals(localSession, &entities.Session{Role: entities.RoleAPIKey, KeyID: key.ID, TenantID: key.TenantID, Scopes: []string{entities.ScopeChat}})
		return gateway.Responses(c)
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"model-a","input":[{"role":"user","content":[{"type":"input_text","text":"hello"}]}],"stream":true}`))
	request.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
	response, err := app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	bodyBytes, _ := io.ReadAll(response.Body)
	stream := string(bodyBytes)
	want := []string{"response.created", "response.output_item.added", "response.content_part.added", "response.output_text.delta", "response.output_text.done", "response.output_item.done", "response.completed", "data: [DONE]"}
	position := -1
	for _, marker := range want {
		next := strings.Index(stream[position+1:], marker)
		if next < 0 {
			t.Fatalf("missing %q in stream: %s", marker, stream)
		}
		position += next + 1
	}
	if !strings.Contains(stream, `"delta":"stream works"`) || response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d stream=%s", response.StatusCode, stream)
	}
}

func TestResponsesStreamEmitsFunctionCallLifecycle(t *testing.T) {
	emitter := newResponsesStreamEmitter("model-a")
	var output bytes.Buffer
	writer := bufio.NewWriter(&output)
	if err := emitter.Created(writer); err != nil {
		t.Fatal(err)
	}
	toolIndex := 0
	chunk, _ := json.Marshal(llm.Chunk{Choices: []llm.ChunkChoice{{Delta: llm.Delta{ToolCalls: []llm.ToolCall{{Index: &toolIndex, ID: "call-1", Type: "function", Function: llm.ToolFunction{Name: "lookup", Arguments: `{"q":`}}}}}}})
	if err := emitter.ChatChunk(writer, chunk); err != nil {
		t.Fatal(err)
	}
	chunk, _ = json.Marshal(llm.Chunk{Choices: []llm.ChunkChoice{{Delta: llm.Delta{ToolCalls: []llm.ToolCall{{Index: &toolIndex, Function: llm.ToolFunction{Arguments: `"go"}`}}}}}}})
	if err := emitter.ChatChunk(writer, chunk); err != nil {
		t.Fatal(err)
	}
	if err := emitter.Completed(writer, llm.Usage{PromptTokens: 4, CompletionTokens: 3}); err != nil {
		t.Fatal(err)
	}
	stream := output.String()
	want := []string{"response.output_item.added", "response.function_call_arguments.delta", "response.function_call_arguments.done", "response.output_item.done", "response.completed"}
	position := -1
	for _, marker := range want {
		next := strings.Index(stream[position+1:], marker)
		if next < 0 {
			t.Fatalf("missing %q in stream: %s", marker, stream)
		}
		position += next + 1
	}
	if !strings.Contains(stream, `"call_id":"call-1"`) || !strings.Contains(stream, `"arguments":"{\"q\":\"go\"}"`) {
		t.Fatalf("stream = %s", stream)
	}
}
