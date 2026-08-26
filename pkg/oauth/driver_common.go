package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type deviceCodeResponse struct {
	DeviceCode                   string `json:"device_code"`
	UserCode                     string `json:"user_code"`
	VerificationURI              string `json:"verification_uri"`
	VerificationURIComplete      string `json:"verification_uri_complete"`
	DeviceCodeCamel              string `json:"deviceCode"`
	UserCodeCamel                string `json:"userCode"`
	VerificationURICamel         string `json:"verificationUri"`
	VerificationURICompleteCamel string `json:"verificationUriComplete"`
	Code                         string `json:"code"`
	VerificationURL              string `json:"verificationUrl"`
	ExpiresIn                    int    `json:"expires_in"`
	ExpiresInCamel               int    `json:"expiresIn"`
	Interval                     int    `json:"interval"`
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func requestJSON(ctx context.Context, client *http.Client, method, endpoint string, body io.Reader, headers map[string]string, target any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return 0, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if target != nil {
		if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(target); err != nil {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}

func tokenFromPayload(payload map[string]any) tokenResponse {
	t := tokenResponse{AccessToken: stringValue(payload, "access_token", "accessToken"), RefreshToken: stringValue(payload, "refresh_token", "refreshToken"), IDToken: stringValue(payload, "id_token", "idToken")}
	if nested, ok := payload["data"].(map[string]any); ok {
		if t.AccessToken == "" {
			t.AccessToken = stringValue(nested, "access_token", "accessToken")
		}
		if t.RefreshToken == "" {
			t.RefreshToken = stringValue(nested, "refresh_token", "refreshToken")
		}
	}
	for _, key := range []string{"expires_in", "expiresIn"} {
		if value, ok := payload[key].(float64); ok {
			t.ExpiresIn = int64(value)
		}
	}
	return t
}

func exchangeForm(ctx context.Context, s *Service, endpoint string, form url.Values) (tokenResponse, error) {
	var payload map[string]any
	status, err := requestJSON(ctx, s.client, http.MethodPost, endpoint, strings.NewReader(form.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Accept": "application/json"}, &payload)
	if err != nil {
		return tokenResponse{}, fmt.Errorf("OAuth token request: %w", err)
	}
	if status < 200 || status >= 300 {
		return tokenResponse{}, fmt.Errorf("OAuth token endpoint returned HTTP %d", status)
	}
	return tokenFromPayload(payload), nil
}

func deviceStartResult(f flow, d deviceCodeResponse) (flow, StartResult, error) {
	if d.ExpiresIn == 0 {
		d.ExpiresIn = d.ExpiresInCamel
	}
	if d.DeviceCode == "" {
		d.DeviceCode = d.DeviceCodeCamel
	}
	if d.UserCode == "" {
		d.UserCode = d.UserCodeCamel
	}
	if d.VerificationURI == "" {
		d.VerificationURI = d.VerificationURICamel
	}
	if d.VerificationURIComplete == "" {
		d.VerificationURIComplete = d.VerificationURICompleteCamel
	}
	if d.DeviceCode == "" {
		return f, StartResult{}, fmt.Errorf("OAuth device authorization omitted device_code")
	}
	f.flowType = "device_code"
	f.deviceCode = d.DeviceCode
	f.interval = d.Interval
	if f.interval <= 0 {
		f.interval = 5
	}
	if d.ExpiresIn > 0 && time.Duration(d.ExpiresIn)*time.Second < time.Until(f.expires) {
		f.expires = time.Now().Add(time.Duration(d.ExpiresIn) * time.Second)
	}
	authorize := d.VerificationURIComplete
	if authorize == "" {
		authorize = d.VerificationURI
	}
	return f, StartResult{FlowID: f.state, FlowType: f.flowType, AuthorizeURL: authorize, VerificationURI: d.VerificationURI, VerificationURIComplete: d.VerificationURIComplete, UserCode: d.UserCode, Interval: f.interval, ExpiresIn: d.ExpiresIn, Instructions: "Open the verification page and enter the device code. This dialog will poll securely until authorization completes."}, nil
}

func pollError(status int, payload map[string]any) error {
	code := stringValue(payload, "error")
	if code == "authorization_pending" || code == "slow_down" || status == http.StatusAccepted || status == http.StatusNotFound {
		return ErrAuthorizationPending
	}
	if code == "access_denied" || status == http.StatusForbidden {
		return ErrAccessDenied
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("OAuth token poll returned HTTP %d", status)
	}
	return nil
}
func stringValue(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := payload[key].(string); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
