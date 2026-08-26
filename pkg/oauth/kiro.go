package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kimnt93/gorouter/pkg/entities"
	"net/http"
	"strings"
)

type kiroDriver struct{}

func (kiroDriver) Start(ctx context.Context, s *Service, f flow) (flow, StartResult, error) {
	return startAWSBuilderID(ctx, s, f)
}
func (kiroDriver) Complete(ctx context.Context, s *Service, f flow, _ string) (tokenResponse, error) {
	return completeAWSBuilderID(ctx, s, f)
}
func startAWSBuilderID(ctx context.Context, s *Service, f flow) (flow, StartResult, error) {
	registration, _ := json.Marshal(map[string]any{"clientName": "kiro-oauth-client", "clientType": "public", "scopes": []string{"codewhisperer:completions", "codewhisperer:analysis", "codewhisperer:conversations"}, "grantTypes": []string{"urn:ietf:params:oauth:grant-type:device_code", "refresh_token"}, "issuerUrl": "https://identitycenter.amazonaws.com/ssoins-722374e8c3c8e6c6"})
	var client struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}
	status, err := requestJSON(ctx, s.client, http.MethodPost, "https://oidc.us-east-1.amazonaws.com/client/register", strings.NewReader(string(registration)), map[string]string{"Content-Type": "application/json", "Accept": "application/json"}, &client)
	if err != nil {
		return f, StartResult{}, err
	}
	if status < 200 || status >= 300 || client.ClientID == "" || client.ClientSecret == "" {
		return f, StartResult{}, fmt.Errorf("AWS client registration failed with HTTP %d", status)
	}
	f.extra["client_id"] = client.ClientID
	f.extra["client_secret"] = client.ClientSecret
	f.extra["region"] = "us-east-1"
	payload, _ := json.Marshal(map[string]string{"clientId": client.ClientID, "clientSecret": client.ClientSecret, "startUrl": "https://view.awsapps.com/start"})
	var d deviceCodeResponse
	status, err = requestJSON(ctx, s.client, http.MethodPost, "https://oidc.us-east-1.amazonaws.com/device_authorization", strings.NewReader(string(payload)), map[string]string{"Content-Type": "application/json", "Accept": "application/json"}, &d)
	if err != nil {
		return f, StartResult{}, err
	}
	if status < 200 || status >= 300 {
		return f, StartResult{}, fmt.Errorf("AWS device authorization returned HTTP %d", status)
	}
	return deviceStartResult(f, d)
}
func completeAWSBuilderID(ctx context.Context, s *Service, f flow) (tokenResponse, error) {
	payload, _ := json.Marshal(map[string]string{"clientId": f.extra["client_id"], "clientSecret": f.extra["client_secret"], "deviceCode": f.deviceCode, "grantType": "urn:ietf:params:oauth:grant-type:device_code"})
	var p map[string]any
	status, err := requestJSON(ctx, s.client, http.MethodPost, "https://oidc."+f.extra["region"]+".amazonaws.com/token", strings.NewReader(string(payload)), map[string]string{"Content-Type": "application/json", "Accept": "application/json"}, &p)
	if err != nil {
		return tokenResponse{}, err
	}
	if err := pollError(status, p); err != nil {
		return tokenResponse{}, err
	}
	t := tokenFromPayload(p)
	t.Metadata = entities.OAuthMetadata{ClientID: f.extra["client_id"], ClientSecret: f.extra["client_secret"], Region: f.extra["region"], AuthMethod: "builder-id"}
	return t, nil
}
