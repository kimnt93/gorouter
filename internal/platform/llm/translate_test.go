package llm

import (
	"encoding/json"
	"testing"
)

func TestToAnthropicUsesTypedPayload(t *testing.T) {
	temperature := 0.2
	req := &ChatRequest{
		Model: "public", Temperature: &temperature,
		Messages: []Message{
			{Role: "system", Content: json.RawMessage(`"be concise"`)},
			{Role: "user", Content: json.RawMessage(`"hello"`)},
		},
		Tools: []Tool{{Type: "function", Function: ToolFunction{Name: "lookup", Description: "look up a value", Parameters: json.RawMessage(`{"type":"object"}`)}}},
	}
	got := ToAnthropic(req)
	if got.Model != "public" || len(got.System) != 1 || got.System[0].Text != "be concise" || len(got.Messages) != 1 || len(got.Tools) != 1 {
		t.Fatalf("translation incomplete: %+v", got)
	}
	if got.System[0].CacheControl == nil || got.Messages[0].Content[0].CacheControl == nil || got.Tools[0].CacheControl == nil {
		t.Fatalf("prompt cache breakpoints missing: %+v", got)
	}
	if !json.Valid(got.Tools[0].InputSchema) {
		t.Fatalf("tool input schema is invalid: %s", got.Tools[0].InputSchema)
	}
}

func TestToAnthropicPreservesClientCacheControl(t *testing.T) {
	req := &ChatRequest{Messages: []Message{{Role: "developer", Content: json.RawMessage(`[{"type":"text","text":"stable","cache_control":{"type":"ephemeral","ttl":"1h"}}]`)}, {Role: "user", Content: json.RawMessage(`"next"`)}}}
	got := ToAnthropic(req)
	if got.System[0].CacheControl == nil || got.System[0].CacheControl.TTL != "1h" {
		t.Fatalf("system cache control = %+v", got.System)
	}
}

func TestAnthropicStreamConverterCollectsEstimatedUsage(t *testing.T) {
	converter := NewAnthropicStreamConverter("public-model")
	events := []struct {
		name string
		data string
	}{
		{"message_start", `{"message":{"usage":{"input_tokens":9}}}`},
		{"content_block_delta", `{"index":0,"delta":{"type":"text_delta","text":"hello world"}}`},
		{"message_delta", `{"delta":{"stop_reason":"end_turn"},"usage":{}}`},
		{"message_stop", `{}`},
	}
	for _, event := range events {
		if _, _, err := converter.Feed(event.name, []byte(event.data)); err != nil {
			t.Fatal(err)
		}
	}
	usage := converter.UsageCollected()
	if usage.PromptTokens != 9 || usage.CompletionTokens == 0 {
		t.Fatalf("usage not collected/estimated: %+v", usage)
	}
	if converter.ContentCollected() != "hello world" || converter.FinishReason() != "stop" {
		t.Fatalf("stream metadata wrong: content=%q finish=%q", converter.ContentCollected(), converter.FinishReason())
	}
}
