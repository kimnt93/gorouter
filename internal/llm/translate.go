package llm

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	anthropicVersion = "2023-06-01"
	defaultMaxTokens = 4096
)

func ToAnthropic(req *ChatRequest) map[string]any {
	body := map[string]any{}
	var sys strings.Builder
	type amsg struct {
		Role    string        `json:"role"`
		Content []interface{} `json:"content"`
	}
	var msgs []map[string]any

	textBlock := func(s string) map[string]any { return map[string]any{"type": "text", "text": s} }

	appendUserText := func(role, text string) {
		if text == "" {
			return
		}
		msgs = append(msgs, map[string]any{"role": role, "content": []any{textBlock(text)}})
	}

	for i := range req.Messages {
		m := &req.Messages[i]
		switch m.Role {
		case "system", "developer":
			sys.WriteString(contentText(m.Content))
			sys.WriteString("\n")
		case "tool":
			block := map[string]any{
				"type":        "tool_result",
				"tool_use_id": m.ToolCallID,
				"content":     contentText(m.Content),
			}
			msgs = append(msgs, map[string]any{"role": "user", "content": []any{block}})
		case "assistant":
			var content []any
			if txt := contentText(m.Content); txt != "" {
				content = append(content, textBlock(txt))
			}
			for _, tc := range m.ToolCalls {
				input := map[string]any{}
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &input)
				content = append(content, map[string]any{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  tc.Function.Name,
					"input": input,
				})
			}
			if len(content) == 0 {
				content = append(content, textBlock(""))
			}
			msgs = append(msgs, map[string]any{"role": "assistant", "content": content})
		default:
			appendUserText("user", contentText(m.Content))
		}
	}

	body["model"] = req.Model
	body["max_tokens"] = maxTokensOf(req)
	body["messages"] = msgs
	if s := strings.TrimSpace(sys.String()); s != "" {
		body["system"] = s
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		body["top_p"] = *req.TopP
	}
	if stops := stopsOf(req); len(stops) > 0 {
		body["stop_sequences"] = stops
	}
	body["stream"] = req.Stream
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tt := map[string]any{"name": t.Function.Name}
			if t.Function.Description != "" {
				tt["description"] = t.Function.Description
			}
			if len(t.Function.Parameters) > 0 {
				var schema any
				if err := json.Unmarshal(t.Function.Parameters, &schema); err == nil {
					tt["input_schema"] = schema
				}
			}
			tools = append(tools, tt)
		}
		body["tools"] = tools
	}
	return body
}

func maxTokensOf(req *ChatRequest) int64 {
	if v := req.OutputLimit(); v > 0 {
		return v
	}
	return defaultMaxTokens
}

func stopsOf(req *ChatRequest) []string {
	if len(req.Stop) == 0 {
		return nil
	}
	var arr []string
	if err := json.Unmarshal(req.Stop, &arr); err == nil {
		return arr
	}
	var s string
	if err := json.Unmarshal(req.Stop, &s); err == nil && s != "" {
		return []string{s}
	}
	return nil
}

type anthropicContentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type anthropicResp struct {
	ID         string                  `json:"id"`
	Model      string                  `json:"model"`
	StopReason string                  `json:"stop_reason"`
	Content    []anthropicContentBlock `json:"content"`
	Usage      anthropicUsage          `json:"usage"`
}

type anthropicUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

func finishFromStopReason(sr string) string {
	switch sr {
	case "tool_use":
		return "tool_calls"
	case "max_tokens":
		return "length"
	default:
		return "stop"
	}
}

func FromAnthropic(body []byte, requestModel string) (*Response, error) {
	var ar anthropicResp
	if err := json.Unmarshal(body, &ar); err != nil {
		return nil, fmt.Errorf("decode anthropic response: %w", err)
	}
	out := &Response{
		ID:      "chatcmpl-" + ar.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   requestModel,
	}
	rm := &ResponseMessage{Role: "assistant"}
	var text strings.Builder
	for _, b := range ar.Content {
		switch b.Type {
		case "text":
			text.WriteString(b.Text)
		case "tool_use":
			args := string(b.Input)
			if args == "" {
				args = "{}"
			}
			rm.ToolCalls = append(rm.ToolCalls, ToolCall{
				ID:       b.ID,
				Type:     "function",
				Function: ToolFunction{Name: b.Name},
			})
			rm.ToolCalls[len(rm.ToolCalls)-1].Function.Arguments = args
		}
	}
	rm.Content = text.String()
	out.Choices = []Choice{{Index: 0, Message: rm, FinishReason: finishFromStopReason(ar.StopReason)}}
	out.Usage = Usage{
		PromptTokens:     ar.Usage.InputTokens,
		CompletionTokens: ar.Usage.OutputTokens,
		CacheReadTokens:  ar.Usage.CacheReadInputTokens,
		CacheWriteTokens: ar.Usage.CacheCreationInputTokens,
	}
	return out, nil
}

type toolAccum struct {
	id   string
	name string
	json strings.Builder
}

type AnthropicStreamConverter struct {
	RequestModel string
	started      bool
	text         strings.Builder
	tools        map[int]*toolAccum
	finish       string
	usage        Usage
	done         bool
}

func NewAnthropicStreamConverter(requestModel string) *AnthropicStreamConverter {
	return &AnthropicStreamConverter{RequestModel: requestModel, tools: map[int]*toolAccum{}}
}

func (c *AnthropicStreamConverter) chunk(delta Delta, finish string, usage *Usage) Chunk {
	return Chunk{
		ID:      "chatcmpl-anthropic-stream",
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   c.RequestModel,
		Choices: []ChunkChoice{{Index: 0, Delta: delta, FinishReason: finish}},
		Usage:   usage,
	}
}

func (c *AnthropicStreamConverter) Feed(event string, data []byte) (out [][]byte, done bool, err error) {
	switch event {
	case "message_start":
		var msg struct {
			Message struct {
				Usage anthropicUsage `json:"usage"`
			} `json:"message"`
		}
		if e := json.Unmarshal(data, &msg); e == nil {
			c.usage.PromptTokens = msg.Message.Usage.InputTokens
			c.usage.CacheReadTokens = msg.Message.Usage.CacheReadInputTokens
			c.usage.CacheWriteTokens = msg.Message.Usage.CacheCreationInputTokens
		}
	case "content_block_start":
		var bs struct {
			Index        int                   `json:"index"`
			ContentBlock anthropicContentBlock `json:"content_block"`
		}
		if e := json.Unmarshal(data, &bs); e == nil && bs.ContentBlock.Type == "tool_use" {
			c.tools[bs.Index] = &toolAccum{id: bs.ContentBlock.ID, name: bs.ContentBlock.Name}
		}
	case "content_block_delta":
		var d struct {
			Index int `json:"index"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
			} `json:"delta"`
		}
		if e := json.Unmarshal(data, &d); e != nil {
			return nil, false, e
		}
		switch d.Delta.Type {
		case "text_delta":
			c.text.WriteString(d.Delta.Text)
			if !c.started {
				c.started = true
				ch := c.chunk(Delta{Role: "assistant"}, "", nil)
				b, _ := json.Marshal(ch)
				out = append(out, b)
			}
			ch := c.chunk(Delta{Content: d.Delta.Text}, "", nil)
			b, _ := json.Marshal(ch)
			out = append(out, b)
		case "input_json_delta":
			if ta := c.tools[d.Index]; ta != nil {
				ta.json.WriteString(d.Delta.PartialJSON)
			}
		}
	case "message_delta":
		var md struct {
			Delta struct {
				StopReason string `json:"stop_reason"`
			} `json:"delta"`
			Usage anthropicUsage `json:"usage"`
		}
		if e := json.Unmarshal(data, &md); e == nil {
			c.finish = finishFromStopReason(md.Delta.StopReason)
			if md.Usage.OutputTokens > 0 {
				c.usage.CompletionTokens = md.Usage.OutputTokens
			}
		}
	case "message_stop":
		u := c.usage
		if u.CompletionTokens == 0 {
			u.CompletionTokens = EstimateTextTokens(c.text.String())
		}
		delta := Delta{}
		if len(c.tools) > 0 {
			for _, ta := range c.tools {
				args := ta.json.String()
				if args == "" {
					args = "{}"
				}
				delta.ToolCalls = append(delta.ToolCalls, ToolCall{
					ID:       ta.id,
					Type:     "function",
					Function: ToolFunction{Name: ta.name, Arguments: args},
				})
			}
		}
		ch := c.chunk(delta, c.finish, &u)
		b, e := json.Marshal(ch)
		if e != nil {
			return nil, true, e
		}
		out = append(out, b)
		c.done = true
		done = true
	}
	return out, done, nil
}

func (c *AnthropicStreamConverter) UsageCollected() Usage { return c.usage }
