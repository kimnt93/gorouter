package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
)

type KiroAdapter struct {
	HTTP      *http.Client
	Persister OAuthTokenPersister
}

func (a *KiroAdapter) client() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return NewHTTPClient()
}
func (a *KiroAdapter) Send(ctx context.Context, cr *entities.CredentialRuntime, model string, raw []byte) (*entities.UpstreamResult, error) {
	result, err := sendKiroNative(ctx, a.client(), cr, model, raw)
	if err == nil && result != nil && result.StatusCode == http.StatusUnauthorized && a.Persister != nil && canRetryOAuth(ctx) {
		result.Body.Close()
		if refreshErr := refreshKiroOAuth(ctx, a.HTTP, a.Persister, cr); refreshErr != nil {
			return nil, refreshErr
		}
		return a.Send(markOAuthRetry(ctx), cr, model, raw)
	}
	return result, err
}
func refreshKiroOAuth(ctx context.Context, client *http.Client, persister OAuthTokenPersister, cr *entities.CredentialRuntime) error {
	region := cr.OAuthMeta.Region
	if region == "" {
		region = "us-east-1"
	}
	return refreshOAuthJSON(ctx, client, persister, cr, "https://oidc."+region+".amazonaws.com/token", map[string]string{"clientId": cr.OAuthMeta.ClientID, "clientSecret": cr.OAuthMeta.ClientSecret, "refreshToken": cr.OAuthRefreh, "grantType": "refresh_token"}, nil)
}

func sendKiroNative(ctx context.Context, client *http.Client, cr *entities.CredentialRuntime, model string, raw []byte) (*entities.UpstreamResult, error) {
	var input ChatRequest
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	payload, err := kiroRequest(input, model, cr.OAuthMeta.ProfileARN)
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(cr.BaseURL, "/")
	if base == "" {
		base = "https://codewhisperer.us-east-1.amazonaws.com"
	}
	if !strings.HasSuffix(base, "/generateAssistantResponse") {
		base += "/generateAssistantResponse"
	}
	invocation, _ := randomUUID()
	result, err := postJSON(ctx, client, base, map[string]string{"Authorization": "Bearer " + cr.OAuthAccess, "Accept": "application/vnd.amazon.eventstream", "Amz-Sdk-Request": "attempt=1; max=3", "Amz-Sdk-Invocation-Id": invocation, "User-Agent": "aws-sdk-js/3.709.0 ua/2.1 os/linux lang/js md/nodejs#22.0 api/codewhispererruntime#3.0"}, payload)
	if err != nil || result.StatusCode < 200 || result.StatusCode >= 300 {
		return result, err
	}
	if input.Stream {
		result.Body = kiroStream(result.Body, model)
		result.Header["Content-Type"] = []string{"text/event-stream"}
		return result, nil
	}
	defer result.Body.Close()
	message := ResponseMessage{Role: "assistant"}
	usage := Usage{}
	err = readAWSEvents(result.Body, func(event awsEvent) error { applyKiroEvent(event, &message, &usage); return nil })
	if err != nil {
		return nil, err
	}
	finish := "stop"
	if len(message.ToolCalls) > 0 {
		finish = "tool_calls"
	}
	response := Response{ID: fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()), Object: "chat.completion", Created: time.Now().Unix(), Model: model, Choices: []Choice{{Index: 0, Message: &message, FinishReason: finish}}, Usage: usage}
	encoded, _ := json.Marshal(response)
	result.Body = io.NopCloser(bytes.NewReader(encoded))
	result.Header["Content-Type"] = []string{"application/json"}
	return result, nil
}

func kiroRequest(input ChatRequest, model, profileARN string) ([]byte, error) {
	history := []map[string]any{}
	system := []string{}
	for _, message := range input.Messages {
		text := rawText(message.Content)
		switch message.Role {
		case "system", "developer":
			if text != "" {
				system = append(system, "<system-reminder>\n"+text+"\n</system-reminder>")
			}
		case "assistant":
			assistant := map[string]any{"content": text}
			if len(message.ToolCalls) > 0 {
				uses := make([]map[string]any, 0, len(message.ToolCalls))
				for _, call := range message.ToolCalls {
					var arguments any = map[string]any{}
					if call.Function.Arguments != "" {
						_ = json.Unmarshal([]byte(call.Function.Arguments), &arguments)
					}
					uses = append(uses, map[string]any{
						"toolUseId": call.ID,
						"name":      call.Function.Name,
						"input":     arguments,
					})
				}
				assistant["toolUses"] = uses
			}
			history = append(history, map[string]any{"assistantResponseMessage": assistant})
		case "tool":
			user := map[string]any{
				"content": "",
				"modelId": model,
				"origin":  "AI_EDITOR",
				"userInputMessageContext": map[string]any{
					"toolResults": []map[string]any{{
						"toolUseId": message.ToolCallID,
						"status":    "success",
						"content":   []map[string]string{{"text": text}},
					}},
				},
			}
			history = append(history, map[string]any{"userInputMessage": user})
		default:
			history = append(history, map[string]any{"userInputMessage": map[string]any{
				"content": text,
				"modelId": model,
				"origin":  "AI_EDITOR",
			}})
		}
	}

	current := map[string]any{"content": "Continue the conversation.", "modelId": model, "origin": "AI_EDITOR"}
	if len(history) > 0 {
		if user, ok := history[len(history)-1]["userInputMessage"].(map[string]any); ok {
			current = user
			history = history[:len(history)-1]
		}
	}
	if len(system) > 0 {
		current["content"] = strings.Join(system, "\n\n") + "\n\n" + fmt.Sprint(current["content"])
	}
	if len(input.Tools) > 0 {
		specs := make([]map[string]any, 0, len(input.Tools))
		for _, tool := range input.Tools {
			var schema any = map[string]any{"type": "object", "properties": map[string]any{}}
			if len(tool.Function.Parameters) > 0 {
				_ = json.Unmarshal(tool.Function.Parameters, &schema)
			}
			description := tool.Function.Description
			if description == "" {
				description = "Tool: " + tool.Function.Name
			}
			if len(description) > 10000 {
				description = description[:10000]
			}
			specs = append(specs, map[string]any{"toolSpecification": map[string]any{
				"name":        tool.Function.Name,
				"description": description,
				"inputSchema": map[string]any{"json": schema},
			}})
		}
		contextValue, _ := current["userInputMessageContext"].(map[string]any)
		if contextValue == nil {
			contextValue = map[string]any{}
		}
		contextValue["tools"] = specs
		current["userInputMessageContext"] = contextValue
	}
	conversationID, _ := randomUUID()
	request := map[string]any{"conversationState": map[string]any{"chatTriggerType": "MANUAL", "conversationId": conversationID, "currentMessage": map[string]any{"userInputMessage": current}, "history": history}}
	if profileARN != "" {
		request["profileArn"] = profileARN
	}
	inference := map[string]any{}
	if n := input.OutputLimit(); n > 0 {
		inference["maxTokens"] = n
	}
	if input.Temperature != nil {
		inference["temperature"] = *input.Temperature
	}
	if input.TopP != nil {
		inference["topP"] = *input.TopP
	}
	if len(inference) > 0 {
		request["inferenceConfig"] = inference
	}
	if input.Reasoning != nil && input.Reasoning.Effort != "" {
		request["additionalModelRequestFields"] = map[string]any{"reasoning": map[string]any{"effort": input.Reasoning.Effort}, "thinking": map[string]any{"type": "adaptive"}, "output_config": map[string]any{"effort": input.Reasoning.Effort}}
	}
	return json.Marshal(request)
}

func applyKiroEvent(event awsEvent, message *ResponseMessage, usage *Usage) {
	switch event.Type {
	case "assistantResponseEvent", "codeEvent":
		if content, ok := event.Payload["content"].(string); ok {
			message.Content += content
		}
	case "toolUseEvent":
		items := []any{event.Payload}
		if values, ok := any(event.Payload).([]any); ok {
			items = values
		}
		for _, value := range items {
			item, ok := value.(map[string]any)
			if !ok {
				continue
			}
			name := stringValueAny(item, "name", "toolName")
			id := stringValueAny(item, "toolUseId", "id")
			args := "{}"
			if input, ok := item["input"].(string); ok {
				args = input
			} else if input := item["input"]; input != nil {
				encoded, _ := json.Marshal(input)
				args = string(encoded)
			}
			message.ToolCalls = append(message.ToolCalls, ToolCall{ID: id, Type: "function", Function: ToolFunction{Name: name, Arguments: args}})
		}
	case "metadataEvent":
		if value, ok := event.Payload["usage"].(map[string]any); ok {
			usage.PromptTokens = int64Value(value, "inputTokens")
			usage.CompletionTokens = int64Value(value, "outputTokens")
		}
	}
}
func stringValueAny(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := m[key].(string); ok {
			return value
		}
	}
	return ""
}
func int64Value(m map[string]any, key string) int64 {
	if value, ok := m[key].(float64); ok {
		return int64(value)
	}
	return 0
}

func kiroStream(upstream io.ReadCloser, model string) io.ReadCloser {
	reader, writer := io.Pipe()
	go func() {
		defer upstream.Close()
		defer writer.Close()
		id := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
		created := time.Now().Unix()
		toolIndex := 0
		_ = readAWSEvents(upstream, func(event awsEvent) error {
			delta := Delta{}
			finish := ""
			switch event.Type {
			case "assistantResponseEvent", "codeEvent":
				delta.Content = stringValueAny(event.Payload, "content")
			case "reasoningContentEvent":
				delta.Content = stringValueAny(event.Payload, "text")
			case "toolUseEvent":
				name := stringValueAny(event.Payload, "name", "toolName")
				callID := stringValueAny(event.Payload, "toolUseId", "id")
				args := "{}"
				if value := event.Payload["input"]; value != nil {
					if text, ok := value.(string); ok {
						args = text
					} else {
						encoded, _ := json.Marshal(value)
						args = string(encoded)
					}
				}
				idx := toolIndex
				toolIndex++
				delta.ToolCalls = []ToolCall{{Index: &idx, ID: callID, Type: "function", Function: ToolFunction{Name: name, Arguments: args}}}
				finish = "tool_calls"
			default:
				return nil
			}
			if delta.Content == "" && len(delta.ToolCalls) == 0 {
				return nil
			}
			chunk := Chunk{ID: id, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []ChunkChoice{{Index: 0, Delta: delta, FinishReason: finish}}}
			encoded, _ := json.Marshal(chunk)
			_, _ = fmt.Fprintf(writer, "data: %s\n\n", encoded)
			return nil
		})
		final := Chunk{ID: id, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []ChunkChoice{{Index: 0, Delta: Delta{}, FinishReason: "stop"}}}
		encoded, _ := json.Marshal(final)
		_, _ = fmt.Fprintf(writer, "data: %s\n\ndata: [DONE]\n\n", encoded)
	}()
	return reader
}

func (a *KiroAdapter) Probe(ctx context.Context, cr *entities.CredentialRuntime) (int, error) {
	body, _ := json.Marshal(ChatRequest{Messages: []Message{{Role: "user", Content: json.RawMessage(`"ping"`)}}, MaxTokens: int64Ptr(1)})
	result, err := sendKiroNative(ctx, a.client(), cr, "claude-haiku-4.5", body)
	if err != nil {
		return 0, err
	}
	defer result.Body.Close()
	return result.StatusCode, nil
}
func (a *KiroAdapter) DiscoverModels(context.Context, *entities.CredentialRuntime) ([]credential.ProviderModel, error) {
	return modelsFor("kiro", "claude-sonnet-5", "claude-sonnet-4.5", "claude-haiku-4.5", "deepseek-3.2", "minimax-m2.5", "glm-5", "qwen3-coder-next", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"), nil
}
