package oauth

import (
	"context"
	"net/url"
)

type xAIDriver struct{}

func (xAIDriver) Start(_ context.Context, s *Service, f flow) (flow, StartResult, error) {
	f.flowType = "authorization_code_pkce"
	f.redirectURI = "http://127.0.0.1:56121/callback"
	q := url.Values{"response_type": {"code"}, "client_id": {s.config.GrokClientID}, "redirect_uri": {f.redirectURI}, "scope": {"openid profile email offline_access grok-cli:access api:access"}, "code_challenge": {pkceChallenge(f.verifier)}, "code_challenge_method": {"S256"}, "state": {f.state}}
	return f, StartResult{AuthorizeURL: "https://auth.x.ai/oauth2/authorize?" + q.Encode(), Instructions: "Open the xAI authorization page, then paste the complete callback URL here."}, nil
}
func (xAIDriver) Complete(ctx context.Context, s *Service, f flow, callback string) (tokenResponse, error) {
	code, state, err := parseCallback(callback)
	if err != nil || state != f.state {
		return tokenResponse{}, ErrBadCallback
	}
	return exchangeForm(ctx, s, "https://auth.x.ai/oauth2/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {s.config.GrokClientID}, "code": {code}, "redirect_uri": {f.redirectURI}, "code_verifier": {f.verifier}})
}
