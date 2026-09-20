package llm

import (
	"context"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
)

// Historical connections stay deletable/auditable. Do not send their credentials
// to a different protocol, create fake model routes, or try a generic HTTP API.
type RetiredDevinAdapter struct{}

func (*RetiredDevinAdapter) Probe(context.Context, *entities.CredentialRuntime) (int, error) {
	return 400, retiredDevinError()
}
func (*RetiredDevinAdapter) DiscoverModels(context.Context, *entities.CredentialRuntime) ([]credential.ProviderModel, error) {
	return nil, retiredDevinError()
}
func (*RetiredDevinAdapter) Send(context.Context, *entities.CredentialRuntime, string, []byte) (*entities.UpstreamResult, error) {
	return devinReject(retiredDevinError()), nil
}
func retiredDevinError() error {
	return devinFailure(400, "This Devin connection is retired; reconnect using Devin and import its live models")
}
