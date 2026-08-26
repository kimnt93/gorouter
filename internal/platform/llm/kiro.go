package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
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
		base = kiroRuntimeHost(kiroRuntimeRegion(cr.OAuthMeta.ProfileARN, cr.OAuthMeta.Region))
	}
	if !strings.HasSuffix(base, "/generateAssistantResponse") {
		base += "/generateAssistantResponse"
	}
	invocation, _ := randomUUID()
	headers := map[string]string{"Authorization": "Bearer " + cr.OAuthAccess, "Accept": "application/vnd.amazon.eventstream", "Amz-Sdk-Request": "attempt=1; max=3", "Amz-Sdk-Invocation-Id": invocation, "User-Agent": "aws-sdk-js/3.709.0 ua/2.1 os/linux lang/js md/nodejs#22.0 api/codewhispererruntime#3.0", "x-amzn-bedrock-cache-control": "enable", "anthropic-beta": "prompt-caching-2024-07-31"}
	if cr.OAuthMeta.AuthMethod == "api_key" {
		headers["tokentype"] = "API_KEY"
	}
	if cr.OAuthMeta.AuthMethod == "external_idp" || cr.OAuthMeta.AuthMethod == "enterprise" {
		headers["TokenType"] = "EXTERNAL_IDP"
	}
	result, err := postJSON(ctx, client, base, headers, payload)
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

var awsRegionPattern = regexp.MustCompile(`^[a-z]{2}-[a-z]+-\d{1,2}$`)

func kiroRuntimeRegion(profileARN, storedRegion string) string {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(profileARN)), ":")
	if len(parts) > 4 && parts[0] == "arn" && parts[2] == "codewhisperer" && awsRegionPattern.MatchString(parts[3]) {
		return parts[3]
	}
	storedRegion = strings.ToLower(strings.TrimSpace(storedRegion))
	if storedRegion == "us-east-1" || storedRegion == "eu-central-1" {
		return storedRegion
	}
	return "us-east-1"
}

func kiroRuntimeHost(region string) string {
	if region == "us-east-1" {
		return "https://codewhisperer.us-east-1.amazonaws.com"
	}
	return "https://q." + region + ".amazonaws.com"
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
	payload, _ := event.Payload.(map[string]any)
	switch event.Type {
	case "assistantResponseEvent", "codeEvent":
		if content, ok := payload["content"].(string); ok {
			message.Content += content
		}
	case "reasoningContentEvent":
		message.ReasoningContent += kiroReasoningText(payload)
	case "toolUseEvent":
		items := kiroToolItems(event.Payload)
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
			updated := false
			for index := range message.ToolCalls {
				if message.ToolCalls[index].ID == id && id != "" {
					message.ToolCalls[index].Function.Name = name
					if _, objectForm := item["input"].(map[string]any); objectForm {
						message.ToolCalls[index].Function.Arguments = args
					} else {
						message.ToolCalls[index].Function.Arguments += args
					}
					updated = true
					break
				}
			}
			if !updated {
				message.ToolCalls = append(message.ToolCalls, ToolCall{ID: id, Type: "function", Function: ToolFunction{Name: name, Arguments: args}})
			}
		}
	case "metadataEvent", "metricsEvent":
		value := payload
		for _, key := range []string{"metricsEvent", "usage", "metadataEvent"} {
			if nested, ok := value[key].(map[string]any); ok {
				value = nested
			}
		}
		usage.PromptTokens = firstInt64Value(value, "inputTokens", "prompt_tokens")
		usage.CompletionTokens = firstInt64Value(value, "outputTokens", "completion_tokens")
		usage.CacheReadTokens = firstInt64Value(value, "cacheReadInputTokens", "cache_read_input_tokens")
		usage.CacheWriteTokens = firstInt64Value(value, "cacheWriteInputTokens", "cache_creation_input_tokens")
	}
}

func kiroToolItems(payload any) []any {
	if values, ok := payload.([]any); ok {
		return values
	}
	if value, ok := payload.(map[string]any); ok {
		return []any{value}
	}
	return nil
}

func kiroReasoningText(payload map[string]any) string {
	if text := stringValueAny(payload, "text"); text != "" {
		return text
	}
	if value, ok := payload["reasoningText"].(string); ok {
		return value
	}
	if value, ok := payload["reasoningText"].(map[string]any); ok {
		return stringValueAny(value, "text", "Text")
	}
	return ""
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

func firstInt64Value(m map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if value := int64Value(m, key); value != 0 {
			return value
		}
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
		toolIndexes := map[string]int{}
		bufferedArgs := map[string]string{}
		hasTools := false
		usage := Usage{}
		writeChunk := func(delta Delta, finish string, includeUsage bool) {
			chunk := Chunk{ID: id, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []ChunkChoice{{Index: 0, Delta: delta, FinishReason: finish}}}
			if includeUsage {
				chunk.Usage = &usage
			}
			encoded, _ := json.Marshal(chunk)
			_, _ = fmt.Fprintf(writer, "data: %s\n\n", encoded)
		}
		flushArgs := func() {
			for callID, args := range bufferedArgs {
				idx := toolIndexes[callID]
				writeChunk(Delta{ToolCalls: []ToolCall{{Index: &idx, Function: ToolFunction{Arguments: args}}}}, "", false)
			}
			clear(bufferedArgs)
		}
		_ = readAWSEvents(upstream, func(event awsEvent) error {
			payload, _ := event.Payload.(map[string]any)
			delta := Delta{}
			switch event.Type {
			case "assistantResponseEvent", "codeEvent":
				delta.Content = stringValueAny(payload, "content")
			case "reasoningContentEvent":
				delta.ReasoningContent = kiroReasoningText(payload)
			case "toolUseEvent":
				for _, rawItem := range kiroToolItems(event.Payload) {
					item, _ := rawItem.(map[string]any)
					name := stringValueAny(item, "name", "toolName")
					callID := stringValueAny(item, "toolUseId", "id")
					idx, known := toolIndexes[callID]
					if !known {
						idx = toolIndex
						toolIndexes[callID] = idx
						toolIndex++
						hasTools = true
						writeChunk(Delta{ToolCalls: []ToolCall{{Index: &idx, ID: callID, Type: "function", Function: ToolFunction{Name: name}}}}, "", false)
					}
					if value := item["input"]; value != nil {
						if text, ok := value.(string); ok {
							writeChunk(Delta{ToolCalls: []ToolCall{{Index: &idx, Function: ToolFunction{Arguments: text}}}}, "", false)
						} else {
							encoded, _ := json.Marshal(value)
							bufferedArgs[callID] = string(encoded)
						}
					}
				}
				return nil
			case "messageStopEvent":
				flushArgs()
				return nil
			case "metadataEvent", "metricsEvent":
				applyKiroEvent(event, &ResponseMessage{}, &usage)
				return nil
			default:
				return nil
			}
			if delta.Content == "" && delta.ReasoningContent == "" {
				return nil
			}
			writeChunk(delta, "", false)
			return nil
		})
		flushArgs()
		finish := "stop"
		if hasTools {
			finish = "tool_calls"
		}
		writeChunk(Delta{}, finish, true)
		_, _ = fmt.Fprint(writer, "data: [DONE]\n\n")
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
