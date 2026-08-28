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

func TestClaudeCodeAdapterUsesSubscriptionMessagesContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || r.URL.RawQuery != "beta=true" {
			t.Fatalf("Claude Code endpoint = %s", r.URL.RequestURI())
		}
		wantBetas := strings.Join([]string{claudeCodeBeta, anthropicOAuthBeta, claudeInterleavedThinking, claudeContextManagementBeta, claudeTokenCountingBeta}, ",")
		if got := r.Header.Get("anthropic-beta"); got != wantBetas {
			t.Fatalf("anthropic-beta = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer subscription-token" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-Claude-Code-Session-Id"); got == "" {
			t.Fatal("Claude Code session header is missing")
		}
		var request AnthropicRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if len(request.System) < 2 || !strings.HasPrefix(request.System[0].Text, "x-anthropic-billing-header:") || !strings.HasPrefix(request.System[1].Text, "You are Claude Code") {
			t.Fatalf("Claude Code system identity = %+v", request.System)
		}
		var identity struct {
			SessionID string `json:"session_id"`
		}
		if request.Metadata == nil || json.Unmarshal([]byte(request.Metadata.UserID), &identity) != nil || identity.SessionID != r.Header.Get("X-Claude-Code-Session-Id") {
			t.Fatalf("Claude Code session identity = %+v metadata=%+v", identity, request.Metadata)
		}
		_, _ = io.WriteString(w, `{"id":"msg_1","content":[],"usage":{}}`)
	}))
	defer server.Close()

	adapter := &ClaudeCodeAdapter{AnthropicAdapter: &AnthropicAdapter{HTTP: server.Client()}}
	runtime := &entities.CredentialRuntime{Kind: entities.KindOAuth, Provider: "claude", BaseURL: server.URL, OAuthAccess: "subscription-token", OAuthMeta: entities.OAuthMetadata{AccountID: "account-id", DeviceID: "device-id"}}
	result, err := adapter.Send(context.Background(), runtime, "claude-opus-4-7", []byte(`{"model":"public","messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	if result.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", result.StatusCode)
	}
}

func TestAnthropicOAuthAdapterDoesNotClaimClaudeCodeContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Fatalf("Anthropic API-key endpoint query = %q", r.URL.RawQuery)
		}
		if got := r.Header.Get("anthropic-beta"); got != anthropicOAuthBeta {
			t.Fatalf("anthropic-beta = %q", got)
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	adapter := &AnthropicAdapter{HTTP: server.Client()}
	runtime := &entities.CredentialRuntime{Kind: entities.KindOAuth, Provider: entities.ProviderAnthropic, BaseURL: server.URL, OAuthAccess: "oauth-token"}
	result, err := adapter.Send(context.Background(), runtime, "claude-test", []byte(`{"messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	result.Body.Close()
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

func TestOpenAIAdapterInjectsStablePromptCacheKeyOnlyForOpenAI(t *testing.T) {
	var keys []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, request.PromptCacheKey)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	adapter := &OpenAIAdapter{HTTP: server.Client()}
	raw := []byte(`{"model":"public","messages":[{"role":"developer","content":"stable instructions"},{"role":"user","content":"question"}]}`)
	for _, provider := range []string{"openai", "opencode-go", entities.ProviderOpenAICompatible} {
		result, err := adapter.Send(context.Background(), &entities.CredentialRuntime{Kind: entities.KindAPIKey, Provider: provider, BaseURL: server.URL, APIKey: "secret"}, "upstream", raw)
		if err != nil {
			t.Fatal(err)
		}
		result.Body.Close()
	}
	if len(keys) != 3 || keys[0] == "" || keys[1] == "" || keys[2] != "" {
		t.Fatalf("prompt cache keys = %#v", keys)
	}
}

func TestClaudeConnectivityProbeUsesDiscoveredModel(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/v1/models":
			_, _ = io.WriteString(w, `{"data":[{"id":"claude-current-from-account"}]}`)
		case "/v1/messages":
			var request AnthropicRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			if request.Model != "claude-current-from-account" {
				t.Fatalf("probe model = %q", request.Model)
			}
			_, _ = io.WriteString(w, `{}`)
		default:
			t.Fatalf("unexpected probe endpoint %s", r.URL.Path)
		}
	}))
	defer server.Close()

	adapter := &ClaudeCodeAdapter{AnthropicAdapter: &AnthropicAdapter{HTTP: server.Client()}}
	status, err := adapter.Probe(context.Background(), &entities.CredentialRuntime{Kind: entities.KindOAuth, Provider: "claude", BaseURL: server.URL, OAuthAccess: "subscription-token"})
	if err != nil || status != http.StatusOK {
		t.Fatalf("status=%d err=%v", status, err)
	}
	if len(paths) != 2 || paths[0] != "/v1/models" || paths[1] != "/v1/messages" {
		t.Fatalf("probe paths = %v", paths)
	}
}
