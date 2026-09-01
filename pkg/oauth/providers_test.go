package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/kimnt93/gorouter/pkg/credential"
)

type rewriteOAuthTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (r rewriteOAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = r.target.Scheme
	clone.URL.Host = r.target.Host
	return r.base.RoundTrip(clone)
}

func oauthTestService(t *testing.T, handler http.Handler) (*Service, *oauthRepoStub) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	client := &http.Client{Transport: rewriteOAuthTransport{target: target, base: http.DefaultTransport}}
	repo := &oauthRepoStub{}
	return New(client, credential.NewService(repo, oauthBoxStub{}), Config{AntigravityClientID: "registered.apps.googleusercontent.com", AntigravityClientSecret: "synthetic-secret"}), repo
}

func TestGitHubCopilotDeviceFlowPollsAndPersistsCopilotToken(t *testing.T) {
	service, repo := oauthTestService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/device/code":
			_ = json.NewEncoder(w).Encode(map[string]any{"device_code": "device", "user_code": "ABCD", "verification_uri": "https://github.com/login/device", "expires_in": 600, "interval": 1})
		case "/login/oauth/access_token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "github-token"})
		case "/copilot_internal/v2/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "copilot-token", "expires_at": 123})
		case "/user":
			_ = json.NewEncoder(w).Encode(map[string]any{"login": "octocat", "email": "octo@example.test"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	start, err := service.StartContext(context.Background(), "github-copilot", "master::")
	if err != nil {
		t.Fatal(err)
	}
	if start.FlowType != "device_code" || start.UserCode != "ABCD" {
		t.Fatalf("start = %+v", start)
	}
	created, err := service.Complete(context.Background(), CompleteInput{Provider: "github-copilot", FlowID: start.FlowID, SessionBinding: "master::"})
	if err != nil {
		t.Fatal(err)
	}
	if created.Provider != "github-copilot" || repo.created.OAuthMeta.CopilotToken != "copilot-token" || repo.created.OAuthMeta.Login != "octocat" {
		t.Fatalf("created input = %+v", repo.created)
	}
}

func TestDeviceFlowPendingRemainsUsable(t *testing.T) {
	polls := 0
	service, _ := oauthTestService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/oauth/device_authorization" {
			_ = json.NewEncoder(w).Encode(map[string]any{"device_code": "d", "user_code": "u", "verification_uri": "https://auth.kimi.com/device", "expires_in": 600})
			return
		}
		polls++
		if polls == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "authorization_pending"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "a", "refresh_token": "r"})
	}))
	start, err := service.StartContext(context.Background(), "kimi-code", "master::")
	if err != nil {
		t.Fatal(err)
	}
	input := CompleteInput{Provider: "kimi-code", FlowID: start.FlowID, SessionBinding: "master::"}
	if _, err = service.Complete(context.Background(), input); err != ErrAuthorizationPending {
		t.Fatalf("pending error = %v", err)
	}
	if _, err = service.Complete(context.Background(), input); err != nil {
		t.Fatal(err)
	}
}

func TestClineEmbeddedCallbackDoesNotRequireReasoningAlias(t *testing.T) {
	service, repo := oauthTestService(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	start, err := service.Start("cline", "master::")
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]string{"accessToken": "access", "refreshToken": "refresh", "email": "cline@example.test"})
	code := url.QueryEscape(strings.TrimRight(base64Std(payload), "="))
	callback := "http://localhost:1455/auth/callback?code=" + code
	_, err = service.Complete(context.Background(), CompleteInput{Provider: "cline", FlowID: start.FlowID, Callback: callback, SessionBinding: "master::"})
	if err != nil {
		t.Fatal(err)
	}
	if repo.created.OAuthMeta.Email != "cline@example.test" {
		t.Fatalf("metadata = %+v", repo.created.OAuthMeta)
	}
}

func base64Std(value []byte) string { return base64.RawStdEncoding.EncodeToString(value) }

func TestEveryRequestedOAuthProviderStartsItsNativeFlow(t *testing.T) {
	service, _ := oauthTestService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/device/code", "/oauth2/device/code", "/api/oauth/device_authorization":
			_ = json.NewEncoder(w).Encode(map[string]any{"device_code": "device", "user_code": "CODE", "verification_uri": "https://example.test/verify", "expires_in": 600, "interval": 1})
		case "/api/device-auth/codes":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "KILO", "verificationUrl": "https://example.test/kilo", "expiresIn": 300})
		case "/client/register":
			_ = json.NewEncoder(w).Encode(map[string]string{"clientId": "aws-client", "clientSecret": "aws-secret"})
		case "/device_authorization":
			_ = json.NewEncoder(w).Encode(map[string]any{"deviceCode": "aws-device", "userCode": "AWS1", "verificationUri": "https://example.test/aws", "expiresIn": 600, "interval": 1})
		default:
			t.Fatalf("unexpected OAuth start endpoint %s", r.URL.Path)
		}
	}))
	tests := []struct{ provider, flow string }{
		{"codex", "authorization_code_pkce"}, {"claude", "authorization_code_pkce"},
		{"github-copilot", "device_code"}, {"cursor", "cursor_poll"}, {"grok-build", "device_code"},
		{"xai-oauth", "authorization_code_pkce"}, {"kimi-code", "device_code"},
		{"cline", "cline_callback"}, {"clinepass", "cline_callback"}, {"kilo-code", "device_code"},
		{"kiro", "device_code"}, {"amazon-q", "device_code"}, {"antigravity", "authorization_code"},
	}
	for _, test := range tests {
		t.Run(test.provider, func(t *testing.T) {
			result, err := service.StartContext(context.Background(), test.provider, "master::")
			if err != nil {
				t.Fatal(err)
			}
			if result.FlowType != test.flow || result.FlowID == "" || result.AuthorizeURL == "" {
				t.Fatalf("start result = %+v", result)
			}
			if test.flow == "device_code" && result.UserCode == "" {
				t.Fatalf("device result omitted user code: %+v", result)
			}
		})
	}
}

func TestKiroAndAmazonQDevicePollPersistDynamicAWSClient(t *testing.T) {
	for _, providerID := range []string{"kiro", "amazon-q"} {
		t.Run(providerID, func(t *testing.T) {
			service, repo := oauthTestService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/client/register":
					_ = json.NewEncoder(w).Encode(map[string]string{"clientId": "client", "clientSecret": "secret"})
				case "/device_authorization":
					_ = json.NewEncoder(w).Encode(map[string]any{"deviceCode": "device", "userCode": "AWS", "verificationUri": "https://example.test/aws", "expiresIn": 600})
				case "/token":
					_ = json.NewEncoder(w).Encode(map[string]any{"accessToken": "access", "refreshToken": "refresh", "expiresIn": 3600})
				default:
					t.Fatalf("unexpected endpoint %s", r.URL.Path)
				}
			}))
			start, err := service.StartContext(context.Background(), providerID, "master::")
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.Complete(context.Background(), CompleteInput{Provider: providerID, FlowID: start.FlowID, SessionBinding: "master::"})
			if err != nil {
				t.Fatal(err)
			}
			if repo.created.OAuthMeta.ClientID != "client" || repo.created.OAuthMeta.ClientSecret != "secret" || repo.created.OAuthMeta.Region != "us-east-1" {
				t.Fatalf("AWS metadata = %+v", repo.created.OAuthMeta)
			}
		})
	}
}

func TestAntigravityCompletesOAuthAndBootstrapsProject(t *testing.T) {
	var tokenForm url.Values
	loadCalls := 0
	service, repo := oauthTestService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = r.ParseForm()
			tokenForm = r.PostForm
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access", "refresh_token": "refresh", "expires_in": 3600})
		case "/oauth2/v1/userinfo":
			_ = json.NewEncoder(w).Encode(map[string]any{"email": "person@example.test"})
		case "/v1internal:loadCodeAssist":
			loadCalls++
			if loadCalls == 1 {
				_ = json.NewEncoder(w).Encode(map[string]any{})
			} else {
				_ = json.NewEncoder(w).Encode(map[string]any{"cloudaicompanionProject": map[string]any{"id": "project-1"}})
			}
		case "/v1internal:onboardUser":
			_ = json.NewEncoder(w).Encode(map[string]any{"done": true})
		default:
			t.Fatalf("unexpected endpoint %s", r.URL.Path)
		}
	}))
	start, err := service.StartContext(context.Background(), "antigravity", "master::")
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.Complete(context.Background(), CompleteInput{
		Provider: "antigravity", FlowID: start.FlowID, Callback: "http://localhost:51121/oauth-callback?code=returned&state=" + start.FlowID, SessionBinding: "master::",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Provider != "antigravity" || repo.created.OAuthMeta.ProjectID != "project-1" || repo.created.OAuthMeta.Email != "person@example.test" {
		t.Fatalf("created=%+v input=%+v", created, repo.created)
	}
	if tokenForm.Get("code_verifier") == "" || tokenForm.Get("client_id") == "" || tokenForm.Get("client_secret") == "" {
		t.Fatalf("token form omitted OAuth fields: %v", tokenForm)
	}
}

func TestAntigravityUsesPublicOAuthClientByDefaultAndAllowsOverrides(t *testing.T) {
	service := New(nil, credential.NewService(&oauthRepoStub{}, oauthBoxStub{}), Config{})
	if !service.OAuthAvailable("antigravity") {
		t.Fatal("Antigravity public OAuth client was unavailable")
	}
	start, err := service.StartContext(context.Background(), "antigravity", "master::")
	if err != nil || !strings.Contains(start.AuthorizeURL, "accounts.google.com") || !strings.Contains(start.AuthorizeURL, "code_challenge") {
		t.Fatalf("default Antigravity start=%+v err=%v", start, err)
	}
	service = New(nil, credential.NewService(&oauthRepoStub{}, oauthBoxStub{}), Config{AntigravityClientID: "not-a-google-client", AntigravityClientSecret: "synthetic-secret"})
	if service.OAuthAvailable("antigravity") {
		t.Fatal("Antigravity advertised OAuth with malformed client ID")
	}
	service = New(nil, credential.NewService(&oauthRepoStub{}, oauthBoxStub{}), Config{AntigravityClientID: "registered.apps.googleusercontent.com", AntigravityClientSecret: "synthetic-secret"})
	if !service.OAuthAvailable("antigravity") {
		t.Fatal("configured Antigravity OAuth was unavailable")
	}
}
