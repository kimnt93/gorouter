package routes_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/kimnt93/gorouter/api/routes"
	"github.com/kimnt93/gorouter/pkg/auth"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
	oauthpkg "github.com/kimnt93/gorouter/pkg/oauth"
)

type oauthRouteKeyLookup map[string]*entities.ApiKey

func (l oauthRouteKeyLookup) GetBySecret(_ context.Context, secret string) (*entities.ApiKey, error) {
	key, ok := l[secret]
	if !ok {
		return nil, entities.ErrNotFound
	}
	return key, nil
}

type oauthRouteCredentialRepo struct {
	created []entities.CredentialInput
}

func (r *oauthRouteCredentialRepo) Create(_ context.Context, input entities.CredentialInput, _ entities.SecretBox) (*entities.Credential, error) {
	r.created = append(r.created, input)
	return &entities.Credential{ID: "cred-oauth", Name: input.Name, Provider: input.Provider, Kind: input.Kind, OwnerTenantID: input.OwnerTenant}, nil
}

func (*oauthRouteCredentialRepo) List(context.Context) ([]entities.Credential, error) {
	return nil, nil
}

func (*oauthRouteCredentialRepo) Update(context.Context, entities.SecretBox, string, entities.CredentialUpdate) (*entities.Credential, error) {
	return nil, entities.ErrNotFound
}

func (*oauthRouteCredentialRepo) Delete(context.Context, string) error { return nil }

func (*oauthRouteCredentialRepo) Runtime(context.Context, entities.SecretBox, string) (*entities.CredentialRuntime, error) {
	return nil, entities.ErrNotFound
}

func (*oauthRouteCredentialRepo) UpdateOAuthTokens(context.Context, entities.SecretBox, string, string, string) error {
	return nil
}

func (*oauthRouteCredentialRepo) RoutesForModel(context.Context, string) ([]entities.RouteCandidate, error) {
	return nil, nil
}

type oauthRouteBox struct{}

func (oauthRouteBox) Seal(value []byte) ([]byte, error) { return value, nil }
func (oauthRouteBox) Open(value []byte) ([]byte, error) { return value, nil }

func TestOAuthRoutesRequireAuthenticationAndCredentialScope(t *testing.T) {
	keys := oauthRouteKeyLookup{
		"without-scope": {ID: "key-no-scope", TenantID: "tenant-a", Enabled: true, Scopes: []string{entities.ScopeChat}},
		"with-scope":    {ID: "key-with-scope", TenantID: "tenant-a", Enabled: true, Scopes: []string{entities.ScopeCredentialsManage}},
	}
	authService := auth.NewService("master-secret", "session-secret", keys)
	oauthService := oauthpkg.New(nil, credential.NewService(&oauthRouteCredentialRepo{}, oauthRouteBox{}), oauthpkg.Config{})
	app := routes.New(routes.Dependencies{Auth: authService, OAuth: oauthService})

	tests := []struct {
		name   string
		secret string
		want   int
	}{
		{name: "anonymous", want: http.StatusUnauthorized},
		{name: "missing scope", secret: "without-scope", want: http.StatusForbidden},
		{name: "delegated credential scope", secret: "with-scope", want: http.StatusOK},
		{name: "master", secret: "master-secret", want: http.StatusOK},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := oauthFiberRequest(t, app, http.MethodPost, "/admin/oauth/claude/start", test.secret, nil)
			defer response.Body.Close()
			if response.StatusCode != test.want {
				body, _ := io.ReadAll(response.Body)
				t.Fatalf("status = %d, want %d: %s", response.StatusCode, test.want, body)
			}
		})
	}
}

func TestOAuthCompleteBindsFlowAndCredentialOwnershipToSession(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "oauth-access", "refresh_token": "oauth-refresh"})
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"oauth_account": map[string]string{"account_uuid": "account-a", "organization_uuid": "org-a"}})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer tokenServer.Close()

	keys := oauthRouteKeyLookup{
		"tenant-a-key": {ID: "key-a", TenantID: "tenant-a", Enabled: true, Scopes: []string{entities.ScopeCredentialsManage}},
		"tenant-b-key": {ID: "key-b", TenantID: "tenant-b", Enabled: true, Scopes: []string{entities.ScopeCredentialsManage}},
	}
	repo := &oauthRouteCredentialRepo{}
	authService := auth.NewService("master-secret", "session-secret", keys)
	oauthService := oauthpkg.New(tokenServer.Client(), credential.NewService(repo, oauthRouteBox{}), oauthpkg.Config{
		ClaudeTokenURL: tokenServer.URL, ClaudeBootstrapURL: tokenServer.URL,
	})
	app := routes.New(routes.Dependencies{Auth: authService, OAuth: oauthService})

	start := startOAuthRoute(t, app, "tenant-a-key")
	response := completeOAuthRoute(t, app, "tenant-b-key", start.FlowID, "tenant-b")
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("cross-session complete status = %d, want %d", response.StatusCode, http.StatusBadRequest)
	}
	if len(repo.created) != 0 {
		t.Fatalf("cross-session flow created credentials: %+v", repo.created)
	}

	start = startOAuthRoute(t, app, "tenant-a-key")
	response = completeOAuthRoute(t, app, "tenant-a-key", start.FlowID, "foreign-tenant")
	response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("tenant complete status = %d, want %d", response.StatusCode, http.StatusCreated)
	}
	if len(repo.created) != 1 || repo.created[0].OwnerTenant == nil || *repo.created[0].OwnerTenant != "tenant-a" {
		t.Fatalf("scoped credential owner = %+v, want tenant-a", repo.created)
	}
	if repo.created[0].OAuthMeta.AccountID != "account-a" || repo.created[0].OAuthMeta.OrganizationID != "org-a" || repo.created[0].OAuthMeta.DeviceID == "" {
		t.Fatalf("OAuth metadata was not passed to credential persistence: %+v", repo.created[0].OAuthMeta)
	}

	start = startOAuthRoute(t, app, "master-secret")
	response = completeOAuthRoute(t, app, "master-secret", start.FlowID, "managed-tenant")
	response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("master complete status = %d, want %d", response.StatusCode, http.StatusCreated)
	}
	if len(repo.created) != 2 || repo.created[1].OwnerTenant == nil || *repo.created[1].OwnerTenant != "managed-tenant" {
		t.Fatalf("master-selected credential owner = %+v, want managed-tenant", repo.created)
	}
}

type oauthStartResponse struct {
	FlowID string `json:"flow_id"`
}

func startOAuthRoute(t *testing.T, app *fiber.App, bearer string) oauthStartResponse {
	t.Helper()
	response := oauthFiberRequest(t, app, http.MethodPost, "/admin/oauth/claude/start", bearer, nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("start OAuth: status=%d body=%s", response.StatusCode, body)
	}
	var result oauthStartResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func completeOAuthRoute(t *testing.T, app *fiber.App, bearer, flowID, owner string) *http.Response {
	t.Helper()
	return oauthFiberRequest(t, app, http.MethodPost, "/admin/oauth/claude/complete", bearer, map[string]any{
		"flow_id": flowID, "callback": "returned-code#" + flowID, "owner_tenant_id": owner,
	})
}

func oauthFiberRequest(t *testing.T, app *fiber.App, method, path, bearer string, body any) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	response, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
