package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

const (
	antigravityOAuthUserAgent = "antigravity/1.11.9 darwin/arm64 google-api-nodejs-client/10.3.0"
	antigravityAPIClient      = "gl-node/22.21.1"
)

var antigravityScopes = []string{"https://www.googleapis.com/auth/cloud-platform", "https://www.googleapis.com/auth/userinfo.email", "https://www.googleapis.com/auth/userinfo.profile", "https://www.googleapis.com/auth/cclog", "https://www.googleapis.com/auth/experimentsandconfigs"}

type antigravityDriver struct{}

func (antigravityDriver) Start(_ context.Context, s *Service, f flow) (flow, StartResult, error) {
	if !s.OAuthAvailable("antigravity") {
		return flow{}, StartResult{}, ErrProviderNotConfigured
	}
	f.flowType = "authorization_code"
	f.redirectURI = "http://localhost:51121/oauth-callback"
	q := url.Values{"response_type": {"code"}, "client_id": {s.config.AntigravityClientID}, "redirect_uri": {f.redirectURI}, "scope": {strings.Join(antigravityScopes, " ")}, "state": {f.state}, "access_type": {"offline"}, "prompt": {"consent"}, "code_challenge": {pkceChallenge(f.verifier)}, "code_challenge_method": {"S256"}}
	return f, StartResult{AuthorizeURL: "https://accounts.google.com/o/oauth2/v2/auth?" + q.Encode(), Instructions: "Open Google authorization, then paste the complete localhost callback URL here."}, nil
}
func (antigravityDriver) Complete(ctx context.Context, s *Service, f flow, callback string) (tokenResponse, error) {
	code, state, err := parseCallback(callback)
	if err != nil || state != f.state {
		return tokenResponse{}, ErrBadCallback
	}
	t, err := exchangeAntigravityForm(ctx, s, url.Values{"grant_type": {"authorization_code"}, "client_id": {s.config.AntigravityClientID}, "client_secret": {s.config.AntigravityClientSecret}, "code": {code}, "redirect_uri": {f.redirectURI}, "code_verifier": {f.verifier}})
	if err != nil {
		return t, err
	}
	t.Metadata = s.antigravityMetadata(ctx, t.AccessToken)
	return t, nil
}
func exchangeAntigravityForm(ctx context.Context, s *Service, form url.Values) (tokenResponse, error) {
	var payload map[string]any
	status, err := requestJSON(ctx, s.client, http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()), map[string]string{
		"Content-Type": "application/x-www-form-urlencoded", "Accept": "application/json", "User-Agent": antigravityOAuthUserAgent,
	}, &payload)
	if err != nil {
		return tokenResponse{}, err
	}
	if status < 200 || status >= 300 {
		return tokenResponse{}, fmt.Errorf("OAuth token endpoint returned HTTP %d", status)
	}
	return tokenFromPayload(payload), nil
}

func (s *Service) antigravityMetadata(ctx context.Context, token string) entities.OAuthMetadata {
	metadataCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	metadata := entities.OAuthMetadata{}
	request := func(method, endpoint string, body io.Reader, headers map[string]string, target any) bool {
		req, err := http.NewRequestWithContext(metadataCtx, method, endpoint, body)
		if err != nil {
			return false
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		resp, err := s.client.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode >= 200 && resp.StatusCode < 300 && json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(target) == nil
	}
	var user struct {
		Email string `json:"email"`
	}
	if request(http.MethodGet, "https://www.googleapis.com/oauth2/v1/userinfo?alt=json", nil, nil, &user) {
		metadata.Email = user.Email
	}
	headers := map[string]string{"Content-Type": "application/json", "User-Agent": antigravityOAuthUserAgent, "X-Goog-Api-Client": antigravityAPIClient}
	payload := `{"metadata":{"ideType":"ANTIGRAVITY"}}`
	var assist struct {
		Project json.RawMessage `json:"cloudaicompanionProject"`
	}
	if request(http.MethodPost, "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist", strings.NewReader(payload), headers, &assist) {
		metadata.ProjectID = antigravityProjectID(assist.Project)
	}
	if metadata.ProjectID == "" {
		// Match the official client: perform one bounded onboarding attempt, then
		// retry project discovery. Request-time calls can retry later if Google is
		// temporarily unavailable.
		var onboard json.RawMessage
		_ = request(http.MethodPost, "https://cloudcode-pa.googleapis.com/v1internal:onboardUser", strings.NewReader(`{"tier_id":"legacy-tier","metadata":{"ideType":"ANTIGRAVITY"}}`), headers, &onboard)
		var retry struct {
			Project json.RawMessage `json:"cloudaicompanionProject"`
		}
		if request(http.MethodPost, "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist", strings.NewReader(payload), headers, &retry) {
			metadata.ProjectID = antigravityProjectID(retry.Project)
		}
	}
	return metadata
}

func antigravityProjectID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return strings.TrimSpace(value)
	}
	var project struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &project)
	return strings.TrimSpace(project.ID)
}
