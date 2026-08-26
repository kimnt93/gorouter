package llm

import (
	"context"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
	"net/http"
)

type KiloCodeAdapter struct{ HTTP *http.Client }

func (a *KiloCodeAdapter) delegate() *OpenAIAdapter { return &OpenAIAdapter{HTTP: a.HTTP} }
func (a *KiloCodeAdapter) Send(c context.Context, r *entities.CredentialRuntime, m string, b []byte) (*entities.UpstreamResult, error) {
	return a.delegate().Send(c, r, m, b)
}
func (a *KiloCodeAdapter) Probe(c context.Context, r *entities.CredentialRuntime) (int, error) {
	return a.delegate().Probe(c, r)
}
func (a *KiloCodeAdapter) DiscoverModels(c context.Context, r *entities.CredentialRuntime) ([]credential.ProviderModel, error) {
	return a.delegate().DiscoverModels(c, r)
}
