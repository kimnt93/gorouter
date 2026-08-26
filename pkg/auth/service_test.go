package auth

import (
	"context"
	"testing"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

type keyLookup struct{ key *entities.ApiKey }

func (l keyLookup) GetBySecret(context.Context, string) (*entities.ApiKey, error) { return l.key, nil }
func (l keyLookup) GetByID(context.Context, string) (*entities.ApiKey, error)     { return l.key, nil }

type identityLookup struct {
	user         entities.User
	organization entities.Organization
	membership   *entities.Membership
}

func (i *identityLookup) UserByID(context.Context, string) (*entities.User, error) {
	return &i.user, nil
}
func (i *identityLookup) OrganizationByID(context.Context, string) (*entities.Organization, error) {
	return &i.organization, nil
}
func (i *identityLookup) Membership(context.Context, string, string) (*entities.Membership, error) {
	if i.membership == nil {
		return nil, entities.ErrNotFound
	}
	return i.membership, nil
}

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

func TestPrincipalLoginAndStaleMembershipRevalidation(t *testing.T) {
	key := &entities.ApiKey{ID: "key_1", Enabled: true, OwnerType: entities.OwnerUser, OwnerUserID: "usr_1", ContextOrganizationID: "org_1", Models: []string{"m1"}, Scopes: []string{entities.ScopeChat}}
	identity := &identityLookup{user: entities.User{ID: "usr_1", Username: "person@example.com", Status: entities.StatusActive}, organization: entities.Organization{ID: "org_1", Name: "Acme", Status: entities.StatusActive}, membership: &entities.Membership{OrganizationID: "org_1", UserID: "usr_1", Role: entities.MembershipMember}}
	service := NewServiceWithIdentity("master", "secret", keyLookup{key}, identity)
	session, err := service.Login(context.Background(), "key")
	if err != nil || session.PrincipalType != entities.PrincipalUser || session.UserID != "usr_1" || session.OrganizationID != "org_1" || session.MembershipRole != entities.MembershipMember {
		t.Fatalf("resolved session=%+v err=%v", session, err)
	}
	identity.membership = nil
	if _, err = service.Revalidate(context.Background(), session); err != ErrDisabled {
		t.Fatalf("removed membership revalidation=%v", err)
	}
}

func TestDisabledOwnerAndOrganizationRejectLogin(t *testing.T) {
	key := &entities.ApiKey{ID: "key_1", Enabled: true, OwnerType: entities.OwnerUser, OwnerUserID: "usr_1", ContextOrganizationID: "org_1"}
	identity := &identityLookup{user: entities.User{ID: "usr_1", Status: entities.StatusDisabled}, organization: entities.Organization{ID: "org_1", Status: entities.StatusActive}, membership: &entities.Membership{}}
	service := NewServiceWithIdentity("master", "secret", keyLookup{key}, identity)
	if _, err := service.Login(context.Background(), "key"); err != ErrDisabled {
		t.Fatalf("disabled user login=%v", err)
	}
	identity.user.Status = entities.StatusActive
	identity.organization.Status = entities.StatusDisabled
	if _, err := service.Login(context.Background(), "key"); err != ErrDisabled {
		t.Fatalf("disabled organization login=%v", err)
	}
}

func TestTamperedTokenRejected(t *testing.T) {
	s := NewService("master", "secret", keyLookup{})
	token, _ := s.IssueToken(&entities.Session{Role: entities.RoleMaster, Expires: 9999999999})
	if _, err := s.VerifyToken(token + "x"); err == nil {
		t.Fatal("tampered token accepted")
	}
}

func TestExpiredTokenRejected(t *testing.T) {
	service := NewService("master", "secret", keyLookup{})
	token, err := service.IssueToken(&entities.Session{Role: entities.RoleMaster, Expires: time.Now().Add(-time.Second).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.VerifyToken(token); err != ErrExpired {
		t.Fatalf("expired token error=%v", err)
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
