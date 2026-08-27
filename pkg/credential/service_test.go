package credential

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

type credentialRepoStub struct{ runtime *entities.CredentialRuntime }

func (r credentialRepoStub) Create(context.Context, entities.CredentialInput, entities.SecretBox) (*entities.Credential, error) {
	return nil, nil
}
func (r credentialRepoStub) List(context.Context) ([]entities.Credential, error) { return nil, nil }
func (r credentialRepoStub) Update(context.Context, entities.SecretBox, string, entities.CredentialUpdate) (*entities.Credential, error) {
	return nil, nil
}
func (r credentialRepoStub) Delete(context.Context, string) error { return nil }
func (r credentialRepoStub) Runtime(context.Context, entities.SecretBox, string) (*entities.CredentialRuntime, error) {
	return r.runtime, nil
}
func (r credentialRepoStub) UpdateOAuthTokens(context.Context, entities.SecretBox, string, string, string) error {
	return nil
}
func (r credentialRepoStub) RoutesForModel(context.Context, string) ([]entities.RouteCandidate, error) {
	return nil, nil
}

type connectivityProbeStub struct {
	status int
	err    error
}

type modelDiscovererProbeStub struct {
	model string
	calls *int
}

func (p modelDiscovererProbeStub) Probe(context.Context, *entities.CredentialRuntime) (int, error) {
	return 200, nil
}

func (p modelDiscovererProbeStub) DiscoverModels(context.Context, *entities.CredentialRuntime) ([]ProviderModel, error) {
	if p.calls != nil {
		*p.calls++
	}
	return []ProviderModel{{ID: p.model}}, nil
}

type discoveryCacheStub struct {
	models  []ProviderModel
	present bool
	deleted int
}

func (c *discoveryCacheStub) Get(context.Context, string) ([]ProviderModel, bool, error) {
	return c.models, c.present, nil
}
func (c *discoveryCacheStub) Set(_ context.Context, _ string, models []ProviderModel, _ time.Duration) error {
	c.models, c.present = models, true
	return nil
}
func (c *discoveryCacheStub) Delete(context.Context, string) error {
	c.present = false
	c.deleted++
	return nil
}

func (p connectivityProbeStub) Probe(context.Context, *entities.CredentialRuntime) (int, error) {
	return p.status, p.err
}

func TestValidateCredential(t *testing.T) {
	tests := []struct {
		name string
		in   CreateInput
		want error
	}{
		{"missing name", CreateInput{Provider: entities.ProviderOpenAICompatible, Kind: entities.KindAPIKey, APIKey: "secret"}, ErrInvalidCredential},
		{"unknown provider", CreateInput{Name: "x", Provider: "other", Kind: entities.KindAPIKey, APIKey: "secret"}, ErrUnsupportedProvider},
		{"missing api key", CreateInput{Name: "x", Provider: entities.ProviderOpenAICompatible, Kind: entities.KindAPIKey}, ErrInvalidCredential},
		{"oauth provider", CreateInput{Name: "x", Provider: entities.ProviderOpenAICompatible, Kind: entities.KindOAuth, OAuthRefresh: "refresh"}, ErrInvalidCredential},
		{"missing refresh", CreateInput{Name: "x", Provider: entities.ProviderAnthropic, Kind: entities.KindOAuth}, ErrInvalidCredential},
		{"bad url", CreateInput{Name: "x", Provider: entities.ProviderAnthropic, Kind: entities.KindAPIKey, APIKey: "secret", BaseURL: "://bad"}, ErrInvalidCredential},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validate(tt.in); !errors.Is(err, tt.want) {
				t.Fatalf("validate() error = %v, want %v", err, tt.want)
			}
		})
	}
	valid := CreateInput{Name: "anthropic", Provider: entities.ProviderAnthropic, Kind: entities.KindOAuth, OAuthRefresh: "refresh", BaseURL: "https://example.test"}
	if err := validate(valid); err != nil {
		t.Fatalf("valid credential rejected: %v", err)
	}
}

func TestConnectivityDoesNotExposeProbeErrors(t *testing.T) {
	runtime := &entities.CredentialRuntime{ID: "cred-1", Provider: entities.ProviderAnthropic}
	service := NewService(credentialRepoStub{runtime: runtime}, nil)
	result, err := service.TestConnectivity(context.Background(), runtime.ID, map[string]ConnectivityProber{
		entities.ProviderAnthropic: connectivityProbeStub{err: errors.New("dial failed with sensitive diagnostics")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || result.Status != 0 {
		t.Fatalf("unexpected connectivity result: %+v", result)
	}
}

func TestResolveModelDiscovererUsesSpecificAdapterThenProtocolFallback(t *testing.T) {
	generic := modelDiscovererProbeStub{model: "generic"}
	specific := modelDiscovererProbeStub{model: "specific"}
	adapters := map[string]ConnectivityProber{"opencode-zen": specific}

	for _, test := range []struct {
		name       string
		providerID string
		adapters   map[string]ConnectivityProber
		want       string
	}{
		{name: "specific adapter", providerID: "opencode-zen", adapters: adapters, want: "specific"},
		{name: "protocol fallback", providerID: "openai", adapters: adapters, want: "generic"},
		{name: "non-discovering adapter falls back", providerID: "opencode-zen", adapters: map[string]ConnectivityProber{"opencode-zen": connectivityProbeStub{}}, want: "generic"},
	} {
		t.Run(test.name, func(t *testing.T) {
			discoverer := ResolveModelDiscoverer(test.providerID, test.adapters, generic, nil, nil)
			models, err := discoverer.DiscoverModels(context.Background(), &entities.CredentialRuntime{})
			if err != nil || len(models) != 1 || models[0].ID != test.want {
				t.Fatalf("models=%+v err=%v", models, err)
			}
		})
	}
	if discoverer := ResolveModelDiscoverer("unknown", nil, generic, nil, nil); discoverer != nil {
		t.Fatalf("unknown provider resolved to %T", discoverer)
	}
}

func TestDiscoverModelsUsesCacheAndMutationInvalidatesIt(t *testing.T) {
	cache := &discoveryCacheStub{}
	service := NewService(credentialRepoStub{runtime: &entities.CredentialRuntime{ID: "cred"}}, nil)
	service.SetModelDiscoveryCache(cache, time.Minute)
	calls := 0
	discoverer := modelDiscovererProbeStub{model: "fresh", calls: &calls}

	for range 2 {
		models, err := service.DiscoverModels(context.Background(), "cred", discoverer)
		if err != nil || len(models) != 1 || models[0].ID != "fresh" {
			t.Fatalf("models=%+v err=%v", models, err)
		}
	}
	if calls != 1 {
		t.Fatalf("provider discovery calls=%d", calls)
	}
	notified := 0
	service.SetCredentialsChanged(func() { notified++ })
	if _, err := service.Update(context.Background(), "cred", entities.CredentialUpdate{Name: "updated", Status: entities.StatusActive}); err != nil {
		t.Fatal(err)
	}
	if cache.deleted != 1 || notified != 1 {
		t.Fatalf("deleted=%d notified=%d", cache.deleted, notified)
	}
	if _, err := service.DiscoverModels(context.Background(), "cred", discoverer); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("provider discovery after invalidation calls=%d", calls)
	}
}
