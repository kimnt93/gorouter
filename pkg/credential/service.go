package credential

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

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
	ID            string `json:"id"`
	OwnedBy       string `json:"owned_by,omitempty"`
	ContextLength int64  `json:"context_length,omitempty"`
}

type ModelDiscoverer interface {
	DiscoverModels(ctx context.Context, runtime *entities.CredentialRuntime) ([]ProviderModel, error)
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
	repo Repository
	box  entities.SecretBox
}

func NewService(repo Repository, box entities.SecretBox) *Service {
	return &Service{repo: repo, box: box}
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
	return s.repo.Create(ctx, in, s.box)
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
		if strings.TrimSpace(in.OAuthRefresh) == "" {
			return fmt.Errorf("%w: oauth_refresh is required for kind oauth", ErrInvalidCredential)
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
	return s.repo.Update(ctx, s.box, id, in)
}

func (s *Service) Delete(ctx context.Context, id string) error { return s.repo.Delete(ctx, id) }

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

func (s *Service) DiscoverModels(ctx context.Context, id string, discoverer ModelDiscoverer) ([]ProviderModel, error) {
	if discoverer == nil {
		return nil, ErrUnsupportedProvider
	}
	runtime, err := s.Runtime(ctx, id)
	if err != nil {
		return nil, err
	}
	models, err := discoverer.DiscoverModels(ctx, runtime)
	if err != nil {
		return nil, err
	}
	return models, nil
}
