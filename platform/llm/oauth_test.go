package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kimnt93/gorouter/pkg/entities"
)

type tokenStore struct {
	id      string
	access  string
	refresh string
}

func (s *tokenStore) UpdateOAuthTokens(_ context.Context, id, access, refresh string) error {
	s.id, s.access, s.refresh = id, access, refresh
	return nil
}

func TestAnthropicOAuthRefresherRotatesAndPersists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request oauthTokenRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.GrantType != "refresh_token" || request.RefreshToken != "old-refresh" || request.ClientID != "client" {
			t.Fatalf("unexpected refresh request: %+v", request)
		}
		_ = json.NewEncoder(w).Encode(oauthTokenResponse{AccessToken: "new-access", RefreshToken: "new-refresh", ExpiresIn: 3600})
	}))
	defer server.Close()

	store := &tokenStore{}
	runtime := &entities.CredentialRuntime{ID: "cred-1", Kind: entities.KindOAuth, OAuthRefreh: "old-refresh"}
	refresher := &AnthropicOAuthRefresher{HTTP: server.Client(), TokenURL: server.URL, ClientID: "client", Persister: store}
	if err := refresher.Refresh(context.Background(), runtime); err != nil {
		t.Fatal(err)
	}
	if runtime.OAuthAccess != "new-access" || runtime.OAuthRefreh != "new-refresh" {
		t.Fatalf("runtime not rotated: %+v", runtime)
	}
	if store.id != runtime.ID || store.access != "new-access" || store.refresh != "new-refresh" {
		t.Fatalf("tokens not persisted: %+v", store)
	}
}

func TestAnthropicAdapterRefreshesOnceOnUnauthorized(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Authorization") != "Bearer fresh" {
			t.Fatalf("refreshed bearer missing: %q", r.Header.Get("Authorization"))
		}
		_, _ = io.WriteString(w, `{"id":"msg_1","content":[],"usage":{}}`)
	}))
	defer server.Close()

	adapter := &AnthropicAdapter{HTTP: server.Client(), Refresh: func(_ context.Context, runtime *entities.CredentialRuntime) error {
		runtime.OAuthAccess = "fresh"
		return nil
	}}
	runtime := &entities.CredentialRuntime{Kind: entities.KindOAuth, Provider: entities.ProviderAnthropic, BaseURL: server.URL, OAuthAccess: "expired", OAuthRefreh: "refresh"}
	raw := []byte(`{"model":"claude","messages":[{"role":"user","content":"hi"}]}`)
	result, err := adapter.Send(context.Background(), runtime, "claude", raw)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	if result.StatusCode != http.StatusOK || calls != 2 {
		t.Fatalf("status=%d calls=%d", result.StatusCode, calls)
	}
}

func TestOpenAIAdapterUsesTypedForwardPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Fatalf("wrong path: %s", r.URL.Path)
		}
		var request ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Model != "upstream-model" || request.StreamOptions == nil || !request.StreamOptions.IncludeUsage {
			t.Fatalf("forward request not rewritten: %+v", request)
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	adapter := &OpenAIAdapter{HTTP: server.Client()}
	runtime := &entities.CredentialRuntime{Kind: entities.KindAPIKey, Provider: entities.ProviderOpenAICompatible, BaseURL: server.URL, APIKey: "secret"}
	raw := []byte(`{"model":"public-model","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	result, err := adapter.Send(context.Background(), runtime, "upstream-model", raw)
	if err != nil {
		t.Fatal(err)
	}
	result.Body.Close()
}
