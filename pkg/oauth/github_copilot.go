package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kimnt93/gorouter/pkg/entities"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type githubCopilotDriver struct{}

func (githubCopilotDriver) Start(ctx context.Context, s *Service, f flow) (flow, StartResult, error) {
	form := url.Values{"client_id": {s.config.GitHubClientID}, "scope": {"read:user"}}
	var d deviceCodeResponse
	status, err := requestJSON(ctx, s.client, http.MethodPost, "https://github.com/login/device/code", strings.NewReader(form.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Accept": "application/json"}, &d)
	if err != nil {
		return f, StartResult{}, err
	}
	if status < 200 || status >= 300 {
		return f, StartResult{}, fmt.Errorf("GitHub device authorization returned HTTP %d", status)
	}
	return deviceStartResult(f, d)
}
func (githubCopilotDriver) Complete(ctx context.Context, s *Service, f flow, _ string) (tokenResponse, error) {
	form := url.Values{"client_id": {s.config.GitHubClientID}, "device_code": {f.deviceCode}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}}
	var p map[string]any
	status, err := requestJSON(ctx, s.client, http.MethodPost, "https://github.com/login/oauth/access_token", strings.NewReader(form.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Accept": "application/json"}, &p)
	if err != nil {
		return tokenResponse{}, err
	}
	if err := pollError(status, p); err != nil {
		return tokenResponse{}, err
	}
	t := tokenFromPayload(p)
	copilot, m := s.githubCopilotMetadata(ctx, t.AccessToken)
	m.CopilotToken = copilot
	t.Metadata = m
	return t, nil
}
func (s *Service) githubCopilotMetadata(ctx context.Context, token string) (string, entities.OAuthMetadata) {
	m := entities.OAuthMetadata{}
	headers := map[string]string{"Authorization": "Bearer " + token, "Accept": "application/json", "X-GitHub-Api-Version": "2022-11-28", "User-Agent": "GitHubCopilotChat/0.26.7"}
	fetch := func(endpoint string, target any) bool {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := s.client.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode >= 200 && resp.StatusCode < 300 && json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(target) == nil
	}
	var c struct {
		Token     string `json:"token"`
		ExpiresAt any    `json:"expires_at"`
	}
	_ = fetch("https://api.github.com/copilot_internal/v2/token", &c)
	var u struct {
		Login string `json:"login"`
		Email string `json:"email"`
	}
	if fetch("https://api.github.com/user", &u) {
		m.Login = u.Login
		m.Email = u.Email
	}
	m.TokenExpiresAt = fmt.Sprint(c.ExpiresAt)
	return c.Token, m
}
