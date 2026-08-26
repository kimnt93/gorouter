package handlers

import (
	"testing"

	"github.com/kimnt93/gorouter/pkg/entities"
)

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
