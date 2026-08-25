package auth

import (
	"context"
	"testing"

	"github.com/kimnt93/gorouter/pkg/entities"
)

type keyLookup struct{ key *entities.ApiKey }

func (l keyLookup) GetBySecret(context.Context, string) (*entities.ApiKey, error) { return l.key, nil }
func (l keyLookup) GetByID(context.Context, string) (*entities.ApiKey, error)     { return l.key, nil }

func TestMasterAndScopedLogin(t *testing.T) {
	key := &entities.ApiKey{ID: "key_1", TenantID: "tenant_1", Enabled: true, Scopes: []string{entities.ScopeUsageRead}}
	s := NewService("master", "session-secret", keyLookup{key: key})
	master, err := s.Login(context.Background(), "master")
	if err != nil || !master.IsMaster() || !master.Has(entities.ScopeModelsManage) {
		t.Fatalf("master login: %+v %v", master, err)
	}
	user, err := s.Login(context.Background(), "api-key")
	if err != nil || user.IsMaster() || !user.Has(entities.ScopeUsageRead) || user.Has(entities.ScopeKeysManage) {
		t.Fatalf("scoped login: %+v %v", user, err)
	}
	token, err := s.IssueToken(user)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.VerifyToken(token)
	if err != nil || got.KeyID != "key_1" {
		t.Fatalf("token: %+v %v", got, err)
	}
}

func TestTamperedTokenRejected(t *testing.T) {
	s := NewService("master", "secret", keyLookup{})
	token, _ := s.IssueToken(&entities.Session{Role: entities.RoleMaster, Expires: 9999999999})
	if _, err := s.VerifyToken(token + "x"); err == nil {
		t.Fatal("tampered token accepted")
	}
}

func TestEmptyMasterKeyCannotAuthenticate(t *testing.T) {
	s := NewService("", "secret", nil)
	if _, err := s.Login(context.Background(), ""); err == nil {
		t.Fatal("empty master key authenticated")
	}
}

func TestNilStoredScopesRemainFailClosed(t *testing.T) {
	key := &entities.ApiKey{ID: "key_1", TenantID: "tenant_1", Enabled: true}
	s := NewService("master", "secret", keyLookup{key: key})
	sess, err := s.Login(context.Background(), "api-key")
	if err != nil {
		t.Fatal(err)
	}
	if sess.Has(entities.ScopeChat) {
		t.Fatal("missing stored scopes unexpectedly granted chat access")
	}
}

func TestRevalidateRefreshesScopesWithoutExtendingSession(t *testing.T) {
	key := &entities.ApiKey{
		ID: "key_1", TenantID: "tenant_1", Enabled: true,
		Scopes: []string{entities.ScopeUsageRead},
	}
	s := NewService("master", "secret", keyLookup{key: key})
	original := &entities.Session{
		Role: entities.RoleAPIKey, KeyID: key.ID, TenantID: key.TenantID,
		Scopes: []string{entities.ScopeChat}, Expires: 9999999999,
	}
	got, err := s.Revalidate(context.Background(), original)
	if err != nil {
		t.Fatal(err)
	}
	if got.Expires != original.Expires || got.Has(entities.ScopeChat) || !got.Has(entities.ScopeUsageRead) {
		t.Fatalf("session was not refreshed safely: %+v", got)
	}
}

func TestRevalidateRejectsDisabledOrMovedKey(t *testing.T) {
	key := &entities.ApiKey{ID: "key_1", TenantID: "tenant_1", Enabled: false}
	s := NewService("master", "secret", keyLookup{key: key})
	sess := &entities.Session{Role: entities.RoleAPIKey, KeyID: key.ID, TenantID: key.TenantID, Expires: 9999999999}
	if _, err := s.Revalidate(context.Background(), sess); err != ErrDisabled {
		t.Fatalf("disabled key error = %v", err)
	}
	key.Enabled = true
	key.TenantID = "tenant_2"
	if _, err := s.Revalidate(context.Background(), sess); err != ErrBadToken {
		t.Fatalf("moved key error = %v", err)
	}
}
