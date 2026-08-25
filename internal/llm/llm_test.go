package llm

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func stringsReader(s string) io.Reader { return strings.NewReader(s) }

func mustReq(t *testing.T, body string) *ChatRequest {
	t.Helper()
	var r ChatRequest
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatal(err)
	}
	return &r
}

func TestToAnthropicBasics(t *testing.T) {
	req := mustReq(t, `{
		"model":"m1",
		"messages":[
			{"role":"system","content":"be nice"},
			{"role":"user","content":"hi"},
			{"role":"assistant","content":"hello"},
			{"role":"user","content":"weather in paris?"}
		],
		"max_tokens": 55,
		"temperature": 0.2
	}`)
	b := ToAnthropic(req)
	if b["system"] != "be nice" {
		t.Fatalf("system wrong: %v", b["system"])
	}
	msgs := b["messages"].([]map[string]any)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages got %d", len(msgs))
	}
	if msgs[0]["role"] != "user" || msgs[2]["role"] != "user" {
		t.Fatalf("roles wrong")
	}
	if b["max_tokens"] != int64(55) && b["max_tokens"] != float64(55) {
		t.Fatalf("max_tokens wrong: %v", b["max_tokens"])
	}
}

func TestToAnthropicToolRoundTrip(t *testing.T) {
	req := mustReq(t, `{
		"model":"m1",
		"messages":[
			{"role":"user","content":"what time is it"},
			{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_time","arguments":"{\"tz\":\"utc\"}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"12:00"}
		],
		"tools":[{"type":"function","function":{"name":"get_time","description":"get time","parameters":{"type":"object"}}}]
	}`)
	b := ToAnthropic(req)
	msgs := b["messages"].([]map[string]any)
	if len(msgs) != 3 {
		t.Fatalf("messages: %d", len(msgs))
	}
	asst := msgs[1]
	if asst["role"] != "assistant" {
		t.Fatal("expected assistant")
	}
	blocks := asst["content"].([]any)
	var toolUse map[string]any
	for _, blk := range blocks {
		m, _ := blk.(map[string]any)
		if m["type"] == "tool_use" {
			toolUse = m
		}
	}
	if toolUse == nil || toolUse["name"] != "get_time" {
		t.Fatalf("tool_use block missing: %v", blocks)
	}
	toolRes := msgs[2]
	rblocks := toolRes["content"].([]any)
	rb, _ := rblocks[0].(map[string]any)
	if rb["type"] != "tool_result" || rb["tool_use_id"] != "call_1" {
		t.Fatalf("tool_result wrong: %v", rb)
	}
	tools := b["tools"].([]map[string]any)
	if tools[0]["name"] != "get_time" || tools[0]["input_schema"] == nil {
		t.Fatalf("tools wrong: %v", tools[0])
	}
}

func TestFromAnthropic(t *testing.T) {
	body := []byte(`{
		"id":"msg_1","model":"claude-x","stop_reason":"tool_use",
		"content":[{"type":"text","text":"calling"},{"type":"tool_use","id":"t1","name":"f","input":{"q":"x"}}],
		"usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":3,"cache_creation_input_tokens":4}
	}`)
	resp, err := FromAnthropic(body, "gpt-x")
	if err != nil {
		t.Fatal(err)
	}
	c := resp.Choices[0]
	if c.FinishReason != "tool_calls" {
		t.Fatalf("finish %q", c.FinishReason)
	}
	if c.Message.Content != "calling" || len(c.Message.ToolCalls) != 1 {
		t.Fatalf("message parse wrong: %+v", c.Message)
	}
	if c.Message.ToolCalls[0].Function.Arguments != `{"q":"x"}` {
		t.Fatalf("args %q", c.Message.ToolCalls[0].Function.Arguments)
	}
	u := resp.Usage
	if u.PromptTokens != 10 || u.CompletionTokens != 5 || u.CacheReadTokens != 3 || u.CacheWriteTokens != 4 {
		t.Fatalf("usage wrong: %+v", u)
	}
	if resp.Model != "gpt-x" {
		t.Fatalf("should keep requested model, got %s", resp.Model)
	}
}

func TestAnthropicStreamConverter(t *testing.T) {
	c := NewAnthropicStreamConverter("m")
	feed := func(ev, data string) ([][]byte, bool) {
		out, done, err := c.Feed(ev, []byte(data))
		if err != nil {
			t.Fatal(err)
		}
		return out, done
	}
	if out, _ := feed("message_start", `{"message":{"usage":{"input_tokens":20}}}`); len(out) != 0 {
		t.Fatal("message_start should not emit")
	}
	feed("content_block_start", `{"index":0,"content_block":{"type":"text"}}`)
	out, _ := feed("content_block_delta", `{"index":0,"delta":{"type":"text_delta","text":"Hi"}}`)
	if len(out) != 2 {
		t.Fatalf("first text delta should emit role+content chunks, got %d", len(out))
	}
	var roleChunk Chunk
	json.Unmarshal(out[0], &roleChunk)
	if roleChunk.Choices[0].Delta.Role != "assistant" {
		t.Fatal("missing role chunk")
	}
	var contentChunk Chunk
	json.Unmarshal(out[1], &contentChunk)
	if contentChunk.Choices[0].Delta.Content != "Hi" {
		t.Fatal("content chunk wrong")
	}
	out, _ = feed("content_block_delta", `{"index":0,"delta":{"type":"text_delta","text":"!"}}`)
	if len(out) != 1 {
		t.Fatal("subsequent deltas emit one chunk")
	}
	feed("message_delta", `{"delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`)
	final, done := feed("message_stop", `{}`)
	if !done || len(final) != 1 {
		t.Fatalf("message_stop must finalize: done=%v n=%d", done, len(final))
	}
	var endChunk Chunk
	json.Unmarshal(final[0], &endChunk)
	if endChunk.Choices[0].FinishReason != "stop" {
		t.Fatalf("finish %q", endChunk.Choices[0].FinishReason)
	}
	if endChunk.Usage == nil || endChunk.Usage.PromptTokens != 20 || endChunk.Usage.CompletionTokens == 0 {
		t.Fatalf("final usage wrong: %+v", endChunk.Usage)
	}
}

func TestScanSSE(t *testing.T) {
	payload := ": keepalive\n\n event: x\n\nevent: alpha\ndata: one\ndata: two\n\ndata: bare\n\n"
	var events []SSEEvent
	if err := ScanSSE(stringsReader(payload), func(e SSEEvent) error {
		events = append(events, e)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events got %d: %+v", len(events), events)
	}
	if events[0].Event != "alpha" || string(events[0].Data) != "one\ntwo" {
		t.Fatalf("event0 wrong: %+v", events[0])
	}
	if events[1].Event != "message" || string(events[1].Data) != "bare" {
		t.Fatalf("event1 wrong: %+v", events[1])
	}
}

func TestEstimatePromptTokens(t *testing.T) {
	req := mustReq(t, `{"model":"m","messages":[{"role":"user","content":"12345678"}]}`)
	got := req.EstimatePromptTokens()
	if got < 3 {
		t.Fatalf("too small: %d", got)
	}
}
