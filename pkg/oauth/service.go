package oauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
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
	ErrInvalidFlow          = errors.New("invalid or expired OAuth flow")
	ErrBadCallback          = errors.New("invalid OAuth callback")
	ErrAuthorizationPending = errors.New("OAuth authorization pending")
	ErrAccessDenied         = errors.New("OAuth authorization denied")
)

const (
	defaultClaudeClientID          = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	defaultCodexClientID           = "app_EMoamEEZ73f0CkXaXp7hrann"
	defaultGitHubClientID          = "Iv1.b507a08c87ecfe98"
	defaultGrokClientID            = "b1a00492-073a-47ea-816f-4c329264a828"
	defaultKimiClientID            = "17e5f671-d194-4dfb-9706-5516cb48c098"
	defaultAntigravityClientID     = ""
	defaultAntigravityClientSecret = ""
)

type Config struct {
	ClaudeClientID          string
	CodexClientID           string
	ClaudeTokenURL          string
	ClaudeBootstrapURL      string
	CodexTokenURL           string
	GitHubClientID          string
	GrokClientID            string
	KimiClientID            string
	AntigravityClientID     string
	AntigravityClientSecret string
	FlowTTL                 time.Duration
}

type StartResult struct {
	FlowID                  string `json:"flow_id"`
	FlowType                string `json:"flow_type"`
	AuthorizeURL            string `json:"authorize_url"`
	VerificationURI         string `json:"verification_uri,omitempty"`
	VerificationURIComplete string `json:"verification_uri_complete,omitempty"`
	UserCode                string `json:"user_code,omitempty"`
	Interval                int    `json:"interval,omitempty"`
	ExpiresIn               int    `json:"expires_in,omitempty"`
	Instructions            string `json:"instructions"`
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
	flowType       string
	deviceCode     string
	interval       int
	extra          map[string]string
}

type tokenResponse struct {
	AccessToken  string                 `json:"access_token"`
	RefreshToken string                 `json:"refresh_token"`
	IDToken      string                 `json:"id_token"`
	Metadata     entities.OAuthMetadata `json:"-"`
	ExpiresIn    int64                  `json:"expires_in,omitempty"`
}

type oauthDriver interface {
	Start(context.Context, *Service, flow) (flow, StartResult, error)
	Complete(context.Context, *Service, flow, string) (tokenResponse, error)
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
	if cfg.GitHubClientID == "" {
		cfg.GitHubClientID = defaultGitHubClientID
	}
	if cfg.GrokClientID == "" {
		cfg.GrokClientID = defaultGrokClientID
	}
	if cfg.KimiClientID == "" {
		cfg.KimiClientID = defaultKimiClientID
	}
	if cfg.AntigravityClientID == "" {
		cfg.AntigravityClientID = defaultAntigravityClientID
	}
	if cfg.AntigravityClientSecret == "" {
		cfg.AntigravityClientSecret = defaultAntigravityClientSecret
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
	return &Service{client: client, credentials: credentials, config: cfg, now: time.Now, flows: map[string]flow{}}
}

func (s *Service) Start(providerID, sessionBinding string) (StartResult, error) {
	return s.StartContext(context.Background(), providerID, sessionBinding)
}

func (s *Service) StartContext(ctx context.Context, providerID, sessionBinding string) (StartResult, error) {
	providerID = strings.ToLower(strings.TrimSpace(providerID))
	definition, ok := provider.Lookup(providerID)
	driver := oauthDrivers[providerID]
	if !ok || definition.Auth != provider.AuthOAuth || driver == nil || sessionBinding == "" {
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
	f := flow{provider: providerID, state: state, verifier: verifier, sessionBinding: sessionBinding, expires: s.now().Add(s.config.FlowTTL), extra: map[string]string{}}
	f, result, err := driver.Start(ctx, s, f)
	if err != nil {
		return StartResult{}, err
	}
	if result.FlowID == "" {
		result.FlowID = state
	}
	if result.FlowType == "" {
		result.FlowType = f.flowType
	}
	s.storeFlow(f)
	return result, nil
}

func (s *Service) Complete(ctx context.Context, in CompleteInput) (*entities.Credential, error) {
	s.mu.Lock()
	f, ok := s.flows[in.FlowID]
	s.mu.Unlock()
	if !ok || !f.expires.After(s.now()) || f.provider != in.Provider || f.sessionBinding != in.SessionBinding {
		return nil, ErrInvalidFlow
	}
	driver := oauthDrivers[f.provider]
	if driver == nil {
		return nil, ErrInvalidFlow
	}
	tokens, err := driver.Complete(ctx, s, f, in.Callback)
	if errors.Is(err, ErrAuthorizationPending) {
		return nil, err
	}
	s.mu.Lock()
	delete(s.flows, in.FlowID)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	definition, _ := provider.Lookup(in.Provider)
	if strings.TrimSpace(tokens.AccessToken) == "" || (definition.OAuthRefreshRequired && strings.TrimSpace(tokens.RefreshToken) == "") {
		return nil, errors.New("OAuth token response omitted required tokens")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = definition.Name
	}
	account := tokens.Metadata.AccountID
	if tokens.Metadata.TokenExpiresAt == "" && tokens.ExpiresIn > 0 {
		tokens.Metadata.TokenExpiresAt = s.now().Add(time.Duration(tokens.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)
	}
	return s.credentials.Create(ctx, entities.CredentialInput{Name: name, Provider: in.Provider, Kind: entities.KindOAuth, OAuthAccess: tokens.AccessToken, OAuthRefresh: tokens.RefreshToken, OAuthIDToken: tokens.IDToken, OAuthAccount: account, OAuthMeta: tokens.Metadata, OwnerTenant: in.OwnerTenant})
}

func (s *Service) storeFlow(f flow) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, existing := range s.flows {
		if !existing.expires.After(s.now()) {
			delete(s.flows, id)
		}
	}
	s.flows[f.state] = f
}

func parseCallback(value string) (string, string, error) {
	value = strings.TrimSpace(value)
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" {
		if code, state := parsed.Query().Get("code"), parsed.Query().Get("state"); code != "" && state != "" {
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

var oauthDrivers = map[string]oauthDriver{
	"codex": codexDriver{}, "claude": claudeDriver{}, "github-copilot": githubCopilotDriver{},
	"cursor": cursorDriver{}, "grok-build": grokBuildDriver{}, "xai-oauth": xAIDriver{},
	"kimi-code": kimiCodeDriver{}, "cline": clineDriver{}, "clinepass": clinePassDriver{},
	"kilo-code": kiloCodeDriver{}, "kiro": kiroDriver{}, "amazon-q": amazonQDriver{},
	"antigravity": antigravityDriver{},
}
