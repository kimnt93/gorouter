package oauth

import (
	"context"
	"encoding/json"
	"github.com/kimnt93/gorouter/pkg/entities"
	"io"
	"net/http"
	"net/url"
	"strings"
)

var antigravityScopes = []string{"https://www.googleapis.com/auth/cloud-platform", "https://www.googleapis.com/auth/userinfo.email", "https://www.googleapis.com/auth/userinfo.profile", "https://www.googleapis.com/auth/cclog", "https://www.googleapis.com/auth/experimentsandconfigs"}

type antigravityDriver struct{}

func (antigravityDriver) Start(_ context.Context, s *Service, f flow) (flow, StartResult, error) {
	f.flowType = "authorization_code"
	f.redirectURI = "http://localhost:51121/oauth-callback"
	q := url.Values{"response_type": {"code"}, "client_id": {s.config.AntigravityClientID}, "redirect_uri": {f.redirectURI}, "scope": {strings.Join(antigravityScopes, " ")}, "state": {f.state}, "access_type": {"offline"}, "prompt": {"consent"}}
	return f, StartResult{AuthorizeURL: "https://accounts.google.com/o/oauth2/v2/auth?" + q.Encode(), Instructions: "Open Google authorization, then paste the complete localhost callback URL here."}, nil
}
func (antigravityDriver) Complete(ctx context.Context, s *Service, f flow, callback string) (tokenResponse, error) {
	code, state, err := parseCallback(callback)
	if err != nil || state != f.state {
		return tokenResponse{}, ErrBadCallback
	}
	t, err := exchangeForm(ctx, s, "https://oauth2.googleapis.com/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {s.config.AntigravityClientID}, "client_secret": {s.config.AntigravityClientSecret}, "code": {code}, "redirect_uri": {f.redirectURI}})
	if err != nil {
		return t, err
	}
	t.Metadata = s.antigravityMetadata(ctx, t.AccessToken)
	return t, nil
}
func (s *Service) antigravityMetadata(ctx context.Context, token string) entities.OAuthMetadata {
	m := entities.OAuthMetadata{}
	request := func(method, endpoint string, body io.Reader, target any) bool {
		req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
		if err != nil {
			return false
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := s.client.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode >= 200 && resp.StatusCode < 300 && json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(target) == nil
	}
	var user map[string]any
	if request(http.MethodGet, "https://www.googleapis.com/oauth2/v1/userinfo?alt=json", nil, &user) {
		m.Email = stringValue(user, "email")
	}
	payload := `{"metadata":{"ideType":"ANTIGRAVITY","platform":"PLATFORM_UNSPECIFIED","pluginType":"GEMINI"}}`
	var assist map[string]any
	if request(http.MethodPost, "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist", strings.NewReader(payload), &assist) {
		if project, ok := assist["cloudaicompanionProject"].(string); ok {
			m.ProjectID = project
		}
		if project, ok := assist["cloudaicompanionProject"].(map[string]any); ok {
			m.ProjectID = stringValue(project, "id")
		}
	}
	return m
}
