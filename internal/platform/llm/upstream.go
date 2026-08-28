package llm

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

const anthropicOAuthBeta = "oauth-2025-04-20"

func NewHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 300 * time.Second,
			IdleConnTimeout:       90 * time.Second,
			MaxIdleConnsPerHost:   64,
		},
	}
}

func postJSON(ctx context.Context, client *http.Client, url string, headers map[string]string, body []byte) (*entities.UpstreamResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	return &entities.UpstreamResult{StatusCode: resp.StatusCode, Header: resp.Header, Body: resp.Body}, nil
}

func get(ctx context.Context, client *http.Client, url string, headers map[string]string) (*entities.UpstreamResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	return &entities.UpstreamResult{StatusCode: resp.StatusCode, Header: resp.Header, Body: resp.Body}, nil
}

type OpenAIAdapter struct {
	HTTP    *http.Client
	Refresh func(context.Context, *entities.CredentialRuntime) error
}

// Send forwards an OpenAI Chat Completions body to the upstream, rewriting the
// model name and forcing usage reporting on streams.
func (a *OpenAIAdapter) Send(ctx context.Context, cr *entities.CredentialRuntime, upstreamModel string, rawBody []byte) (*entities.UpstreamResult, error) {
	body, stream, err := prepareOpenAIRequest(rawBody, upstreamModel, SupportsOpenAIPromptCacheKey(cr.Provider))
	if err != nil {
		return nil, err
	}
	token := cr.APIKey
	if cr.Kind == entities.KindOAuth {
		token = cr.OAuthAccess
	}
	headers := map[string]string{
		"Accept":        "application/json",
		"Authorization": "Bearer " + token,
	}
	applyOAuthProviderHeaders(headers, cr)
	if stream {
		headers["Accept"] = "text/event-stream"
	}
	send := func() (*entities.UpstreamResult, error) {
		return postJSON(ctx, a.httpClient(), openAIEndpoint(cr.BaseURL, "chat/completions"), headers, body)
	}
	result, err := send()
	if err == nil && result.StatusCode == http.StatusUnauthorized && cr.Kind == entities.KindOAuth && a.Refresh != nil {
		result.Body.Close()
		if refreshErr := a.Refresh(ctx, cr); refreshErr != nil {
			return nil, fmt.Errorf("oauth refresh failed: %w", refreshErr)
		}
		headers["Authorization"] = "Bearer " + cr.OAuthAccess
		return send()
	}
	return result, err
}

func openAIBase(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return "https://api.openai.com/v1"
	}
	if parsed, err := url.Parse(base); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		path := strings.TrimRight(parsed.Path, "/")
		for _, suffix := range []string{"/chat/completions", "/responses", "/models", "/chat", "/messages"} {
			if strings.HasSuffix(path, suffix) {
				parsed.Path = strings.TrimRight(strings.TrimSuffix(path, suffix), "/")
				parsed.RawPath = ""
				return strings.TrimRight(parsed.String(), "/")
			}
		}
	}
	return base
}

func openAIEndpoint(baseURL, endpoint string) string {
	base := openAIBase(baseURL)
	if parsed, err := url.Parse(base); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		parsed.Path = strings.TrimRight(parsed.Path, "/") + "/" + strings.TrimLeft(endpoint, "/")
		parsed.RawPath = ""
		return parsed.String()
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(endpoint, "/")
}

func prepareOpenAIRequest(rawBody []byte, upstreamModel string, injectPromptCacheKey bool) ([]byte, bool, error) {
	var request ChatRequest
	if err := json.Unmarshal(rawBody, &request); err != nil {
		return nil, false, fmt.Errorf("parse OpenAI request: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rawBody, &fields); err != nil {
		return nil, false, fmt.Errorf("parse OpenAI request object: %w", err)
	}
	if fields == nil {
		return nil, false, errors.New("parse OpenAI request object: expected JSON object")
	}
	model, _ := json.Marshal(upstreamModel)
	fields["model"] = model
	if injectPromptCacheKey {
		if _, exists := fields["prompt_cache_key"]; !exists {
			if key := ProviderPromptCacheKey(&request); key != "" {
				fields["prompt_cache_key"], _ = json.Marshal(key)
			}
		}
	}
	if request.Stream {
		var options map[string]json.RawMessage
		if rawOptions, ok := fields["stream_options"]; ok {
			_ = json.Unmarshal(rawOptions, &options)
		}
		if options == nil {
			options = make(map[string]json.RawMessage)
		}
		includeUsage, _ := json.Marshal(true)
		options["include_usage"] = includeUsage
		encodedOptions, _ := json.Marshal(options)
		fields["stream_options"] = encodedOptions
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return nil, false, fmt.Errorf("encode OpenAI request: %w", err)
	}
	return encoded, request.Stream, nil
}

func (a *OpenAIAdapter) httpClient() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return NewHTTPClient()
}

func (a *OpenAIAdapter) Probe(ctx context.Context, cr *entities.CredentialRuntime) (int, error) {
	token := cr.APIKey
	if cr.Kind == entities.KindOAuth {
		token = cr.OAuthAccess
	}
	headers := map[string]string{"Accept": "application/json", "Authorization": "Bearer " + token}
	applyOAuthProviderHeaders(headers, cr)
	load := func() (*entities.UpstreamResult, error) {
		return get(ctx, a.httpClient(), openAIEndpoint(cr.BaseURL, "models"), headers)
	}
	res, err := load()
	if err != nil {
		return 0, err
	}
	if res.StatusCode == http.StatusUnauthorized && cr.Kind == entities.KindOAuth && a.Refresh != nil {
		res.Body.Close()
		if err := a.Refresh(ctx, cr); err != nil {
			return 0, err
		}
		headers["Authorization"] = "Bearer " + cr.OAuthAccess
		res, err = load()
		if err != nil {
			return 0, err
		}
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	return res.StatusCode, nil
}

func applyOAuthProviderHeaders(headers map[string]string, cr *entities.CredentialRuntime) {
	if cr == nil || cr.Kind != entities.KindOAuth {
		return
	}
	switch cr.Provider {
	case "grok-build":
		headers["User-Agent"] = "grok-cli/1.0"
	case "cline", "clinepass":
		headers["X-Client-Type"] = "extension"
	case "kilo-code":
		headers["HTTP-Referer"] = "https://kilo.ai"
		headers["X-Title"] = "Kilo Code"
	}
}

type AnthropicAdapter struct {
	HTTP          *http.Client
	OAuthClientID string
	Refresh       func(ctx context.Context, cr *entities.CredentialRuntime) error
}

// Send translates an OpenAI request to the Anthropic Messages API and relays it.
// OAuth credentials transparently refresh once on 401 before failing.
func (a *AnthropicAdapter) Send(ctx context.Context, cr *entities.CredentialRuntime, upstreamModel string, rawBody []byte) (*entities.UpstreamResult, error) {
	var req ChatRequest
	if err := json.Unmarshal(rawBody, &req); err != nil {
		return nil, fmt.Errorf("parse request: %w", err)
	}
	translated := ToAnthropic(&req)
	translated.Model = upstreamModel
	if cr.Kind == entities.KindOAuth && cr.OAuthMeta.AccountID != "" && cr.OAuthMeta.DeviceID != "" {
		sessionID, sessionErr := randomUUID()
		if sessionErr != nil {
			return nil, sessionErr
		}
		identity, _ := json.Marshal(struct {
			DeviceID    string `json:"device_id"`
			AccountUUID string `json:"account_uuid"`
			SessionID   string `json:"session_id"`
		}{DeviceID: cr.OAuthMeta.DeviceID, AccountUUID: cr.OAuthMeta.AccountID, SessionID: sessionID})
		translated.Metadata = &AnthropicMetadata{UserID: string(identity)}
	}
	body, err := json.Marshal(translated)
	if err != nil {
		return nil, err
	}
	url := anthropicBase(cr.BaseURL) + "/v1/messages"
	if cr.Provider == "kimi-code" {
		url += "?beta=true"
	}

	send := func() (*entities.UpstreamResult, error) {
		headers, headerErr := anthropicHeaders(cr)
		if headerErr != nil {
			return nil, headerErr
		}
		return postJSON(ctx, a.httpClient(), url, headers, body)
	}

	res, err := send()
	if err != nil {
		return nil, err
	}
	if res.StatusCode == http.StatusUnauthorized && cr.Kind == entities.KindOAuth && a.Refresh != nil {
		res.Body.Close()
		if rerr := a.Refresh(ctx, cr); rerr != nil {
			return nil, fmt.Errorf("oauth refresh failed: %w", rerr)
		}
		return send()
	}
	return res, nil
}

func (a *AnthropicAdapter) Probe(ctx context.Context, cr *entities.CredentialRuntime) (int, error) {
	req := &ChatRequest{
		Model:     "claude-3-5-haiku-latest",
		Messages:  []Message{{Role: "user", Content: json.RawMessage(`"ping"`)}},
		MaxTokens: int64Ptr(1),
	}
	body, err := json.Marshal(ToAnthropic(req))
	if err != nil {
		return 0, err
	}
	base := anthropicBase(cr.BaseURL)
	headers, err := anthropicHeaders(cr)
	if err != nil {
		return 0, err
	}
	res, err := postJSON(ctx, a.httpClient(), base+"/v1/messages", headers, body)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	return res.StatusCode, nil
}

func anthropicBase(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return "https://api.anthropic.com"
	}
	for _, suffix := range []string{"/v1/messages", "/v1/models"} {
		if strings.HasSuffix(base, suffix) {
			base = strings.TrimRight(strings.TrimSuffix(base, suffix), "/")
			break
		}
	}
	return base
}

func (a *AnthropicAdapter) httpClient() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return NewHTTPClient()
}

func int64Ptr(v int64) *int64 { return &v }

func anthropicHeaders(cr *entities.CredentialRuntime) (map[string]string, error) {
	headers := map[string]string{"anthropic-version": anthropicVersion}
	if cr.Provider == "kimi-code" && cr.Kind == entities.KindOAuth {
		headers["x-api-key"] = cr.OAuthAccess
		headers["Authorization"] = "Bearer " + cr.OAuthAccess
		headers["User-Agent"] = "kimi-code-cli/0.26.0"
		headers["X-Msh-Platform"] = "kimi_code_cli"
		headers["X-Msh-Version"] = "0.26.0"
		headers["X-Msh-Device-Name"] = "gorouter"
		headers["X-Msh-Device-Model"] = "server"
		headers["X-Msh-Os-Version"] = "linux"
		if cr.OAuthMeta.DeviceID != "" {
			headers["X-Msh-Device-Id"] = cr.OAuthMeta.DeviceID
		}
		return headers, nil
	}
	switch cr.Kind {
	case entities.KindAPIKey:
		headers["x-api-key"] = cr.APIKey
	case entities.KindOAuth:
		headers["Authorization"] = "Bearer " + cr.OAuthAccess
		headers["anthropic-beta"] = anthropicOAuthBeta
		headers["anthropic-dangerous-direct-browser-access"] = "true"
		headers["x-app"] = "cli"
		headers["User-Agent"] = "claude-cli/2.1.219 (external, cli)"
	default:
		return nil, fmt.Errorf("unsupported credential kind %q", cr.Kind)
	}
	return headers, nil
}

func randomUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

// ParseRequest decodes an OpenAI-format chat body for policy checks.
func ParseRequest(rawBody []byte) (*ChatRequest, error) {
	var req ChatRequest
	if err := json.Unmarshal(rawBody, &req); err != nil {
		return nil, err
	}
	return &req, nil
}

// IsDeterministic reports whether a request may be served from / stored in the
// prompt cache: greedy sampling and no tool definitions.
func IsDeterministic(req *ChatRequest) bool {
	if req.Temperature != nil && *req.Temperature != 0 {
		return false
	}
	if req.TopP != nil && *req.TopP != 1 {
		return false
	}
	if req.N != nil && *req.N != 1 {
		return false
	}
	if req.FrequencyPenalty != nil && *req.FrequencyPenalty != 0 {
		return false
	}
	if req.PresencePenalty != nil && *req.PresencePenalty != 0 {
		return false
	}
	if len(req.Tools) != 0 || len(req.ToolChoice) != 0 {
		return false
	}
	for _, message := range req.Messages {
		if len(message.ToolCalls) != 0 || message.ToolCallID != "" {
			return false
		}
	}
	return true
}
