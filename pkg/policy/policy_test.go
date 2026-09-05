package policy

import (
	"errors"
	"testing"

	"github.com/kimnt93/gorouter/pkg/entities"
)

func TestAuthorizationMatrix(t *testing.T) {
	master := entities.Principal{Type: entities.PrincipalMaster}
	admin := entities.Principal{Type: entities.PrincipalUser, UserID: "u1", OrganizationID: "o1", MembershipRole: entities.MembershipAdmin, Scopes: []string{entities.ScopeMembersManage, entities.ScopeKeysManage, entities.ScopeUsageRead}}
	member := entities.Principal{Type: entities.PrincipalUser, UserID: "u2", OrganizationID: "o1", MembershipRole: entities.MembershipMember, Scopes: []string{entities.ScopeMembersManage, entities.ScopeKeysManage, entities.ScopeUsageRead}}
	org := entities.Principal{Type: entities.PrincipalOrganization, OrganizationID: "o1", Scopes: []string{entities.ScopeKeysManage, entities.ScopeUsageRead}}

	if ManageUsers(master) != nil || !errors.Is(ManageUsers(admin), ErrForbidden) {
		t.Fatal("only master may manage users")
	}
	if ManageMembers(admin, "o1") != nil || !errors.Is(ManageMembers(member, "o1"), ErrConcealed) || !errors.Is(ManageMembers(admin, "o2"), ErrConcealed) {
		t.Fatal("membership matrix mismatch")
	}
	userKey := entities.ApiKey{OwnerType: entities.OwnerUser, OwnerUserID: "u1", ContextOrganizationID: "o1"}
	orgKey := entities.ApiKey{OwnerType: entities.OwnerOrganization, OwnerOrganizationID: "o1", ContextOrganizationID: "o1"}
	if ManageKey(admin, userKey) != nil || ManageKey(admin, orgKey) != nil || ManageKey(org, orgKey) != nil {
		t.Fatal("expected owners/admin to manage their keys")
	}
	if ManageKey(admin, entities.ApiKey{OwnerType: entities.OwnerUser, OwnerUserID: "u2", ContextOrganizationID: "o1"}) != nil {
		t.Fatal("organization admin should manage a member key scoped to the same organization")
	}
	if !errors.Is(ManageKey(member, orgKey), ErrConcealed) || !errors.Is(ManageKey(admin, entities.ApiKey{OwnerType: entities.OwnerUser, OwnerUserID: "u2"}), ErrConcealed) {
		t.Fatal("horizontal key access was not concealed")
	}
	if ViewKeyMetadata(admin, entities.ApiKey{OwnerType: entities.OwnerUser, OwnerUserID: "u2", ContextOrganizationID: "o1"}) != nil {
		t.Fatal("admin should see scoped personal key safe metadata")
	}
	if _, err := UsageVisibility(member, true); !errors.Is(err, ErrForbidden) {
		t.Fatal("member must not get organization-wide usage")
	}
	if visibility, err := UsageVisibility(member, false); err != nil || visibility.UserID != "u2" || visibility.OrganizationWide {
		t.Fatal("member should get only own usage")
	}
}

func TestCanGrantDoesNotEscalate(t *testing.T) {
	actor := entities.Principal{Type: entities.PrincipalUser, Scopes: []string{entities.ScopeChat, entities.ScopeUsageRead}}
	if !CanGrant(actor, []string{entities.ScopeChat}, []string{"m1"}, []string{"m1"}) {
		t.Fatal("valid subset rejected")
	}
	if CanGrant(actor, []string{entities.ScopeKeysManage}, []string{"m1"}, []string{"m1"}) || CanGrant(actor, []string{entities.ScopeChat}, []string{"m2"}, []string{"m1"}) {
		t.Fatal("scope/model escalation accepted")
	}
}

func TestCredentialVisibility(t *testing.T) {
	organizationA, organizationB := "org-a", "org-b"
	for name, test := range map[string]struct {
		master  bool
		context string
		owner   *string
		want    bool
	}{
		"master foreign":        {master: true, owner: &organizationB, want: true},
		"personal global":       {want: true},
		"personal organization": {owner: &organizationA},
		"organization global":   {context: organizationA, want: true},
		"organization own":      {context: organizationA, owner: &organizationA, want: true},
		"organization foreign":  {context: organizationA, owner: &organizationB},
	} {
		t.Run(name, func(t *testing.T) {
			if got := CredentialVisible(test.master, test.context, test.owner); got != test.want {
				t.Fatalf("got %v want %v", got, test.want)
			}
		})
	}
}

func TestRoleBasedUserCanCreateOrganization(t *testing.T) {
	if err := ManageOrganizations(entities.Principal{Type: entities.PrincipalUser, UserID: "user-1", UserRole: entities.UserRoleOrgManager}); err != nil {
		t.Fatal(err)
	}
	if err := ManageOrganizations(entities.Principal{Type: entities.PrincipalOrganization, OrganizationID: "org-1"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("error=%v", err)
	}
}
