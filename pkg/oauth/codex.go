package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/kimnt93/gorouter/pkg/entities"
)

type codexDriver struct{}

func (codexDriver) Start(_ context.Context, s *Service, f flow) (flow, StartResult, error) {
	f.flowType = "authorization_code_pkce"
	f.redirectURI = "http://localhost:1455/auth/callback"
	q := url.Values{"response_type": {"code"}, "client_id": {s.config.CodexClientID}, "redirect_uri": {f.redirectURI}, "scope": {"openid profile email offline_access"}, "code_challenge": {pkceChallenge(f.verifier)}, "code_challenge_method": {"S256"}, "state": {f.state}, "id_token_add_organizations": {"true"}, "codex_cli_simplified_flow": {"true"}, "originator": {"codex_cli_rs"}, "prompt": {"login"}}
	return f, StartResult{AuthorizeURL: "https://auth.openai.com/oauth/authorize?" + q.Encode(), Instructions: "Open the authorization URL, finish sign-in, then paste the full callback URL or returned code#state here."}, nil
}
func (codexDriver) Complete(ctx context.Context, s *Service, f flow, callback string) (tokenResponse, error) {
	code, state, err := parseCallback(callback)
	if err != nil || state != f.state {
		return tokenResponse{}, ErrBadCallback
	}
	t, err := exchangeForm(ctx, s, s.config.CodexTokenURL, url.Values{"grant_type": {"authorization_code"}, "client_id": {s.config.CodexClientID}, "code": {code}, "redirect_uri": {f.redirectURI}, "code_verifier": {f.verifier}})
	if err != nil {
		return t, err
	}
	account := codexAccountID(t.IDToken)
	if account == "" {
		return tokenResponse{}, errors.New("Codex OAuth identity token omitted the account binding")
	}
	t.Metadata = entities.OAuthMetadata{AccountID: account}
	return t, nil
}
func codexAccountID(idToken string) string {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Auth struct {
			AccountID string `json:"chatgpt_account_id"`
			PlanType  string `json:"chatgpt_plan_type"`
			Orgs      []struct {
				ID        string `json:"id"`
				IsDefault bool   `json:"is_default"`
				Role      string `json:"role"`
				Title     string `json:"title"`
			} `json:"organizations"`
		} `json:"https://api.openai.com/auth"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	account := strings.TrimSpace(claims.Auth.AccountID)
	plan := strings.ToLower(strings.TrimSpace(claims.Auth.PlanType))
	if strings.Contains(plan, "team") {
		return account
	}
	if plan == "" || plan == "free" {
		for _, org := range claims.Auth.Orgs {
			if org.IsDefault || strings.TrimSpace(org.ID) == "" {
				continue
			}
			title, role := strings.ToLower(org.Title), strings.ToLower(org.Role)
			if strings.Contains(title, "team") || strings.Contains(title, "business") || strings.Contains(title, "workspace") || strings.Contains(title, "org") || role == "admin" || role == "member" {
				return strings.TrimSpace(org.ID)
			}
		}
	}
	return account
}
