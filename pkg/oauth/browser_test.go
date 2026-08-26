package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
)

type oauthRepoStub struct{ created entities.CredentialInput }

func (r *oauthRepoStub) Create(_ context.Context, input entities.CredentialInput, _ entities.SecretBox) (*entities.Credential, error) {
	r.created = input
	return &entities.Credential{ID: "cred-oauth", Name: input.Name, Provider: input.Provider, Kind: input.Kind, BaseURL: input.BaseURL}, nil
}
func (*oauthRepoStub) List(context.Context) ([]entities.Credential, error) { return nil, nil }
func (*oauthRepoStub) Update(context.Context, entities.SecretBox, string, entities.CredentialUpdate) (*entities.Credential, error) {
	return nil, entities.ErrNotFound
}
func (*oauthRepoStub) Delete(context.Context, string) error { return nil }
func (*oauthRepoStub) Runtime(context.Context, entities.SecretBox, string) (*entities.CredentialRuntime, error) {
	return nil, entities.ErrNotFound
}
func (*oauthRepoStub) UpdateOAuthTokens(context.Context, entities.SecretBox, string, string, string) error {
	return nil
}
func (*oauthRepoStub) RoutesForModel(context.Context, string) ([]entities.RouteCandidate, error) {
	return nil, nil
}

type oauthBoxStub struct{}

func (oauthBoxStub) Seal(value []byte) ([]byte, error) { return value, nil }
func (oauthBoxStub) Open(value []byte) ([]byte, error) { return value, nil }

func jwtWithAccount(account string) string {
	payload, _ := json.Marshal(map[string]any{"https://api.openai.com/auth": map[string]string{"chatgpt_account_id": account}})
	return "e30." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

func jwtWithCodexAuth(auth map[string]any) string {
	payload, _ := json.Marshal(map[string]any{"https://api.openai.com/auth": auth})
	return "e30." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

func TestCodexBrowserFlowUsesPKCEAndPersistsAccountMetadata(t *testing.T) {
	repo := &oauthRepoStub{}
	var exchanged url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Fatalf("content type = %q", r.Header.Get("Content-Type"))
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		exchanged = r.PostForm
		_ = json.NewEncoder(w).Encode(tokenResponse{AccessToken: "access-secret", RefreshToken: "refresh-secret", IDToken: jwtWithAccount("acct-123")})
	}))
	defer server.Close()
	service := New(server.Client(), credential.NewService(repo, oauthBoxStub{}), Config{CodexClientID: "client-id", CodexTokenURL: server.URL})
	start, err := service.Start("codex", "master::")
	if err != nil {
		t.Fatal(err)
	}
	authURL, _ := url.Parse(start.AuthorizeURL)
	query := authURL.Query()
	if query.Get("state") != start.FlowID || query.Get("code_challenge") == "" || query.Get("code_challenge_method") != "S256" || query.Get("prompt") != "login" {
		t.Fatalf("authorization query = %v", query)
	}
	created, err := service.Complete(context.Background(), CompleteInput{Provider: "codex", FlowID: start.FlowID, Callback: "http://localhost:1455/auth/callback?code=returned-code&state=" + url.QueryEscape(start.FlowID), SessionBinding: "master::"})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "cred-oauth" || repo.created.OAuthAccess != "access-secret" || repo.created.OAuthRefresh != "refresh-secret" || repo.created.OAuthAccount != "acct-123" || repo.created.OAuthIDToken == "" {
		t.Fatalf("created credential = %+v input=%+v", created, repo.created)
	}
	if exchanged.Get("code") != "returned-code" || exchanged.Get("code_verifier") == "" || exchanged.Get("client_id") != "client-id" {
		t.Fatalf("token form = %v", exchanged)
	}
	if _, err := service.Complete(context.Background(), CompleteInput{Provider: "codex", FlowID: start.FlowID, Callback: "http://localhost:1455/auth/callback?code=x&state=" + start.FlowID, SessionBinding: "master::"}); !errors.Is(err, ErrInvalidFlow) {
		t.Fatalf("single-use flow error = %v", err)
	}
}

func TestClaudeFlowValidatesSessionStateAndExpiry(t *testing.T) {
	repo := &oauthRepoStub{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["state"] == "" || body["code_verifier"] == "" || body["code"] != "claude-code" {
			t.Fatalf("body = %v", body)
		}
		_ = json.NewEncoder(w).Encode(tokenResponse{AccessToken: "claude-access", RefreshToken: "claude-refresh"})
	}))
	defer server.Close()
	service := New(server.Client(), credential.NewService(repo, oauthBoxStub{}), Config{ClaudeTokenURL: server.URL, FlowTTL: time.Minute})
	start, err := service.Start("claude", "apikey:key-1:tenant-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Complete(context.Background(), CompleteInput{Provider: "claude", FlowID: start.FlowID, Callback: "claude-code#" + start.FlowID, SessionBinding: "wrong"}); !errors.Is(err, ErrInvalidFlow) {
		t.Fatalf("session mismatch error = %v", err)
	}
	start, _ = service.Start("claude", "apikey:key-1:tenant-1")
	service.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	if _, err := service.Complete(context.Background(), CompleteInput{Provider: "claude", FlowID: start.FlowID, Callback: "claude-code#" + start.FlowID, SessionBinding: "apikey:key-1:tenant-1"}); !errors.Is(err, ErrInvalidFlow) {
		t.Fatalf("expiry error = %v", err)
	}
}

func TestParseCallbackRejectsMissingState(t *testing.T) {
	if _, _, err := parseCallback(strings.Repeat("a", 20)); !errors.Is(err, ErrBadCallback) {
		t.Fatalf("error = %v", err)
	}
}

func TestCodexAccountIDSelectsTeamWorkspaceForFreePlan(t *testing.T) {
	token := jwtWithCodexAuth(map[string]any{
		"chatgpt_account_id": "personal-account",
		"chatgpt_plan_type":  "free",
		"organizations": []map[string]any{
			{"id": "personal-account", "is_default": true, "role": "owner", "title": "Personal"},
			{"id": "team-account", "is_default": false, "role": "member", "title": "Engineering workspace"},
		},
	})
	if got := codexAccountID(token); got != "team-account" {
		t.Fatalf("Codex workspace = %q", got)
	}

	teamToken := jwtWithCodexAuth(map[string]any{
		"chatgpt_account_id": "selected-team", "chatgpt_plan_type": "team",
		"organizations": []map[string]any{{"id": "other-team", "role": "member", "title": "Other workspace"}},
	})
	if got := codexAccountID(teamToken); got != "selected-team" {
		t.Fatalf("selected Codex team workspace = %q", got)
	}
}

func TestCodexBrowserFlowRejectsMissingAccountBinding(t *testing.T) {
	repo := &oauthRepoStub{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(tokenResponse{AccessToken: "access", RefreshToken: "refresh", IDToken: "invalid"})
	}))
	defer server.Close()
	service := New(server.Client(), credential.NewService(repo, oauthBoxStub{}), Config{CodexTokenURL: server.URL})
	start, err := service.Start("codex", "master::")
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Complete(context.Background(), CompleteInput{Provider: "codex", FlowID: start.FlowID, Callback: "code#" + start.FlowID, SessionBinding: "master::"})
	if err == nil || !strings.Contains(err.Error(), "account binding") {
		t.Fatalf("missing account binding error = %v", err)
	}
	if repo.created.Provider != "" {
		t.Fatalf("credential persisted without account binding: %+v", repo.created)
	}
}
