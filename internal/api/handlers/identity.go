package handlers

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	responseapi "github.com/kimnt93/gorouter/internal/api"
	"github.com/kimnt93/gorouter/internal/api/presenter"
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
// @Failure 400,401,403,409,500 {object} presenter.Error
// @Router /admin/users [get]
// @Router /admin/users [post]
func (a *Admin) Users(c fiber.Ctx) error {
	actor := principalFromSession(SessionFrom(c))
	if err := policy.ManageUsers(actor); err != nil {
		return presenter.Forbidden(c, "only master may manage users")
	}
	if a.IdentitySvc == nil || a.IdentityRepo == nil {
		return presenter.ServerError(c, "identity service unavailable")
	}
	if c.Method() == fiber.MethodGet {
		users, next, err := a.IdentityRepo.ListUsers(c.Context(), pageQuery(c))
		if err != nil {
			return presenter.ServerError(c, "failed to list users")
		}
		return responseapi.JSON(c, UserListResponse{Object: "list", Data: users, NextCursor: next})
	}
	var body UserCreateRequest
	if err := c.Bind().Body(&body); err != nil {
		return presenter.BadRequest(c, "invalid user request")
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
		return responseapi.JSONStatus(c, fiber.StatusCreated, UserCreateResponse{User: user, InitialKey: &initial})
	}
	user, err := a.IdentitySvc.CreateUser(c.Context(), actor, body.Username)
	if err != nil {
		return identityError(c, err)
	}
	return responseapi.JSONStatus(c, fiber.StatusCreated, UserCreateResponse{User: user})
}

// UserByID returns user detail or changes user status.
// @Summary Get or update a user
// @Tags users
// @Security BearerAuth
// @Param id path string true "User ID"
// @Param request body UserStatusRequest false "Required for PATCH"
// @Success 200 {object} UserDetailResponse
// @Failure 400,401,403,404,500 {object} presenter.Error
// @Router /admin/users/{id} [get]
// @Router /admin/users/{id} [patch]
func (a *Admin) UserByID(c fiber.Ctx) error {
	actor := principalFromSession(SessionFrom(c))
	if err := policy.ManageUsers(actor); err != nil {
		return presenter.Forbidden(c, "only master may manage users")
	}
	user, err := a.IdentityRepo.UserByID(c.Context(), c.Params("id"))
	if err != nil {
		return identityError(c, err)
	}
	if c.Method() == fiber.MethodGet {
		memberships, memberErr := a.IdentityRepo.ListMembershipsForUser(c.Context(), user.ID)
		if memberErr != nil {
			return presenter.ServerError(c, "failed to load user memberships")
		}
		return responseapi.JSON(c, UserDetailResponse{User: user, Memberships: memberships})
	}
	var body UserStatusRequest
	if err = c.Bind().Body(&body); err != nil {
		return presenter.BadRequest(c, "invalid user update")
	}
	if err = a.IdentitySvc.SetUserStatus(c.Context(), actor, user.ID, body.Status); err != nil {
		return identityError(c, err)
	}
	return responseapi.JSON(c, okResponse{OK: true})
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
// @Failure 400,401,403,409,500 {object} presenter.Error
// @Router /admin/organizations [get]
// @Router /admin/organizations [post]
func (a *Admin) Organizations(c fiber.Ctx) error {
	actor := principalFromSession(SessionFrom(c))
	if a.IdentityRepo == nil {
		return presenter.ServerError(c, "identity service unavailable")
	}
	if c.Method() == fiber.MethodPost {
		if err := policy.ManageOrganizations(actor); err != nil {
			return presenter.Forbidden(c, "only master may create organizations")
		}
		var body OrganizationCreateRequest
		if err := c.Bind().Body(&body); err != nil {
			return presenter.BadRequest(c, "invalid organization request")
		}
		organization, err := a.IdentitySvc.CreateOrganization(c.Context(), actor, body.Name)
		if err != nil {
			return identityError(c, err)
		}
		return responseapi.JSONStatus(c, fiber.StatusCreated, organization)
	}
	organizations, next, err := a.IdentityRepo.ListOrganizations(c.Context(), pageQuery(c))
	if err != nil {
		return presenter.ServerError(c, "failed to list organizations")
	}
	if actor.Type != entities.PrincipalMaster {
		allowed := map[string]bool{}
		if actor.Type == entities.PrincipalOrganization {
			allowed[actor.OrganizationID] = true
		} else if actor.OrganizationID != "" {
			allowed[actor.OrganizationID] = true
		} else {
			memberships, _ := a.IdentityRepo.ListMembershipsForUser(c.Context(), actor.UserID)
			for _, membership := range memberships {
				allowed[membership.OrganizationID] = true
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
	return responseapi.JSON(c, OrganizationListResponse{Object: "list", Data: organizations, NextCursor: next})
}

// OrganizationByID returns or updates an organization.
// @Summary Get or update an organization
// @Tags organizations
// @Security BearerAuth
// @Param id path string true "Organization ID"
// @Param request body OrganizationUpdateRequest false "Required for PATCH"
// @Success 200 {object} OrganizationDetailResponse
// @Failure 400,401,403,404,409,500 {object} presenter.Error
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
		return presenter.NotFound(c, "organization not found")
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
		return responseapi.JSON(c, OrganizationDetailResponse{Organization: organization, Membership: own})
	}
	var body OrganizationUpdateRequest
	if err := c.Bind().Body(&body); err != nil {
		return presenter.BadRequest(c, "invalid organization update")
	}
	organization, err := a.IdentitySvc.UpdateOrganization(c.Context(), actor, id, body.Name, body.Status)
	if err != nil {
		return identityError(c, err)
	}
	return responseapi.JSON(c, organization)
}

// Members lists or adds organization memberships.
// @Summary List or add organization members
// @Tags memberships
// @Security BearerAuth
// @Param id path string true "Organization ID"
// @Param request body MembershipCreateRequest false "Required for POST"
// @Success 200 {object} MembershipListResponse
// @Success 201 {object} entities.Membership
// @Failure 400,401,403,404,409,500 {object} presenter.Error
// @Router /admin/organizations/{id}/members [get]
// @Router /admin/organizations/{id}/members [post]
func (a *Admin) Members(c fiber.Ctx) error {
	actor := principalFromSession(SessionFrom(c))
	organizationID := c.Params("id")
	if err := policy.ManageMembers(actor, organizationID); err != nil {
		return presenter.Forbidden(c, "membership administration is not allowed")
	}
	if c.Method() == fiber.MethodGet {
		members, err := a.IdentityRepo.ListMemberships(c.Context(), organizationID)
		if err != nil {
			return presenter.ServerError(c, "failed to list members")
		}
		return responseapi.JSON(c, MembershipListResponse{Object: "list", Data: members})
	}
	var body MembershipCreateRequest
	if err := c.Bind().Body(&body); err != nil {
		return presenter.BadRequest(c, "invalid membership request")
	}
	membership, err := a.IdentitySvc.AddMembership(c.Context(), actor, organizationID, body.UserID, body.Role)
	if err != nil {
		return identityError(c, err)
	}
	return responseapi.JSONStatus(c, fiber.StatusCreated, membership)
}

// MemberByID changes a membership role or removes it.
// @Summary Update or remove an organization member
// @Tags memberships
// @Security BearerAuth
// @Param id path string true "Organization ID"
// @Param user_id path string true "User ID"
// @Param request body MembershipUpdateRequest false "Required for PATCH"
// @Success 200 {object} OKResponse
// @Failure 400,401,403,404,409,500 {object} presenter.Error
// @Router /admin/organizations/{id}/members/{user_id} [patch]
// @Router /admin/organizations/{id}/members/{user_id} [delete]
func (a *Admin) MemberByID(c fiber.Ctx) error {
	actor := principalFromSession(SessionFrom(c))
	organizationID, userID := c.Params("id"), c.Params("user_id")
	if err := policy.ManageMembers(actor, organizationID); err != nil {
		return presenter.Forbidden(c, "membership administration is not allowed")
	}
	var err error
	if c.Method() == fiber.MethodDelete {
		err = a.IdentitySvc.RemoveMembership(c.Context(), actor, organizationID, userID)
	} else {
		var body MembershipUpdateRequest
		if bindErr := c.Bind().Body(&body); bindErr != nil {
			return presenter.BadRequest(c, "invalid membership update")
		}
		err = a.IdentitySvc.ChangeMembershipRole(c.Context(), actor, organizationID, userID, body.Role)
	}
	if err != nil {
		return identityError(c, err)
	}
	return responseapi.JSON(c, okResponse{OK: true})
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
// @Failure 400,401,403,500 {object} presenter.Error
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
		return presenter.Forbidden(c, "audit access is not allowed")
	}
	limit, _ := strconv.Atoi(c.Query("limit", "100"))
	query := entities.AuditQuery{Visibility: visibility, OrganizationID: c.Query("organization_id"), Cursor: c.Query("cursor"), Limit: limit, ActorID: c.Query("actor_id"), Action: c.Query("action"), TargetType: c.Query("target_type"), TargetID: c.Query("target_id")}
	if value := c.Query("since"); value != "" {
		parsed, parseErr := time.Parse(time.RFC3339, value)
		if parseErr != nil {
			return presenter.BadRequest(c, "since must be RFC3339")
		}
		query.Since = &parsed
	}
	if value := c.Query("until"); value != "" {
		parsed, parseErr := time.Parse(time.RFC3339, value)
		if parseErr != nil {
			return presenter.BadRequest(c, "until must be RFC3339")
		}
		query.Until = &parsed
	}
	page, err := a.AuditRepo.QueryAudit(c.Context(), query)
	if err != nil {
		return presenter.ServerError(c, "failed to load audit events")
	}
	return responseapi.JSON(c, AuditListResponse{Object: "list", Data: page.Data, NextCursor: page.NextCursor})
}

func identityError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, entities.ErrNotFound):
		return presenter.NotFound(c, "resource not found")
	case errors.Is(err, entities.ErrConflict):
		return presenter.Err(c, fiber.StatusConflict, "resource already exists", "conflict", "duplicate")
	case errors.Is(err, entities.ErrInvalidUsername), errors.Is(err, entities.ErrInvalidName), errors.Is(err, entities.ErrInvalidStatus), errors.Is(err, entities.ErrInvalidRole), errors.Is(err, entities.ErrInvalidOwnership):
		return presenter.BadRequest(c, err.Error())
	default:
		return presenter.BadRequest(c, err.Error())
	}
}
