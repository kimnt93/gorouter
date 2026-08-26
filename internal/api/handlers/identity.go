package handlers

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/kimnt93/gorouter/api/presenter"
	"github.com/kimnt93/gorouter/pkg/apikey"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/policy"
)

type listResponse[T any] struct {
	Object     string `json:"object"`
	Data       []T    `json:"data"`
	NextCursor string `json:"next_cursor,omitempty"`
}

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
		return c.JSON(listResponse[entities.User]{Object: "list", Data: users, NextCursor: next})
	}
	var body struct {
		Username           string `json:"username"`
		GenerateInitialKey bool   `json:"generate_initial_key"`
		InitialKey         struct {
			Name   string   `json:"name"`
			Models []string `json:"models"`
			Scopes []string `json:"scopes"`
		} `json:"initial_key"`
	}
	if err := c.Bind().Body(&body); err != nil {
		return presenter.BadRequest(c, "invalid user request")
	}
	user, err := a.IdentitySvc.CreateUser(c.Context(), actor, body.Username)
	if err != nil {
		return identityError(c, err)
	}
	response := struct {
		User *entities.User         `json:"user"`
		Key  *createdAPIKeyResponse `json:"initial_key,omitempty"`
	}{User: user}
	if body.GenerateInitialKey {
		name := strings.TrimSpace(body.InitialKey.Name)
		if name == "" {
			name = "Initial login key"
		}
		key, keyErr := a.KeysSvc.Create(c.Context(), apikey.CreateInput{Name: name, Models: body.InitialKey.Models, Scopes: body.InitialKey.Scopes, OwnerType: entities.OwnerUser, OwnerUserID: user.ID})
		if keyErr != nil {
			return presenter.ServerError(c, "user created but initial key creation failed")
		}
		response.Key = &createdAPIKeyResponse{ID: key.ID, Name: key.Name, KeyPrefix: key.SecretPrefix, Models: key.Models, Scopes: key.Scopes, QuotaUSD: key.QuotaUSD, QuotaPeriod: key.QuotaPeriod, RPM: key.RPM, Enabled: key.Enabled, Plaintext: key.Plaintext}
	}
	return c.Status(fiber.StatusCreated).JSON(response)
}

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
		return c.JSON(struct {
			User        *entities.User        `json:"user"`
			Memberships []entities.Membership `json:"memberships"`
		}{user, memberships})
	}
	var body struct {
		Status string `json:"status"`
	}
	if err = c.Bind().Body(&body); err != nil {
		return presenter.BadRequest(c, "invalid user update")
	}
	if err = a.IdentitySvc.SetUserStatus(c.Context(), actor, user.ID, body.Status); err != nil {
		return identityError(c, err)
	}
	return c.JSON(okResponse{OK: true})
}

func (a *Admin) Organizations(c fiber.Ctx) error {
	actor := principalFromSession(SessionFrom(c))
	if a.IdentityRepo == nil {
		return presenter.ServerError(c, "identity service unavailable")
	}
	if c.Method() == fiber.MethodPost {
		if err := policy.ManageOrganizations(actor); err != nil {
			return presenter.Forbidden(c, "only master may create organizations")
		}
		var body struct {
			Name string `json:"name"`
		}
		if err := c.Bind().Body(&body); err != nil {
			return presenter.BadRequest(c, "invalid organization request")
		}
		organization, err := a.IdentitySvc.CreateOrganization(c.Context(), actor, body.Name)
		if err != nil {
			return identityError(c, err)
		}
		return c.Status(fiber.StatusCreated).JSON(organization)
	}
	organizations, next, err := a.IdentityRepo.ListOrganizations(c.Context(), pageQuery(c))
	if err != nil {
		return presenter.ServerError(c, "failed to list organizations")
	}
	if actor.Type != entities.PrincipalMaster {
		allowed := map[string]bool{}
		if actor.Type == entities.PrincipalOrganization {
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
	return c.JSON(listResponse[entities.Organization]{Object: "list", Data: organizations, NextCursor: next})
}

func (a *Admin) OrganizationByID(c fiber.Ctx) error {
	actor := principalFromSession(SessionFrom(c))
	id := c.Params("id")
	if actor.Type == entities.PrincipalUser && actor.OrganizationID != id {
		if membership, err := a.IdentityRepo.Membership(c.Context(), id, actor.UserID); err == nil {
			actor.OrganizationID = id
			actor.MembershipRole = membership.Role
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
		return c.JSON(struct {
			Organization *entities.Organization `json:"organization"`
			Membership   *entities.Membership   `json:"membership,omitempty"`
		}{organization, own})
	}
	var body struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := c.Bind().Body(&body); err != nil {
		return presenter.BadRequest(c, "invalid organization update")
	}
	organization, err := a.IdentitySvc.UpdateOrganization(c.Context(), actor, id, body.Name, body.Status)
	if err != nil {
		return identityError(c, err)
	}
	return c.JSON(organization)
}

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
		return c.JSON(listResponse[entities.Membership]{Object: "list", Data: members})
	}
	var body struct {
		UserID string `json:"user_id"`
		Role   string `json:"role"`
	}
	if err := c.Bind().Body(&body); err != nil {
		return presenter.BadRequest(c, "invalid membership request")
	}
	membership, err := a.IdentitySvc.AddMembership(c.Context(), actor, organizationID, body.UserID, body.Role)
	if err != nil {
		return identityError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(membership)
}

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
		var body struct {
			Role string `json:"role"`
		}
		if bindErr := c.Bind().Body(&body); bindErr != nil {
			return presenter.BadRequest(c, "invalid membership update")
		}
		err = a.IdentitySvc.ChangeMembershipRole(c.Context(), actor, organizationID, userID, body.Role)
	}
	if err != nil {
		return identityError(c, err)
	}
	return c.JSON(okResponse{OK: true})
}

func (a *Admin) AuditEvents(c fiber.Ctx) error {
	actor := principalFromSession(SessionFrom(c))
	visibility, err := policy.AuditVisibility(actor)
	if err != nil {
		return presenter.Forbidden(c, "audit access is not allowed")
	}
	limit, _ := strconv.Atoi(c.Query("limit", "100"))
	query := entities.AuditQuery{Visibility: visibility, Cursor: c.Query("cursor"), Limit: limit, ActorID: c.Query("actor_id"), Action: c.Query("action"), TargetType: c.Query("target_type"), TargetID: c.Query("target_id")}
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
	return c.JSON(struct {
		Object     string                `json:"object"`
		Data       []entities.AuditEvent `json:"data"`
		NextCursor string                `json:"next_cursor,omitempty"`
	}{"list", page.Data, page.NextCursor})
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
