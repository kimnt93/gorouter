package handlers

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/kimnt93/gorouter/pkg/apikey"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/seal"
)

type credentialOwnerRepo struct{ created entities.CredentialInput }

func (r *credentialOwnerRepo) Create(_ context.Context, input entities.CredentialInput, _ entities.SecretBox) (*entities.Credential, error) {
	r.created = input
	return &entities.Credential{ID: "cred_1", Name: input.Name, Provider: input.Provider, Kind: input.Kind, OwnerTenantID: input.OwnerTenant, OwnerUserID: input.OwnerUserID}, nil
}
func (*credentialOwnerRepo) List(context.Context) ([]entities.Credential, error) { return nil, nil }
func (*credentialOwnerRepo) Update(context.Context, entities.SecretBox, string, entities.CredentialUpdate) (*entities.Credential, error) {
	return nil, nil
}
func (*credentialOwnerRepo) Delete(context.Context, string) error { return nil }
func (*credentialOwnerRepo) Runtime(context.Context, entities.SecretBox, string) (*entities.CredentialRuntime, error) {
	return nil, entities.ErrNotFound
}
func (*credentialOwnerRepo) UpdateOAuthTokens(context.Context, entities.SecretBox, string, string, string) error {
	return nil
}
func (*credentialOwnerRepo) RoutesForModel(context.Context, string) ([]entities.RouteCandidate, error) {
	return nil, nil
}

func TestScopedSessionCannotGrantElevatedScopes(t *testing.T) {
	session := &entities.Session{Role: entities.RoleAPIKey, Scopes: []string{entities.ScopeKeysManage, entities.ScopeChat}}
	if !scopesAllowedBySession(session, []string{entities.ScopeChat}) {
		t.Fatal("session could not delegate a scope it holds")
	}
	if scopesAllowedBySession(session, []string{entities.ScopeCredentialsManage}) {
		t.Fatal("session delegated an elevated scope")
	}
}

func TestSessionResponseContainsOnlySafePrincipalMetadata(t *testing.T) {
	response := sessionResponse(&entities.Session{Role: entities.RoleAPIKey, KeyID: "key_private", TenantID: "org_1", PrincipalType: entities.PrincipalUser, UserID: "user_1", Username: "person@example.com", OrganizationID: "org_1", MembershipRole: entities.MembershipAdmin, Scopes: []string{entities.ScopeUsageRead}})
	if !response.OK || response.UserID != "user_1" || response.OrganizationID != "org_1" || len(response.Scopes) != 1 {
		t.Fatalf("session response=%+v", response)
	}
}

func TestCredentialCreateDerivesPersonalOwnerAndIgnoresRequestedOrganization(t *testing.T) {
	repo := &credentialOwnerRepo{}
	admin := &Admin{CredsSvc: credential.NewService(repo, nil)}
	app := fiber.New()
	app.Post("/credentials", func(c fiber.Ctx) error {
		c.Locals(localSession, &entities.Session{Role: entities.RoleAPIKey, PrincipalType: entities.PrincipalUser, UserID: "user_1", TenantID: "org_1"})
		return c.Next()
	}, admin.Credentials)
	request := httptest.NewRequest("POST", "/credentials", strings.NewReader(`{"name":"mine","provider":"openai-compatible","kind":"api_key","base_url":"https://example.test/v1","api_key":"secret","owner_tenant_id":"org_foreign"}`))
	request.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
	response, err := app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != fiber.StatusCreated {
		t.Fatalf("status=%d", response.StatusCode)
	}
	if repo.created.OwnerUserID != "user_1" || repo.created.OwnerTenant != nil {
		t.Fatalf("derived owner user=%q tenant=%v", repo.created.OwnerUserID, repo.created.OwnerTenant)
	}
	if strings.TrimSpace(repo.created.APIKey) != "secret" {
		t.Fatal("credential input was not bound")
	}
}

func TestCredentialListReturnsOnlyAuthenticatedUsersConnections(t *testing.T) {
	repo := &credentialListRepo{items: []entities.Credential{{ID: "mine", OwnerUserID: "user_1"}, {ID: "foreign", OwnerUserID: "user_2"}, {ID: "legacy-global"}}}
	admin := &Admin{CredsSvc: credential.NewService(repo, nil)}
	app := fiber.New()
	app.Get("/credentials", func(c fiber.Ctx) error {
		c.Locals(localSession, &entities.Session{Role: entities.RoleAPIKey, PrincipalType: entities.PrincipalUser, UserID: "user_1"})
		return admin.Credentials(c)
	})
	response, err := app.Test(httptest.NewRequest("GET", "/credentials", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var credentials []entities.Credential
	if err := json.NewDecoder(response.Body).Decode(&credentials); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != fiber.StatusOK || len(credentials) != 1 || credentials[0].ID != "mine" {
		t.Fatalf("status=%d credentials=%+v", response.StatusCode, credentials)
	}
}

type credentialListRepo struct{ items []entities.Credential }

func (r *credentialListRepo) Create(context.Context, entities.CredentialInput, entities.SecretBox) (*entities.Credential, error) {
	return nil, nil
}
func (r *credentialListRepo) List(context.Context) ([]entities.Credential, error) {
	return r.items, nil
}
func (*credentialListRepo) Update(context.Context, entities.SecretBox, string, entities.CredentialUpdate) (*entities.Credential, error) {
	return nil, nil
}
func (*credentialListRepo) Delete(context.Context, string) error { return nil }
func (*credentialListRepo) Runtime(context.Context, entities.SecretBox, string) (*entities.CredentialRuntime, error) {
	return nil, entities.ErrNotFound
}
func (*credentialListRepo) UpdateOAuthTokens(context.Context, entities.SecretBox, string, string, string) error {
	return nil
}
func (*credentialListRepo) RoutesForModel(context.Context, string) ([]entities.RouteCandidate, error) {
	return nil, nil
}

type revealKeyRepo struct {
	key    entities.ApiKey
	sealed []byte
}

func (*revealKeyRepo) Create(context.Context, string, string, []string, []string, *float64, *int) (*entities.ApiKey, error) {
	return nil, nil
}
func (*revealKeyRepo) GetBySecret(context.Context, string) (*entities.ApiKey, error) {
	return nil, entities.ErrNotFound
}
func (r *revealKeyRepo) GetByID(_ context.Context, id string) (*entities.ApiKey, error) {
	if id != r.key.ID {
		return nil, entities.ErrNotFound
	}
	key := r.key
	return &key, nil
}
func (r *revealKeyRepo) GetByIDForTenant(ctx context.Context, _ string, id string) (*entities.ApiKey, error) {
	return r.GetByID(ctx, id)
}
func (r *revealKeyRepo) List(context.Context) ([]entities.ApiKey, error) {
	return []entities.ApiKey{r.key}, nil
}
func (*revealKeyRepo) ListByTenant(context.Context, string) ([]entities.ApiKey, error) {
	return nil, nil
}
func (*revealKeyRepo) Patch(context.Context, string, *bool, *[]string, *[]string, **float64, **int) error {
	return nil
}
func (*revealKeyRepo) PatchForTenant(context.Context, string, string, *bool, *[]string, *[]string, **float64, **int) error {
	return nil
}
func (*revealKeyRepo) Delete(context.Context, string) error                  { return nil }
func (*revealKeyRepo) DeleteForTenant(context.Context, string, string) error { return nil }
func (r *revealKeyRepo) StorePlaintext(_ context.Context, _ string, plaintext string, box entities.SecretBox) error {
	var err error
	r.sealed, err = box.Seal([]byte(plaintext))
	return err
}
func (r *revealKeyRepo) RevealPlaintext(_ context.Context, _ string, box entities.SecretBox) (string, error) {
	plain, err := box.Open(r.sealed)
	return string(plain), err
}

func TestKeysRevealAllowsMasterAndOrganizationAdminButConcealsForeignAdmin(t *testing.T) {
	box, err := seal.New("synthetic-api-key-reveal-key")
	if err != nil {
		t.Fatal(err)
	}
	repo := &revealKeyRepo{key: entities.ApiKey{ID: "key-1", OwnerType: entities.OwnerUser, OwnerUserID: "member-1", ContextOrganizationID: "org-1"}}
	service := apikey.NewService(repo, func(string) string { return "" }, func() string { return "" })
	service.SetSecretBox(box)
	sealed, err := box.Seal([]byte("nr-synthetic-secret"))
	if err != nil {
		t.Fatal(err)
	}
	repo.sealed = sealed
	admin := &Admin{KeysSvc: service}
	for _, test := range []struct {
		name    string
		session *entities.Session
		want    int
	}{
		{"master", &entities.Session{Role: entities.RoleMaster, PrincipalType: entities.PrincipalMaster}, fiber.StatusOK},
		{"organization admin", &entities.Session{Role: entities.RoleAPIKey, PrincipalType: entities.PrincipalUser, UserID: "admin-1", OrganizationID: "org-1", MembershipRole: entities.MembershipAdmin, Scopes: []string{entities.ScopeKeysManage}}, fiber.StatusOK},
		{"foreign admin", &entities.Session{Role: entities.RoleAPIKey, PrincipalType: entities.PrincipalUser, UserID: "admin-2", OrganizationID: "org-2", MembershipRole: entities.MembershipAdmin, Scopes: []string{entities.ScopeKeysManage}}, fiber.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := fiber.New()
			app.Get("/keys/:id/reveal", func(c fiber.Ctx) error { c.Locals(localSession, test.session); return admin.KeysReveal(c) })
			response, err := app.Test(httptest.NewRequest("GET", "/keys/key-1/reveal", nil))
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != test.want {
				t.Fatalf("status=%d want=%d", response.StatusCode, test.want)
			}
			if test.want == fiber.StatusOK {
				var body APIKeyRevealResponse
				if err := json.NewDecoder(response.Body).Decode(&body); err != nil || body.Plaintext != "nr-synthetic-secret" {
					t.Fatalf("body=%+v err=%v", body, err)
				}
				if response.Header.Get(fiber.HeaderCacheControl) != "no-store" {
					t.Fatalf("cache-control=%q", response.Header.Get(fiber.HeaderCacheControl))
				}
			}
		})
	}
}

func TestCredentialCreateRejectsMasterAndOrganizationPrincipals(t *testing.T) {
	for _, session := range []*entities.Session{
		{Role: entities.RoleMaster, PrincipalType: entities.PrincipalMaster},
		{Role: entities.RoleAPIKey, PrincipalType: entities.PrincipalOrganization, OrganizationID: "org_1"},
	} {
		repo := &credentialOwnerRepo{}
		admin := &Admin{CredsSvc: credential.NewService(repo, nil)}
		app := fiber.New()
		app.Post("/credentials", func(c fiber.Ctx) error { c.Locals(localSession, session); return admin.Credentials(c) })
		request := httptest.NewRequest("POST", "/credentials", strings.NewReader(`{"name":"shared","api_key":"secret"}`))
		request.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
		response, err := app.Test(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != fiber.StatusForbidden {
			t.Fatalf("principal=%s status=%d", session.PrincipalType, response.StatusCode)
		}
	}
}
