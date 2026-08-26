package oauth

import (
	"context"
	"fmt"
	"github.com/kimnt93/gorouter/pkg/entities"
	"net/http"
	"net/url"
	"strings"
)

type kiloCodeDriver struct{}

func (kiloCodeDriver) Start(ctx context.Context, s *Service, f flow) (flow, StartResult, error) {
	var d deviceCodeResponse
	status, err := requestJSON(ctx, s.client, http.MethodPost, "https://api.kilo.ai/api/device-auth/codes", strings.NewReader("{}"), map[string]string{"Content-Type": "application/json", "Accept": "application/json"}, &d)
	if err != nil {
		return f, StartResult{}, err
	}
	if status < 200 || status >= 300 {
		return f, StartResult{}, fmt.Errorf("Kilo device authorization returned HTTP %d", status)
	}
	d.DeviceCode = d.Code
	d.UserCode = d.Code
	d.VerificationURI = d.VerificationURL
	d.VerificationURIComplete = d.VerificationURL
	if d.ExpiresIn == 0 {
		d.ExpiresIn = 300
	}
	if d.Interval == 0 {
		d.Interval = 3
	}
	return deviceStartResult(f, d)
}
func (kiloCodeDriver) Complete(ctx context.Context, s *Service, f flow, _ string) (tokenResponse, error) {
	var p map[string]any
	status, err := requestJSON(ctx, s.client, http.MethodGet, "https://api.kilo.ai/api/device-auth/codes/"+url.PathEscape(f.deviceCode), nil, map[string]string{"Accept": "application/json"}, &p)
	if err != nil {
		return tokenResponse{}, err
	}
	if err := pollError(status, p); err != nil {
		return tokenResponse{}, err
	}
	if stringValue(p, "status") != "approved" {
		return tokenResponse{}, ErrAuthorizationPending
	}
	t := tokenResponse{AccessToken: stringValue(p, "token")}
	t.Metadata = entities.OAuthMetadata{Email: stringValue(p, "userEmail")}
	return t, nil
}
