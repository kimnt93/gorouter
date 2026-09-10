package credential

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/provider"
)

var (
	ErrInvalidCredential   = errors.New("invalid credential")
	ErrUnsupportedProvider = errors.New("unsupported credential provider")
)

type ConnectivityResult struct {
	OK        bool  `json:"ok"`
	Status    int   `json:"status,omitempty"`
	LatencyMS int64 `json:"latency_ms"`
}

type ConnectivityProber interface {
	Probe(ctx context.Context, runtime *entities.CredentialRuntime) (status int, err error)
}

type ProviderModel struct {
	ID                       string                         `json:"id"`
	Object                   string                         `json:"object,omitempty"`
	Created                  int64                          `json:"created,omitempty"`
	OwnedBy                  string                         `json:"owned_by,omitempty"`
	Permission               []any                          `json:"permission,omitempty"`
	Root                     string                         `json:"root,omitempty"`
	Parent                   *string                        `json:"parent,omitempty"`
	APIFormat                string                         `json:"api_format,omitempty"`
	ContextLength            int64                          `json:"context_length,omitempty"`
	MaxOutputTokens          int64                          `json:"max_output_tokens,omitempty"`
	SupportedEndpoints       []string                       `json:"supported_endpoints,omitempty"`
	Capabilities             map[string]any                 `json:"capabilities,omitempty"`
	InputModalities          []string                       `json:"input_modalities,omitempty"`
	OutputModalities         []string                       `json:"output_modalities,omitempty"`
	MaxInputTokens           int64                          `json:"max_input_tokens,omitempty"`
	MaxContextWindow         int64                          `json:"max_context_window,omitempty"`
	Name                     string                         `json:"name,omitempty"`
	Description              string                         `json:"description,omitempty"`
	DefaultReasoningLevel    string                         `json:"default_reasoning_level,omitempty"`
	SupportedReasoningLevels []entities.ModelReasoningLevel `json:"supported_reasoning_levels,omitempty"`
	SupportsOriginalImage    bool                           `json:"supports_image_detail_original,omitempty"`
	SupportsReasoningSummary bool                           `json:"supports_reasoning_summary_parameter,omitempty"`
	SupportsParallelTools    bool                           `json:"supports_parallel_tool_calls,omitempty"`
	SupportsVerbosity        bool                           `json:"support_verbosity,omitempty"`
	DefaultVerbosity         string                         `json:"default_verbosity,omitempty"`
}

type ModelDiscoverer interface {
	DiscoverModels(ctx context.Context, runtime *entities.CredentialRuntime) ([]ProviderModel, error)
}

// ModelDiscoveryCache stores only provider-reported, non-secret model metadata.
// Implementations must namespace entries by credential ID.
type ModelDiscoveryCache interface {
	Get(ctx context.Context, credentialID string) ([]ProviderModel, bool, error)
	Set(ctx context.Context, credentialID string, models []ProviderModel, ttl time.Duration) error
	Delete(ctx context.Context, credentialID string) error
}

// ResolveModelDiscoverer prefers a provider-specific adapter and falls back to
// the provider's wire protocol. Keeping this resolution in one place prevents
// automatic catalog refresh and the management API from drifting apart when a
// provider is added or its adapter changes.
func ResolveModelDiscoverer(providerID string, adapters map[string]ConnectivityProber, openAI, anthropic, codex ModelDiscoverer) ModelDiscoverer {
	if adapter := adapters[providerID]; adapter != nil {
		if discoverer, ok := adapter.(ModelDiscoverer); ok {
			return discoverer
		}
	}
	switch provider.ProtocolFor(providerID) {
	case provider.ProtocolOpenAI:
		return openAI
	case provider.ProtocolAnthropic:
		return anthropic
	case provider.ProtocolCodex:
		return codex
	default:
		return nil
	}
}

type Repository interface {
	Create(ctx context.Context, in entities.CredentialInput, box entities.SecretBox) (*entities.Credential, error)
	List(ctx context.Context) ([]entities.Credential, error)
	Update(ctx context.Context, box entities.SecretBox, id string, in entities.CredentialUpdate) (*entities.Credential, error)
	Delete(ctx context.Context, id string) error
	Runtime(ctx context.Context, box entities.SecretBox, id string) (*entities.CredentialRuntime, error)
	UpdateOAuthTokens(ctx context.Context, box entities.SecretBox, id, access, refresh string) error
	RoutesForModel(ctx context.Context, model string) ([]entities.RouteCandidate, error)
}

type Service struct {
	repo               Repository
	box                entities.SecretBox
	discoveryCache     ModelDiscoveryCache
	discoveryTTL       time.Duration
	credentialsChanged []func()
	discoveryGroup     singleflight.Group
}

func NewService(repo Repository, box entities.SecretBox) *Service {
	return &Service{repo: repo, box: box}
}

func (s *Service) SetModelDiscoveryCache(cache ModelDiscoveryCache, ttl time.Duration) {
	s.discoveryCache = cache
	s.discoveryTTL = ttl
}

// SetCredentialsChanged registers a lightweight notifier. Callers should use
// it to wake background reconciliation, not perform provider I/O inline.
func (s *Service) SetCredentialsChanged(notify func()) {
	s.credentialsChanged = nil
	s.AddCredentialsChanged(notify)
}

func (s *Service) AddCredentialsChanged(notify func()) {
	if notify != nil {
		s.credentialsChanged = append(s.credentialsChanged, notify)
	}
}

func (s *Service) changed(ctx context.Context, id string) {
	if s.discoveryCache != nil && id != "" {
		_ = s.discoveryCache.Delete(ctx, id)
	}
	for _, notify := range s.credentialsChanged {
		notify()
	}
}

type CreateInput = entities.CredentialInput

func (s *Service) Create(ctx context.Context, in CreateInput) (*entities.Credential, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Provider = strings.ToLower(strings.TrimSpace(in.Provider))
	in.Kind = strings.ToLower(strings.TrimSpace(in.Kind))
	in.BaseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	if resolved, ok := provider.ResolveBaseURL(in.Provider, in.BaseURL); ok {
		in.BaseURL = resolved
	}
	if err := validate(in); err != nil {
		return nil, err
	}
	created, err := s.repo.Create(ctx, in, s.box)
	if err == nil && created != nil {
		s.changed(ctx, created.ID)
	}
	return created, err
}

func validate(in CreateInput) error {
	if strings.TrimSpace(in.Name) == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidCredential)
	}
	definition, ok := provider.Lookup(in.Provider)
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnsupportedProvider, in.Provider)
	}
	switch in.Kind {
	case entities.KindAPIKey:
		if strings.TrimSpace(in.APIKey) == "" {
			return fmt.Errorf("%w: api_key is required for kind api_key", ErrInvalidCredential)
		}
	case entities.KindOAuth:
		if !definition.OAuthSupported && in.Provider != entities.ProviderAnthropic {
			return fmt.Errorf("%w: oauth is not supported for %s", ErrInvalidCredential, in.Provider)
		}
		if definition.OAuthRefreshRequired && strings.TrimSpace(in.OAuthRefresh) == "" {
			return fmt.Errorf("%w: oauth_refresh is required for kind oauth", ErrInvalidCredential)
		}
		if strings.TrimSpace(in.OAuthAccess) == "" && strings.TrimSpace(in.OAuthRefresh) == "" {
			return fmt.Errorf("%w: oauth token is required for kind oauth", ErrInvalidCredential)
		}
	default:
		return fmt.Errorf("%w: unsupported kind %q", ErrInvalidCredential, in.Kind)
	}
	if in.BaseURL != "" {
		u, err := url.ParseRequestURI(in.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("%w: base_url must be an absolute HTTP(S) URL", ErrInvalidCredential)
		}
	}
	return nil
}

func (s *Service) List(ctx context.Context) ([]entities.Credential, error) {
	return s.repo.List(ctx)
}

func (s *Service) Update(ctx context.Context, id string, in entities.CredentialUpdate) (*entities.Credential, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.BaseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	in.Status = strings.ToLower(strings.TrimSpace(in.Status))
	if in.Name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidCredential)
	}
	if in.Status != "active" && in.Status != "disabled" {
		return nil, fmt.Errorf("%w: status must be active or disabled", ErrInvalidCredential)
	}
	if in.BaseURL != "" {
		u, err := url.ParseRequestURI(in.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("%w: base_url must be an absolute HTTP(S) URL", ErrInvalidCredential)
		}
	}
	updated, err := s.repo.Update(ctx, s.box, id, in)
	if err == nil {
		s.changed(ctx, id)
	}
	return updated, err
}

func (s *Service) Delete(ctx context.Context, id string) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	s.changed(ctx, id)
	return nil
}

func (s *Service) Runtime(ctx context.Context, id string) (*entities.CredentialRuntime, error) {
	return s.repo.Runtime(ctx, s.box, id)
}

func (s *Service) UpdateOAuthTokens(ctx context.Context, id, access, refresh string) error {
	return s.repo.UpdateOAuthTokens(ctx, s.box, id, access, refresh)
}

func (s *Service) Routes(ctx context.Context, model string) ([]entities.RouteCandidate, error) {
	return s.repo.RoutesForModel(ctx, model)
}

// TestConnectivity loads a credential only at runtime and delegates the probe
// to its provider adapter. The result intentionally excludes upstream bodies,
// which can contain provider diagnostics or account information.
func (s *Service) TestConnectivity(ctx context.Context, id string, probes map[string]ConnectivityProber) (*ConnectivityResult, error) {
	runtime, err := s.Runtime(ctx, id)
	if err != nil {
		return nil, err
	}
	probe := probes[runtime.Provider]
	if probe == nil {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedProvider, runtime.Provider)
	}
	started := time.Now()
	status, probeErr := probe.Probe(ctx, runtime)
	result := &ConnectivityResult{OK: probeErr == nil && status >= 200 && status < 300, Status: status, LatencyMS: time.Since(started).Milliseconds()}
	return result, nil
}

func (s *Service) RefreshDiscoveredModels(ctx context.Context, id string, discoverer ModelDiscoverer) ([]ProviderModel, error) {
	if s.discoveryCache != nil {
		_ = s.discoveryCache.Delete(ctx, id)
	}
	return s.DiscoverModels(ctx, id, discoverer)
}

func (s *Service) DiscoverModels(ctx context.Context, id string, discoverer ModelDiscoverer) ([]ProviderModel, error) {
	if discoverer == nil {
		return nil, ErrUnsupportedProvider
	}
	runtime, err := s.Runtime(ctx, id)
	if err != nil {
		return nil, err
	}
	if s.discoveryCache != nil && s.discoveryTTL > 0 {
		if cached, ok, cacheErr := s.discoveryCache.Get(ctx, id); cacheErr == nil && ok {
			return cached, nil
		}
	}
	result, err, _ := s.discoveryGroup.Do(id, func() (any, error) {
		if s.discoveryCache != nil && s.discoveryTTL > 0 {
			if cached, ok, cacheErr := s.discoveryCache.Get(ctx, id); cacheErr == nil && ok {
				return cached, nil
			}
		}
		models, discoverErr := discoverer.DiscoverModels(ctx, runtime)
		if discoverErr != nil {
			return nil, discoverErr
		}
		if s.discoveryCache != nil && s.discoveryTTL > 0 {
			_ = s.discoveryCache.Set(ctx, id, models, s.discoveryTTL)
		}
		return models, nil
	})
	if err != nil {
		return nil, err
	}
	models, _ := result.([]ProviderModel)
	return append([]ProviderModel(nil), models...), nil
}

// AccountLabel returns non-secret subscription identity for management display.
// Metadata populated by the OAuth provider wins; JWT claims are a fallback.
func AccountLabel(runtime *entities.CredentialRuntime) string {
	if runtime == nil || runtime.Kind != entities.KindOAuth {
		return ""
	}
	for _, value := range []string{runtime.OAuthMeta.Email, runtime.OAuthMeta.Login} {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	for _, token := range []string{runtime.OAuthIDToken, runtime.OAuthAccess} {
		if value := jwtAccountLabel(token); value != "" {
			return value
		}
	}
	for _, value := range []string{runtime.OAuthAccount, runtime.OAuthMeta.AccountID, runtime.OAuthMeta.PrincipalID, runtime.OAuthMeta.OrganizationID} {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return "connected account"
}

func jwtAccountLabel(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims map[string]any
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	for _, key := range []string{"email", "preferred_username", "login", "name", "username"} {
		if value, ok := claims[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	for _, namespace := range []string{"https://api.openai.com/auth", "https://api.x.ai/auth"} {
		if nested, ok := claims[namespace].(map[string]any); ok {
			for _, key := range []string{"email", "login", "username", "user_id", "account_id", "chatgpt_account_id"} {
				if value, ok := nested[key].(string); ok && strings.TrimSpace(value) != "" {
					return strings.TrimSpace(value)
				}
			}
		}
	}
	if value, ok := claims["sub"].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

// APIKeyPreview returns a stable, non-reversible display preview. It is used to
// repair legacy/imported credentials whose persisted preview is empty.
func APIKeyPreview(runtime *entities.CredentialRuntime) string {
	if runtime == nil || runtime.Kind != entities.KindAPIKey {
		return ""
	}
	secret := strings.TrimSpace(runtime.APIKey)
	if len(secret) <= 8 {
		return "••••••"
	}
	return secret[:6] + "…" + secret[len(secret)-4:]
}

// MaskPreview normalizes a persisted safe preview into the public display
// label without ever requiring plaintext secret material.
func MaskPreview(preview string) string {
	preview = strings.TrimSpace(preview)
	if preview == "" {
		return "encrypted API key"
	}
	if preview == "…" {
		return "••••••"
	}
	parts := strings.SplitN(preview, "…", 2)
	if len(parts) == 2 {
		return parts[0] + "******" + parts[1]
	}
	return preview
}
