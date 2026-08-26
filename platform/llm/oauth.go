package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

const defaultAnthropicOAuthTokenURL = "https://api.anthropic.com/v1/oauth/token"
const defaultCodexOAuthTokenURL = "https://auth.openai.com/oauth/token"

type OAuthTokenPersister interface {
	UpdateOAuthTokens(ctx context.Context, id, access, refresh string) error
}

// AnthropicOAuthRefresher owns the provider-specific refresh exchange. TokenURL
// is injectable for tests and self-hosted compatible services.
type AnthropicOAuthRefresher struct {
	HTTP      *http.Client
	TokenURL  string
	ClientID  string
	Persister OAuthTokenPersister
}

type CodexOAuthRefresher struct {
	HTTP      *http.Client
	TokenURL  string
	ClientID  string
	Persister OAuthTokenPersister
}

type oauthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type,omitempty"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
}

type oauthTokenRequest struct {
	GrantType    string `json:"grant_type"`
	RefreshToken string `json:"refresh_token"`
	ClientID     string `json:"client_id"`
}

func refreshOAuthForm(ctx context.Context, client *http.Client, persister OAuthTokenPersister, cr *entities.CredentialRuntime, endpoint, clientID, clientSecret string, extra url.Values) error {
	if cr == nil || cr.Kind != entities.KindOAuth || strings.TrimSpace(cr.OAuthRefreh) == "" {
		return errors.New("oauth refresh token is unavailable")
	}
	if client == nil {
		client = NewHTTPClient()
	}
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {cr.OAuthRefreh}}
	if clientID != "" {
		form.Set("client_id", clientID)
	}
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}
	for key, values := range extra {
		for _, value := range values {
			form.Add(key, value)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("oauth token request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("oauth token endpoint returned HTTP %d", resp.StatusCode)
	}
	var token oauthTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return err
	}
	if token.AccessToken == "" {
		return errors.New("oauth token response omitted access_token")
	}
	if token.RefreshToken == "" {
		token.RefreshToken = cr.OAuthRefreh
	}
	if persister == nil {
		return errors.New("oauth token persister is not configured")
	}
	if err := persister.UpdateOAuthTokens(ctx, cr.ID, token.AccessToken, token.RefreshToken); err != nil {
		return err
	}
	cr.OAuthAccess, cr.OAuthRefreh = token.AccessToken, token.RefreshToken
	return nil
}

func refreshOAuthJSON(ctx context.Context, client *http.Client, persister OAuthTokenPersister, cr *entities.CredentialRuntime, endpoint string, payload any, headers map[string]string) error {
	if cr == nil || cr.Kind != entities.KindOAuth || strings.TrimSpace(cr.OAuthRefreh) == "" {
		return errors.New("oauth refresh token is unavailable")
	}
	if client == nil {
		client = NewHTTPClient()
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(encoded)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("oauth token endpoint returned HTTP %d", resp.StatusCode)
	}
	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return err
	}
	if nested, ok := raw["data"].(map[string]any); ok {
		raw = nested
	}
	access := oauthString(raw, "access_token", "accessToken")
	refresh := oauthString(raw, "refresh_token", "refreshToken")
	if access == "" {
		return errors.New("oauth token response omitted access token")
	}
	if refresh == "" {
		refresh = cr.OAuthRefreh
	}
	if persister == nil {
		return errors.New("oauth token persister is not configured")
	}
	if err := persister.UpdateOAuthTokens(ctx, cr.ID, access, refresh); err != nil {
		return err
	}
	cr.OAuthAccess, cr.OAuthRefreh = access, refresh
	return nil
}
func oauthString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
func oauthTokenNeedsRefresh(cr *entities.CredentialRuntime) bool {
	if cr == nil || cr.OAuthRefreh == "" || cr.OAuthMeta.TokenExpiresAt == "" {
		return false
	}
	expiry, err := time.Parse(time.RFC3339, cr.OAuthMeta.TokenExpiresAt)
	return err == nil && !expiry.After(time.Now().Add(2*time.Minute))
}

type oauthRefreshAttemptKey struct{}

func canRetryOAuth(ctx context.Context) bool { return ctx.Value(oauthRefreshAttemptKey{}) == nil }
func markOAuthRetry(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauthRefreshAttemptKey{}, true)
}

func (r *AnthropicOAuthRefresher) Refresh(ctx context.Context, cr *entities.CredentialRuntime) error {
	if cr == nil || cr.Kind != entities.KindOAuth || strings.TrimSpace(cr.OAuthRefreh) == "" {
		return errors.New("oauth refresh token is unavailable")
	}
	client := r.HTTP
	if client == nil {
		client = NewHTTPClient()
	}
	endpoint := r.TokenURL
	if endpoint == "" {
		endpoint = defaultAnthropicOAuthTokenURL
	}
	payload, err := json.Marshal(oauthTokenRequest{GrantType: "refresh_token", RefreshToken: cr.OAuthRefreh, ClientID: r.ClientID})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("oauth token request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("oauth token endpoint returned HTTP %d", resp.StatusCode)
	}
	var token oauthTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return fmt.Errorf("decode oauth token response: %w", err)
	}
	if token.AccessToken == "" {
		return errors.New("oauth token response omitted access_token")
	}
	if token.RefreshToken == "" {
		token.RefreshToken = cr.OAuthRefreh
	}
	if r.Persister == nil {
		return errors.New("oauth token persister is not configured")
	}
	if err := r.Persister.UpdateOAuthTokens(ctx, cr.ID, token.AccessToken, token.RefreshToken); err != nil {
		return fmt.Errorf("persist refreshed oauth tokens: %w", err)
	}
	cr.OAuthAccess = token.AccessToken
	cr.OAuthRefreh = token.RefreshToken
	return nil
}

func (r *CodexOAuthRefresher) Refresh(ctx context.Context, cr *entities.CredentialRuntime) error {
	if cr == nil || cr.Kind != entities.KindOAuth || strings.TrimSpace(cr.OAuthRefreh) == "" {
		return errors.New("oauth refresh token is unavailable")
	}
	client := r.HTTP
	if client == nil {
		client = NewHTTPClient()
	}
	endpoint := r.TokenURL
	if endpoint == "" {
		endpoint = defaultCodexOAuthTokenURL
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {cr.OAuthRefreh},
		"client_id":     {r.ClientID},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("oauth token request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("oauth token endpoint returned HTTP %d", resp.StatusCode)
	}
	var token oauthTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return fmt.Errorf("decode oauth token response: %w", err)
	}
	if token.AccessToken == "" {
		return errors.New("oauth token response omitted access_token")
	}
	if token.RefreshToken == "" {
		token.RefreshToken = cr.OAuthRefreh
	}
	if r.Persister == nil {
		return errors.New("oauth token persister is not configured")
	}
	if err := r.Persister.UpdateOAuthTokens(ctx, cr.ID, token.AccessToken, token.RefreshToken); err != nil {
		return fmt.Errorf("persist refreshed oauth tokens: %w", err)
	}
	cr.OAuthAccess = token.AccessToken
	cr.OAuthRefreh = token.RefreshToken
	return nil
}
