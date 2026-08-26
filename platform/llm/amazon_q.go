package llm

import (
	"context"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
	"net/http"
)

type AmazonQAdapter struct {
	HTTP      *http.Client
	Persister OAuthTokenPersister
}

func (a *AmazonQAdapter) client() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return NewHTTPClient()
}
func (a *AmazonQAdapter) Send(c context.Context, r *entities.CredentialRuntime, m string, b []byte) (*entities.UpstreamResult, error) {
	result, err := sendKiroNative(c, a.client(), r, m, b)
	if err == nil && result != nil && result.StatusCode == http.StatusUnauthorized && a.Persister != nil && canRetryOAuth(c) {
		result.Body.Close()
		if refreshErr := refreshKiroOAuth(c, a.HTTP, a.Persister, r); refreshErr != nil {
			return nil, refreshErr
		}
		return a.Send(markOAuthRetry(c), r, m, b)
	}
	return result, err
}
func (a *AmazonQAdapter) Probe(c context.Context, r *entities.CredentialRuntime) (int, error) {
	return (&KiroAdapter{HTTP: a.client(), Persister: a.Persister}).Probe(c, r)
}
func (a *AmazonQAdapter) DiscoverModels(context.Context, *entities.CredentialRuntime) ([]credential.ProviderModel, error) {
	return modelsFor("amazon-q", "claude-sonnet-5", "claude-sonnet-4.5", "claude-haiku-4.5", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"), nil
}
