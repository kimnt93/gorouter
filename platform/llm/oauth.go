package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/kimnt93/gorouter/pkg/entities"
)

const defaultAnthropicOAuthTokenURL = "https://console.anthropic.com/v1/oauth/token"

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

type oauthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type,omitempty"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
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
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {cr.OAuthRefreh},
	}
	if r.ClientID != "" {
		form.Set("client_id", r.ClientID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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
