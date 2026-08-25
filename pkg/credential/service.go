package credential

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
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

type Repository interface {
	Create(ctx context.Context, in entities.CredentialInput, box entities.SecretBox) (*entities.Credential, error)
	List(ctx context.Context) ([]entities.Credential, error)
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
	if err := validate(in); err != nil {
		return nil, err
	}
	return s.repo.Create(ctx, in, s.box)
}

func validate(in CreateInput) error {
	if strings.TrimSpace(in.Name) == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidCredential)
	}
	switch in.Provider {
	case entities.ProviderOpenAICompatible, entities.ProviderAnthropic:
	default:
		return fmt.Errorf("%w: %q", ErrUnsupportedProvider, in.Provider)
	}
	switch in.Kind {
	case entities.KindAPIKey:
		if strings.TrimSpace(in.APIKey) == "" {
			return fmt.Errorf("%w: api_key is required for kind api_key", ErrInvalidCredential)
		}
	case entities.KindOAuth:
		if in.Provider != entities.ProviderAnthropic {
			return fmt.Errorf("%w: oauth is only supported for anthropic", ErrInvalidCredential)
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
