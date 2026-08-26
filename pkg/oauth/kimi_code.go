package oauth

import (
	"context"
	"fmt"
	"github.com/kimnt93/gorouter/pkg/entities"
	"net/http"
	"net/url"
	"strings"
)

type kimiCodeDriver struct{}

func (kimiCodeDriver) Start(ctx context.Context, s *Service, f flow) (flow, StartResult, error) {
	device, err := randomHex(16)
	if err != nil {
		return f, StartResult{}, err
	}
	f.extra["device_id"] = device
	form := url.Values{"client_id": {s.config.KimiClientID}}
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Accept": "application/json"}
	addKimiHeaders(headers, device)
	var d deviceCodeResponse
	status, err := requestJSON(ctx, s.client, http.MethodPost, "https://auth.kimi.com/api/oauth/device_authorization", strings.NewReader(form.Encode()), headers, &d)
	if err != nil {
		return f, StartResult{}, err
	}
	if status < 200 || status >= 300 {
		return f, StartResult{}, fmt.Errorf("Kimi device authorization returned HTTP %d", status)
	}
	return deviceStartResult(f, d)
}
func (kimiCodeDriver) Complete(ctx context.Context, s *Service, f flow, _ string) (tokenResponse, error) {
	form := url.Values{"client_id": {s.config.KimiClientID}, "device_code": {f.deviceCode}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}}
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Accept": "application/json"}
	addKimiHeaders(headers, f.extra["device_id"])
	var p map[string]any
	status, err := requestJSON(ctx, s.client, http.MethodPost, "https://auth.kimi.com/api/oauth/token", strings.NewReader(form.Encode()), headers, &p)
	if err != nil {
		return tokenResponse{}, err
	}
	if err := pollError(status, p); err != nil {
		return tokenResponse{}, err
	}
	t := tokenFromPayload(p)
	t.Metadata = entities.OAuthMetadata{DeviceID: f.extra["device_id"]}
	return t, nil
}
func addKimiHeaders(h map[string]string, device string) {
	h["X-Msh-Platform"] = "kimi_code_cli"
	h["X-Msh-Version"] = "0.26.0"
	h["X-Msh-Device-Name"] = "gorouter"
	h["X-Msh-Device-Model"] = "server"
	h["X-Msh-Os-Version"] = "linux"
	h["X-Msh-Device-Id"] = device
}
