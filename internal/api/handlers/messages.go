package handlers

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v3"

	responseapi "github.com/kimnt93/gorouter/internal/api"
	"github.com/kimnt93/gorouter/internal/platform/llm"
	"github.com/kimnt93/gorouter/pkg/entities"
)

// MessagesRequest is the Anthropic Messages wire contract accepted by Claude
// Code. Content and system stay raw so text-block arrays remain lossless.
type MessagesRequest struct {
	Model         string            `json:"model"`
	MaxTokens     int64             `json:"max_tokens"`
	Messages      []MessagesMessage `json:"messages"`
	System        json.RawMessage   `json:"system,omitempty"`
	Temperature   *float64          `json:"temperature,omitempty"`
	TopP          *float64          `json:"top_p,omitempty"`
	StopSequences []string          `json:"stop_sequences,omitempty"`
	Stream        bool              `json:"stream,omitempty"`
	Tools         []MessagesTool    `json:"tools,omitempty"`
	ToolChoice    *MessagesChoice   `json:"tool_choice,omitempty"`
	Metadata      *MessagesMetadata `json:"metadata,omitempty"`
}

type MessagesMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type MessagesTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type MessagesChoice struct {
	Type               string `json:"type"`
	Name               string `json:"name,omitempty"`
	DisableParallelUse bool   `json:"disable_parallel_tool_use,omitempty"`
}

type MessagesMetadata struct {
	UserID string `json:"user_id,omitempty"`
}

type MessagesResponse struct {
	ID           string                 `json:"id"`
	Type         string                 `json:"type"`
	Role         string                 `json:"role"`
	Model        string                 `json:"model"`
	Content      []MessagesContentBlock `json:"content"`
	StopReason   *string                `json:"stop_reason"`
	StopSequence *string                `json:"stop_sequence"`
	Usage        MessagesUsage          `json:"usage"`
}

type MessagesContentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type MessagesUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens,omitempty"`
}

// Messages accepts Anthropic Messages requests used by Claude Code and routes
// them through the same authorization, quota, credential, and usage lifecycle
// as OpenAI-compatible chat requests.
// @Summary Create an Anthropic message
// @Description Accepts an Anthropic Messages request and translates it through the gateway chat pipeline.
// @Tags gateway
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param request body MessagesRequest true "Messages request"
// @Success 200 {object} MessagesResponse
// @Failure 400,401,403,404,429,500,502,503 {object} responseapi.ErrorResponse
// @Router /v1/messages [post]
func (g *Gateway) Messages(c fiber.Ctx) error {
	var input MessagesRequest
	if err := c.Bind().Body(&input); err != nil {
		return responseapi.For(c).BadRequest("invalid messages request").Send()
	}
	req, err := input.chatRequest()
	if err != nil || req.Model == "" || len(req.Messages) == 0 || input.MaxTokens < 0 {
		return responseapi.For(c).BadRequest("model and messages are required").Send()
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return responseapi.For(c).BadRequest("invalid messages request").Send()
	}
	c.Request().SetBody(raw)
	c.Locals("messages_mode", true)
	if err := g.Chat(c); err != nil {
		return err
	}
	if input.Stream || c.Response().StatusCode() < 200 || c.Response().StatusCode() >= 300 {
		return nil
	}
	var chatResponse llm.Response
	if err := json.Unmarshal(c.Response().Body(), &chatResponse); err != nil {
		return responseapi.For(c).Error(fiber.StatusBadGateway, "invalid upstream response", "upstream_error", "invalid_response").Send()
	}
	out := messagesResponse(chatResponse, input.Model)
	encoded, err := json.Marshal(out)
	if err != nil {
		return responseapi.For(c).InternalError("failed to encode response").Send()
	}
	c.Set("Content-Type", fiber.MIMEApplicationJSON)
	return c.Send(encoded)
}

func (r MessagesRequest) chatRequest() (*llm.ChatRequest, error) {
	req := &llm.ChatRequest{Model: r.Model, Temperature: r.Temperature, TopP: r.TopP, Stream: r.Stream}
	if r.MaxTokens > 0 {
		req.MaxCompletionTokens = &r.MaxTokens
	}
	if len(r.StopSequences) > 0 {
		req.Stop, _ = json.Marshal(r.StopSequences)
	}
	if system := messagesText(r.System); system != "" {
		req.Messages = append(req.Messages, llm.Message{Role: "developer", Content: quotedRaw(system)})
	}
	for _, message := range r.Messages {
		if message.Role != "user" && message.Role != "assistant" {
			return nil, fmt.Errorf("unsupported role %q", message.Role)
		}
		var text string
		if json.Unmarshal(message.Content, &text) == nil {
			req.Messages = append(req.Messages, llm.Message{Role: message.Role, Content: quotedRaw(text)})
			continue
		}
		var blocks []messagesInputBlock
		if err := json.Unmarshal(message.Content, &blocks); err != nil {
			return nil, err
		}
		var textParts []string
		var toolCalls []llm.ToolCall
		flushText := func() {
			if len(textParts) == 0 {
				return
			}
			req.Messages = append(req.Messages, llm.Message{Role: message.Role, Content: quotedRaw(strings.Join(textParts, "\n")), ToolCalls: toolCalls})
			textParts = nil
			toolCalls = nil
		}
		for _, block := range blocks {
			switch block.Type {
			case "text":
				textParts = append(textParts, block.Text)
			case "tool_use":
				input := block.Input
				if !json.Valid(input) {
					input = json.RawMessage(`{}`)
				}
				toolCalls = append(toolCalls, llm.ToolCall{ID: block.ID, Type: "function", Function: llm.ToolFunction{Name: block.Name, Arguments: string(input)}})
			case "tool_result":
				flushText()
				req.Messages = append(req.Messages, llm.Message{Role: "tool", ToolCallID: block.ToolUseID, Content: quotedRaw(messagesText(block.Content))})
			}
		}
		flushText()
		if len(toolCalls) > 0 {
			req.Messages = append(req.Messages, llm.Message{Role: "assistant", Content: json.RawMessage(`""`), ToolCalls: toolCalls})
		}
	}
	for _, tool := range r.Tools {
		if strings.TrimSpace(tool.Name) == "" {
			continue
		}
		schema := tool.InputSchema
		if !json.Valid(schema) {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		req.Tools = append(req.Tools, llm.Tool{Type: "function", Function: llm.ToolFunction{Name: tool.Name, Description: tool.Description, Parameters: schema}})
	}
	if r.ToolChoice != nil {
		switch r.ToolChoice.Type {
		case "auto":
			req.ToolChoice = json.RawMessage(`"auto"`)
		case "any":
			req.ToolChoice = json.RawMessage(`"required"`)
		case "tool":
			req.ToolChoice, _ = json.Marshal(map[string]any{"type": "function", "function": map[string]string{"name": r.ToolChoice.Name}})
		case "none":
			req.ToolChoice = json.RawMessage(`"none"`)
		}
	}
	if r.Metadata != nil {
		req.User = r.Metadata.UserID
	}
	return req, nil
}

type messagesInputBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

func messagesText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == "text" && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func messagesResponse(response llm.Response, model string) MessagesResponse {
	stopReason := "end_turn"
	out := MessagesResponse{ID: response.ID, Type: "message", Role: "assistant", Model: model, StopReason: &stopReason, Content: []MessagesContentBlock{}, Usage: MessagesUsage{InputTokens: response.Usage.PromptTokens, OutputTokens: response.Usage.CompletionTokens, CacheReadInputTokens: response.Usage.CacheReadTokens, CacheCreationInputTokens: response.Usage.CacheWriteTokens}}
	if out.ID == "" {
		out.ID = entities.NewID("msg")
	}
	if len(response.Choices) == 0 || response.Choices[0].Message == nil {
		return out
	}
	choice := response.Choices[0]
	if choice.Message.Content != "" {
		out.Content = append(out.Content, MessagesContentBlock{Type: "text", Text: choice.Message.Content})
	}
	for _, call := range choice.Message.ToolCalls {
		input := json.RawMessage(call.Function.Arguments)
		if !json.Valid(input) {
			input = json.RawMessage(`{}`)
		}
		out.Content = append(out.Content, MessagesContentBlock{Type: "tool_use", ID: call.ID, Name: call.Function.Name, Input: input})
	}
	switch choice.FinishReason {
	case "tool_calls":
		stopReason = "tool_use"
	case "length":
		stopReason = "max_tokens"
	}
	return out
}

type messagesStreamEmitter struct {
	model      string
	messageID  string
	textIndex  int
	nextIndex  int
	textOpen   bool
	toolOpen   map[int]int
	toolOrder  []int
	stopReason string
}

func newMessagesStreamEmitter(model string) *messagesStreamEmitter {
	return &messagesStreamEmitter{model: model, messageID: entities.NewID("msg"), textIndex: -1, toolOpen: make(map[int]int), stopReason: "end_turn"}
}

func (s *messagesStreamEmitter) Created(w *bufio.Writer) error {
	return s.write(w, "message_start", map[string]any{"message": MessagesResponse{ID: s.messageID, Type: "message", Role: "assistant", Model: s.model, Content: []MessagesContentBlock{}, Usage: MessagesUsage{}}})
}

func (s *messagesStreamEmitter) ChatChunk(w *bufio.Writer, raw []byte) error {
	var chunk llm.Chunk
	if err := json.Unmarshal(raw, &chunk); err != nil {
		return err
	}
	for _, choice := range chunk.Choices {
		if choice.Delta.Content != "" {
			if !s.textOpen {
				s.textIndex = s.nextIndex
				s.nextIndex++
				s.textOpen = true
				if err := s.write(w, "content_block_start", map[string]any{"index": s.textIndex, "content_block": MessagesContentBlock{Type: "text", Text: ""}}); err != nil {
					return err
				}
			}
			if err := s.write(w, "content_block_delta", map[string]any{"index": s.textIndex, "delta": map[string]any{"type": "text_delta", "text": choice.Delta.Content}}); err != nil {
				return err
			}
		}
		for _, call := range choice.Delta.ToolCalls {
			key := 0
			if call.Index != nil {
				key = *call.Index
			}
			index, exists := s.toolOpen[key]
			if !exists {
				index = s.nextIndex
				s.nextIndex++
				s.toolOpen[key] = index
				s.toolOrder = append(s.toolOrder, key)
				if err := s.write(w, "content_block_start", map[string]any{"index": index, "content_block": MessagesContentBlock{Type: "tool_use", ID: call.ID, Name: call.Function.Name, Input: json.RawMessage(`{}`)}}); err != nil {
					return err
				}
			}
			if call.Function.Arguments != "" {
				if err := s.write(w, "content_block_delta", map[string]any{"index": index, "delta": map[string]any{"type": "input_json_delta", "partial_json": call.Function.Arguments}}); err != nil {
					return err
				}
			}
		}
		switch choice.FinishReason {
		case "tool_calls":
			s.stopReason = "tool_use"
		case "length":
			s.stopReason = "max_tokens"
		}
	}
	return nil
}

func (s *messagesStreamEmitter) Completed(w *bufio.Writer, usage llm.Usage) error {
	if s.textOpen {
		if err := s.write(w, "content_block_stop", map[string]any{"index": s.textIndex}); err != nil {
			return err
		}
	}
	for _, key := range s.toolOrder {
		if err := s.write(w, "content_block_stop", map[string]any{"index": s.toolOpen[key]}); err != nil {
			return err
		}
	}
	if err := s.write(w, "message_delta", map[string]any{"delta": map[string]any{"stop_reason": s.stopReason, "stop_sequence": nil}, "usage": MessagesUsage{OutputTokens: usage.CompletionTokens}}); err != nil {
		return err
	}
	return s.write(w, "message_stop", map[string]any{})
}

func (s *messagesStreamEmitter) write(w *bufio.Writer, event string, data map[string]any) error {
	body, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err = w.WriteString("event: " + event + "\n" + "data: " + string(body) + "\n\n"); err != nil {
		return err
	}
	return w.Flush()
}
