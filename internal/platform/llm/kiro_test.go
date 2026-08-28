package llm

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func awsTestEvent(eventType string, payload any) []byte {
	headerName := []byte(":event-type")
	headerValue := []byte(eventType)
	headers := []byte{byte(len(headerName))}
	headers = append(headers, headerName...)
	headers = append(headers, 7, byte(len(headerValue)>>8), byte(len(headerValue)))
	headers = append(headers, headerValue...)
	body, _ := json.Marshal(payload)
	total := 12 + len(headers) + len(body) + 4
	frame := make([]byte, total)
	binary.BigEndian.PutUint32(frame[0:4], uint32(total))
	binary.BigEndian.PutUint32(frame[4:8], uint32(len(headers)))
	copy(frame[12:], headers)
	copy(frame[12+len(headers):], body)
	return frame
}

func TestKiroRequestPreservesHistoryToolsAndRequestScopedReasoning(t *testing.T) {
	input := ChatRequest{
		Messages: []Message{
			{Role: "system", Content: json.RawMessage(`"system"`)},
			{Role: "user", Content: json.RawMessage(`"question"`)},
			{Role: "assistant", ToolCalls: []ToolCall{{
				ID:       "call-1",
				Type:     "function",
				Function: ToolFunction{Name: "lookup", Arguments: `{"q":"x"}`},
			}}},
			{Role: "tool", ToolCallID: "call-1", Content: json.RawMessage(`"result"`)},
		},
		Reasoning: &Reasoning{Effort: "high"},
		Tools:     []Tool{{Type: "function", Function: ToolFunction{Name: "lookup", Description: "Lookup", Parameters: json.RawMessage(`{"type":"object"}`)}}},
	}
	body, err := kiroRequest(input, "gpt-5.6-sol", "arn:aws:codewhisperer:eu-central-1:1:profile/test")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte("gpt-5.6-sol-high")) {
		t.Fatal("reasoning effort leaked into model id")
	}
	var request map[string]any
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatal(err)
	}
	if request["profileArn"] == "" {
		t.Fatal("profile ARN omitted")
	}
	fields := request["additionalModelRequestFields"].(map[string]any)
	if fields["output_config"].(map[string]any)["effort"] != "high" {
		t.Fatalf("reasoning fields = %#v", fields)
	}
	state := request["conversationState"].(map[string]any)
	current := state["currentMessage"].(map[string]any)["userInputMessage"].(map[string]any)
	context := current["userInputMessageContext"].(map[string]any)
	if len(context["tools"].([]any)) != 1 || len(context["toolResults"].([]any)) != 1 {
		t.Fatalf("current message context = %#v", context)
	}
}

func TestKiroRequestKeepsConversationIDStableAcrossTurns(t *testing.T) {
	build := func(content string) string {
		body, err := kiroRequest(ChatRequest{SessionID: "session-a", Messages: []Message{{Role: "user", Content: json.RawMessage(`"` + content + `"`)}}}, "model", "")
		if err != nil {
			t.Fatal(err)
		}
		var request struct {
			ConversationState struct {
				ConversationID string `json:"conversationId"`
			} `json:"conversationState"`
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Fatal(err)
		}
		return request.ConversationState.ConversationID
	}
	if first, second := build("one"), build("two"); first == "" || first != second {
		t.Fatalf("conversation IDs %q %q", first, second)
	}
}

func TestKiroRuntimeRegionUsesProfileARNAndRejectsOIDCOnlyRegion(t *testing.T) {
	if got := kiroRuntimeRegion("arn:aws:codewhisperer:eu-central-1:123:profile/test", "eu-north-1"); got != "eu-central-1" {
		t.Fatalf("profile region = %q", got)
	}
	if got := kiroRuntimeRegion("", "eu-north-1"); got != "us-east-1" {
		t.Fatalf("OIDC-only region was used as runtime region: %q", got)
	}
	if got := kiroRuntimeHost("eu-central-1"); got != "https://q.eu-central-1.amazonaws.com" {
		t.Fatalf("runtime host = %q", got)
	}
}

func TestKiroStreamBuffersGrowingToolObjectsAndEmitsReasoningUsage(t *testing.T) {
	stream := bytes.Join([][]byte{
		awsTestEvent("reasoningContentEvent", map[string]any{"text": "think"}),
		awsTestEvent("toolUseEvent", map[string]any{"toolUseId": "call-1", "name": "lookup", "input": map[string]any{"q": "x"}}),
		awsTestEvent("toolUseEvent", map[string]any{"toolUseId": "call-1", "name": "lookup", "input": map[string]any{"q": "xyz"}}),
		awsTestEvent("metadataEvent", map[string]any{"usage": map[string]any{"inputTokens": 8, "outputTokens": 2}}),
		awsTestEvent("messageStopEvent", map[string]any{}),
	}, nil)
	body, err := io.ReadAll(kiroStream(io.NopCloser(bytes.NewReader(stream)), "gpt-5.6-sol"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{`"reasoning_content":"think"`, `"name":"lookup"`, `\"q\":\"xyz\"`, `"finish_reason":"tool_calls"`, `"prompt_tokens":8`, "data: [DONE]"} {
		if !strings.Contains(text, want) {
			t.Fatalf("stream omitted %q: %s", want, text)
		}
	}
	if strings.Contains(text, `\"q\":\"x\"`) {
		t.Fatalf("stream emitted stale growing tool arguments: %s", text)
	}
}
