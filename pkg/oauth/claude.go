package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/kimnt93/gorouter/pkg/entities"
)

type claudeDriver struct{}

func (claudeDriver) Start(_ context.Context, s *Service, f flow) (flow, StartResult, error) {
	f.flowType = "authorization_code_pkce"
	f.redirectURI = "https://platform.claude.com/oauth/code/callback"
	q := url.Values{"code": {"true"}, "client_id": {s.config.ClaudeClientID}, "response_type": {"code"}, "redirect_uri": {f.redirectURI}, "scope": {"org:create_api_key user:profile user:inference user:sessions:claude_code user:mcp_servers"}, "code_challenge": {pkceChallenge(f.verifier)}, "code_challenge_method": {"S256"}, "state": {f.state}, "prompt": {"login"}}
	return f, StartResult{AuthorizeURL: "https://claude.ai/oauth/authorize?" + q.Encode(), Instructions: "Open the authorization URL, then paste the returned code#state value here."}, nil
}
func (claudeDriver) Complete(ctx context.Context, s *Service, f flow, callback string) (tokenResponse, error) {
	code, state, err := parseCallback(callback)
	if err != nil || state != f.state {
		return tokenResponse{}, ErrBadCallback
	}
	payload, _ := json.Marshal(map[string]string{"code": code, "state": state, "grant_type": "authorization_code", "client_id": s.config.ClaudeClientID, "redirect_uri": f.redirectURI, "code_verifier": f.verifier})
	var raw map[string]any
	status, err := requestJSON(ctx, s.client, http.MethodPost, s.config.ClaudeTokenURL, strings.NewReader(string(payload)), map[string]string{"Content-Type": "application/json", "Accept": "application/json"}, &raw)
	if err != nil {
		return tokenResponse{}, err
	}
	if status < 200 || status >= 300 {
		return tokenResponse{}, fmt.Errorf("OAuth token endpoint returned HTTP %d", status)
	}
	t := tokenFromPayload(raw)
	t.Metadata = s.claudeMetadata(ctx, t.AccessToken)
	return t, nil
}
func (s *Service) claudeMetadata(ctx context.Context, token string) entities.OAuthMetadata {
	m := entities.OAuthMetadata{}
	if device, err := randomHex(32); err == nil {
		m.DeviceID = device
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.config.ClaudeBootstrapURL, nil)
	if err != nil {
		return m
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "claude-cli/2.1.219 (external, cli)")
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	resp, err := s.client.Do(req)
	if err != nil {
		return m
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return m
	}
	var p struct {
		Account struct {
			AccountID      string `json:"account_uuid"`
			OrganizationID string `json:"organization_uuid"`
		} `json:"oauth_account"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&p) == nil {
		m.AccountID = p.Account.AccountID
		m.OrganizationID = p.Account.OrganizationID
	}
	return m
}
