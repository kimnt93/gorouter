package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
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
	HTTP *http.Client
}

// Send forwards an OpenAI Chat Completions body to the upstream, rewriting the
// model name and forcing usage reporting on streams.
func (a *OpenAIAdapter) Send(ctx context.Context, cr *entities.CredentialRuntime, upstreamModel string, rawBody []byte) (*entities.UpstreamResult, error) {
	var parsed ChatRequest
	if err := json.Unmarshal(rawBody, &parsed); err != nil {
		return nil, fmt.Errorf("re-encode request: %w", err)
	}
	parsed.Model = upstreamModel
	if parsed.Stream {
		parsed.StreamOptions = &StreamOptions{IncludeUsage: true}
	}
	body, err := json.Marshal(&parsed)
	if err != nil {
		return nil, err
	}
	base := openAIBase(cr.BaseURL)
	url := base + "/chat/completions"
	headers := map[string]string{"Authorization": "Bearer " + cr.APIKey}
	return postJSON(ctx, a.HTTP, url, headers, body)
}

func openAIBase(baseURL string) string {
	base := strings.TrimSuffix(baseURL, "/")
	if base == "" {
		return "https://api.openai.com/v1"
	}
	return base
}

func (a *OpenAIAdapter) Probe(ctx context.Context, cr *entities.CredentialRuntime) (int, error) {
	res, err := get(ctx, a.HTTP, openAIBase(cr.BaseURL)+"/models", map[string]string{"Authorization": "Bearer " + cr.APIKey})
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	return res.StatusCode, nil
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
	body, err := json.Marshal(translated)
	if err != nil {
		return nil, err
	}
	base := strings.TrimSuffix(cr.BaseURL, "/")
	if base == "" {
		base = "https://api.anthropic.com"
	}
	url := base + "/v1/messages"

	send := func() (*entities.UpstreamResult, error) {
		headers := map[string]string{"anthropic-version": anthropicVersion}
		switch cr.Kind {
		case entities.KindAPIKey:
			headers["x-api-key"] = cr.APIKey
		case entities.KindOAuth:
			headers["Authorization"] = "Bearer " + cr.OAuthAccess
			headers["anthropic-beta"] = anthropicOAuthBeta
		default:
			return nil, fmt.Errorf("unsupported credential kind %q", cr.Kind)
		}
		return postJSON(ctx, a.HTTP, url, headers, body)
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
	base := strings.TrimSuffix(cr.BaseURL, "/")
	if base == "" {
		base = "https://api.anthropic.com"
	}
	headers := map[string]string{"anthropic-version": anthropicVersion}
	switch cr.Kind {
	case entities.KindAPIKey:
		headers["x-api-key"] = cr.APIKey
	case entities.KindOAuth:
		headers["Authorization"] = "Bearer " + cr.OAuthAccess
		headers["anthropic-beta"] = anthropicOAuthBeta
	default:
		return 0, fmt.Errorf("unsupported credential kind %q", cr.Kind)
	}
	res, err := postJSON(ctx, a.HTTP, base+"/v1/messages", headers, body)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	return res.StatusCode, nil
}

func int64Ptr(v int64) *int64 { return &v }

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
