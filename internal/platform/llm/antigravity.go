package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
)

const (
	antigravityRuntimeBaseURL  = "https://daily-cloudcode-pa.googleapis.com"
	antigravityFallbackBaseURL = "https://cloudcode-pa.googleapis.com"
	antigravityUserAgent       = "antigravity/1.11.9 darwin/arm64"
)

type AntigravityAdapter struct {
	HTTP         *http.Client
	Persister    OAuthTokenPersister
	ClientID     string
	ClientSecret string
}

func (a *AntigravityAdapter) client() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return NewHTTPClient()
}

func (a *AntigravityAdapter) Send(ctx context.Context, cr *entities.CredentialRuntime, model string, raw []byte) (*entities.UpstreamResult, error) {
	var input ChatRequest
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, fmt.Errorf("parse Antigravity request: %w", err)
	}
	payload, err := antigravityRequest(input, model, cr.OAuthMeta.ProjectID)
	if err != nil {
		return nil, err
	}
	path := "/v1internal:generateContent"
	if input.Stream {
		path = "/v1internal:streamGenerateContent?alt=sse"
	}
	base := strings.TrimRight(cr.BaseURL, "/")
	if base == "" {
		base = antigravityRuntimeBaseURL
	}
	headers := map[string]string{"Authorization": "Bearer " + cr.OAuthAccess, "Accept": "application/json", "User-Agent": antigravityUserAgent}
	if project := strings.TrimSpace(cr.OAuthMeta.ProjectID); project != "" {
		headers["x-goog-user-project"] = project
	}
	result, err := postJSON(ctx, a.client(), base+path, headers, payload)
	if err == nil && result.StatusCode == http.StatusUnauthorized && a.Persister != nil && canRetryOAuth(ctx) {
		result.Body.Close()
		if refreshErr := refreshOAuthForm(ctx, a.HTTP, a.Persister, cr, "https://oauth2.googleapis.com/token", a.ClientID, a.ClientSecret, nil); refreshErr != nil {
			return nil, refreshErr
		}
		return a.Send(markOAuthRetry(ctx), cr, model, raw)
	}
	if err != nil || result.StatusCode < 200 || result.StatusCode >= 300 {
		return result, err
	}
	if input.Stream {
		result.Body = antigravityStream(result.Body, model)
		result.Header["Content-Type"] = []string{"text/event-stream"}
		return result, nil
	}
	defer result.Body.Close()
	body, err := io.ReadAll(io.LimitReader(result.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	converted, err := antigravityResponse(body, model)
	if err != nil {
		return nil, err
	}
	result.Body = io.NopCloser(bytes.NewReader(converted))
	return result, nil
}

func antigravityRequest(input ChatRequest, model, project string) ([]byte, error) {
	contents := make([]map[string]any, 0, len(input.Messages))
	var system []string
	for _, message := range input.Messages {
		if message.Role == "system" || message.Role == "developer" {
			system = append(system, rawText(message.Content))
			continue
		}
		role := "user"
		if message.Role == "assistant" {
			role = "model"
		}
		parts := []map[string]any{}
		if text := rawText(message.Content); text != "" {
			parts = append(parts, map[string]any{"text": text})
		}
		for _, call := range message.ToolCalls {
			var args any = map[string]any{}
			_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
			parts = append(parts, map[string]any{"functionCall": map[string]any{"name": call.Function.Name, "args": args, "id": call.ID}})
		}
		if message.Role == "tool" {
			parts = []map[string]any{{"functionResponse": map[string]any{"name": message.Name, "id": message.ToolCallID, "response": map[string]any{"result": rawText(message.Content)}}}}
		}
		if len(parts) > 0 {
			contents = append(contents, map[string]any{"role": role, "parts": parts})
		}
	}
	request := map[string]any{"contents": contents}
	generation := map[string]any{}
	if n := input.OutputLimit(); n > 0 {
		generation["maxOutputTokens"] = n
	}
	if input.Temperature != nil {
		generation["temperature"] = *input.Temperature
	}
	if input.TopP != nil {
		generation["topP"] = *input.TopP
	}
	if input.Reasoning != nil && input.Reasoning.Effort != "" {
		generation["thinkingConfig"] = map[string]any{"thinkingLevel": input.Reasoning.Effort, "includeThoughts": true}
	}
	if len(generation) > 0 {
		request["generationConfig"] = generation
	}
	if len(system) > 0 {
		request["systemInstruction"] = map[string]any{"role": "user", "parts": []map[string]any{{"text": strings.Join(system, "\n\n")}}}
	}
	if len(input.Tools) > 0 {
		declarations := make([]map[string]any, 0, len(input.Tools))
		for _, tool := range input.Tools {
			var params any = map[string]any{"type": "object"}
			if len(tool.Function.Parameters) > 0 {
				_ = json.Unmarshal(tool.Function.Parameters, &params)
			}
			declarations = append(declarations, map[string]any{"name": tool.Function.Name, "description": tool.Function.Description, "parameters": params})
		}
		request["tools"] = []map[string]any{{"functionDeclarations": declarations}}
		request["toolConfig"] = map[string]any{"functionCallingConfig": map[string]any{"mode": "AUTO"}}
	}
	envelope := map[string]any{"model": model, "userAgent": "antigravity", "requestType": "agent", "requestId": fmt.Sprintf("agent-%d", time.Now().UnixNano()), "request": request}
	if project != "" {
		envelope["project"] = project
	}
	return json.Marshal(envelope)
}

func rawText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var parts []map[string]any
	if json.Unmarshal(raw, &parts) == nil {
		var b strings.Builder
		for _, part := range parts {
			if value, ok := part["text"].(string); ok {
				b.WriteString(value)
			}
		}
		return b.String()
	}
	return string(raw)
}

func antigravityResponse(body []byte, model string) ([]byte, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}
	raw := body
	if value := envelope["response"]; len(value) > 0 {
		raw = value
	}
	var response struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text         string `json:"text"`
					FunctionCall *struct {
						Name string          `json:"name"`
						Args json.RawMessage `json:"args"`
						ID   string          `json:"id"`
					} `json:"functionCall"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		Usage struct {
			Prompt     int64 `json:"promptTokenCount"`
			Completion int64 `json:"candidatesTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	message := ResponseMessage{Role: "assistant"}
	for _, candidate := range response.Candidates {
		for _, part := range candidate.Content.Parts {
			message.Content += part.Text
			if part.FunctionCall != nil {
				message.ToolCalls = append(message.ToolCalls, ToolCall{ID: part.FunctionCall.ID, Type: "function", Function: ToolFunction{Name: part.FunctionCall.Name, Arguments: string(part.FunctionCall.Args)}})
			}
		}
	}
	finish := "stop"
	if len(message.ToolCalls) > 0 {
		finish = "tool_calls"
	}
	out := Response{ID: fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()), Object: "chat.completion", Created: time.Now().Unix(), Model: model, Choices: []Choice{{Index: 0, Message: &message, FinishReason: finish}}, Usage: Usage{PromptTokens: response.Usage.Prompt, CompletionTokens: response.Usage.Completion}}
	return json.Marshal(out)
}

func antigravityStream(upstream io.ReadCloser, model string) io.ReadCloser {
	reader, writer := io.Pipe()
	go func() {
		defer upstream.Close()
		defer writer.Close()
		scanner := bufio.NewScanner(upstream)
		scanner.Buffer(make([]byte, 64<<10), 16<<20)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "" {
				continue
			}
			converted, err := antigravityResponse([]byte(payload), model)
			if err != nil {
				continue
			}
			var response Response
			if json.Unmarshal(converted, &response) != nil || len(response.Choices) == 0 {
				continue
			}
			delta := Delta{Role: "assistant", Content: response.Choices[0].Message.Content, ToolCalls: response.Choices[0].Message.ToolCalls}
			chunk := Chunk{ID: response.ID, Object: "chat.completion.chunk", Created: response.Created, Model: model, Choices: []ChunkChoice{{Index: 0, Delta: delta, FinishReason: response.Choices[0].FinishReason}}, Usage: &response.Usage}
			encoded, _ := json.Marshal(chunk)
			_, _ = fmt.Fprintf(writer, "data: %s\n\n", encoded)
		}
		_, _ = io.WriteString(writer, "data: [DONE]\n\n")
	}()
	return reader
}

func (a *AntigravityAdapter) Probe(ctx context.Context, cr *entities.CredentialRuntime) (int, error) {
	base := strings.TrimRight(cr.BaseURL, "/")
	if base == "" {
		base = antigravityRuntimeBaseURL
	}
	payload := []byte(`{"metadata":{"ideType":"ANTIGRAVITY","platform":"PLATFORM_UNSPECIFIED","pluginType":"GEMINI"}}`)
	res, err := postJSON(ctx, a.client(), base+"/v1internal:loadCodeAssist", map[string]string{"Authorization": "Bearer " + cr.OAuthAccess, "Accept": "application/json"}, payload)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	return res.StatusCode, nil
}
func (a *AntigravityAdapter) DiscoverModels(ctx context.Context, cr *entities.CredentialRuntime) ([]credential.ProviderModel, error) {
	if cr == nil || strings.TrimSpace(cr.OAuthAccess) == "" {
		return nil, fmt.Errorf("Antigravity OAuth token is unavailable")
	}
	configuredBase := strings.TrimRight(cr.BaseURL, "/")
	bases := []string{configuredBase}
	if configuredBase == "" || configuredBase == antigravityRuntimeBaseURL || configuredBase == antigravityFallbackBaseURL {
		bases = []string{antigravityRuntimeBaseURL, antigravityFallbackBaseURL, "https://daily-cloudcode-pa.sandbox.googleapis.com"}
	}
	body := []byte(`{}`)
	if project := strings.TrimSpace(cr.OAuthMeta.ProjectID); project != "" {
		body, _ = json.Marshal(struct {
			Project string `json:"project"`
		}{Project: project})
	}
	var lastStatus int
	for _, base := range bases {
		result, err := postJSON(ctx, a.client(), base+"/v1internal:fetchAvailableModels", map[string]string{
			"Authorization": "Bearer " + cr.OAuthAccess,
			"Accept":        "application/json",
			"User-Agent":    antigravityUserAgent,
		}, body)
		if err != nil {
			continue
		}
		lastStatus = result.StatusCode
		if result.StatusCode < 200 || result.StatusCode >= 300 {
			_, _ = io.Copy(io.Discard, io.LimitReader(result.Body, 1<<20))
			result.Body.Close()
			if result.StatusCode == http.StatusUnauthorized || result.StatusCode == http.StatusForbidden {
				break
			}
			continue
		}
		payload, readErr := io.ReadAll(io.LimitReader(result.Body, 8<<20))
		result.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		models, parseErr := parseAntigravityModels(payload)
		if parseErr != nil {
			return nil, parseErr
		}
		if len(models) > 0 {
			return models, nil
		}
	}
	if lastStatus != 0 {
		return nil, fmt.Errorf("Antigravity model discovery returned HTTP %d", lastStatus)
	}
	return nil, fmt.Errorf("Antigravity model discovery failed")
}

func parseAntigravityModels(payload []byte) ([]credential.ProviderModel, error) {
	var response struct {
		Models map[string]struct {
			DisplayName     string `json:"displayName"`
			Description     string `json:"description"`
			IsInternal      bool   `json:"isInternal"`
			ContextWindow   int64  `json:"contextWindow"`
			MaxOutputTokens int64  `json:"maxOutputTokens"`
		} `json:"models"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		return nil, fmt.Errorf("parse Antigravity models: %w", err)
	}
	ids := make([]string, 0, len(response.Models))
	for id, item := range response.Models {
		if item.IsInternal || !antigravityChatModel(id) {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	models := make([]credential.ProviderModel, 0, len(ids))
	for _, id := range ids {
		item := response.Models[id]
		models = append(models, credential.ProviderModel{
			ID: id, Object: "model", OwnedBy: "google-antigravity", Name: item.DisplayName,
			Description: item.Description, ContextLength: item.ContextWindow, MaxOutputTokens: item.MaxOutputTokens,
			InputModalities: []string{"text", "image"}, OutputModalities: []string{"text"},
		})
	}
	return models, nil
}

func antigravityChatModel(id string) bool {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		return false
	}
	for _, token := range []string{"image", "imagen", "audio", "tts", "embedding", "embed", "video", "veo", "tab_"} {
		if strings.Contains(id, token) {
			return false
		}
	}
	return strings.Contains(id, "gemini") || strings.Contains(id, "claude") || strings.Contains(id, "gpt")
}
