package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/kimnt93/gorouter/pkg/entities"
)

type grokBuildDriver struct{}

func (grokBuildDriver) Start(ctx context.Context, s *Service, f flow) (flow, StartResult, error) {
	form := url.Values{"client_id": {s.config.GrokClientID}, "scope": {"openid profile email offline_access grok-cli:access api:access conversations:read conversations:write workspaces:read workspaces:write"}}
	var d deviceCodeResponse
	status, err := requestJSON(ctx, s.client, http.MethodPost, "https://auth.x.ai/oauth2/device/code", strings.NewReader(form.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Accept": "application/json", "X-Grok-Client-Version": "0.2.106", "X-Grok-Client-Surface": "ui"}, &d)
	if err != nil {
		return f, StartResult{}, err
	}
	if status < 200 || status >= 300 {
		return f, StartResult{}, fmt.Errorf("Grok Build device authorization returned HTTP %d", status)
	}
	return deviceStartResult(f, d)
}
func (grokBuildDriver) Complete(ctx context.Context, s *Service, f flow, _ string) (tokenResponse, error) {
	form := url.Values{"client_id": {s.config.GrokClientID}, "device_code": {f.deviceCode}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}}
	var p map[string]any
	status, err := requestJSON(ctx, s.client, http.MethodPost, "https://auth.x.ai/oauth2/token", strings.NewReader(form.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Accept": "application/json", "X-Grok-Client-Version": "0.2.106", "X-Grok-Client-Surface": "ui"}, &p)
	if err != nil {
		return tokenResponse{}, err
	}
	if err := pollError(status, p); err != nil {
		return tokenResponse{}, err
	}
	t := tokenFromPayload(p)
	t.Metadata = grokBuildMetadata(t.AccessToken, t.IDToken)
	return t, nil
}

func grokBuildMetadata(tokens ...string) entities.OAuthMetadata {
	m := entities.OAuthMetadata{}
	for _, token := range tokens {
		parts := strings.Split(token, ".")
		if len(parts) != 3 {
			continue
		}
		raw, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			continue
		}
		var claims map[string]any
		if json.Unmarshal(raw, &claims) != nil {
			continue
		}
		auth, _ := claims["https://api.x.ai/auth"].(map[string]any)
		if auth == nil {
			auth = claims
		}
		if m.Email == "" {
			m.Email = stringValue(auth, "email")
			if m.Email == "" {
				m.Email = stringValue(claims, "email")
			}
		}
		if m.PrincipalType == "" {
			m.PrincipalType = stringValue(auth, "principal_type")
		}
		if m.PrincipalID == "" {
			m.PrincipalID = stringValue(auth, "principal_id")
		}
		if m.TeamID == "" {
			m.TeamID = stringValue(auth, "team_id")
		}
		if m.OrganizationID == "" {
			m.OrganizationID = stringValue(auth, "organization_id")
		}
		if m.AccountID == "" {
			m.AccountID = stringValue(auth, "user_id")
			if m.AccountID == "" {
				m.AccountID = stringValue(claims, "sub")
			}
		}
	}
	if (strings.EqualFold(m.PrincipalType, "team") || strings.EqualFold(m.PrincipalType, "organization")) && m.PrincipalID != "" {
		m.AccountID = m.PrincipalID
	}
	return m
}
