package routes_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/kimnt93/gorouter/internal/api/routes"
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

type oauthRouteIdentityRepo struct {
	user entities.User
}

func (*oauthRouteIdentityRepo) CreateUser(context.Context, entities.User) error { return nil }
func (r *oauthRouteIdentityRepo) UserByID(_ context.Context, id string) (*entities.User, error) {
	if id != r.user.ID {
		return nil, entities.ErrNotFound
	}
	user := r.user
	return &user, nil
}
func (*oauthRouteIdentityRepo) UserByNormalizedUsername(context.Context, string) (*entities.User, error) {
	return nil, entities.ErrNotFound
}
func (*oauthRouteIdentityRepo) ListUsers(context.Context, entities.PageQuery) ([]entities.User, string, error) {
	return nil, "", nil
}
func (*oauthRouteIdentityRepo) UpdateUserStatus(context.Context, string, string, time.Time) error {
	return nil
}
func (*oauthRouteIdentityRepo) CreateOrganization(context.Context, entities.Organization) error {
	return nil
}
func (*oauthRouteIdentityRepo) OrganizationByID(context.Context, string) (*entities.Organization, error) {
	return nil, entities.ErrNotFound
}
func (*oauthRouteIdentityRepo) OrganizationByNormalizedName(context.Context, string) (*entities.Organization, error) {
	return nil, entities.ErrNotFound
}
func (*oauthRouteIdentityRepo) ListOrganizations(context.Context, entities.PageQuery) ([]entities.Organization, string, error) {
	return nil, "", nil
}
func (*oauthRouteIdentityRepo) UpdateOrganization(context.Context, entities.Organization) error {
	return nil
}
func (*oauthRouteIdentityRepo) PutMembership(context.Context, entities.Membership) error { return nil }
func (*oauthRouteIdentityRepo) Membership(context.Context, string, string) (*entities.Membership, error) {
	return nil, entities.ErrNotFound
}
func (*oauthRouteIdentityRepo) ListMemberships(context.Context, string) ([]entities.Membership, error) {
	return nil, nil
}
func (*oauthRouteIdentityRepo) ListMembershipsForUser(context.Context, string) ([]entities.Membership, error) {
	return nil, nil
}
func (*oauthRouteIdentityRepo) CountActiveOrganizationAdmins(context.Context, string) (int, error) {
	return 0, nil
}
func (*oauthRouteIdentityRepo) DeleteMembership(context.Context, string, string) error { return nil }

type oauthRouteBox struct{}

func (oauthRouteBox) Seal(value []byte) ([]byte, error) { return value, nil }
func (oauthRouteBox) Open(value []byte) ([]byte, error) { return value, nil }

type oauthRouteRewriteTransport struct {
	target *url.URL
}

func (r oauthRouteRewriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.URL.Scheme = r.target.Scheme
	clone.URL.Host = r.target.Host
	return http.DefaultTransport.RoundTrip(clone)
}

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

func TestOAuthCompleteBindsCredentialToAuthenticatedUser(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "oauth-access", "refresh_token": "oauth-refresh"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"oauth_account": map[string]string{"account_uuid": "account-a"}})
	}))
	defer tokenServer.Close()
	user := entities.User{ID: "user-a", Username: "user-a@example.test", Status: entities.StatusActive}
	keys := oauthRouteKeyLookup{"user-key": {ID: "key-a", OwnerType: entities.OwnerUser, OwnerUserID: user.ID, Enabled: true, Scopes: []string{entities.ScopeCredentialsManage}}}
	repo := &oauthRouteCredentialRepo{}
	authService := auth.NewServiceWithIdentity("master-secret", "session-secret", keys, &oauthRouteIdentityRepo{user: user})
	oauthService := oauthpkg.New(tokenServer.Client(), credential.NewService(repo, oauthRouteBox{}), oauthpkg.Config{ClaudeTokenURL: tokenServer.URL, ClaudeBootstrapURL: tokenServer.URL})
	app := routes.New(routes.Dependencies{Auth: authService, OAuth: oauthService})

	start := startOAuthRoute(t, app, "user-key")
	response := completeOAuthRoute(t, app, "user-key", start.FlowID, "ignored")
	response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("user complete status=%d", response.StatusCode)
	}
	if len(repo.created) != 1 || repo.created[0].OwnerUserID != user.ID || repo.created[0].OwnerTenant != nil {
		t.Fatalf("created ownership=%+v", repo.created)
	}

	start = startOAuthRoute(t, app, "master-secret")
	response = completeOAuthRoute(t, app, "master-secret", start.FlowID, "ignored")
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("master complete status=%d", response.StatusCode)
	}
}

func TestOAuthDeviceRouteReturnsUserCodeAndAcceptedWhilePending(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/oauth/device_authorization":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code": "device", "user_code": "ABCD-EFGH",
				"verification_uri": "https://auth.kimi.com/device", "expires_in": 600, "interval": 1,
			})
		case "/api/oauth/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "authorization_pending"})
		default:
			t.Fatalf("unexpected OAuth endpoint %s", r.URL.Path)
		}
	}))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)
	client := &http.Client{Transport: oauthRouteRewriteTransport{target: target}}
	service := oauthpkg.New(client, credential.NewService(&oauthRouteCredentialRepo{}, oauthRouteBox{}), oauthpkg.Config{})
	user := entities.User{ID: "user-a", Username: "user-a@example.test", Status: entities.StatusActive}
	keys := oauthRouteKeyLookup{"user-key": {ID: "key-a", OwnerType: entities.OwnerUser, OwnerUserID: user.ID, Enabled: true, Scopes: []string{entities.ScopeCredentialsManage}}}
	app := routes.New(routes.Dependencies{Auth: auth.NewServiceWithIdentity("master-secret", "session-secret", keys, &oauthRouteIdentityRepo{user: user}), OAuth: service})

	startResponse := oauthFiberRequest(t, app, http.MethodPost, "/admin/oauth/kimi-code/start", "user-key", nil)
	defer startResponse.Body.Close()
	var start struct {
		FlowID       string `json:"flow_id"`
		FlowType     string `json:"flow_type"`
		UserCode     string `json:"user_code"`
		AuthorizeURL string `json:"authorize_url"`
	}
	if err := json.NewDecoder(startResponse.Body).Decode(&start); err != nil {
		t.Fatal(err)
	}
	if startResponse.StatusCode != http.StatusOK || start.FlowType != "device_code" || start.UserCode != "ABCD-EFGH" || start.AuthorizeURL == "" {
		t.Fatalf("device start status=%d payload=%+v", startResponse.StatusCode, start)
	}

	completeResponse := oauthFiberRequest(t, app, http.MethodPost, "/admin/oauth/kimi-code/complete", "user-key", map[string]any{"flow_id": start.FlowID})
	defer completeResponse.Body.Close()
	if completeResponse.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(completeResponse.Body)
		t.Fatalf("pending status=%d body=%s", completeResponse.StatusCode, body)
	}
	var pending map[string]string
	if err := json.NewDecoder(completeResponse.Body).Decode(&pending); err != nil {
		t.Fatal(err)
	}
	if pending["status"] != "authorization_pending" {
		t.Fatalf("pending payload=%v", pending)
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

func TestOAuthCompleteRejectsClientSuppliedOwnerAndMasterCreation(t *testing.T) {
	repo := &oauthRouteCredentialRepo{}
	service := oauthpkg.New(nil, credential.NewService(repo, oauthRouteBox{}), oauthpkg.Config{})
	app := routes.New(routes.Dependencies{Auth: auth.NewService("master-secret", "session-secret", oauthRouteKeyLookup{}), OAuth: service})
	start := startOAuthRoute(t, app, "master-secret")
	response := oauthFiberRequest(t, app, http.MethodPost, "/admin/oauth/claude/complete", "master-secret", map[string]any{"flow_id": start.FlowID, "owner_user_id": "user-control"})
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden || len(repo.created) != 0 {
		t.Fatalf("status=%d created=%+v", response.StatusCode, repo.created)
	}
}

func TestAntigravityIsAdvertisedAndStartsWithPublicOAuthClient(t *testing.T) {
	service := oauthpkg.New(nil, credential.NewService(&oauthRouteCredentialRepo{}, oauthRouteBox{}), oauthpkg.Config{})
	app := routes.New(routes.Dependencies{
		Auth:  auth.NewService("master-secret", "session-secret", oauthRouteKeyLookup{}),
		OAuth: service, OAuthAvailable: service.OAuthAvailable,
	})
	response := oauthFiberRequest(t, app, http.MethodGet, "/admin/providers", "master-secret", nil)
	defer response.Body.Close()
	var catalog struct {
		Data []struct {
			ID             string `json:"id"`
			OAuthSupported bool   `json:"oauth_supported"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&catalog); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, provider := range catalog.Data {
		if provider.ID == "antigravity" {
			found = provider.OAuthSupported
		}
	}
	if !found {
		t.Fatal("Antigravity OAuth was not advertised")
	}

	start := oauthFiberRequest(t, app, http.MethodPost, "/admin/oauth/antigravity/start", "master-secret", nil)
	defer start.Body.Close()
	if start.StatusCode != http.StatusOK {
		t.Fatalf("start status=%d", start.StatusCode)
	}
	var flow struct {
		FlowType     string `json:"flow_type"`
		AuthorizeURL string `json:"authorize_url"`
	}
	if err := json.NewDecoder(start.Body).Decode(&flow); err != nil {
		t.Fatal(err)
	}
	if flow.FlowType != "authorization_code" || !strings.Contains(flow.AuthorizeURL, "accounts.google.com") || !strings.Contains(flow.AuthorizeURL, "code_challenge") {
		t.Fatalf("flow=%+v", flow)
	}
}
