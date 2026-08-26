package llm

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestCursorAgentRequestCarriesCanonicalModelReasoningAndTools(t *testing.T) {
	input := ChatRequest{
		Messages:  []Message{{Role: "user", Content: json.RawMessage(`"hello"`)}},
		Reasoning: &Reasoning{Effort: "medium"},
		Tools: []Tool{{Type: "function", Function: ToolFunction{
			Name:        "lookup",
			Description: "Look up a value",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`),
		}}},
	}
	payload := cursorAgentRequest(input, "gpt-5.6-sol", "conversation", "message")
	run := protoField(payload, 1)
	if got := string(protoField(protoField(run, 3), 1)); got != "gpt-5.6-sol" {
		t.Fatalf("model details model = %q", got)
	}
	requested := protoField(run, 9)
	if got := string(protoField(requested, 1)); got != "gpt-5.6-sol" {
		t.Fatalf("requested model = %q", got)
	}
	parameter := protoField(requested, 3)
	if got := string(protoField(parameter, 1)); got != "reasoning" {
		t.Fatalf("reasoning parameter id = %q", got)
	}
	if got := string(protoField(parameter, 2)); got != "medium" {
		t.Fatalf("reasoning parameter value = %q", got)
	}
	toolEnvelope := protoField(run, 4)
	definition := protoField(toolEnvelope, 1)
	if got := string(protoField(definition, 1)); got != "lookup" {
		t.Fatalf("tool name = %q", got)
	}
	if bytes.Contains(payload, []byte("gpt-5.6-sol-medium")) {
		t.Fatal("reasoning effort leaked into model id")
	}
}

func TestReadCursorFramesAcknowledgesRequestContextAndDecodesText(t *testing.T) {
	exec := bytes.Join([][]byte{
		append(protoTag(1, 0), protoVarint(7)...),
		protoString(15, "exec-1"),
		protoBytes(10, nil),
	}, nil)
	textUpdate := protoBytes(1, protoString(1, "hello"))
	serverPayload := append(protoBytes(2, exec), protoBytes(1, textUpdate)...)

	var replies bytes.Buffer
	var text string
	err := readCursorFrames(bytes.NewReader(cursorConnectFrame(serverPayload)), &replies, func(event cursorEvent) {
		if event.Kind == "text" {
			text += event.Text
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if text != "hello" {
		t.Fatalf("decoded text = %q", text)
	}
	frames := replies.Bytes()
	if len(frames) < 5 {
		t.Fatal("request-context acknowledgement was not written")
	}
	ack := frames[5:]
	execClient := protoField(ack, 2)
	if got := string(protoField(execClient, 15)); got != "exec-1" {
		t.Fatalf("ack exec id = %q", got)
	}
	if protoField(execClient, 10) == nil {
		t.Fatal("ack omitted request_context_result")
	}
}

func TestReadCursorFramesDecodesMCPToolCall(t *testing.T) {
	value := cursorProtoValue("weather")
	argument := append(protoString(1, "q"), protoBytes(2, value)...)
	mcp := bytes.Join([][]byte{
		protoString(3, "cursor-call"),
		protoString(5, "lookup"),
		protoBytes(2, argument),
	}, nil)
	exec := bytes.Join([][]byte{
		append(protoTag(1, 0), protoVarint(9)...),
		protoString(15, "exec-tool"),
		protoBytes(11, mcp),
	}, nil)
	serverPayload := protoBytes(2, exec)

	var call ToolCall
	if err := readCursorFrames(bytes.NewReader(cursorConnectFrame(serverPayload)), nil, func(event cursorEvent) {
		if event.Kind == "tool" {
			call = event.ToolCall
		}
	}); err != nil {
		t.Fatal(err)
	}
	if call.Function.Name != "lookup" || call.Function.Arguments != `{"q":"weather"}` {
		t.Fatalf("decoded tool call = %+v", call)
	}
}
