package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
)

// GrokBuildAdapter implements the Grok Build CLI subscription. Unlike xAI's
// public API, this entitlement uses the Responses endpoint and CLI identity
// headers at cli-chat-proxy.grok.com.
type GrokBuildAdapter struct {
	HTTP      *http.Client
	Persister OAuthTokenPersister
	ClientID  string
}

func (a *GrokBuildAdapter) client() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return NewHTTPClient()
}

func grokBuildBase(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return "https://cli-chat-proxy.grok.com/v1"
	}
	for _, suffix := range []string{"/responses", "/models"} {
		base = strings.TrimRight(strings.TrimSuffix(base, suffix), "/")
	}
	return base
}

func grokBuildHeaders(cr *entities.CredentialRuntime, model string, stream bool) map[string]string {
	accept := "application/json"
	if stream {
		accept = "text/event-stream"
	}
	headers := map[string]string{
		"Authorization":            "Bearer " + cr.OAuthAccess,
		"Accept":                   accept,
		"User-Agent":               "grok-shell/0.2.106 (linux; x86_64)",
		"X-Grok-Client-Version":    "0.2.106",
		"X-Grok-Client-Identifier": "grok-shell",
		"X-Grok-Client-Mode":       "headless",
		"X-XAI-Token-Auth":         "xai-grok-cli",
		"X-Authenticateresponse":   "authenticate-response",
		"X-Grok-Model-Override":    model,
	}
	if cr.OAuthMeta.AccountID != "" {
		headers["X-Userid"] = cr.OAuthMeta.AccountID
		headers["X-Grok-User-Id"] = cr.OAuthMeta.AccountID
	}
	if cr.OAuthMeta.Email != "" {
		headers["X-Email"] = cr.OAuthMeta.Email
	}
	if cr.OAuthMeta.PrincipalType != "" {
		headers["X-Grok-Principal-Type"] = cr.OAuthMeta.PrincipalType
	}
	return headers
}

func (a *GrokBuildAdapter) refresh(ctx context.Context, cr *entities.CredentialRuntime) error {
	return refreshOAuthForm(ctx, a.HTTP, a.Persister, cr, "https://auth.x.ai/oauth2/token", a.ClientID, "", nil)
}

func (a *GrokBuildAdapter) Send(ctx context.Context, cr *entities.CredentialRuntime, model string, raw []byte) (*entities.UpstreamResult, error) {
	var input ChatRequest
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, fmt.Errorf("parse Grok Build request: %w", err)
	}
	payload := toCodexRequest(&input, model)
	payload.Stream = true // the Responses collector also handles non-stream clients
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	send := func() (*entities.UpstreamResult, error) {
		return postJSON(ctx, a.client(), grokBuildBase(cr.BaseURL)+"/responses", grokBuildHeaders(cr, model, true), encoded)
	}
	result, err := send()
	if err != nil {
		return nil, err
	}
	if result.StatusCode == http.StatusUnauthorized && a.Persister != nil && canRetryOAuth(ctx) {
		result.Body.Close()
		if err := a.refresh(ctx, cr); err != nil {
			return nil, err
		}
		return a.Send(markOAuthRetry(ctx), cr, model, raw)
	}
	if result.StatusCode < 200 || result.StatusCode >= 300 {
		return result, nil
	}
	if input.Stream {
		reader, writer := io.Pipe()
		upstream := result.Body
		go func() {
			defer upstream.Close()
			_ = writer.CloseWithError(transformCodexStream(upstream, writer, model))
		}()
		result.Body = reader
		result.Header = http.Header{"Content-Type": []string{"text/event-stream"}}
		return result, nil
	}
	response, err := collectCodexResponse(result.Body, model)
	result.Body.Close()
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(response)
	result.Body = io.NopCloser(bytes.NewReader(body))
	result.Header = http.Header{"Content-Type": []string{"application/json"}}
	return result, nil
}

func (a *GrokBuildAdapter) Probe(ctx context.Context, cr *entities.CredentialRuntime) (int, error) {
	result, err := get(ctx, a.client(), grokBuildBase(cr.BaseURL)+"/models", grokBuildHeaders(cr, "", false))
	if err != nil {
		return 0, err
	}
	defer result.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(result.Body, 64<<10))
	return result.StatusCode, nil
}

func (a *GrokBuildAdapter) DiscoverModels(ctx context.Context, cr *entities.CredentialRuntime) ([]credential.ProviderModel, error) {
	result, err := get(ctx, a.client(), grokBuildBase(cr.BaseURL)+"/models", grokBuildHeaders(cr, "", false))
	if err == nil {
		defer result.Body.Close()
		if result.StatusCode >= 200 && result.StatusCode < 300 {
			models, decodeErr := decodeProviderModels(io.LimitReader(result.Body, maxModelCatalogBytes))
			if decodeErr == nil && len(models) > 0 {
				return models, nil
			}
		}
	}
	return modelsFor("grok-build", "grok-composer-2.5-fast", "grok-code-fast-1", "grok-4.1-fast", "grok-4.1"), nil
}
