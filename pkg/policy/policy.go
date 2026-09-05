// Package policy centralizes capability and object-level authorization for
// identity, organization, key, usage, and audit operations.
package policy

import (
	"errors"

	"github.com/kimnt93/gorouter/pkg/entities"
)

var (
	ErrForbidden = errors.New("forbidden")
	ErrConcealed = errors.New("not found")
)

func ManageUsers(actor entities.Principal) error {
	if actor.Type != entities.PrincipalMaster {
		return ErrForbidden
	}
	return nil
}

func ManageOrganizations(actor entities.Principal) error {
	if actor.Type == entities.PrincipalMaster || actor.Type == entities.PrincipalUser && actor.UserID != "" {
		return nil
	}
	return ErrForbidden
}

func ViewOrganization(actor entities.Principal, organizationID string) error {
	if actor.Type == entities.PrincipalMaster || actor.OrganizationID == organizationID {
		return nil
	}
	return ErrConcealed
}

func ManageMembers(actor entities.Principal, organizationID string) error {
	if actor.Type == entities.PrincipalMaster {
		return nil
	}
	if !actor.HasScope(entities.ScopeMembersManage) {
		return ErrForbidden
	}
	if actor.OrganizationID != organizationID || actor.MembershipRole != entities.MembershipAdmin {
		return ErrConcealed
	}
	return nil
}

func ManageKey(actor entities.Principal, key entities.ApiKey) error {
	if actor.Type == entities.PrincipalMaster {
		return nil
	}
	if !actor.HasScope(entities.ScopeKeysManage) {
		return ErrForbidden
	}
	switch key.OwnerType {
	case entities.OwnerUser:
		if actor.Type == entities.PrincipalUser && actor.UserID == key.OwnerUserID {
			return nil
		}
		if actor.Type == entities.PrincipalUser && actor.MembershipRole == entities.MembershipAdmin &&
			actor.OrganizationID != "" && actor.OrganizationID == key.ContextOrganizationID {
			return nil
		}
	case entities.OwnerOrganization:
		if actor.OrganizationID == key.OwnerOrganizationID &&
			(actor.Type == entities.PrincipalOrganization || actor.MembershipRole == entities.MembershipAdmin) {
			return nil
		}
	}
	return ErrConcealed
}

// ViewKeyMetadata additionally lets an organization administrator see safe
// metadata for user-owned keys scoped to that organization.
func ViewKeyMetadata(actor entities.Principal, key entities.ApiKey) error {
	if err := ManageKey(actor, key); err == nil {
		return nil
	}
	if actor.HasScope(entities.ScopeKeysManage) && actor.MembershipRole == entities.MembershipAdmin &&
		actor.OrganizationID != "" && actor.OrganizationID == key.ContextOrganizationID {
		return nil
	}
	return ErrConcealed
}

func UsageVisibility(actor entities.Principal, organizationWide bool) (entities.UsageVisibility, error) {
	if !actor.HasScope(entities.ScopeUsageRead) {
		return entities.UsageVisibility{}, ErrForbidden
	}
	v := entities.UsageVisibility{
		PrincipalType:  actor.Type,
		UserID:         actor.UserID,
		OrganizationID: actor.OrganizationID,
	}
	if actor.Type == entities.PrincipalMaster {
		v.OrganizationWide = organizationWide
		return v, nil
	}
	if organizationWide {
		if actor.OrganizationID == "" || (actor.Type == entities.PrincipalUser && actor.MembershipRole != entities.MembershipAdmin) {
			return entities.UsageVisibility{}, ErrForbidden
		}
		v.OrganizationWide = true
	}
	return v, nil
}

func AuditVisibility(actor entities.Principal) (entities.UsageVisibility, error) {
	if actor.Type == entities.PrincipalMaster {
		return entities.UsageVisibility{PrincipalType: actor.Type, OrganizationWide: true}, nil
	}
	if !actor.HasScope(entities.ScopeUsageRead) || actor.OrganizationID == "" ||
		(actor.Type == entities.PrincipalUser && actor.MembershipRole != entities.MembershipAdmin) {
		return entities.UsageVisibility{}, ErrForbidden
	}
	return entities.UsageVisibility{PrincipalType: actor.Type, OrganizationID: actor.OrganizationID, OrganizationWide: true}, nil
}

func CanGrant(actor entities.Principal, scopes, models []string, allowedModels []string) bool {
	if actor.Type == entities.PrincipalMaster {
		return true
	}
	for _, scope := range scopes {
		if !actor.HasScope(scope) {
			return false
		}
	}
	allowed := make(map[string]struct{}, len(allowedModels))
	for _, model := range allowedModels {
		allowed[model] = struct{}{}
	}
	for _, model := range models {
		if _, ok := allowed[model]; !ok {
			return false
		}
	}
	return true
}

// CredentialVisible enforces global-plus-organization credential visibility.
// Personal context sees global credentials only; organization context also
// sees credentials owned by that organization; master sees every credential.
func CredentialVisible(master bool, organizationID string, ownerOrganizationID *string) bool {
	if master || ownerOrganizationID == nil {
		return true
	}
	return organizationID != "" && *ownerOrganizationID == organizationID
}
