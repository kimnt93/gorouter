package llm

import (
	"context"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
	"net/http"
)

// OpenCodeGoAdapter is API-key based because OmniRoute exposes OpenCode Go as
// an OpenAI-compatible API-key provider rather than an OAuth subscription.
type OpenCodeGoAdapter struct{ HTTP *http.Client }

func (a *OpenCodeGoAdapter) delegate() *OpenAIAdapter { return &OpenAIAdapter{HTTP: a.HTTP} }
func (a *OpenCodeGoAdapter) Send(c context.Context, r *entities.CredentialRuntime, m string, b []byte) (*entities.UpstreamResult, error) {
	return a.delegate().Send(c, r, m, b)
}
func (a *OpenCodeGoAdapter) Probe(c context.Context, r *entities.CredentialRuntime) (int, error) {
	return a.delegate().Probe(c, r)
}
func (a *OpenCodeGoAdapter) DiscoverModels(c context.Context, r *entities.CredentialRuntime) ([]credential.ProviderModel, error) {
	return a.delegate().DiscoverModels(c, r)
}
