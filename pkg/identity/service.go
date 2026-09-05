package identity

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/kimnt93/gorouter/pkg/apikey"
	"github.com/kimnt93/gorouter/pkg/entities"
)

var (
	ErrLastAdmin        = errors.New("cannot remove or demote the last active organization administrator")
	ErrInactiveUser     = errors.New("user is disabled")
	ErrInactiveOrg      = errors.New("organization is disabled")
	ErrMembershipNeeded = errors.New("active organization membership is required")
)

type Repository interface {
	entities.UserRepository
	entities.OrganizationRepository
	entities.MembershipRepository
}
type atomicMembershipRepository interface {
	ChangeMembershipRoleAtomic(context.Context, string, string, string) (bool, error)
	DeleteMembershipAtomic(context.Context, string, string) (bool, error)
}

type Service struct {
	repo  Repository
	audit entities.AuditRepository
	now   func() time.Time
	cache AuthorizationCache
}
type AuthorizationCache interface {
	InvalidateUser(context.Context, string) error
	InvalidateOrganization(context.Context, string) error
}

func NewService(repo Repository, audit entities.AuditRepository) *Service {
	return &Service{repo: repo, audit: audit, now: func() time.Time { return time.Now().UTC() }}
}
func (s *Service) SetAuthorizationCache(cache AuthorizationCache) { s.cache = cache }

func (s *Service) CreateUser(ctx context.Context, actor entities.Principal, username string) (*entities.User, error) {
	user, err := s.prepareUser(actor, username)
	if err != nil {
		return nil, err
	}
	if err := s.repo.CreateUser(ctx, *user); err != nil {
		return nil, err
	}
	if err := s.appendAudit(ctx, actor, "user.create", "user", user.ID, "", map[string]string{"username": user.NormalizedUsername}); err != nil {
		return nil, err
	}
	return user, nil
}

func (s *Service) CreateUserWithInitialKey(ctx context.Context, actor entities.Principal, username string, keys *apikey.Service, input apikey.CreateInput) (*entities.User, *entities.ApiKey, error) {
	user, err := s.prepareUser(actor, username)
	if err != nil {
		return nil, nil, err
	}
	if keys == nil {
		return nil, nil, errors.New("API key service unavailable")
	}
	audits := []entities.AuditEvent(nil)
	if s.audit != nil {
		now := s.now()
		label := actor.Username
		if actor.Type == entities.PrincipalMaster {
			label = "master"
		}
		audits = []entities.AuditEvent{
			{ID: entities.NewID("audit"), TS: now, ActorType: actor.Type, ActorID: actorID(actor), ActorLabel: label, Action: "user.create", TargetType: "user", TargetID: user.ID, SafeMetadata: map[string]string{"username": user.NormalizedUsername}},
			{ID: entities.NewID("audit"), TS: now, ActorType: actor.Type, ActorID: actorID(actor), ActorLabel: label, Action: "key.create", TargetType: "api_key", SafeMetadata: map[string]string{"name": strings.TrimSpace(input.Name), "owner_type": entities.OwnerUser}},
		}
	}
	key, err := keys.CreateUserWithInitialKey(ctx, *user, input, audits)
	if err != nil {
		return nil, nil, err
	}
	return user, key, nil
}

func (s *Service) prepareUser(actor entities.Principal, username string) (*entities.User, error) {
	if actor.Type != entities.PrincipalMaster {
		return nil, errors.New("forbidden")
	}
	normalized, err := entities.NormalizeUsername(username)
	if err != nil {
		return nil, err
	}
	now := s.now()
	user := &entities.User{ID: entities.NewID("usr"), Username: normalized, NormalizedUsername: normalized, Status: entities.StatusActive, CreatedAt: now, UpdatedAt: now}
	return user, nil
}

func (s *Service) SetUserStatus(ctx context.Context, actor entities.Principal, id, status string) error {
	if actor.Type != entities.PrincipalMaster {
		return errors.New("forbidden")
	}
	status = strings.ToLower(strings.TrimSpace(status))
	if !entities.ValidStatus(status) {
		return entities.ErrInvalidStatus
	}
	if err := s.repo.UpdateUserStatus(ctx, id, status, s.now()); err != nil {
		return err
	}
	if s.cache != nil {
		if err := s.cache.InvalidateUser(ctx, id); err != nil {
			return err
		}
	}
	return s.appendAudit(ctx, actor, "user.status", "user", id, "", map[string]string{"status": status})
}

func (s *Service) DeleteUser(ctx context.Context, actor entities.Principal, id string) error {
	if actor.Type != entities.PrincipalMaster {
		return errors.New("forbidden")
	}
	id = strings.TrimSpace(id)
	user, err := s.repo.UserByID(ctx, id)
	if err != nil {
		return err
	}
	if s.cache != nil {
		if err = s.cache.InvalidateUser(ctx, id); err != nil {
			return err
		}
	}
	if err = s.repo.DeleteUserCascade(ctx, id); err != nil {
		return err
	}
	return s.appendAudit(ctx, actor, "user.delete", "user", id, "", map[string]string{"username": user.NormalizedUsername})
}

func (s *Service) CreateOrganization(ctx context.Context, actor entities.Principal, name string) (*entities.Organization, error) {
	if actor.Type != entities.PrincipalMaster && (actor.Type != entities.PrincipalUser || actor.UserID == "") {
		return nil, errors.New("forbidden")
	}
	name, normalized, err := entities.NormalizeOrganizationName(name)
	if err != nil {
		return nil, err
	}
	now := s.now()
	organization := &entities.Organization{ID: entities.NewID("org"), Name: name, NormalizedName: normalized, Status: entities.StatusActive, CreatedAt: now, UpdatedAt: now}
	if err := s.repo.CreateOrganization(ctx, *organization); err != nil {
		return nil, err
	}
	if actor.Type == entities.PrincipalUser {
		membership := entities.Membership{OrganizationID: organization.ID, UserID: actor.UserID, Role: entities.MembershipAdmin, CreatedAt: now, CreatedByActorType: actor.Type, CreatedByActorID: actor.UserID}
		if err := s.repo.PutMembership(ctx, membership); err != nil {
			return nil, err
		}
	}
	if err := s.appendAudit(ctx, actor, "organization.create", "organization", organization.ID, organization.ID, map[string]string{"name": name}); err != nil {
		return nil, err
	}
	return organization, nil
}

func (s *Service) UpdateOrganization(ctx context.Context, actor entities.Principal, id, name, status string) (*entities.Organization, error) {
	if actor.Type != entities.PrincipalMaster {
		return nil, errors.New("forbidden")
	}
	organization, err := s.repo.OrganizationByID(ctx, id)
	if err != nil {
		return nil, err
	}
	metadata := map[string]string{}
	if strings.TrimSpace(name) != "" {
		display, normalized, normalizeErr := entities.NormalizeOrganizationName(name)
		if normalizeErr != nil {
			return nil, normalizeErr
		}
		organization.Name, organization.NormalizedName = display, normalized
		metadata["name"] = display
	}
	if strings.TrimSpace(status) != "" {
		status = strings.ToLower(strings.TrimSpace(status))
		if !entities.ValidStatus(status) {
			return nil, entities.ErrInvalidStatus
		}
		organization.Status = status
		metadata["status"] = status
	}
	organization.UpdatedAt = s.now()
	if err = s.repo.UpdateOrganization(ctx, *organization); err != nil {
		return nil, err
	}
	if s.cache != nil {
		if err = s.cache.InvalidateOrganization(ctx, id); err != nil {
			return nil, err
		}
	}
	if err = s.appendAudit(ctx, actor, "organization.update", "organization", id, id, metadata); err != nil {
		return nil, err
	}
	return organization, nil
}

func (s *Service) AddMembership(ctx context.Context, actor entities.Principal, organizationID, userID, role string) (*entities.Membership, error) {
	if err := authorizeMembership(actor, organizationID); err != nil {
		return nil, err
	}
	if !entities.ValidMembershipRole(role) {
		return nil, entities.ErrInvalidRole
	}
	user, err := s.repo.UserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user.Status != entities.StatusActive {
		return nil, ErrInactiveUser
	}
	organization, err := s.repo.OrganizationByID(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	if organization.Status != entities.StatusActive {
		return nil, ErrInactiveOrg
	}
	membership := &entities.Membership{OrganizationID: organizationID, UserID: userID, Role: role, CreatedAt: s.now(), CreatedByActorType: actor.Type, CreatedByActorID: actorID(actor)}
	if err := s.repo.PutMembership(ctx, *membership); err != nil {
		return nil, err
	}
	if s.cache != nil {
		if err := s.cache.InvalidateUser(ctx, userID); err != nil {
			return nil, err
		}
		if err := s.cache.InvalidateOrganization(ctx, organizationID); err != nil {
			return nil, err
		}
	}
	if err := s.appendAudit(ctx, actor, "membership.add", "membership", organizationID+":"+userID, organizationID, map[string]string{"role": role}); err != nil {
		return nil, err
	}
	return membership, nil
}

func (s *Service) ChangeMembershipRole(ctx context.Context, actor entities.Principal, organizationID, userID, role string) error {
	if err := authorizeMembership(actor, organizationID); err != nil {
		return err
	}
	if !entities.ValidMembershipRole(role) {
		return entities.ErrInvalidRole
	}
	if repository, ok := s.repo.(atomicMembershipRepository); ok {
		lastAdmin, err := repository.ChangeMembershipRoleAtomic(ctx, organizationID, userID, role)
		if err != nil {
			return err
		}
		if lastAdmin {
			return ErrLastAdmin
		}
		if s.cache != nil {
			if err = s.cache.InvalidateUser(ctx, userID); err != nil {
				return err
			}
			if err = s.cache.InvalidateOrganization(ctx, organizationID); err != nil {
				return err
			}
		}
		return s.appendAudit(ctx, actor, "membership.role", "membership", organizationID+":"+userID, organizationID, map[string]string{"role": role})
	}
	existing, err := s.repo.Membership(ctx, organizationID, userID)
	if err != nil {
		return err
	}
	if existing.Role == entities.MembershipAdmin && role != entities.MembershipAdmin {
		if err := s.protectLastAdmin(ctx, organizationID); err != nil {
			return err
		}
	}
	existing.Role = role
	if err := s.repo.PutMembership(ctx, *existing); err != nil {
		return err
	}
	if s.cache != nil {
		if err := s.cache.InvalidateUser(ctx, userID); err != nil {
			return err
		}
		if err := s.cache.InvalidateOrganization(ctx, organizationID); err != nil {
			return err
		}
	}
	return s.appendAudit(ctx, actor, "membership.role", "membership", organizationID+":"+userID, organizationID, map[string]string{"role": role})
}

func (s *Service) RemoveMembership(ctx context.Context, actor entities.Principal, organizationID, userID string) error {
	if err := authorizeMembership(actor, organizationID); err != nil {
		return err
	}
	if repository, ok := s.repo.(atomicMembershipRepository); ok {
		lastAdmin, err := repository.DeleteMembershipAtomic(ctx, organizationID, userID)
		if err != nil {
			return err
		}
		if lastAdmin {
			return ErrLastAdmin
		}
		if s.cache != nil {
			if err = s.cache.InvalidateUser(ctx, userID); err != nil {
				return err
			}
			if err = s.cache.InvalidateOrganization(ctx, organizationID); err != nil {
				return err
			}
		}
		return s.appendAudit(ctx, actor, "membership.remove", "membership", organizationID+":"+userID, organizationID, nil)
	}
	existing, err := s.repo.Membership(ctx, organizationID, userID)
	if err != nil {
		return err
	}
	if existing.Role == entities.MembershipAdmin {
		if err := s.protectLastAdmin(ctx, organizationID); err != nil {
			return err
		}
	}
	if err := s.repo.DeleteMembership(ctx, organizationID, userID); err != nil {
		return err
	}
	if s.cache != nil {
		if err := s.cache.InvalidateUser(ctx, userID); err != nil {
			return err
		}
		if err := s.cache.InvalidateOrganization(ctx, organizationID); err != nil {
			return err
		}
	}
	return s.appendAudit(ctx, actor, "membership.remove", "membership", organizationID+":"+userID, organizationID, nil)
}

func (s *Service) ValidateUserKeyContext(ctx context.Context, userID, organizationID string) error {
	if organizationID == "" {
		return nil
	}
	user, err := s.repo.UserByID(ctx, userID)
	if err != nil || user.Status != entities.StatusActive {
		return ErrMembershipNeeded
	}
	organization, err := s.repo.OrganizationByID(ctx, organizationID)
	if err != nil || organization.Status != entities.StatusActive {
		return ErrMembershipNeeded
	}
	if _, err := s.repo.Membership(ctx, organizationID, userID); err != nil {
		return ErrMembershipNeeded
	}
	return nil
}

func (s *Service) protectLastAdmin(ctx context.Context, organizationID string) error {
	count, err := s.repo.CountActiveOrganizationAdmins(ctx, organizationID)
	if err != nil {
		return err
	}
	if count <= 1 {
		return ErrLastAdmin
	}
	return nil
}

func authorizeMembership(actor entities.Principal, organizationID string) error {
	if actor.Type == entities.PrincipalMaster {
		return nil
	}
	if !actor.HasScope(entities.ScopeMembersManage) || actor.OrganizationID != organizationID || actor.MembershipRole != entities.MembershipAdmin {
		return errors.New("forbidden")
	}
	return nil
}

func actorID(actor entities.Principal) string {
	if actor.Type == entities.PrincipalUser {
		return actor.UserID
	}
	if actor.Type == entities.PrincipalOrganization {
		return actor.OrganizationID
	}
	return entities.PrincipalMaster
}

func (s *Service) appendAudit(ctx context.Context, actor entities.Principal, action, targetType, targetID, organizationID string, metadata map[string]string) error {
	if s.audit == nil {
		return nil
	}
	label := actor.Username
	if actor.Type == entities.PrincipalMaster {
		label = "master"
	} else if actor.Type == entities.PrincipalOrganization {
		label = "org:" + actor.OrganizationName
	}
	return s.audit.AppendAudit(ctx, entities.AuditEvent{ID: entities.NewID("audit"), TS: s.now(), ActorType: actor.Type, ActorID: actorID(actor), ActorLabel: label, OrganizationID: organizationID, Action: action, TargetType: targetType, TargetID: targetID, SafeMetadata: metadata})
}
