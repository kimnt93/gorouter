package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
)

// Keep the backend fingerprint aligned with the current Codex CLI protocol.
// OpenAI's model catalog can return an empty list for stale client versions.
const codexClientVersion = "0.149.0"
const codexChatInstructions = "You are a ChatGPT agent."

type CodexAdapter struct {
	HTTP    *http.Client
	Refresh func(context.Context, *entities.CredentialRuntime) error
}

type codexContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL any    `json:"image_url,omitempty"`
}

type codexInput struct {
	Type      string         `json:"type"`
	Role      string         `json:"role,omitempty"`
	Content   []codexContent `json:"content,omitempty"`
	CallID    string         `json:"call_id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Arguments string         `json:"arguments,omitempty"`
	Output    *string        `json:"output,omitempty"`
	Status    string         `json:"status,omitempty"`
}

type codexTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type codexRequest struct {
	Model          string          `json:"model"`
	Instructions   string          `json:"instructions,omitempty"`
	Input          []codexInput    `json:"input"`
	Tools          []codexTool     `json:"tools,omitempty"`
	ToolChoice     json.RawMessage `json:"tool_choice,omitempty"`
	Reasoning      *Reasoning      `json:"reasoning,omitempty"`
	Stream         bool            `json:"stream"`
	Store          bool            `json:"store"`
	PromptCacheKey string          `json:"prompt_cache_key,omitempty"`
}

func codexBase(baseURL string) string {
	base := strings.TrimRight(baseURL, "/")
	if base == "" {
		base = "https://chatgpt.com/backend-api"
	}
	for _, suffix := range []string{"/codex/responses", "/codex/models"} {
		if strings.HasSuffix(base, suffix) {
			base = strings.TrimRight(strings.TrimSuffix(base, suffix), "/")
			break
		}
	}
	if strings.HasSuffix(base, "/codex") {
		return base
	}
	return base + "/codex"
}

func codexHeaders(cr *entities.CredentialRuntime) map[string]string {
	headers := map[string]string{
		"Authorization":         "Bearer " + cr.OAuthAccess,
		"Accept":                "text/event-stream",
		"Openai-Beta":           "responses=experimental",
		"X-Codex-Beta-Features": "responses_websockets",
		"originator":            "codex_cli_rs",
		"User-Agent":            "codex_cli_rs/" + codexClientVersion,
		"Version":               codexClientVersion,
	}
	if cr.OAuthAccount != "" {
		headers["chatgpt-account-id"] = cr.OAuthAccount
	}
	return headers
}

func messageText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		var values []string
		for _, part := range parts {
			if part.Text != "" {
				values = append(values, part.Text)
			}
		}
		return strings.Join(values, "\n")
	}
	return string(raw)
}

func codexMessageContent(raw json.RawMessage, role string) []codexContent {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		contentType := "input_text"
		if role == "assistant" {
			contentType = "output_text"
		}
		if text == "" {
			return nil
		}
		return []codexContent{{Type: contentType, Text: text}}
	}
	var parts []struct {
		Type     string          `json:"type"`
		Text     string          `json:"text"`
		ImageURL json.RawMessage `json:"image_url"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return nil
	}
	out := make([]codexContent, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "text", "input_text", "output_text":
			contentType := "input_text"
			if role == "assistant" {
				contentType = "output_text"
			}
			if part.Text != "" {
				out = append(out, codexContent{Type: contentType, Text: part.Text})
			}
		case "image_url", "input_image":
			if role == "assistant" || len(part.ImageURL) == 0 || string(part.ImageURL) == "null" {
				continue
			}
			var imageURL any
			if json.Unmarshal(part.ImageURL, &imageURL) == nil {
				out = append(out, codexContent{Type: "input_image", ImageURL: imageURL})
			}
		}
	}
	return out
}

func toCodexRequest(request *ChatRequest, model string) codexRequest {
	cacheKey := ProviderPromptCacheKey(request)
	out := codexRequest{Model: model, Stream: true, Store: false, Reasoning: request.Reasoning, PromptCacheKey: cacheKey}
	out.ToolChoice = codexToolChoice(request.ToolChoice)
	var instructions []string
	knownCallIDs := make(map[string]bool)
	for _, message := range request.Messages {
		for _, call := range message.ToolCalls {
			if call.ID != "" && strings.TrimSpace(call.Function.Name) != "" {
				knownCallIDs[call.ID] = true
			}
		}
	}
	for _, message := range request.Messages {
		text := messageText(message.Content)
		if message.Role == "system" || message.Role == "developer" {
			if text != "" {
				instructions = append(instructions, text)
			}
			continue
		}
		if message.Role == "tool" {
			if knownCallIDs[message.ToolCallID] {
				out.Input = append(out.Input, codexInput{Type: "function_call_output", CallID: message.ToolCallID, Output: stringPointer(text), Status: "completed"})
			}
			continue
		}
		role := message.Role
		if role != "assistant" {
			role = "user"
		}
		content := codexMessageContent(message.Content, role)
		if len(content) > 0 {
			out.Input = append(out.Input, codexInput{Type: "message", Role: role, Content: content})
		}
		if role == "assistant" {
			for _, call := range message.ToolCalls {
				name := strings.TrimSpace(call.Function.Name)
				if name == "" || call.ID == "" {
					continue
				}
				arguments := call.Function.Arguments
				if arguments == "" {
					arguments = "{}"
				}
				out.Input = append(out.Input, codexInput{Type: "function_call", CallID: call.ID, Name: name, Arguments: arguments, Status: "completed"})
			}
		}
	}
	out.Instructions = strings.Join(instructions, "\n\n")
	if strings.TrimSpace(out.Instructions) == "" {
		// The Codex Responses backend rejects a request without instructions,
		// including otherwise valid one-turn chat requests.
		out.Instructions = codexChatInstructions
	}
	if len(out.Input) == 0 {
		out.Input = []codexInput{{Type: "message", Role: "user", Content: []codexContent{{Type: "input_text", Text: "..."}}}}
	}
	for _, tool := range request.Tools {
		if tool.Type != "function" || tool.Function.Name == "" {
			continue
		}
		out.Tools = append(out.Tools, codexTool{Type: "function", Name: tool.Function.Name, Description: tool.Function.Description, Parameters: tool.Function.Parameters})
	}
	outputIDs := make(map[string]bool)
	for _, item := range out.Input {
		if item.Type == "function_call_output" {
			outputIDs[item.CallID] = true
		}
	}
	if len(outputIDs) != len(knownCallIDs) {
		repaired := make([]codexInput, 0, len(out.Input)+len(knownCallIDs)-len(outputIDs))
		for _, item := range out.Input {
			repaired = append(repaired, item)
			if item.Type == "function_call" && !outputIDs[item.CallID] {
				repaired = append(repaired, codexInput{Type: "function_call_output", CallID: item.CallID, Output: stringPointer(""), Status: "completed"})
				outputIDs[item.CallID] = true
			}
		}
		out.Input = repaired
	}
	return out
}

func stringPointer(value string) *string {
	return &value
}

func codexToolChoice(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	choice, ok := value.(map[string]any)
	if !ok || choice["type"] != "function" {
		return raw
	}
	function, ok := choice["function"].(map[string]any)
	if !ok {
		return raw
	}
	name, _ := function["name"].(string)
	if name == "" {
		return raw
	}
	converted, _ := json.Marshal(map[string]string{"type": "function", "name": name})
	return converted
}

func (a *CodexAdapter) Send(ctx context.Context, cr *entities.CredentialRuntime, upstreamModel string, rawBody []byte) (*entities.UpstreamResult, error) {
	var request ChatRequest
	if err := json.Unmarshal(rawBody, &request); err != nil {
		return nil, fmt.Errorf("parse Codex request: %w", err)
	}
	payload, err := json.Marshal(toCodexRequest(&request, upstreamModel))
	if err != nil {
		return nil, err
	}
	client := a.HTTP
	if client == nil {
		client = NewHTTPClient()
	}
	headers := codexHeaders(cr)
	if sessionID := ProviderPromptCacheKey(&request); sessionID != "" {
		headers["session_id"] = sessionID
	}
	send := func() (*entities.UpstreamResult, error) {
		return postJSON(ctx, client, codexBase(cr.BaseURL)+"/responses", headers, payload)
	}
	result, err := send()
	if err != nil {
		return nil, err
	}
	if result.StatusCode == http.StatusUnauthorized && a.Refresh != nil {
		result.Body.Close()
		if err := a.Refresh(ctx, cr); err != nil {
			return nil, fmt.Errorf("Codex OAuth refresh failed: %w", err)
		}
		headers = codexHeaders(cr)
		if sessionID := ProviderPromptCacheKey(&request); sessionID != "" {
			headers["session_id"] = sessionID
		}
		result, err = send()
		if err != nil {
			return nil, err
		}
	}
	if result.StatusCode < 200 || result.StatusCode >= 300 {
		return result, nil
	}
	if request.Stream {
		reader, writer := io.Pipe()
		upstream := result.Body
		go func() {
			defer upstream.Close()
			err := transformCodexStream(upstream, writer, upstreamModel)
			_ = writer.CloseWithError(err)
		}()
		result.Body = reader
		result.Header = http.Header{"Content-Type": []string{"text/event-stream"}}
		return result, nil
	}
	response, err := collectCodexResponse(result.Body, upstreamModel)
	result.Body.Close()
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(response)
	result.Body = io.NopCloser(bytes.NewReader(body))
	result.Header = http.Header{"Content-Type": []string{"application/json"}}
	return result, nil
}

type codexEvent struct {
	Type        string          `json:"type"`
	Delta       string          `json:"delta"`
	OutputIndex int             `json:"output_index"`
	ItemID      string          `json:"item_id"`
	Item        codexOutputItem `json:"item"`
	Response    struct {
		ID     string `json:"id"`
		Model  string `json:"model"`
		Status string `json:"status"`
		Usage  struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
			InputDetails struct {
				CachedTokens int64 `json:"cached_tokens"`
			} `json:"input_tokens_details"`
		} `json:"usage"`
		Output []codexOutputItem `json:"output"`
	} `json:"response"`
}

type codexOutputItem struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Content   []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func codexUsage(totalInput, output, cachedInput int64) Usage {
	if cachedInput < 0 {
		cachedInput = 0
	}
	if cachedInput > totalInput {
		cachedInput = totalInput
	}
	return Usage{
		PromptTokens:     totalInput - cachedInput,
		CompletionTokens: output,
		CacheReadTokens:  cachedInput,
	}
}

type codexToolStreamState struct {
	Index     int
	ID        string
	Name      string
	Arguments string
	Started   bool
	Done      bool
}

func toolCallDelta(index int, id, name, arguments string, includeHeader bool) ToolCall {
	call := ToolCall{Index: &index, Function: ToolFunction{Arguments: arguments}}
	if includeHeader {
		call.ID = id
		call.Type = "function"
		call.Function.Name = name
	}
	return call
}

func transformCodexStream(source io.Reader, target io.Writer, model string) error {
	writer := bufio.NewWriter(target)
	created := time.Now().Unix()
	id := "chatcmpl-codex"
	started := false
	completed := false
	toolCalls := make(map[string]*codexToolStreamState)
	toolOrder := make([]*codexToolStreamState, 0)
	currentTool := ""
	writeChunk := func(chunk Chunk) error {
		body, _ := json.Marshal(chunk)
		if _, err := writer.WriteString("data: " + string(body) + "\n\n"); err != nil {
			return err
		}
		return writer.Flush()
	}
	err := ScanSSE(source, func(event SSEEvent) error {
		var value codexEvent
		if json.Unmarshal(event.Data, &value) != nil {
			return nil
		}
		if value.Response.ID != "" {
			id = value.Response.ID
		}
		if value.Type == "response.output_text.delta" && value.Delta != "" {
			delta := Delta{Content: value.Delta}
			if !started {
				delta.Role = "assistant"
				started = true
			}
			return writeChunk(Chunk{ID: id, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []ChunkChoice{{Index: 0, Delta: delta}}})
		}
		if value.Type == "response.output_item.added" && value.Item.Type == "function_call" {
			key := value.Item.ID
			if key == "" {
				key = value.Item.CallID
			}
			if key == "" {
				key = fmt.Sprintf("tool-%d", len(toolOrder))
			}
			state := &codexToolStreamState{Index: len(toolOrder), ID: value.Item.CallID, Name: value.Item.Name, Arguments: value.Item.Arguments}
			if state.ID == "" {
				state.ID = key
			}
			toolCalls[key] = state
			if value.Item.CallID != "" {
				toolCalls[value.Item.CallID] = state
			}
			toolOrder = append(toolOrder, state)
			currentTool = key
			if state.Name == "" {
				return nil
			}
			state.Started = true
			return writeChunk(Chunk{ID: id, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []ChunkChoice{{Index: 0, Delta: Delta{ToolCalls: []ToolCall{toolCallDelta(state.Index, state.ID, state.Name, "", true)}}}}})
		}
		if value.Type == "response.function_call_arguments.delta" && value.Delta != "" {
			key := value.ItemID
			if key == "" {
				key = currentTool
			}
			state := toolCalls[key]
			if state == nil {
				return nil
			}
			state.Arguments += value.Delta
			if !state.Started {
				return nil
			}
			return writeChunk(Chunk{ID: id, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []ChunkChoice{{Index: 0, Delta: Delta{ToolCalls: []ToolCall{toolCallDelta(state.Index, "", "", value.Delta, false)}}}}})
		}
		if value.Type == "response.output_item.done" && value.Item.Type == "function_call" {
			key := value.Item.ID
			if key == "" {
				key = value.Item.CallID
			}
			state := toolCalls[key]
			if state == nil {
				state = &codexToolStreamState{Index: len(toolOrder), ID: value.Item.CallID, Name: value.Item.Name}
				toolOrder = append(toolOrder, state)
			}
			if state.ID == "" {
				state.ID = value.Item.CallID
			}
			if state.Name == "" {
				state.Name = value.Item.Name
			}
			if state.ID == "" || state.Name == "" {
				return nil
			}
			if !state.Started {
				state.Started = true
				state.Arguments = value.Item.Arguments
				state.Done = true
				return writeChunk(Chunk{ID: id, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []ChunkChoice{{Index: 0, Delta: Delta{ToolCalls: []ToolCall{toolCallDelta(state.Index, state.ID, state.Name, value.Item.Arguments, true)}}}}})
			}
			state.Done = true
			if value.Item.Arguments != "" && strings.HasPrefix(value.Item.Arguments, state.Arguments) && len(value.Item.Arguments) > len(state.Arguments) {
				suffix := value.Item.Arguments[len(state.Arguments):]
				state.Arguments = value.Item.Arguments
				return writeChunk(Chunk{ID: id, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []ChunkChoice{{Index: 0, Delta: Delta{ToolCalls: []ToolCall{toolCallDelta(state.Index, "", "", suffix, false)}}}}})
			}
			return nil
		}
		if value.Type == "response.failed" || value.Type == "response.incomplete" || value.Type == "error" {
			return fmt.Errorf("Codex stream ended with %s", value.Type)
		}
		if value.Type == "response.completed" {
			completed = true
			for _, item := range value.Response.Output {
				if item.Type != "function_call" || item.CallID == "" || item.Name == "" {
					continue
				}
				seen := false
				for _, state := range toolOrder {
					if state.ID == item.CallID {
						seen = true
						break
					}
				}
				if !seen {
					index := len(toolOrder)
					toolOrder = append(toolOrder, &codexToolStreamState{Index: index, ID: item.CallID, Name: item.Name, Arguments: item.Arguments, Started: true, Done: true})
					if err := writeChunk(Chunk{ID: id, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []ChunkChoice{{Index: 0, Delta: Delta{ToolCalls: []ToolCall{toolCallDelta(index, item.CallID, item.Name, item.Arguments, true)}}}}}); err != nil {
						return err
					}
				}
			}
			usage := codexUsage(value.Response.Usage.InputTokens, value.Response.Usage.OutputTokens, value.Response.Usage.InputDetails.CachedTokens)
			finishReason := "stop"
			if len(toolOrder) != 0 {
				finishReason = "tool_calls"
			}
			return writeChunk(Chunk{ID: id, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []ChunkChoice{{Index: 0, Delta: Delta{}, FinishReason: finishReason}}, Usage: &usage})
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !completed {
		return io.ErrUnexpectedEOF
	}
	_, err = writer.WriteString("data: [DONE]\n\n")
	if err == nil {
		err = writer.Flush()
	}
	return err
}

func collectCodexResponse(source io.Reader, model string) (Response, error) {
	response := Response{ID: "chatcmpl-codex", Object: "chat.completion", Created: time.Now().Unix(), Model: model}
	completed := false
	var content strings.Builder
	toolCalls := make(map[string]*ToolCall)
	toolOrder := make([]string, 0)
	currentTool := ""
	err := ScanSSE(source, func(event SSEEvent) error {
		var value codexEvent
		if err := json.Unmarshal(event.Data, &value); err != nil {
			return nil
		}
		if value.Response.ID != "" {
			response.ID = value.Response.ID
		}
		if value.Type == "response.output_text.delta" {
			content.WriteString(value.Delta)
		}
		if value.Type == "response.output_item.added" && value.Item.Type == "function_call" {
			key := value.Item.ID
			if key == "" {
				key = value.Item.CallID
			}
			if key != "" {
				call := &ToolCall{ID: value.Item.CallID, Type: "function", Function: ToolFunction{Name: value.Item.Name, Arguments: value.Item.Arguments}}
				toolCalls[key] = call
				if value.Item.CallID != "" {
					toolCalls[value.Item.CallID] = call
				}
				toolOrder = append(toolOrder, key)
				currentTool = key
			}
		}
		if value.Type == "response.function_call_arguments.delta" {
			key := value.ItemID
			if key == "" {
				key = currentTool
			}
			if call := toolCalls[key]; call != nil {
				call.Function.Arguments += value.Delta
			}
		}
		if value.Type == "response.output_item.done" && value.Item.Type == "function_call" {
			key := value.Item.ID
			if key == "" {
				key = value.Item.CallID
			}
			call := toolCalls[key]
			if call == nil && key != "" {
				call = &ToolCall{}
				toolCalls[key] = call
				toolOrder = append(toolOrder, key)
			}
			if call != nil {
				call.ID = value.Item.CallID
				call.Type = "function"
				call.Function.Name = value.Item.Name
				if value.Item.Arguments != "" {
					call.Function.Arguments = value.Item.Arguments
				}
			}
		}
		if value.Type == "response.failed" || value.Type == "response.incomplete" || value.Type == "error" {
			return fmt.Errorf("Codex response ended with %s", value.Type)
		}
		if value.Type == "response.completed" {
			completed = true
			response.Usage = codexUsage(value.Response.Usage.InputTokens, value.Response.Usage.OutputTokens, value.Response.Usage.InputDetails.CachedTokens)
			if content.Len() == 0 {
				for _, item := range value.Response.Output {
					for _, part := range item.Content {
						if part.Type == "output_text" {
							content.WriteString(part.Text)
						}
					}
				}
			}
			for _, item := range value.Response.Output {
				if item.Type != "function_call" || item.CallID == "" || item.Name == "" {
					continue
				}
				call := toolCalls[item.CallID]
				if call == nil {
					call = &ToolCall{}
					toolCalls[item.CallID] = call
					toolOrder = append(toolOrder, item.CallID)
				}
				call.ID = item.CallID
				call.Type = "function"
				call.Function.Name = item.Name
				call.Function.Arguments = item.Arguments
			}
		}
		return nil
	})
	if err != nil {
		return Response{}, err
	}
	if !completed {
		return Response{}, io.ErrUnexpectedEOF
	}
	message := &ResponseMessage{Role: "assistant", Content: content.String()}
	seen := make(map[*ToolCall]bool)
	for _, key := range toolOrder {
		call := toolCalls[key]
		if call == nil || seen[call] || call.ID == "" || call.Function.Name == "" {
			continue
		}
		seen[call] = true
		message.ToolCalls = append(message.ToolCalls, *call)
	}
	finishReason := "stop"
	if len(message.ToolCalls) != 0 {
		finishReason = "tool_calls"
	}
	response.Choices = []Choice{{Index: 0, Message: message, FinishReason: finishReason}}
	return response, nil
}

func (a *CodexAdapter) Probe(ctx context.Context, cr *entities.CredentialRuntime) (int, error) {
	payload := []byte(`{"model":"gpt-5.5","input":[],"stream":false,"store":false}`)
	client := a.HTTP
	if client == nil {
		client = NewHTTPClient()
	}
	headers := codexHeaders(cr)
	headers["Accept"] = "application/json"
	result, err := postJSON(ctx, client, codexBase(cr.BaseURL)+"/responses", headers, payload)
	if err != nil {
		return 0, err
	}
	defer result.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(result.Body, 64<<10))
	if result.StatusCode == http.StatusBadRequest {
		return http.StatusNoContent, nil
	}
	return result.StatusCode, nil
}

func (a *CodexAdapter) DiscoverModels(ctx context.Context, cr *entities.CredentialRuntime) ([]credential.ProviderModel, error) {
	client := a.HTTP
	if client == nil {
		client = NewHTTPClient()
	}
	endpoint := codexBase(cr.BaseURL) + "/models?client_version=" + url.QueryEscape(codexClientVersion)
	headers := codexHeaders(cr)
	headers["Accept"] = "application/json"
	headers["Content-Type"] = "application/json"
	result, err := get(ctx, client, endpoint, headers)
	if err != nil {
		return nil, err
	}
	defer result.Body.Close()
	if result.StatusCode < 200 || result.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(result.Body, 64<<10))
		return nil, fmt.Errorf("Codex model endpoint returned HTTP %d", result.StatusCode)
	}
	var payload json.RawMessage
	if err := json.NewDecoder(io.LimitReader(result.Body, maxModelCatalogBytes)).Decode(&payload); err != nil {
		return nil, err
	}
	var root map[string]json.RawMessage
	var items []json.RawMessage
	if json.Unmarshal(payload, &root) == nil {
		_ = json.Unmarshal(root["models"], &items)
		if len(items) == 0 {
			_ = json.Unmarshal(root["data"], &items)
		}
		if len(items) == 0 {
			for id, raw := range root {
				if id == "object" || id == "models" || id == "data" {
					continue
				}
				var record map[string]json.RawMessage
				if json.Unmarshal(raw, &record) == nil && record != nil {
					if _, ok := record["id"]; !ok {
						record["id"], _ = json.Marshal(id)
					}
					encoded, _ := json.Marshal(record)
					items = append(items, encoded)
				}
			}
		}
	} else {
		_ = json.Unmarshal(payload, &items)
	}
	seen := map[string]credential.ProviderModel{}
	for _, item := range items {
		var record struct {
			Object                   string                         `json:"object"`
			Created                  int64                          `json:"created"`
			Slug                     string                         `json:"slug"`
			ID                       string                         `json:"id"`
			Model                    string                         `json:"model"`
			Name                     string                         `json:"name"`
			DisplayName              string                         `json:"display_name"`
			DisplayNameCamel         string                         `json:"displayName"`
			OwnedBy                  string                         `json:"owned_by"`
			OwnedByCamel             string                         `json:"ownedBy"`
			Permission               []any                          `json:"permission"`
			Root                     string                         `json:"root"`
			Parent                   *string                        `json:"parent"`
			APIFormat                string                         `json:"api_format"`
			APIFormatCamel           string                         `json:"apiFormat"`
			Visibility               string                         `json:"visibility"`
			Supported                *bool                          `json:"supported_in_api"`
			SupportedInAPI           *bool                          `json:"supportedInApi"`
			Context                  int64                          `json:"context_window"`
			ContextCamel             int64                          `json:"contextWindow"`
			ContextLength            int64                          `json:"context_length"`
			MaxContextWindow         int64                          `json:"max_context_window"`
			MaxContextCamel          int64                          `json:"maxContextWindow"`
			MaxInputTokens           int64                          `json:"max_input_tokens"`
			MaxInputCamel            int64                          `json:"maxInputTokens"`
			MaxOutputTokens          int64                          `json:"max_output_tokens"`
			MaxOutputCamel           int64                          `json:"maxOutputTokens"`
			SupportedEndpoints       []string                       `json:"supported_endpoints"`
			EndpointsCamel           []string                       `json:"supportedEndpoints"`
			Capabilities             map[string]any                 `json:"capabilities"`
			InputModalities          []string                       `json:"input_modalities"`
			InputCamel               []string                       `json:"inputModalities"`
			OutputModalities         []string                       `json:"output_modalities"`
			OutputCamel              []string                       `json:"outputModalities"`
			Description              string                         `json:"description"`
			DefaultReasoning         string                         `json:"default_reasoning_level"`
			DefaultReasoningCamel    string                         `json:"defaultReasoningLevel"`
			ReasoningLevels          []entities.ModelReasoningLevel `json:"supported_reasoning_levels"`
			ReasoningLevelsCamel     []entities.ModelReasoningLevel `json:"supportedReasoningLevels"`
			SupportsOriginalImage    bool                           `json:"supports_image_detail_original"`
			SupportsReasoningSummary bool                           `json:"supports_reasoning_summary_parameter"`
			SupportsParallelTools    bool                           `json:"supports_parallel_tool_calls"`
			SupportsVerbosity        bool                           `json:"support_verbosity"`
			DefaultVerbosity         string                         `json:"default_verbosity"`
		}
		if json.Unmarshal(item, &record) != nil || strings.EqualFold(record.Visibility, "hide") || (record.Supported != nil && !*record.Supported) || (record.SupportedInAPI != nil && !*record.SupportedInAPI) {
			continue
		}
		id := record.Slug
		if id == "" {
			id = record.ID
		}
		if id == "" {
			id = record.Model
		}
		if id == "" {
			id = record.Name
		}
		if id == "" {
			continue
		}
		// Preserve the upstream context_length semantics in the public model
		// listing. max_input_tokens is a separate limit and must not replace it.
		contextLength := record.ContextLength
		if contextLength == 0 {
			contextLength = record.ContextCamel
		}
		if contextLength == 0 {
			contextLength = record.MaxContextWindow
		}
		if contextLength == 0 {
			contextLength = record.MaxContextCamel
		}
		if contextLength == 0 {
			contextLength = record.Context
		}
		if contextLength == 0 {
			contextLength = record.MaxInputTokens
		}
		if contextLength == 0 {
			contextLength = record.MaxInputCamel
		}
		object := record.Object
		if object == "" {
			object = "model"
		}
		ownedBy := record.OwnedBy
		if ownedBy == "" {
			ownedBy = record.OwnedByCamel
		}
		if ownedBy == "" {
			ownedBy = "codex"
		}
		apiFormat := record.APIFormat
		if apiFormat == "" {
			apiFormat = record.APIFormatCamel
		}
		name := record.Name
		if name == "" {
			name = record.DisplayName
		}
		if name == "" {
			name = record.DisplayNameCamel
		}
		maxInput := record.MaxInputTokens
		if maxInput == 0 {
			maxInput = record.MaxInputCamel
		}
		maxOutput := record.MaxOutputTokens
		if maxOutput == 0 {
			maxOutput = record.MaxOutputCamel
		}
		endpoints := record.SupportedEndpoints
		if len(endpoints) == 0 {
			endpoints = record.EndpointsCamel
		}
		inputModalities := record.InputModalities
		if len(inputModalities) == 0 {
			inputModalities = record.InputCamel
		}
		outputModalities := record.OutputModalities
		if len(outputModalities) == 0 {
			outputModalities = record.OutputCamel
		}
		defaultReasoning := record.DefaultReasoning
		if defaultReasoning == "" {
			defaultReasoning = record.DefaultReasoningCamel
		}
		reasoningLevels := record.ReasoningLevels
		if len(reasoningLevels) == 0 {
			reasoningLevels = record.ReasoningLevelsCamel
		}
		maxContext := record.MaxContextWindow
		if maxContext == 0 {
			maxContext = record.MaxContextCamel
		}
		model := credential.ProviderModel{
			ID:                 id,
			Object:             object,
			Created:            record.Created,
			OwnedBy:            ownedBy,
			Permission:         record.Permission,
			Root:               record.Root,
			Parent:             record.Parent,
			APIFormat:          apiFormat,
			ContextLength:      contextLength,
			MaxOutputTokens:    maxOutput,
			SupportedEndpoints: endpoints,
			Capabilities:       record.Capabilities,
			InputModalities:    inputModalities,
			OutputModalities:   outputModalities,
			MaxInputTokens:     maxInput,
			Name:               name,
			Description:        record.Description, MaxContextWindow: maxContext,
			DefaultReasoningLevel: defaultReasoning, SupportedReasoningLevels: reasoningLevels,
			SupportsOriginalImage: record.SupportsOriginalImage, SupportsReasoningSummary: record.SupportsReasoningSummary,
			SupportsParallelTools: record.SupportsParallelTools, SupportsVerbosity: record.SupportsVerbosity, DefaultVerbosity: record.DefaultVerbosity,
		}
		enrichCodexModel(&model)
		seen[id] = model
	}
	models := make([]credential.ProviderModel, 0, len(seen))
	for _, model := range seen {
		models = append(models, model)
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models, nil
}

func enrichCodexModel(model *credential.ProviderModel) {
	if model.Root == "" {
		model.Root = model.ID
	}
	if model.Permission == nil {
		model.Permission = []any{}
	}
	if model.APIFormat == "" {
		model.APIFormat = "responses"
	}
	if len(model.SupportedEndpoints) == 0 {
		model.SupportedEndpoints = []string{"responses"}
	}
	if len(model.InputModalities) == 0 {
		model.InputModalities = []string{"text"}
	}
	if len(model.OutputModalities) == 0 {
		model.OutputModalities = []string{"text"}
	}
	if model.MaxInputTokens == 0 {
		model.MaxInputTokens = model.ContextLength
	}
}

var _ entities.Upstream = (*CodexAdapter)(nil)
var _ credential.ConnectivityProber = (*CodexAdapter)(nil)
var _ credential.ModelDiscoverer = (*CodexAdapter)(nil)
