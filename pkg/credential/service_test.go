package credential

import (
	"context"
	"errors"
	"testing"

	"github.com/kimnt93/gorouter/pkg/entities"
)

type credentialRepoStub struct{ runtime *entities.CredentialRuntime }

func (r credentialRepoStub) Create(context.Context, entities.CredentialInput, entities.SecretBox) (*entities.Credential, error) {
	return nil, nil
}
func (r credentialRepoStub) List(context.Context) ([]entities.Credential, error) { return nil, nil }
func (r credentialRepoStub) Delete(context.Context, string) error                { return nil }
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
