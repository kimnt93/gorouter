package local

import (
	"context"
	"encoding/base64"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

type IdentityRepo struct{ s *Store }

type lookupRecord struct {
	ID string `json:"id"`
}

func NewIdentityRepo(store *Store) *IdentityRepo { return &IdentityRepo{s: store} }

func (r *IdentityRepo) CreateUser(ctx context.Context, user entities.User) error {
	return r.s.mutate(ctx, "user_username:"+user.NormalizedUsername, func() error {
		if _, err := get[lookupRecord](ctx, r.s, "user_username", user.NormalizedUsername); err == nil {
			return entities.ErrConflict
		} else if !errors.Is(err, entities.ErrNotFound) {
			return err
		}
		if err := r.s.put(ctx, "user", user.ID, user); err != nil {
			return err
		}
		return r.s.put(ctx, "user_username", user.NormalizedUsername, lookupRecord{ID: user.ID})
	})
}

func (r *IdentityRepo) UserByID(ctx context.Context, id string) (*entities.User, error) {
	user, err := get[entities.User](ctx, r.s, "user", id)
	return user, err
}

func (r *IdentityRepo) UserByNormalizedUsername(ctx context.Context, normalized string) (*entities.User, error) {
	lookup, err := get[lookupRecord](ctx, r.s, "user_username", normalized)
	if err != nil {
		return nil, err
	}
	return r.UserByID(ctx, lookup.ID)
}

func (r *IdentityRepo) ListUsers(ctx context.Context, query entities.PageQuery) ([]entities.User, string, error) {
	users, err := list[entities.User](ctx, r.s, "user")
	if err != nil {
		return nil, "", err
	}
	filtered := users[:0]
	needle := strings.ToLower(strings.TrimSpace(query.Query))
	cursor := decodeConfigCursor(query.Cursor)
	for _, user := range users {
		if cursor != "" && user.ID >= cursor || needle != "" && !strings.Contains(user.NormalizedUsername, needle) || query.Status != "" && user.Status != query.Status {
			continue
		}
		filtered = append(filtered, user)
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].ID > filtered[j].ID })
	return pageUsers(filtered, boundedConfigLimit(query.Limit))
}

func (r *IdentityRepo) UpdateUserStatus(ctx context.Context, id, status string, updatedAt time.Time) error {
	return r.s.mutate(ctx, "user:"+id, func() error {
		user, err := r.UserByID(ctx, id)
		if err != nil {
			return err
		}
		user.Status, user.UpdatedAt = status, updatedAt
		return r.s.put(ctx, "user", id, user)
	})
}

func (r *IdentityRepo) DeleteUserCascade(ctx context.Context, id string) error {
	return r.s.mutate(ctx, "user-delete:"+id, func() error {
		user, err := r.UserByID(ctx, id)
		if err != nil {
			return err
		}
		keys, err := list[storedAPIKey](ctx, r.s, "api_key")
		if err != nil {
			return err
		}
		for _, key := range keys {
			if key.OwnerUserID != id && key.CredentialOwnerUserID != id {
				continue
			}
			if err = r.s.del(ctx, "api_key", key.ID); err != nil && !errors.Is(err, entities.ErrNotFound) {
				return err
			}
			if err = r.s.del(ctx, "api_key_hash", key.Hash); err != nil && !errors.Is(err, entities.ErrNotFound) {
				return err
			}
		}
		credentials, err := list[storedCredential](ctx, r.s, "credential")
		if err != nil {
			return err
		}
		credentialIDs := map[string]bool{}
		for _, credential := range credentials {
			if credential.OwnerUserID == id {
				credentialIDs[credential.ID] = true
			}
		}
		models, err := list[entities.ModelDef](ctx, r.s, "model")
		if err != nil {
			return err
		}
		for _, model := range models {
			routes := model.Routes[:0]
			for _, route := range model.Routes {
				if !credentialIDs[route.CredentialID] {
					routes = append(routes, route)
				}
			}
			if len(routes) != len(model.Routes) {
				model.Routes = routes
				if err = r.s.put(ctx, "model", model.Name, model); err != nil {
					return err
				}
			}
		}
		for credentialID := range credentialIDs {
			if err = r.s.del(ctx, "provider_quota", credentialID); err != nil && !errors.Is(err, entities.ErrNotFound) {
				return err
			}
			if err = r.s.del(ctx, "credential", credentialID); err != nil && !errors.Is(err, entities.ErrNotFound) {
				return err
			}
		}
		memberships, err := r.ListMembershipsForUser(ctx, id)
		if err != nil {
			return err
		}
		for _, membership := range memberships {
			if err = r.s.del(ctx, "organization_membership", membershipKey(membership.OrganizationID, id)); err != nil {
				return err
			}
		}
		if err = r.s.del(ctx, "user_username", user.NormalizedUsername); err != nil {
			return err
		}
		return r.s.del(ctx, "user", id)
	})
}

func (r *IdentityRepo) CreateOrganization(ctx context.Context, organization entities.Organization) error {
	return r.s.mutate(ctx, "organization_name:"+organization.NormalizedName, func() error {
		if _, err := get[lookupRecord](ctx, r.s, "organization_name", organization.NormalizedName); err == nil {
			return entities.ErrConflict
		} else if !errors.Is(err, entities.ErrNotFound) {
			return err
		}
		if err := r.s.put(ctx, "organization", organization.ID, organization); err != nil {
			return err
		}
		return r.s.put(ctx, "organization_name", organization.NormalizedName, lookupRecord{ID: organization.ID})
	})
}

func (r *IdentityRepo) OrganizationByID(ctx context.Context, id string) (*entities.Organization, error) {
	organization, err := get[entities.Organization](ctx, r.s, "organization", id)
	if err == nil {
		return organization, nil
	}
	legacy, legacyErr := get[entities.Tenant](ctx, r.s, "tenant", id)
	if legacyErr != nil {
		return nil, err
	}
	name, normalized, _ := entities.NormalizeOrganizationName(legacy.Name)
	return &entities.Organization{ID: legacy.ID, Name: name, NormalizedName: normalized, Status: entities.StatusActive, CreatedAt: legacy.CreatedAt, UpdatedAt: legacy.CreatedAt}, nil
}

func (r *IdentityRepo) OrganizationByNormalizedName(ctx context.Context, normalized string) (*entities.Organization, error) {
	lookup, err := get[lookupRecord](ctx, r.s, "organization_name", normalized)
	if err == nil {
		return r.OrganizationByID(ctx, lookup.ID)
	}
	legacy, legacyErr := list[entities.Tenant](ctx, r.s, "tenant")
	if legacyErr == nil {
		for _, tenant := range legacy {
			if strings.EqualFold(strings.TrimSpace(tenant.Name), normalized) {
				return r.OrganizationByID(ctx, tenant.ID)
			}
		}
	}
	return nil, err
}

func (r *IdentityRepo) ListOrganizations(ctx context.Context, query entities.PageQuery) ([]entities.Organization, string, error) {
	organizations, err := list[entities.Organization](ctx, r.s, "organization")
	if err != nil {
		return nil, "", err
	}
	legacy, _ := list[entities.Tenant](ctx, r.s, "tenant")
	seen := make(map[string]struct{}, len(organizations))
	for _, organization := range organizations {
		seen[organization.ID] = struct{}{}
	}
	for _, tenant := range legacy {
		if _, ok := seen[tenant.ID]; ok {
			continue
		}
		name, normalized, _ := entities.NormalizeOrganizationName(tenant.Name)
		organizations = append(organizations, entities.Organization{ID: tenant.ID, Name: name, NormalizedName: normalized, Status: entities.StatusActive, CreatedAt: tenant.CreatedAt, UpdatedAt: tenant.CreatedAt})
	}
	filtered := organizations[:0]
	needle, cursor := strings.ToLower(strings.TrimSpace(query.Query)), decodeConfigCursor(query.Cursor)
	for _, organization := range organizations {
		if cursor != "" && organization.ID >= cursor || needle != "" && !strings.Contains(organization.NormalizedName, needle) || query.Status != "" && organization.Status != query.Status {
			continue
		}
		filtered = append(filtered, organization)
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].ID > filtered[j].ID })
	limit := boundedConfigLimit(query.Limit)
	var next string
	if len(filtered) > limit {
		next = encodeConfigCursor(filtered[limit-1].ID)
		filtered = filtered[:limit]
	}
	return filtered, next, nil
}

func (r *IdentityRepo) UpdateOrganization(ctx context.Context, organization entities.Organization) error {
	return r.s.mutate(ctx, "organization:"+organization.ID, func() error {
		current, err := r.OrganizationByID(ctx, organization.ID)
		if err != nil {
			return err
		}
		if current.NormalizedName != organization.NormalizedName {
			if _, err := get[lookupRecord](ctx, r.s, "organization_name", organization.NormalizedName); err == nil {
				return entities.ErrConflict
			} else if !errors.Is(err, entities.ErrNotFound) {
				return err
			}
			if err := r.s.put(ctx, "organization_name", organization.NormalizedName, lookupRecord{ID: organization.ID}); err != nil {
				return err
			}
			_ = r.s.del(ctx, "organization_name", current.NormalizedName)
		}
		return r.s.put(ctx, "organization", organization.ID, organization)
	})
}

func membershipKey(organizationID, userID string) string { return organizationID + ":" + userID }

func (r *IdentityRepo) PutMembership(ctx context.Context, membership entities.Membership) error {
	key := membershipKey(membership.OrganizationID, membership.UserID)
	return r.s.mutate(ctx, "organization_membership:"+key, func() error {
		if _, err := r.OrganizationByID(ctx, membership.OrganizationID); err != nil {
			return err
		}
		if _, err := r.UserByID(ctx, membership.UserID); err != nil {
			return err
		}
		return r.s.put(ctx, "organization_membership", key, membership)
	})
}

func (r *IdentityRepo) Membership(ctx context.Context, organizationID, userID string) (*entities.Membership, error) {
	return get[entities.Membership](ctx, r.s, "organization_membership", membershipKey(organizationID, userID))
}

func (r *IdentityRepo) ListMemberships(ctx context.Context, organizationID string) ([]entities.Membership, error) {
	all, err := list[entities.Membership](ctx, r.s, "organization_membership")
	out := all[:0]
	for _, membership := range all {
		if membership.OrganizationID == organizationID {
			out = append(out, membership)
		}
	}
	return out, err
}

func (r *IdentityRepo) ListMembershipsForUser(ctx context.Context, userID string) ([]entities.Membership, error) {
	all, err := list[entities.Membership](ctx, r.s, "organization_membership")
	out := all[:0]
	for _, membership := range all {
		if membership.UserID == userID {
			out = append(out, membership)
		}
	}
	return out, err
}

func (r *IdentityRepo) CountActiveOrganizationAdmins(ctx context.Context, organizationID string) (int, error) {
	organization, err := r.OrganizationByID(ctx, organizationID)
	if err != nil || organization.Status != entities.StatusActive {
		return 0, err
	}
	memberships, err := r.ListMemberships(ctx, organizationID)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, membership := range memberships {
		if membership.Role != entities.MembershipAdmin {
			continue
		}
		user, userErr := r.UserByID(ctx, membership.UserID)
		if userErr == nil && user.Status == entities.StatusActive {
			count++
		}
	}
	return count, nil
}

func (r *IdentityRepo) DeleteMembership(ctx context.Context, organizationID, userID string) error {
	key := membershipKey(organizationID, userID)
	return r.s.mutate(ctx, "organization_membership:"+key, func() error { return r.s.del(ctx, "organization_membership", key) })
}

func (r *IdentityRepo) ChangeMembershipRoleAtomic(ctx context.Context, organizationID, userID, role string) (bool, error) {
	lastAdmin := false
	err := r.s.mutate(ctx, "organization_memberships:"+organizationID, func() error {
		membership, err := r.Membership(ctx, organizationID, userID)
		if err != nil {
			return err
		}
		if membership.Role == entities.MembershipAdmin && role != entities.MembershipAdmin {
			count, countErr := r.CountActiveOrganizationAdmins(ctx, organizationID)
			if countErr != nil {
				return countErr
			}
			if count <= 1 {
				lastAdmin = true
				return nil
			}
		}
		membership.Role = role
		return r.s.put(ctx, "organization_membership", membershipKey(organizationID, userID), membership)
	})
	return lastAdmin, err
}
func (r *IdentityRepo) DeleteMembershipAtomic(ctx context.Context, organizationID, userID string) (bool, error) {
	lastAdmin := false
	err := r.s.mutate(ctx, "organization_memberships:"+organizationID, func() error {
		membership, err := r.Membership(ctx, organizationID, userID)
		if err != nil {
			return err
		}
		if membership.Role == entities.MembershipAdmin {
			count, countErr := r.CountActiveOrganizationAdmins(ctx, organizationID)
			if countErr != nil {
				return countErr
			}
			if count <= 1 {
				lastAdmin = true
				return nil
			}
		}
		return r.s.del(ctx, "organization_membership", membershipKey(organizationID, userID))
	})
	return lastAdmin, err
}

func boundedConfigLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	if limit > 500 {
		return 500
	}
	return limit
}
func encodeConfigCursor(id string) string { return base64.RawURLEncoding.EncodeToString([]byte(id)) }
func decodeConfigCursor(value string) string {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return ""
	}
	return string(decoded)
}
func pageUsers(users []entities.User, limit int) ([]entities.User, string, error) {
	var next string
	if len(users) > limit {
		next = encodeConfigCursor(users[limit-1].ID)
		users = users[:limit]
	}
	return users, next, nil
}
