package usage

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/kimnt93/gorouter/pkg/entities"
)

const (
	maxConversationEntries = 128
	maxConversationText    = 16 << 10
	maxConversationTotal   = 256 << 10
)

type wireMessage struct {
	Role             string          `json:"role"`
	Name             string          `json:"name,omitempty"`
	Content          json.RawMessage `json:"content,omitempty"`
	Reasoning        json.RawMessage `json:"reasoning,omitempty"`
	ReasoningContent json.RawMessage `json:"reasoning_content,omitempty"`
	Thinking         json.RawMessage `json:"thinking,omitempty"`
	ToolCallID       string          `json:"tool_call_id,omitempty"`
	ToolCalls        []wireToolCall  `json:"tool_calls,omitempty"`
}

type wireToolCall struct {
	ID       string       `json:"id,omitempty"`
	Index    *int         `json:"index,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function wireFunction `json:"function,omitempty"`
}

type wireFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type chatWire struct {
	Messages []wireMessage `json:"messages,omitempty"`
	Choices  []struct {
		Message *wireMessage `json:"message,omitempty"`
		Delta   *wireMessage `json:"delta,omitempty"`
	} `json:"choices,omitempty"`
}

type traceBuilder struct {
	entries   []entities.ConversationEntry
	total     int
	truncated bool
	tools     map[string]int
}

func newTraceBuilder() *traceBuilder { return &traceBuilder{tools: make(map[string]int)} }

func (b *traceBuilder) add(entry entities.ConversationEntry) {
	entry.Role = strings.TrimSpace(entry.Role)
	entry.Type = strings.TrimSpace(entry.Type)
	entry.Name = strings.TrimSpace(entry.Name)
	entry.ToolCallID = strings.TrimSpace(entry.ToolCallID)
	if entry.Type == "" {
		entry.Type = "text"
	}
	if entry.Role == "" {
		entry.Role = "assistant"
	}
	if entry.Content == "" && entry.Name == "" {
		return
	}
	remaining := maxConversationTotal - b.total
	if remaining <= 0 || len(b.entries) >= maxConversationEntries {
		b.truncated = true
		return
	}
	if len(entry.Content) > maxConversationText {
		entry.Content = entry.Content[:maxConversationText]
		b.truncated = true
	}
	if len(entry.Content) > remaining {
		entry.Content = entry.Content[:remaining]
		b.truncated = true
	}
	b.total += len(entry.Content)
	b.entries = append(b.entries, entry)
}

func (b *traceBuilder) appendText(role, kind, content string) {
	if content == "" {
		return
	}
	if len(b.entries) > 0 {
		last := &b.entries[len(b.entries)-1]
		if last.Role == role && last.Type == kind && last.Name == "" && last.ToolCallID == "" && len(last.Content) < maxConversationText && b.total < maxConversationTotal {
			remaining := min(maxConversationText-len(last.Content), maxConversationTotal-b.total)
			if len(content) > remaining {
				content = content[:remaining]
				b.truncated = true
			}
			last.Content += content
			b.total += len(content)
			return
		}
	}
	b.add(entities.ConversationEntry{Role: role, Type: kind, Content: content})
}

func rawText(raw json.RawMessage) string {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var blocks []struct {
		Type     string          `json:"type"`
		Text     string          `json:"text"`
		Thinking string          `json:"thinking"`
		Content  json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var out strings.Builder
		for _, block := range blocks {
			value := block.Text
			if value == "" {
				value = block.Thinking
			}
			if value == "" {
				value = rawText(block.Content)
			}
			if value != "" {
				if out.Len() > 0 {
					out.WriteByte('\n')
				}
				out.WriteString(value)
			}
		}
		return out.String()
	}
	return strings.TrimSpace(string(raw))
}

func (b *traceBuilder) message(message wireMessage) {
	role := message.Role
	if role == "" {
		role = "assistant"
	}
	b.appendText(role, "text", rawText(message.Content))
	reasoning := rawText(message.ReasoningContent)
	if reasoning == "" {
		reasoning = rawText(message.Reasoning)
	}
	if reasoning == "" {
		reasoning = rawText(message.Thinking)
	}
	b.appendText(role, "reasoning", reasoning)
	if message.ToolCallID != "" {
		b.add(entities.ConversationEntry{Role: role, Type: "tool_result", ToolCallID: message.ToolCallID, Name: message.Name, Content: rawText(message.Content)})
	}
	for _, call := range message.ToolCalls {
		b.tool(call, role)
	}
}

func (b *traceBuilder) tool(call wireToolCall, role string) {
	key := call.ID
	if key == "" && call.Index != nil {
		key = "index:" + strconv.Itoa(*call.Index)
	}
	if index, ok := b.tools[key]; ok && key != "" {
		entry := &b.entries[index]
		if call.Function.Name != "" {
			entry.Name = call.Function.Name
		}
		remaining := min(maxConversationText-len(entry.Content), maxConversationTotal-b.total)
		arguments := call.Function.Arguments
		if len(arguments) > remaining {
			arguments = arguments[:max(0, remaining)]
			b.truncated = true
		}
		entry.Content += arguments
		b.total += len(arguments)
		return
	}
	entry := entities.ConversationEntry{Role: role, Type: "tool_call", Name: call.Function.Name, ToolCallID: call.ID, Content: call.Function.Arguments}
	before := len(b.entries)
	b.add(entry)
	if key != "" && len(b.entries) > before {
		b.tools[key] = len(b.entries) - 1
	}
}

func normalizeConversation(request, response string) ([]entities.ConversationEntry, bool) {
	builder := newTraceBuilder()
	if len(request) > maxConversationTotal {
		request = request[:maxConversationTotal]
		builder.truncated = true
	}
	var req chatWire
	if json.Unmarshal([]byte(request), &req) == nil {
		for _, message := range req.Messages {
			builder.message(message)
		}
	}
	if len(req.Messages) == 0 && strings.TrimSpace(request) != "" {
		builder.add(entities.ConversationEntry{Role: "user", Type: "text", Content: request})
	}
	requestEntries := len(builder.entries)

	originalResponse := response
	processed := 0
	responseTruncated := false
	for len(response) > 0 && processed < maxConversationTotal && !responseTruncated {
		line := response
		if next := strings.IndexByte(response, '\n'); next >= 0 {
			line, response = response[:next], response[next+1:]
		} else {
			response = ""
		}
		processed += len(line)
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "data:"))
		if line == "" || line == "[DONE]" {
			continue
		}
		var chunk chatWire
		if json.Unmarshal([]byte(line), &chunk) != nil {
			continue
		}
		beforeTruncated := builder.truncated
		for _, choice := range chunk.Choices {
			if choice.Message != nil {
				builder.message(*choice.Message)
			}
			if choice.Delta != nil {
				builder.message(*choice.Delta)
			}
		}
		responseTruncated = !beforeTruncated && builder.truncated
	}
	if (processed >= maxConversationTotal || responseTruncated) && len(response) > 0 {
		builder.truncated = true
	}
	if len(builder.entries) == requestEntries && strings.TrimSpace(originalResponse) != "" {
		builder.add(entities.ConversationEntry{Role: "assistant", Type: "text", Content: originalResponse})
	}
	return builder.entries, builder.truncated
}
