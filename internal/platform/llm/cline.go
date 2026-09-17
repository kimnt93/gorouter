package llm

import (
	"context"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
	"net/http"
)

type ClineAdapter struct {
	Refresh   func(context.Context, *entities.CredentialRuntime) error
	HTTP      *http.Client
	Persister OAuthTokenPersister
}

func (a *ClineAdapter) refresh(c context.Context, r *entities.CredentialRuntime) error {
	return refreshOAuthJSON(c, a.HTTP, a.Persister, r, "https://api.cline.bot/api/v1/auth/refresh", map[string]string{"refreshToken": r.OAuthRefreh, "grantType": "refresh_token", "clientType": "extension"}, nil)
}
func (a *ClineAdapter) refreshRuntime(c context.Context, r *entities.CredentialRuntime) error {
	if a.Refresh != nil {
		return a.Refresh(c, r)
	}
	return a.refresh(c, r)
}
func (a *ClineAdapter) delegate() *OpenAIAdapter {
	return &OpenAIAdapter{HTTP: a.HTTP, Refresh: a.refreshRuntime}
}
func (a *ClineAdapter) Send(c context.Context, r *entities.CredentialRuntime, m string, b []byte) (*entities.UpstreamResult, error) {
	return a.delegate().Send(c, r, m, b)
}
func (a *ClineAdapter) Probe(c context.Context, r *entities.CredentialRuntime) (int, error) {
	return a.delegate().Probe(c, r)
}
func (a *ClineAdapter) DiscoverModels(c context.Context, r *entities.CredentialRuntime) ([]credential.ProviderModel, error) {
	models, err := a.delegate().DiscoverModels(c, r)
	if err == nil && len(models) > 0 {
		return models, nil
	}
	return modelsFor("cline", "default"), nil
}

func (a *ClineAdapter) RefreshToken(ctx context.Context, cr *entities.CredentialRuntime) error {
	return a.refresh(ctx, cr)
}
