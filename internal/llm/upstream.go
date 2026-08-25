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
)

const (
	ProviderOpenAICompatible = "openai-compatible"
	ProviderAnthropic        = "anthropic"

	KindAPIKey = "api_key"
	KindOAuth  = "oauth"
)

type CredentialRuntime struct {
	ID          string
	Provider    string
	Kind        string
	BaseURL     string
	APIKey      string
	OAuthAccess string
	OAuthRefreh string
}

type Result struct {
	StatusCode int
	Header     http.Header
	Body       io.ReadCloser
}

type RefreshFunc func(ctx context.Context, cr *CredentialRuntime) error

type Adapter interface {
	Send(ctx context.Context, cr *CredentialRuntime, upstreamModel string, rawBody []byte, parsed *ChatRequest) (*Result, error)
}

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

func postJSON(ctx context.Context, client *http.Client, url string, headers map[string]string, body []byte) (*Result, error) {
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
	return &Result{StatusCode: resp.StatusCode, Header: resp.Header, Body: resp.Body}, nil
}

type OpenAIAdapter struct {
	HTTP *http.Client
}

func (a *OpenAIAdapter) Send(ctx context.Context, cr *CredentialRuntime, upstreamModel string, rawBody []byte, parsed *ChatRequest) (*Result, error) {
	var m map[string]any
	if err := json.Unmarshal(rawBody, &m); err != nil {
		return nil, fmt.Errorf("re-encode request: %w", err)
	}
	m["model"] = upstreamModel
	delete(m, "max_completion_tokens")
	if parsed.Stream {
		so, ok := m["stream_options"].(map[string]any)
		if !ok {
			so = map[string]any{}
			m["stream_options"] = so
		}
		so["include_usage"] = true
	}
	body, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	base := strings.TrimSuffix(cr.BaseURL, "/")
	url := base + "/chat/completions"
	headers := map[string]string{"Authorization": "Bearer " + cr.APIKey}
	return postJSON(ctx, a.HTTP, url, headers, body)
}

type AnthropicAdapter struct {
	HTTP          *http.Client
	Refresh       RefreshFunc
	OAuthClientID string
}

const anthropicOAuthBeta = "oauth-2025-04-20"

func (a *AnthropicAdapter) headers(cr *CredentialRuntime) (map[string]string, error) {
	h := map[string]string{"anthropic-version": anthropicVersion}
	switch cr.Kind {
	case KindAPIKey:
		h["x-api-key"] = cr.APIKey
	case KindOAuth:
		h["Authorization"] = "Bearer " + cr.OAuthAccess
		h["anthropic-beta"] = anthropicOAuthBeta
	default:
		return nil, fmt.Errorf("unsupported credential kind %q", cr.Kind)
	}
	return h, nil
}

func (a *AnthropicAdapter) Send(ctx context.Context, cr *CredentialRuntime, upstreamModel string, _ []byte, parsed *ChatRequest) (*Result, error) {
	translated := ToAnthropic(parsed)
	translated["model"] = upstreamModel
	body, err := json.Marshal(translated)
	if err != nil {
		return nil, err
	}
	base := strings.TrimSuffix(cr.BaseURL, "/")
	if base == "" {
		base = "https://api.anthropic.com"
	}
	url := base + "/v1/messages"
	headers, err := a.headers(cr)
	if err != nil {
		return nil, err
	}
	res, err := postJSON(ctx, a.HTTP, url, headers, body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode == http.StatusUnauthorized && cr.Kind == KindOAuth && a.Refresh != nil {
		res.Body.Close()
		if rerr := a.Refresh(ctx, cr); rerr != nil {
			return nil, fmt.Errorf("oauth refresh failed: %w", rerr)
		}
		headers, herr := a.headers(cr)
		if herr != nil {
			return nil, herr
		}
		return postJSON(ctx, a.HTTP, url, headers, body)
	}
	return res, nil
}
