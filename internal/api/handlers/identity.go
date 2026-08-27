package handlers

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	responseapi "github.com/kimnt93/gorouter/internal/api"
	"github.com/kimnt93/gorouter/pkg/apikey"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/policy"
)

func principalFromSession(session *entities.Session) entities.Principal {
	if session == nil {
		return entities.Principal{}
	}
	typeName := session.PrincipalType
	if typeName == "" {
		if session.IsMaster() {
			typeName = entities.PrincipalMaster
		} else {
			typeName = entities.PrincipalOrganization
		}
	}
	return entities.Principal{Type: typeName, KeyID: session.KeyID, UserID: session.UserID, Username: session.Username, OrganizationID: session.OrganizationID, MembershipRole: session.MembershipRole, Scopes: append([]string(nil), session.Scopes...)}
}

func pageQuery(c fiber.Ctx) entities.PageQuery {
	limit, _ := strconv.Atoi(c.Query("limit", "100"))
	return entities.PageQuery{Cursor: c.Query("cursor"), Limit: limit, Query: c.Query("q"), Status: c.Query("status")}
}

// Users lists or creates users.
// @Summary List or create users
// @Tags users
// @Security BearerAuth
// @Param cursor query string false "Opaque cursor"
// @Param limit query int false "Page size" default(100) maximum(500)
// @Param q query string false "Username search"
// @Param status query string false "Status filter"
// @Param request body UserCreateRequest false "Required for POST"
// @Success 200 {object} UserListResponse
// @Success 201 {object} UserCreateResponse
// @Failure 400,401,403,409,500 {object} responseapi.ErrorResponse
// @Router /admin/users [get]
// @Router /admin/users [post]
func (a *Admin) Users(c fiber.Ctx) error {
	actor := principalFromSession(SessionFrom(c))
	if err := policy.ManageUsers(actor); err != nil {
		return responseapi.For(c).Forbidden("only master may manage users").Send()
	}
	if a.IdentitySvc == nil || a.IdentityRepo == nil {
		return responseapi.For(c).InternalError("identity service unavailable").Send()
	}
	if c.Method() == fiber.MethodGet {
		users, next, err := a.IdentityRepo.ListUsers(c.Context(), pageQuery(c))
		if err != nil {
			return responseapi.For(c).InternalError("failed to list users").Send()
		}
		return responseapi.For(c).Response().
			Status(fiber.StatusOK).
			Object("list").
			Data(users).
			Next(next).
			Send()
	}
	var body UserCreateRequest
	if err := c.Bind().Body(&body); err != nil {
		return responseapi.For(c).BadRequest("invalid user request").Send()
	}
	if body.GenerateInitialKey {
		name := strings.TrimSpace(body.InitialKey.Name)
		if name == "" {
			name = "Initial login key"
		}
		user, key, err := a.IdentitySvc.CreateUserWithInitialKey(c.Context(), actor, body.Username, a.KeysSvc, apikey.CreateInput{Name: name, Models: body.InitialKey.Models, Scopes: body.InitialKey.Scopes})
		if err != nil {
			return identityError(c, err)
		}
		initial := keyCreatedResponse(key)
		return responseapi.For(c).Response().Status(fiber.StatusCreated).Data(UserCreateResponse{User: user, InitialKey: &initial}).Send()
	}
	user, err := a.IdentitySvc.CreateUser(c.Context(), actor, body.Username)
	if err != nil {
		return identityError(c, err)
	}
	return responseapi.For(c).Response().Status(fiber.StatusCreated).Data(UserCreateResponse{User: user}).Send()
}

// UserByID returns user detail or changes user status.
// @Summary Get or update a user
// @Tags users
// @Security BearerAuth
// @Param id path string true "User ID"
// @Param request body UserStatusRequest false "Required for PATCH"
// @Success 200 {object} UserDetailResponse
// @Failure 400,401,403,404,500 {object} responseapi.ErrorResponse
// @Router /admin/users/{id} [get]
// @Router /admin/users/{id} [patch]
func (a *Admin) UserByID(c fiber.Ctx) error {
	actor := principalFromSession(SessionFrom(c))
	if err := policy.ManageUsers(actor); err != nil {
		return responseapi.For(c).Forbidden("only master may manage users").Send()
	}
	user, err := a.IdentityRepo.UserByID(c.Context(), c.Params("id"))
	if err != nil {
		return identityError(c, err)
	}
	if c.Method() == fiber.MethodGet {
		memberships, memberErr := a.IdentityRepo.ListMembershipsForUser(c.Context(), user.ID)
		if memberErr != nil {
			return responseapi.For(c).InternalError("failed to load user memberships").Send()
		}
		keys, keyErr := a.KeysSvc.List(c.Context())
		if keyErr != nil {
			return responseapi.For(c).InternalError("failed to load user keys").Send()
		}
		ownedKeys := make([]entities.ApiKey, 0)
		for _, key := range keys {
			if key.OwnerUserID == user.ID {
				ownedKeys = append(ownedKeys, key)
			}
		}
		since := time.Now().UTC().Add(-30 * 24 * time.Hour)
		usageQuery := entities.UsageQuery{Visibility: entities.UsageVisibility{PrincipalType: entities.PrincipalMaster}, UserID: user.ID, Since: &since, Limit: 20}
		summary, usageErr := a.UsageSvc.SummaryQuery(c.Context(), usageQuery)
		if usageErr != nil {
			return responseapi.For(c).InternalError("failed to load user usage").Send()
		}
		recent, recentErr := a.UsageSvc.Query(c.Context(), usageQuery)
		if recentErr != nil {
			return responseapi.For(c).InternalError("failed to load recent user activity").Send()
		}
		return responseapi.For(c).Response().Status(fiber.StatusOK).Data(UserDetailResponse{User: user, Memberships: memberships, Keys: ownedKeys, Usage: summary, Recent: recent.Data}).Send()
	}
	var body UserStatusRequest
	if err = c.Bind().Body(&body); err != nil {
		return responseapi.For(c).BadRequest("invalid user update").Send()
	}
	if err = a.IdentitySvc.SetUserStatus(c.Context(), actor, user.ID, body.Status); err != nil {
		return identityError(c, err)
	}
	return responseapi.For(c).Response().Status(fiber.StatusOK).Data(okResponse{OK: true}).Send()
}

// Organizations lists visible organizations or creates one as master.
// @Summary List or create organizations
// @Tags organizations
// @Security BearerAuth
// @Param cursor query string false "Opaque cursor"
// @Param limit query int false "Page size" default(100) maximum(500)
// @Param q query string false "Name search"
// @Param status query string false "Status filter"
// @Param request body OrganizationCreateRequest false "Required for POST"
// @Success 200 {object} OrganizationListResponse
// @Success 201 {object} entities.Organization
// @Failure 400,401,403,409,500 {object} responseapi.ErrorResponse
// @Router /admin/organizations [get]
// @Router /admin/organizations [post]
func (a *Admin) Organizations(c fiber.Ctx) error {
	actor := principalFromSession(SessionFrom(c))
	if a.IdentityRepo == nil {
		return responseapi.For(c).InternalError("identity service unavailable").Send()
	}
	if c.Method() == fiber.MethodPost {
		if err := policy.ManageOrganizations(actor); err != nil {
			return responseapi.For(c).Forbidden("only master may create organizations").Send()
		}
		var body OrganizationCreateRequest
		if err := c.Bind().Body(&body); err != nil {
			return responseapi.For(c).BadRequest("invalid organization request").Send()
		}
		organization, err := a.IdentitySvc.CreateOrganization(c.Context(), actor, body.Name)
		if err != nil {
			return identityError(c, err)
		}
		return responseapi.For(c).Response().Status(fiber.StatusCreated).Data(organization).Send()
	}
	organizations, next, err := a.IdentityRepo.ListOrganizations(c.Context(), pageQuery(c))
	if err != nil {
		return responseapi.For(c).InternalError("failed to list organizations").Send()
	}
	membershipRoles := map[string]string{}
	if actor.Type != entities.PrincipalMaster {
		allowed := map[string]bool{}
		if actor.Type == entities.PrincipalOrganization {
			allowed[actor.OrganizationID] = true
			membershipRoles[actor.OrganizationID] = entities.MembershipAdmin
		} else if actor.OrganizationID != "" {
			allowed[actor.OrganizationID] = true
			membershipRoles[actor.OrganizationID] = actor.MembershipRole
		} else {
			memberships, _ := a.IdentityRepo.ListMembershipsForUser(c.Context(), actor.UserID)
			for _, membership := range memberships {
				allowed[membership.OrganizationID] = true
				membershipRoles[membership.OrganizationID] = membership.Role
			}
		}
		filtered := organizations[:0]
		for _, organization := range organizations {
			if allowed[organization.ID] {
				filtered = append(filtered, organization)
			}
		}
		organizations = filtered
		next = ""
	}
	items := make([]OrganizationListItem, 0, len(organizations))
	for _, organization := range organizations {
		members, memberErr := a.IdentityRepo.ListMemberships(c.Context(), organization.ID)
		if memberErr != nil {
			return responseapi.For(c).InternalError("failed to count organization members").Send()
		}
		items = append(items, OrganizationListItem{Organization: organization, MemberCount: len(members), MembershipRole: membershipRoles[organization.ID]})
	}
	return responseapi.For(c).Response().
		Status(fiber.StatusOK).
		Object("list").
		Data(items).
		Next(next).
		Send()
}

// OrganizationByID returns or updates an organization.
// @Summary Get or update an organization
// @Tags organizations
// @Security BearerAuth
// @Param id path string true "Organization ID"
// @Param request body OrganizationUpdateRequest false "Required for PATCH"
// @Success 200 {object} OrganizationDetailResponse
// @Failure 400,401,403,404,409,500 {object} responseapi.ErrorResponse
// @Router /admin/organizations/{id} [get]
// @Router /admin/organizations/{id} [patch]
func (a *Admin) OrganizationByID(c fiber.Ctx) error {
	actor := principalFromSession(SessionFrom(c))
	id := c.Params("id")
	if actor.Type == entities.PrincipalUser && actor.OrganizationID != id {
		// An organization-scoped key is fixed to its signed context. A personal
		// key may select any current membership without mutating key context.
		if actor.OrganizationID == "" {
			if membership, err := a.IdentityRepo.Membership(c.Context(), id, actor.UserID); err == nil {
				actor.OrganizationID = id
				actor.MembershipRole = membership.Role
			}
		}
	}
	if err := policy.ViewOrganization(actor, id); err != nil {
		return responseapi.For(c).NotFound("organization not found").Send()
	}
	if c.Method() == fiber.MethodGet {
		organization, err := a.IdentityRepo.OrganizationByID(c.Context(), id)
		if err != nil {
			return identityError(c, err)
		}
		var own *entities.Membership
		if actor.Type == entities.PrincipalUser {
			own, _ = a.IdentityRepo.Membership(c.Context(), id, actor.UserID)
		}
		return responseapi.For(c).Response().Status(fiber.StatusOK).Data(OrganizationDetailResponse{Organization: organization, Membership: own}).Send()
	}
	var body OrganizationUpdateRequest
	if err := c.Bind().Body(&body); err != nil {
		return responseapi.For(c).BadRequest("invalid organization update").Send()
	}
	organization, err := a.IdentitySvc.UpdateOrganization(c.Context(), actor, id, body.Name, body.Status)
	if err != nil {
		return identityError(c, err)
	}
	return responseapi.For(c).Response().Status(fiber.StatusOK).Data(organization).Send()
}

// Members lists or adds organization memberships.
// @Summary List or add organization members
// @Tags memberships
// @Security BearerAuth
// @Param id path string true "Organization ID"
// @Param request body MembershipCreateRequest false "Required for POST"
// @Success 200 {object} MembershipListResponse
// @Success 201 {object} entities.Membership
// @Failure 400,401,403,404,409,500 {object} responseapi.ErrorResponse
// @Router /admin/organizations/{id}/members [get]
// @Router /admin/organizations/{id}/members [post]
func (a *Admin) Members(c fiber.Ctx) error {
	actor := principalFromSession(SessionFrom(c))
	organizationID := c.Params("id")
	if err := policy.ManageMembers(actor, organizationID); err != nil {
		return responseapi.For(c).Forbidden("membership administration is not allowed").Send()
	}
	if c.Method() == fiber.MethodGet {
		members, err := a.IdentityRepo.ListMemberships(c.Context(), organizationID)
		if err != nil {
			return responseapi.For(c).InternalError("failed to list members").Send()
		}
		items := make([]MembershipListItem, 0, len(members))
		for _, member := range members {
			item := MembershipListItem{Membership: member}
			if user, userErr := a.IdentityRepo.UserByID(c.Context(), member.UserID); userErr == nil {
				item.Username = user.Username
			}
			items = append(items, item)
		}
		return responseapi.For(c).Response().
			Status(fiber.StatusOK).
			Object("list").
			Data(items).
			Send()
	}
	var body MembershipCreateRequest
	if err := c.Bind().Body(&body); err != nil {
		return responseapi.For(c).BadRequest("invalid membership request").Send()
	}
	membership, err := a.IdentitySvc.AddMembership(c.Context(), actor, organizationID, body.UserID, body.Role)
	if err != nil {
		return identityError(c, err)
	}
	return responseapi.For(c).Response().Status(fiber.StatusCreated).Data(membership).Send()
}

// MemberByID changes a membership role or removes it.
// @Summary Update or remove an organization member
// @Tags memberships
// @Security BearerAuth
// @Param id path string true "Organization ID"
// @Param user_id path string true "User ID"
// @Param request body MembershipUpdateRequest false "Required for PATCH"
// @Success 200 {object} OKResponse
// @Failure 400,401,403,404,409,500 {object} responseapi.ErrorResponse
// @Router /admin/organizations/{id}/members/{user_id} [patch]
// @Router /admin/organizations/{id}/members/{user_id} [delete]
func (a *Admin) MemberByID(c fiber.Ctx) error {
	actor := principalFromSession(SessionFrom(c))
	organizationID, userID := c.Params("id"), c.Params("user_id")
	if err := policy.ManageMembers(actor, organizationID); err != nil {
		return responseapi.For(c).Forbidden("membership administration is not allowed").Send()
	}
	var err error
	if c.Method() == fiber.MethodDelete {
		err = a.IdentitySvc.RemoveMembership(c.Context(), actor, organizationID, userID)
	} else {
		var body MembershipUpdateRequest
		if bindErr := c.Bind().Body(&body); bindErr != nil {
			return responseapi.For(c).BadRequest("invalid membership update").Send()
		}
		err = a.IdentitySvc.ChangeMembershipRole(c.Context(), actor, organizationID, userID, body.Role)
	}
	if err != nil {
		return identityError(c, err)
	}
	return responseapi.For(c).Response().Status(fiber.StatusOK).Data(okResponse{OK: true}).Send()
}

// AuditEvents returns principal-filtered administrative audit history.
// @Summary List audit events
// @Tags audit
// @Security BearerAuth
// @Param cursor query string false "Opaque cursor"
// @Param limit query int false "Page size" default(100) maximum(500)
// @Param since query string false "RFC3339 lower time bound"
// @Param until query string false "RFC3339 upper time bound"
// @Param organization_id query string false "Organization filter"
// @Param actor_id query string false "Actor ID"
// @Param action query string false "Action"
// @Param target_type query string false "Target type"
// @Param target_id query string false "Target ID"
// @Success 200 {object} AuditListResponse
// @Failure 400,401,403,500 {object} responseapi.ErrorResponse
// @Router /admin/audit/events [get]
func (a *Admin) AuditEvents(c fiber.Ctx) error {
	actor := principalFromSession(SessionFrom(c))
	requestedOrganization := strings.TrimSpace(c.Query("organization_id"))
	if actor.Type == entities.PrincipalUser && requestedOrganization != "" && actor.OrganizationID == "" {
		if membership, membershipErr := a.IdentityRepo.Membership(c.Context(), requestedOrganization, actor.UserID); membershipErr == nil {
			actor.OrganizationID, actor.MembershipRole = requestedOrganization, membership.Role
		}
	}
	visibility, err := policy.AuditVisibility(actor)
	if err != nil {
		return responseapi.For(c).Forbidden("audit access is not allowed").Send()
	}
	limit, _ := strconv.Atoi(c.Query("limit", "100"))
	query := entities.AuditQuery{Visibility: visibility, OrganizationID: c.Query("organization_id"), Cursor: c.Query("cursor"), Limit: limit, ActorID: c.Query("actor_id"), Action: c.Query("action"), TargetType: c.Query("target_type"), TargetID: c.Query("target_id")}
	if value := c.Query("since"); value != "" {
		parsed, parseErr := time.Parse(time.RFC3339, value)
		if parseErr != nil {
			return responseapi.For(c).BadRequest("since must be RFC3339").Send()
		}
		query.Since = &parsed
	}
	if value := c.Query("until"); value != "" {
		parsed, parseErr := time.Parse(time.RFC3339, value)
		if parseErr != nil {
			return responseapi.For(c).BadRequest("until must be RFC3339").Send()
		}
		query.Until = &parsed
	}
	page, err := a.AuditRepo.QueryAudit(c.Context(), query)
	if err != nil {
		return responseapi.For(c).InternalError("failed to load audit events").Send()
	}
	return responseapi.For(c).Response().
		Status(fiber.StatusOK).
		Object("list").
		Data(page.Data).
		Next(page.NextCursor).
		Send()
}

func identityError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, entities.ErrNotFound):
		return responseapi.For(c).NotFound("resource not found").Send()
	case errors.Is(err, entities.ErrConflict):
		return responseapi.For(c).Error(fiber.StatusConflict, "resource already exists", "conflict", "duplicate").Send()
	case errors.Is(err, entities.ErrInvalidUsername), errors.Is(err, entities.ErrInvalidName), errors.Is(err, entities.ErrInvalidStatus), errors.Is(err, entities.ErrInvalidRole), errors.Is(err, entities.ErrInvalidOwnership):
		return responseapi.For(c).BadRequest(err.Error()).Send()
	default:
		return responseapi.For(c).BadRequest(err.Error()).Send()
	}
}
