package llm

import (
	"context"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
	"io"
	"net/http"
)

type KimiCodeAdapter struct {
	HTTP      *http.Client
	Persister OAuthTokenPersister
	ClientID  string
}

func (a *KimiCodeAdapter) refresh(c context.Context, r *entities.CredentialRuntime) error {
	return refreshOAuthForm(c, a.HTTP, a.Persister, r, "https://auth.kimi.com/api/oauth/token", a.ClientID, "", nil)
}
func (a *KimiCodeAdapter) delegate() *AnthropicAdapter {
	return &AnthropicAdapter{HTTP: a.HTTP, Refresh: a.refresh}
}
func (a *KimiCodeAdapter) Send(c context.Context, r *entities.CredentialRuntime, m string, b []byte) (*entities.UpstreamResult, error) {
	return a.delegate().Send(c, r, m, b)
}
func (a *KimiCodeAdapter) Probe(c context.Context, r *entities.CredentialRuntime) (int, error) {
	headers, err := anthropicHeaders(r)
	if err != nil {
		return 0, err
	}
	result, err := get(c, a.delegate().httpClient(), anthropicBase(r.BaseURL)+"/v1/models", headers)
	if err != nil {
		return 0, err
	}
	defer result.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(result.Body, 64<<10))
	return result.StatusCode, nil
}
func (a *KimiCodeAdapter) DiscoverModels(ctx context.Context, runtime *entities.CredentialRuntime) ([]credential.ProviderModel, error) {
	headers, err := anthropicHeaders(runtime)
	if err != nil {
		return modelsFor("kimi-code", "k3", "kimi-for-coding", "kimi-for-coding-highspeed"), nil
	}
	result, err := get(ctx, a.delegate().httpClient(), anthropicBase(runtime.BaseURL)+"/v1/models", headers)
	if err == nil {
		defer result.Body.Close()
		if result.StatusCode >= 200 && result.StatusCode < 300 {
			models, decodeErr := decodeProviderModels(io.LimitReader(result.Body, maxModelCatalogBytes))
			if decodeErr == nil && len(models) > 0 {
				return models, nil
			}
		}
	}
	return modelsFor("kimi-code", "k3", "kimi-for-coding", "kimi-for-coding-highspeed"), nil
}
