package handlers

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
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
