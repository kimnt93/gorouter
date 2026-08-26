package llm

import (
	"context"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
	"net/http"
)

type XAIAdapter struct {
	HTTP      *http.Client
	Persister OAuthTokenPersister
	ClientID  string
}

func (a *XAIAdapter) refresh(c context.Context, r *entities.CredentialRuntime) error {
	return refreshOAuthForm(c, a.HTTP, a.Persister, r, "https://auth.x.ai/oauth2/token", a.ClientID, "", nil)
}
func (a *XAIAdapter) delegate() *OpenAIAdapter {
	return &OpenAIAdapter{HTTP: a.HTTP, Refresh: a.refresh}
}
func (a *XAIAdapter) Send(c context.Context, r *entities.CredentialRuntime, m string, b []byte) (*entities.UpstreamResult, error) {
	return a.delegate().Send(c, r, m, b)
}
func (a *XAIAdapter) Probe(c context.Context, r *entities.CredentialRuntime) (int, error) {
	return a.delegate().Probe(c, r)
}
func (a *XAIAdapter) DiscoverModels(c context.Context, r *entities.CredentialRuntime) ([]credential.ProviderModel, error) {
	models, err := a.delegate().DiscoverModels(c, r)
	if err == nil && len(models) > 0 {
		return models, nil
	}
	return modelsFor("xai-oauth", "grok-4.1", "grok-4.1-fast", "grok-code-fast-1"), nil
}
