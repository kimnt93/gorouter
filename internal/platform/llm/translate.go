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

func ToAnthropic(req *ChatRequest) *AnthropicRequest {
	body := &AnthropicRequest{}
	var msgs []AnthropicMessage

	textBlock := func(s string, cacheControl *CacheControl) AnthropicContentBlock {
		return AnthropicContentBlock{Type: "text", Text: s, CacheControl: normalizedCacheControl(cacheControl)}
	}

	appendUserText := func(role, text string, cacheControl *CacheControl) {
		if text == "" {
			return
		}
		msgs = append(msgs, AnthropicMessage{Role: role, Content: []AnthropicContentBlock{textBlock(text, cacheControl)}})
	}

	for i := range req.Messages {
		m := &req.Messages[i]
		switch m.Role {
		case "system", "developer":
			if text := contentText(m.Content); text != "" {
				body.System = append(body.System, textBlock(text, messageCacheControl(m)))
			}
		case "tool":
			block := AnthropicContentBlock{Type: "tool_result", ToolUseID: m.ToolCallID, Content: contentText(m.Content), CacheControl: normalizedCacheControl(messageCacheControl(m))}
			msgs = append(msgs, AnthropicMessage{Role: "user", Content: []AnthropicContentBlock{block}})
		case "assistant":
			var content []AnthropicContentBlock
			if txt := contentText(m.Content); txt != "" {
				content = append(content, textBlock(txt, messageCacheControl(m)))
			}
			for _, tc := range m.ToolCalls {
				input := json.RawMessage(tc.Function.Arguments)
				if !json.Valid(input) {
					input = json.RawMessage(`{}`)
				}
				content = append(content, AnthropicContentBlock{Type: "tool_use", ID: tc.ID, Name: tc.Function.Name, Input: input})
			}
			if len(content) == 0 {
				content = append(content, textBlock("", messageCacheControl(m)))
			}
			msgs = append(msgs, AnthropicMessage{Role: "assistant", Content: content})
		default:
			appendUserText("user", contentText(m.Content), messageCacheControl(m))
		}
	}

	body.Model = req.Model
	body.MaxTokens = maxTokensOf(req)
	body.Messages = msgs
	body.Temperature = req.Temperature
	body.TopP = req.TopP
	if stops := stopsOf(req); len(stops) > 0 {
		body.StopSequences = stops
	}
	body.Stream = req.Stream
	if len(req.Tools) > 0 {
		tools := make([]AnthropicTool, 0, len(req.Tools))
		for _, t := range req.Tools {
			schema := t.Function.Parameters
			if !json.Valid(schema) {
				schema = json.RawMessage(`{"type":"object"}`)
			}
			tools = append(tools, AnthropicTool{Name: t.Function.Name, Description: t.Function.Description, InputSchema: schema, CacheControl: normalizedCacheControl(t.CacheControl)})
		}
		body.Tools = tools
	}
	applyAnthropicPromptCache(body)
	return body
}

func normalizedCacheControl(value *CacheControl) *CacheControl {
	if value == nil {
		return nil
	}
	control := *value
	if strings.TrimSpace(control.Type) == "" {
		control.Type = "ephemeral"
	}
	if control.Type != "ephemeral" {
		return nil
	}
	if control.TTL != "" && control.TTL != "5m" && control.TTL != "1h" {
		control.TTL = ""
	}
	return &control
}

func messageCacheControl(message *Message) *CacheControl {
	if message.CacheControl != nil {
		return message.CacheControl
	}
	var blocks []struct {
		CacheControl *CacheControl `json:"cache_control"`
	}
	if json.Unmarshal(message.Content, &blocks) == nil {
		for i := len(blocks) - 1; i >= 0; i-- {
			if blocks[i].CacheControl != nil {
				return blocks[i].CacheControl
			}
		}
	}
	return nil
}

func applyAnthropicPromptCache(body *AnthropicRequest) {
	const maxBreakpoints = 4
	breakpoints := 0
	keep := func(target **CacheControl) {
		if *target == nil {
			return
		}
		if breakpoints >= maxBreakpoints {
			*target = nil
			return
		}
		breakpoints++
	}
	for i := range body.System {
		keep(&body.System[i].CacheControl)
	}
	for i := range body.Tools {
		keep(&body.Tools[i].CacheControl)
	}
	for i := range body.Messages {
		for j := range body.Messages[i].Content {
			keep(&body.Messages[i].Content[j].CacheControl)
		}
	}
	add := func(target **CacheControl) {
		if breakpoints < maxBreakpoints && *target == nil {
			*target = &CacheControl{Type: "ephemeral"}
			breakpoints++
		}
	}
	if len(body.System) > 0 {
		add(&body.System[len(body.System)-1].CacheControl)
	}
	if len(body.Tools) > 0 {
		add(&body.Tools[len(body.Tools)-1].CacheControl)
	}
	if len(body.Messages) > 0 && len(body.Messages[len(body.Messages)-1].Content) > 0 {
		last := &body.Messages[len(body.Messages)-1].Content
		add(&(*last)[len(*last)-1].CacheControl)
	}
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
		c.usage = u
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

func (c *AnthropicStreamConverter) ContentCollected() string { return c.text.String() }

func (c *AnthropicStreamConverter) FinishReason() string {
	if c.finish == "" {
		return "stop"
	}
	return c.finish
}
