package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/kimnt93/gorouter/pkg/entities"
	"net/http"
	"net/url"
	"strings"
)

type clineDriver struct{}

func (clineDriver) Start(_ context.Context, _ *Service, f flow) (flow, StartResult, error) {
	return startCline(f)
}
func (clineDriver) Complete(ctx context.Context, s *Service, f flow, callback string) (tokenResponse, error) {
	return completeCline(ctx, s, f, callback)
}
func startCline(f flow) (flow, StartResult, error) {
	f.flowType = "cline_callback"
	f.redirectURI = "http://localhost:1455/auth/callback"
	q := url.Values{"client_type": {"extension"}, "callback_url": {f.redirectURI}, "redirect_uri": {f.redirectURI}, "state": {f.state}}
	return f, StartResult{AuthorizeURL: "https://api.cline.bot/api/v1/auth/authorize?" + q.Encode(), Instructions: "Open the Cline authorization page, then paste the complete callback URL here."}, nil
}
func completeCline(ctx context.Context, s *Service, f flow, callback string) (tokenResponse, error) {
	code, state, err := clineCallbackCode(callback)
	if err != nil {
		return tokenResponse{}, err
	}
	if state != "" && state != f.state {
		return tokenResponse{}, ErrBadCallback
	}
	if t, ok := decodeClineCode(code); ok {
		return t, nil
	}
	payload, _ := json.Marshal(map[string]string{"grant_type": "authorization_code", "code": code, "client_type": "extension", "redirect_uri": f.redirectURI})
	var p map[string]any
	status, err := requestJSON(ctx, s.client, http.MethodPost, "https://api.cline.bot/api/v1/auth/token", strings.NewReader(string(payload)), map[string]string{"Content-Type": "application/json", "Accept": "application/json"}, &p)
	if err != nil {
		return tokenResponse{}, err
	}
	if status < 200 || status >= 300 {
		return tokenResponse{}, fmt.Errorf("Cline token exchange returned HTTP %d", status)
	}
	t := tokenFromPayload(p)
	t.Metadata.Email = stringValue(p, "email")
	if nested, ok := p["data"].(map[string]any); ok {
		if user, ok := nested["userInfo"].(map[string]any); ok {
			t.Metadata.Email = stringValue(user, "email")
		}
	}
	return t, nil
}
func clineCallbackCode(value string) (string, string, error) {
	value = strings.TrimSpace(value)
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" {
		if code := parsed.Query().Get("code"); code != "" {
			return code, parsed.Query().Get("state"), nil
		}
	}
	if value != "" {
		return value, "", nil
	}
	return "", "", ErrBadCallback
}
func decodeClineCode(code string) (tokenResponse, bool) {
	if decoded, err := url.QueryUnescape(code); err == nil {
		code = decoded
	}
	data, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(code, "="))
	if err != nil {
		data, err = base64.StdEncoding.DecodeString(code)
	}
	if err != nil {
		return tokenResponse{}, false
	}
	if end := strings.LastIndexByte(string(data), '}'); end >= 0 {
		data = data[:end+1]
	}
	var p map[string]any
	if json.Unmarshal(data, &p) != nil {
		return tokenResponse{}, false
	}
	t := tokenResponse{AccessToken: stringValue(p, "accessToken", "access_token"), RefreshToken: stringValue(p, "refreshToken", "refresh_token"), Metadata: entities.OAuthMetadata{Email: stringValue(p, "email")}}
	return t, t.AccessToken != ""
}
