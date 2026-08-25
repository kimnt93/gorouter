package credential

import (
	"errors"
	"testing"

	"github.com/kimnt93/gorouter/pkg/entities"
)

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
