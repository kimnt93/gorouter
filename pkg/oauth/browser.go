package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/provider"
)

var (
	ErrInvalidFlow = errors.New("invalid or expired OAuth flow")
	ErrBadCallback = errors.New("invalid OAuth callback")
)

const (
	defaultClaudeClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	defaultCodexClientID  = "app_EMoamEEZ73f0CkXaXp7hrann"
)

type Config struct {
	ClaudeClientID     string
	CodexClientID      string
	ClaudeTokenURL     string
	ClaudeBootstrapURL string
	CodexTokenURL      string
	FlowTTL            time.Duration
}

type StartResult struct {
	FlowID       string `json:"flow_id"`
	AuthorizeURL string `json:"authorize_url"`
	Instructions string `json:"instructions"`
}

type CompleteInput struct {
	Provider       string
	FlowID         string
	Callback       string
	Name           string
	OwnerTenant    *string
	SessionBinding string
}

type flow struct {
	provider       string
	state          string
	verifier       string
	redirectURI    string
	sessionBinding string
	expires        time.Time
}

type Service struct {
	client      *http.Client
	credentials *credential.Service
	config      Config
	now         func() time.Time
	mu          sync.Mutex
	flows       map[string]flow
}

func New(client *http.Client, credentials *credential.Service, cfg Config) *Service {
	if client == nil {
		client = http.DefaultClient
	}
	if cfg.ClaudeClientID == "" {
		cfg.ClaudeClientID = defaultClaudeClientID
	}
	if cfg.CodexClientID == "" {
		cfg.CodexClientID = defaultCodexClientID
	}
	if cfg.ClaudeTokenURL == "" {
		cfg.ClaudeTokenURL = "https://api.anthropic.com/v1/oauth/token"
	}
	if cfg.ClaudeBootstrapURL == "" {
		cfg.ClaudeBootstrapURL = "https://api.anthropic.com/api/claude_cli/bootstrap"
	}
	if cfg.CodexTokenURL == "" {
		cfg.CodexTokenURL = "https://auth.openai.com/oauth/token"
	}
	if cfg.FlowTTL <= 0 {
		cfg.FlowTTL = 10 * time.Minute
	}
	return &Service{client: client, credentials: credentials, config: cfg, now: time.Now, flows: make(map[string]flow)}
}

func (s *Service) Start(providerID, sessionBinding string) (StartResult, error) {
	definition, ok := provider.Lookup(providerID)
	if !ok || definition.Auth != provider.AuthOAuth || sessionBinding == "" {
		return StartResult{}, ErrInvalidFlow
	}
	state, err := randomToken(32)
	if err != nil {
		return StartResult{}, err
	}
	verifier, err := randomToken(64)
	if err != nil {
		return StartResult{}, err
	}
	challengeBytes := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(challengeBytes[:])
	f := flow{provider: providerID, state: state, verifier: verifier, sessionBinding: sessionBinding, expires: s.now().Add(s.config.FlowTTL)}
	var authorize string
	switch providerID {
	case "claude":
		f.redirectURI = "https://platform.claude.com/oauth/code/callback"
		q := url.Values{"code": {"true"}, "client_id": {s.config.ClaudeClientID}, "response_type": {"code"}, "redirect_uri": {f.redirectURI}, "scope": {"org:create_api_key user:profile user:inference user:sessions:claude_code user:mcp_servers"}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}, "state": {state}, "prompt": {"login"}}
		authorize = "https://claude.ai/oauth/authorize?" + q.Encode()
	case "codex":
		f.redirectURI = "http://localhost:1455/auth/callback"
		q := url.Values{"response_type": {"code"}, "client_id": {s.config.CodexClientID}, "redirect_uri": {f.redirectURI}, "scope": {"openid profile email offline_access"}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}, "state": {state}, "id_token_add_organizations": {"true"}, "codex_cli_simplified_flow": {"true"}, "originator": {"codex_cli_rs"}, "prompt": {"login"}}
		authorize = "https://auth.openai.com/oauth/authorize?" + q.Encode()
	default:
		return StartResult{}, ErrInvalidFlow
	}
	s.mu.Lock()
	for id, existing := range s.flows {
		if !existing.expires.After(s.now()) {
			delete(s.flows, id)
		}
	}
	s.flows[state] = f
	s.mu.Unlock()
	return StartResult{FlowID: state, AuthorizeURL: authorize, Instructions: "Open the authorization URL, finish sign-in, then paste the full callback URL or returned code#state here."}, nil
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
}

func (s *Service) Complete(ctx context.Context, in CompleteInput) (*entities.Credential, error) {
	s.mu.Lock()
	f, ok := s.flows[in.FlowID]
	if ok {
		delete(s.flows, in.FlowID)
	}
	s.mu.Unlock()
	if !ok || !f.expires.After(s.now()) || f.provider != in.Provider || f.sessionBinding != in.SessionBinding {
		return nil, ErrInvalidFlow
	}
	code, callbackState, err := parseCallback(in.Callback)
	if err != nil || callbackState != f.state {
		return nil, ErrBadCallback
	}
	tokens, err := s.exchange(ctx, f, code)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(tokens.AccessToken) == "" || strings.TrimSpace(tokens.RefreshToken) == "" {
		return nil, errors.New("OAuth token response omitted required tokens")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = map[string]string{"claude": "Claude Code", "codex": "OpenAI Codex"}[in.Provider]
	}
	account := ""
	metadata := entities.OAuthMetadata{}
	if in.Provider == "codex" {
		account = codexAccountID(tokens.IDToken)
		if account == "" {
			return nil, errors.New("Codex OAuth identity token omitted the account binding")
		}
		metadata.AccountID = account
	} else if in.Provider == "claude" {
		metadata = s.claudeMetadata(ctx, tokens.AccessToken)
		account = metadata.AccountID
	}
	return s.credentials.Create(ctx, entities.CredentialInput{Name: name, Provider: in.Provider, Kind: entities.KindOAuth, OAuthAccess: tokens.AccessToken, OAuthRefresh: tokens.RefreshToken, OAuthIDToken: tokens.IDToken, OAuthAccount: account, OAuthMeta: metadata, OwnerTenant: in.OwnerTenant})
}

func (s *Service) claudeMetadata(ctx context.Context, accessToken string) entities.OAuthMetadata {
	metadata := entities.OAuthMetadata{}
	device, err := randomHex(32)
	if err == nil {
		metadata.DeviceID = device
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.config.ClaudeBootstrapURL, nil)
	if err != nil {
		return metadata
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "claude-cli/2.1.219 (external, cli)")
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	resp, err := s.client.Do(req)
	if err != nil {
		return metadata
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return metadata
	}
	var payload struct {
		Account struct {
			AccountID      string `json:"account_uuid"`
			OrganizationID string `json:"organization_uuid"`
		} `json:"oauth_account"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload) == nil {
		metadata.AccountID = payload.Account.AccountID
		metadata.OrganizationID = payload.Account.OrganizationID
	}
	return metadata
}

func (s *Service) exchange(ctx context.Context, f flow, code string) (tokenResponse, error) {
	var req *http.Request
	var err error
	if f.provider == "claude" {
		payload, _ := json.Marshal(struct {
			Code        string `json:"code"`
			State       string `json:"state"`
			GrantType   string `json:"grant_type"`
			ClientID    string `json:"client_id"`
			RedirectURI string `json:"redirect_uri"`
			Verifier    string `json:"code_verifier"`
		}{code, f.state, "authorization_code", s.config.ClaudeClientID, f.redirectURI, f.verifier})
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, s.config.ClaudeTokenURL, strings.NewReader(string(payload)))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
		}
	} else {
		form := url.Values{"grant_type": {"authorization_code"}, "client_id": {s.config.CodexClientID}, "code": {code}, "redirect_uri": {f.redirectURI}, "code_verifier": {f.verifier}}
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, s.config.CodexTokenURL, strings.NewReader(form.Encode()))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}
	if err != nil {
		return tokenResponse{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return tokenResponse{}, fmt.Errorf("OAuth token request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return tokenResponse{}, fmt.Errorf("OAuth token endpoint returned HTTP %d", resp.StatusCode)
	}
	var tokens tokenResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tokens); err != nil {
		return tokenResponse{}, fmt.Errorf("decode OAuth token response: %w", err)
	}
	return tokens, nil
}

func parseCallback(value string) (string, string, error) {
	value = strings.TrimSpace(value)
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" {
		code, state := parsed.Query().Get("code"), parsed.Query().Get("state")
		if code != "" && state != "" {
			return code, state, nil
		}
	}
	parts := strings.SplitN(value, "#", 2)
	if len(parts) == 2 && strings.TrimSpace(parts[0]) != "" && strings.TrimSpace(parts[1]) != "" {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
	}
	return "", "", ErrBadCallback
}

func randomToken(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func randomHex(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b), nil
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
	accountID := strings.TrimSpace(claims.Auth.AccountID)
	plan := strings.ToLower(strings.TrimSpace(claims.Auth.PlanType))
	if strings.Contains(plan, "team") {
		return accountID
	}
	if plan == "" || plan == "free" {
		for _, organization := range claims.Auth.Orgs {
			if organization.IsDefault || strings.TrimSpace(organization.ID) == "" {
				continue
			}
			title := strings.ToLower(organization.Title)
			role := strings.ToLower(organization.Role)
			if strings.Contains(title, "team") || strings.Contains(title, "business") ||
				strings.Contains(title, "workspace") || strings.Contains(title, "org") ||
				role == "admin" || role == "member" {
				return strings.TrimSpace(organization.ID)
			}
		}
	}
	return accountID
}
