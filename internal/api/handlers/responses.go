package handlers

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	responseapi "github.com/kimnt93/gorouter/internal/api"
	"github.com/kimnt93/gorouter/internal/platform/llm"
	"github.com/kimnt93/gorouter/pkg/entities"
)

// ResponsesRequest is the subset of the OpenAI Responses request understood by
// the gateway. Input is intentionally raw because the API permits either text
// or a list of role/content items.
type ResponsesRequest struct {
	Model           string          `json:"model"`
	Input           json.RawMessage `json:"input"`
	Instructions    string          `json:"instructions,omitempty"`
	Stream          bool            `json:"stream,omitempty"`
	Tools           []ResponsesTool `json:"tools,omitempty"`
	ToolChoice      json.RawMessage `json:"tool_choice,omitempty"`
	Reasoning       *llm.Reasoning  `json:"reasoning,omitempty"`
	MaxOutputTokens *int64          `json:"max_output_tokens,omitempty"`
}

// ResponsesTool uses the flat function-tool shape from the Responses API.
type ResponsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// Responses accepts OpenAI Responses API requests and routes them through the
// same authentication, quota, ownership, credential and usage path as chat.
// @Summary Create a response
// @Tags gateway
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param request body ResponsesRequest true "Responses request"
// @Success 200 {object} ResponsesResponse
// @Failure 400,401,403,404,429,500,502,503 {object} responseapi.ErrorResponse
// @Router /v1/responses [post]
func (g *Gateway) Responses(c fiber.Ctx) error {
	var input ResponsesRequest
	if err := c.Bind().Body(&input); err != nil {
		return responseapi.For(c).BadRequest("invalid responses request").Send()
	}
	req, err := input.chatRequest()
	if err != nil || req.Model == "" || len(req.Messages) == 0 {
		return responseapi.For(c).BadRequest("model and input are required").Send()
	}
	req.Stream = input.Stream
	raw, _ := json.Marshal(req)
	c.Request().SetBody(raw)
	c.Locals("responses_mode", true)
	if err := g.Chat(c); err != nil {
		return err
	}
	if input.Stream {
		return nil
	}
	if c.Response().StatusCode() < 200 || c.Response().StatusCode() >= 300 {
		return nil
	}
	body := append([]byte(nil), c.Response().Body()...)
	var chatResp llm.Response
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return responseapi.For(c).Error(fiber.StatusBadGateway, "invalid upstream response", "upstream_error", "invalid_response").Send()
	}
	text := responseText(chatResp)
	responseID := chatResp.ID
	if responseID == "" {
		responseID = entities.NewID("resp")
	}
	out := ResponsesResponse{
		ID: responseID, Object: "response", CreatedAt: time.Now().Unix(), Model: input.Model,
		Status: "completed", Background: false, OutputText: text,
		Output: []ResponsesOutput{{ID: entities.NewID("msg"), Type: "message", Role: "assistant", Content: []ResponsesContent{{Type: "output_text", Text: text, Annotations: []any{}, Logprobs: []any{}}}}},
		Usage: ResponsesUsage{
			InputTokens: chatResp.Usage.PromptTokens + chatResp.Usage.CacheReadTokens, OutputTokens: chatResp.Usage.CompletionTokens,
			TotalTokens:         chatResp.Usage.Total() + chatResp.Usage.CacheReadTokens,
			InputTokensDetails:  ResponsesInputTokensDetails{CachedTokens: chatResp.Usage.CacheReadTokens, CacheWriteTokens: chatResp.Usage.CacheWriteTokens},
			OutputTokensDetails: ResponsesOutputTokensDetails{},
		},
	}
	encoded, _ := json.Marshal(out)
	c.Set("Content-Type", "application/json")
	return c.Send(encoded)
}

type ResponsesResponse struct {
	ID         string            `json:"id"`
	Object     string            `json:"object"`
	CreatedAt  int64             `json:"created_at"`
	Model      string            `json:"model"`
	Status     string            `json:"status"`
	Background bool              `json:"background"`
	Error      *ResponsesError   `json:"error"`
	OutputText string            `json:"output_text"`
	Output     []ResponsesOutput `json:"output"`
	Usage      ResponsesUsage    `json:"usage"`
}
type ResponsesError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}
type ResponsesOutput struct {
	ID      string             `json:"id"`
	Type    string             `json:"type"`
	Role    string             `json:"role"`
	Content []ResponsesContent `json:"content"`
}
type ResponsesContent struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	Annotations []any  `json:"annotations"`
	Logprobs    []any  `json:"logprobs"`
}
type ResponsesUsage struct {
	InputTokens         int64                        `json:"input_tokens"`
	OutputTokens        int64                        `json:"output_tokens"`
	TotalTokens         int64                        `json:"total_tokens"`
	InputTokensDetails  ResponsesInputTokensDetails  `json:"input_tokens_details"`
	OutputTokensDetails ResponsesOutputTokensDetails `json:"output_tokens_details"`
}
type ResponsesInputTokensDetails struct {
	CacheWriteTokens int64 `json:"cache_write_tokens"`
	CachedTokens     int64 `json:"cached_tokens"`
}
type ResponsesOutputTokensDetails struct {
	ReasoningTokens int64 `json:"reasoning_tokens"`
}

func responseText(r llm.Response) string {
	if len(r.Choices) == 0 || r.Choices[0].Message == nil {
		return ""
	}
	return r.Choices[0].Message.Content
}

func (r ResponsesRequest) chatRequest() (*llm.ChatRequest, error) {
	request := &llm.ChatRequest{Model: r.Model, Reasoning: r.Reasoning, MaxCompletionTokens: r.MaxOutputTokens}
	for _, tool := range r.Tools {
		if tool.Type != "function" || strings.TrimSpace(tool.Name) == "" {
			continue
		}
		request.Tools = append(request.Tools, llm.Tool{Type: "function", Function: llm.ToolFunction{Name: tool.Name, Description: tool.Description, Parameters: tool.Parameters}})
	}
	request.ToolChoice = responsesToolChoice(r.ToolChoice)
	if r.Instructions != "" {
		request.Messages = append(request.Messages, llm.Message{Role: "developer", Content: quotedRaw(r.Instructions)})
	}
	if len(r.Input) == 0 {
		return request, nil
	}
	var text string
	if json.Unmarshal(r.Input, &text) == nil {
		request.Messages = append(request.Messages, llm.Message{Role: "user", Content: quotedRaw(text)})
		return request, nil
	}
	var items []responsesInputItem
	if err := json.Unmarshal(r.Input, &items); err != nil {
		return nil, err
	}
	for _, item := range items {
		switch item.Type {
		case "function_call":
			if item.CallID != "" && item.Name != "" {
				request.Messages = append(request.Messages, llm.Message{Role: "assistant", Content: json.RawMessage(`""`), ToolCalls: []llm.ToolCall{{ID: item.CallID, Type: "function", Function: llm.ToolFunction{Name: item.Name, Arguments: item.Arguments}}}})
			}
		case "function_call_output":
			request.Messages = append(request.Messages, llm.Message{Role: "tool", ToolCallID: item.CallID, Content: quotedRaw(responsesItemText(item.Output))})
		case "reasoning":
			continue
		default:
			role := item.Role
			if role == "" {
				role = "user"
			}
			content, contentErr := responsesContent(item.Content)
			if contentErr != nil {
				return nil, contentErr
			}
			request.Messages = append(request.Messages, llm.Message{Role: role, Content: content})
		}
	}
	return request, nil
}

type responsesInputItem struct {
	Type      string          `json:"type"`
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	CallID    string          `json:"call_id"`
	Name      string          `json:"name"`
	Arguments string          `json:"arguments"`
	Output    json.RawMessage `json:"output"`
}

func responsesContent(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage(`""`), nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return quotedRaw(text), nil
	}
	var parts []struct {
		Type     string          `json:"type"`
		Text     string          `json:"text"`
		ImageURL json.RawMessage `json:"image_url"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, err
	}
	if len(parts) == 1 && (parts[0].Type == "input_text" || parts[0].Type == "output_text" || parts[0].Type == "text") {
		return quotedRaw(parts[0].Text), nil
	}
	converted := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "input_text", "output_text", "text":
			converted = append(converted, map[string]any{"type": "text", "text": part.Text})
		case "input_image", "image_url":
			var imageURL any
			if len(part.ImageURL) != 0 {
				_ = json.Unmarshal(part.ImageURL, &imageURL)
			}
			converted = append(converted, map[string]any{"type": "image_url", "image_url": imageURL})
		}
	}
	if len(converted) == 0 {
		return json.RawMessage(`""`), nil
	}
	encoded, err := json.Marshal(converted)
	return encoded, err
}

func responsesItemText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	content, err := responsesContent(raw)
	if err != nil {
		return ""
	}
	if json.Unmarshal(content, &text) == nil {
		return text
	}
	return string(content)
}

func responsesToolChoice(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var choice struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &choice) == nil && choice.Type == "function" && choice.Name != "" {
		converted, _ := json.Marshal(map[string]any{"type": "function", "function": map[string]string{"name": choice.Name}})
		return converted
	}
	return raw
}

func quotedRaw(value string) json.RawMessage {
	encoded, _ := json.Marshal(value)
	return encoded
}

type responsesStreamEmitter struct {
	responseID string
	messageID  string
	model      string
	createdAt  int64
	sequence   int
	text       strings.Builder
	message    bool
	messageOut int
	tools      map[int]*responsesStreamTool
	nextOutput int
}

type responsesStreamTool struct {
	OutputIndex int
	ItemID      string
	CallID      string
	Name        string
	Arguments   strings.Builder
	Started     bool
}

func newResponsesStreamEmitter(model string) *responsesStreamEmitter {
	return &responsesStreamEmitter{
		responseID: entities.NewID("resp"), messageID: entities.NewID("msg"),
		model: model, createdAt: time.Now().Unix(), messageOut: -1,
		tools: make(map[int]*responsesStreamTool),
	}
}

func (s *responsesStreamEmitter) Created(w *bufio.Writer) error {
	return s.write(w, "response.created", map[string]any{"response": s.response("in_progress", nil, nil)})
}

func (s *responsesStreamEmitter) ChatChunk(w *bufio.Writer, raw []byte) error {
	var chunk llm.Chunk
	if err := json.Unmarshal(raw, &chunk); err != nil {
		return nil
	}
	for _, choice := range chunk.Choices {
		if choice.Delta.Content != "" {
			if err := s.startMessage(w); err != nil {
				return err
			}
			s.text.WriteString(choice.Delta.Content)
			if err := s.write(w, "response.output_text.delta", map[string]any{
				"item_id": s.messageID, "output_index": s.messageOut, "content_index": 0,
				"delta": choice.Delta.Content, "logprobs": []any{},
			}); err != nil {
				return err
			}
		}
		for _, call := range choice.Delta.ToolCalls {
			if err := s.toolDelta(w, call); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *responsesStreamEmitter) startMessage(w *bufio.Writer) error {
	if s.message {
		return nil
	}
	s.message = true
	s.messageOut = s.nextOutput
	s.nextOutput++
	item := map[string]any{"id": s.messageID, "type": "message", "status": "in_progress", "role": "assistant", "content": []any{}}
	if err := s.write(w, "response.output_item.added", map[string]any{"output_index": s.messageOut, "item": item}); err != nil {
		return err
	}
	part := map[string]any{"type": "output_text", "text": "", "annotations": []any{}, "logprobs": []any{}}
	return s.write(w, "response.content_part.added", map[string]any{"item_id": s.messageID, "output_index": s.messageOut, "content_index": 0, "part": part})
}

func (s *responsesStreamEmitter) toolDelta(w *bufio.Writer, call llm.ToolCall) error {
	index := 0
	if call.Index != nil {
		index = *call.Index
	}
	tool := s.tools[index]
	if tool == nil {
		tool = &responsesStreamTool{OutputIndex: s.nextOutput, ItemID: entities.NewID("fc")}
		s.nextOutput++
		s.tools[index] = tool
	}
	if call.ID != "" {
		tool.CallID = call.ID
	}
	if call.Function.Name != "" {
		tool.Name = call.Function.Name
	}
	if !tool.Started && tool.CallID != "" && tool.Name != "" {
		tool.Started = true
		item := map[string]any{"id": tool.ItemID, "type": "function_call", "status": "in_progress", "call_id": tool.CallID, "name": tool.Name, "arguments": ""}
		if err := s.write(w, "response.output_item.added", map[string]any{"output_index": tool.OutputIndex, "item": item}); err != nil {
			return err
		}
	}
	if call.Function.Arguments != "" {
		tool.Arguments.WriteString(call.Function.Arguments)
		if tool.Started {
			return s.write(w, "response.function_call_arguments.delta", map[string]any{"item_id": tool.ItemID, "output_index": tool.OutputIndex, "delta": call.Function.Arguments})
		}
	}
	return nil
}

func (s *responsesStreamEmitter) Completed(w *bufio.Writer, usage llm.Usage) error {
	indexedOutput := make([]any, s.nextOutput)
	if s.message {
		part := map[string]any{"type": "output_text", "text": s.text.String(), "annotations": []any{}, "logprobs": []any{}}
		if err := s.write(w, "response.output_text.done", map[string]any{"item_id": s.messageID, "output_index": s.messageOut, "content_index": 0, "text": s.text.String(), "logprobs": []any{}}); err != nil {
			return err
		}
		if err := s.write(w, "response.content_part.done", map[string]any{"item_id": s.messageID, "output_index": s.messageOut, "content_index": 0, "part": part}); err != nil {
			return err
		}
		item := map[string]any{"id": s.messageID, "type": "message", "status": "completed", "role": "assistant", "content": []any{part}}
		if err := s.write(w, "response.output_item.done", map[string]any{"output_index": s.messageOut, "item": item}); err != nil {
			return err
		}
		indexedOutput[s.messageOut] = item
	}
	for index := 0; index < len(s.tools); index++ {
		tool := s.tools[index]
		if tool == nil || !tool.Started {
			continue
		}
		item := map[string]any{"id": tool.ItemID, "type": "function_call", "status": "completed", "call_id": tool.CallID, "name": tool.Name, "arguments": tool.Arguments.String()}
		if err := s.write(w, "response.function_call_arguments.done", map[string]any{"item_id": tool.ItemID, "output_index": tool.OutputIndex, "arguments": tool.Arguments.String()}); err != nil {
			return err
		}
		if err := s.write(w, "response.output_item.done", map[string]any{"output_index": tool.OutputIndex, "item": item}); err != nil {
			return err
		}
		indexedOutput[tool.OutputIndex] = item
	}
	output := make([]any, 0, len(indexedOutput))
	for _, item := range indexedOutput {
		if item != nil {
			output = append(output, item)
		}
	}
	responseUsage := map[string]any{
		"input_tokens":          usage.PromptTokens + usage.CacheReadTokens,
		"input_tokens_details":  map[string]any{"cached_tokens": usage.CacheReadTokens},
		"output_tokens":         usage.CompletionTokens,
		"output_tokens_details": map[string]any{"reasoning_tokens": 0},
		"total_tokens":          usage.Total() + usage.CacheReadTokens,
	}
	if err := s.write(w, "response.completed", map[string]any{"response": s.response("completed", output, responseUsage)}); err != nil {
		return err
	}
	_, err := w.WriteString("data: [DONE]\n\n")
	if err == nil {
		err = w.Flush()
	}
	return err
}

func (s *responsesStreamEmitter) response(status string, output []any, usage any) map[string]any {
	if output == nil {
		output = []any{}
	}
	return map[string]any{
		"id": s.responseID, "object": "response", "created_at": s.createdAt,
		"status": status, "model": s.model, "output": output, "usage": usage,
		"error": nil, "incomplete_details": nil,
	}
}

func (s *responsesStreamEmitter) write(w *bufio.Writer, eventType string, fields map[string]any) error {
	s.sequence++
	fields["type"] = eventType
	fields["sequence_number"] = s.sequence
	encoded, err := json.Marshal(fields)
	if err != nil {
		return fmt.Errorf("encode responses stream event: %w", err)
	}
	if _, err = w.WriteString("event: " + eventType + "\ndata: " + string(encoded) + "\n\n"); err != nil {
		return err
	}
	return w.Flush()
}
