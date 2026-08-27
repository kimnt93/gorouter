package llm

import (
	"context"
	"encoding/json"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
	"io"
	"net/http"
	"strconv"
	"time"
)

type CopilotAdapter struct{ HTTP *http.Client }

func (a *CopilotAdapter) client() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return NewHTTPClient()
}
func (a *CopilotAdapter) refreshToken(ctx context.Context, cr *entities.CredentialRuntime) {
	if cr.OAuthAccess == "" {
		return
	}
	if copilotTokenExpiry(cr.OAuthMeta.TokenExpiresAt).After(time.Now().Add(time.Minute)) && cr.OAuthMeta.CopilotToken != "" {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/copilot_internal/v2/token", nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+cr.OAuthAccess)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "GitHubCopilotChat/0.26.7")
	resp, err := a.client().Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return
	}
	var p struct {
		Token string `json:"token"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&p) == nil && p.Token != "" {
		cr.OAuthMeta.CopilotToken = p.Token
	}
}

func copilotTokenExpiry(value string) time.Time {
	if expiry, err := time.Parse(time.RFC3339, value); err == nil {
		return expiry
	}
	if unix, err := strconv.ParseInt(value, 10, 64); err == nil {
		return time.Unix(unix, 0)
	}
	return time.Time{}
}
func (a *CopilotAdapter) headers(cr *entities.CredentialRuntime, stream bool) map[string]string {
	token := cr.OAuthMeta.CopilotToken
	if token == "" {
		token = cr.OAuthAccess
	}
	accept := "application/json"
	if stream {
		accept = "text/event-stream"
	}
	return map[string]string{"Authorization": "Bearer " + token, "Accept": accept, "Copilot-Integration-Id": "vscode-chat", "Editor-Version": "vscode/1.96.0", "Editor-Plugin-Version": "copilot-chat/0.26.7", "User-Agent": "GitHubCopilotChat/0.26.7", "Openai-Intent": "conversation-panel", "X-GitHub-Api-Version": "2022-11-28"}
}
func (a *CopilotAdapter) Send(ctx context.Context, cr *entities.CredentialRuntime, model string, raw []byte) (*entities.UpstreamResult, error) {
	a.refreshToken(ctx, cr)
	body, stream, err := prepareOpenAIRequest(raw, model, false)
	if err != nil {
		return nil, err
	}
	return postJSON(ctx, a.client(), "https://api.githubcopilot.com/chat/completions", a.headers(cr, stream), body)
}
func (a *CopilotAdapter) Probe(ctx context.Context, cr *entities.CredentialRuntime) (int, error) {
	a.refreshToken(ctx, cr)
	res, err := get(ctx, a.client(), "https://api.githubcopilot.com/models", a.headers(cr, false))
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	return res.StatusCode, nil
}
func (a *CopilotAdapter) DiscoverModels(context.Context, *entities.CredentialRuntime) ([]credential.ProviderModel, error) {
	return modelsFor("github-copilot", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-5.5", "claude-sonnet-5", "claude-sonnet-4.6", "claude-haiku-4.5", "gemini-3.1-pro-preview"), nil
}
