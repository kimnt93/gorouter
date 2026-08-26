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
