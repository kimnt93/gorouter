package handlers

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	responseapi "github.com/kimnt93/gorouter/internal/api"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/orgmodel"
)

type OrganizationModelRequest struct {
	Name           string   `json:"name"`
	Kind           string   `json:"kind"`
	Targets        []string `json:"targets"`
	Enabled        bool     `json:"enabled"`
	WeeklyLimitUSD *float64 `json:"weekly_limit_usd"`
}
type OrganizationGrantRequest struct {
	Model          string   `json:"model"`
	UserID         string   `json:"user_id"`
	Enabled        bool     `json:"enabled"`
	WeeklyLimitUSD *float64 `json:"weekly_limit_usd"`
}

// OrganizationModels lists or publishes organization aliases and groups.
// @Summary List or publish organization models
// @Tags organization-models
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Organization ID"
// @Param request body OrganizationModelRequest false "Alias or group; POST"
// @Success 200 {array} entities.OrganizationModel
// @Success 201 {object} entities.OrganizationModel
// @Failure 400,401,403,404,409,503 {object} responseapi.ErrorResponse
// @Router /admin/organizations/{id}/models [get]
// @Router /admin/organizations/{id}/models [post]
func (a *Admin) OrganizationModels(c fiber.Ctx) error {
	if a.OrgModels == nil {
		return orgModelError(c, errors.New("unavailable"))
	}
	actor, readErr := a.principalForRead(c)
	if readErr != nil {
		return orgModelError(c, orgmodel.ErrForbidden)
	}
	org := strings.Clone(c.Params("id"))
	if requested := c.Query("organization_id"); requested != "" && requested != org {
		return orgModelError(c, orgmodel.ErrForbidden)
	}
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()
	c.Set(fiber.HeaderCacheControl, "no-store")
	if _, err := a.OrgModels.Manage(ctx, actor, org); err != nil {
		return orgModelError(c, err)
	}
	if c.Method() == fiber.MethodGet {
		records, err := a.OrgModels.Repo.List(ctx, org)
		if err != nil {
			return orgModelError(c, err)
		}
		return responseapi.For(c).Response().Status(200).Data(records).Send()
	}
	var input OrganizationModelRequest
	if err := c.Bind().Body(&input); err != nil {
		return orgModelError(c, orgmodel.ErrInvalid)
	}
	record, err := a.OrgModels.Publish(ctx, actor, org, input.Name, input.Kind, input.Targets, input.WeeklyLimitUSD, input.Enabled)
	if err != nil {
		return orgModelError(c, err)
	}
	return responseapi.For(c).Response().Status(201).Data(record).Send()
}

// OrganizationModelGrants assigns models/groups and optional per-user limits.
// @Summary List or assign organization models to users
// @Tags organization-models
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Organization ID"
// @Param request body OrganizationGrantRequest false "Assignment; POST"
// @Success 200 {array} entities.OrganizationModelGrant
// @Success 201 {array} entities.OrganizationModelGrant
// @Failure 400,401,403,404,503 {object} responseapi.ErrorResponse
// @Router /admin/organizations/{id}/model-grants [get]
// @Router /admin/organizations/{id}/model-grants [post]
func (a *Admin) OrganizationModelGrants(c fiber.Ctx) error {
	if a.OrgModels == nil {
		return orgModelError(c, errors.New("unavailable"))
	}
	actor, readErr := a.principalForRead(c)
	if readErr != nil {
		return orgModelError(c, orgmodel.ErrForbidden)
	}
	org := strings.Clone(c.Params("id"))
	if requested := c.Query("organization_id"); requested != "" && requested != org {
		return orgModelError(c, orgmodel.ErrForbidden)
	}
	ctx, cancel := context.WithTimeout(c.Context(), 10*time.Second)
	defer cancel()
	c.Set(fiber.HeaderCacheControl, "no-store")
	if _, err := a.OrgModels.Manage(ctx, actor, org); err != nil {
		return orgModelError(c, err)
	}
	if c.Method() == fiber.MethodGet {
		records, err := a.OrgModels.Repo.Grants(ctx, org, "")
		if err != nil {
			return orgModelError(c, err)
		}
		return responseapi.For(c).Response().Status(200).Data(records).Send()
	}
	var input OrganizationGrantRequest
	if err := c.Bind().Body(&input); err != nil {
		return orgModelError(c, orgmodel.ErrInvalid)
	}
	record, err := a.OrgModels.AssignPackage(ctx, actor, org, input.Model, input.UserID, input.WeeklyLimitUSD, input.Enabled)
	if err != nil {
		return orgModelError(c, err)
	}
	return responseapi.For(c).Response().Status(201).Data(record).Send()
}
func orgModelError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, entities.ErrConflict):
		return responseapi.For(c).Conflict("alias name or source already has a mapping", "alias_conflict").Send()
	case errors.Is(err, orgmodel.ErrForbidden):
		return responseapi.For(c).Forbidden("organization model access denied").Send()
	case errors.Is(err, entities.ErrNotFound):
		return responseapi.For(c).NotFound("organization model not found").Send()
	case errors.Is(err, orgmodel.ErrInvalid):
		return responseapi.For(c).BadRequest("invalid alias, group, assignment, or weekly limit").Send()
	case errors.Is(err, orgmodel.ErrBudget):
		return responseapi.For(c).Error(429, "organization model usage limit exceeded", "insufficient_quota", "organization_model_quota_exceeded").Send()
	default:
		return responseapi.For(c).Error(503, "organization model service unavailable", "service_unavailable", "organization_model_unavailable").Send()
	}
}

// PersonalModelAliases lists or creates the current user's one-to-one aliases.
// @Summary List or create personal model aliases
// @Tags model-aliases
// @Security BearerAuth
// @Param request body OrganizationModelRequest false "Alias; POST"
// @Success 200 {array} entities.OrganizationModel
// @Success 201 {object} entities.OrganizationModel
// @Failure 400,401,403,409,503 {object} responseapi.ErrorResponse
// @Router /admin/model-aliases [get]
// @Router /admin/model-aliases [post]
func (a *Admin) PersonalModelAliases(c fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	if a.OrgModels == nil {
		return orgModelError(c, errors.New("unavailable"))
	}
	actor := principalFromSession(SessionFrom(c))
	if c.Method() == fiber.MethodGet {
		all, err := a.OrgModels.PersonalAliases(c.Context(), actor)
		if err != nil {
			return orgModelError(c, err)
		}
		return responseapi.For(c).Response().Status(200).Data(all).Send()
	}
	var input OrganizationModelRequest
	if err := c.Bind().Body(&input); err != nil || len(input.Targets) != 1 || input.Kind != "alias" || input.WeeklyLimitUSD != nil && *input.WeeklyLimitUSD != 0 {
		return orgModelError(c, orgmodel.ErrInvalid)
	}
	v, err := a.OrgModels.PublishPersonal(c.Context(), actor, input.Name, input.Targets[0], input.Enabled)
	if err != nil {
		return orgModelError(c, err)
	}
	return responseapi.For(c).Response().Status(201).Data(v).Send()
}

// PersonalModelGrants assigns a personally owned alias to another user.
// @Summary Assign a personal model alias
// @Tags model-aliases
// @Security BearerAuth
// @Param request body OrganizationGrantRequest true "User assignment and weekly limit; zero unlimited"
// @Success 201 {object} entities.OrganizationModelGrant
// @Failure 400,401,403,404,503 {object} responseapi.ErrorResponse
// @Router /admin/model-grants [post]
func (a *Admin) PersonalModelGrants(c fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	if a.OrgModels == nil {
		return orgModelError(c, errors.New("unavailable"))
	}
	var input OrganizationGrantRequest
	if err := c.Bind().Body(&input); err != nil {
		return orgModelError(c, orgmodel.ErrInvalid)
	}
	v, err := a.OrgModels.AssignPersonal(c.Context(), principalFromSession(SessionFrom(c)), input.Model, input.UserID, input.WeeklyLimitUSD, input.Enabled)
	if err != nil {
		return orgModelError(c, err)
	}
	return responseapi.For(c).Response().Status(201).Data(v).Send()
}

type SelfModelLimitRequest struct {
	Model          string   `json:"model"`
	WeeklyLimitUSD *float64 `json:"weekly_limit_usd"`
}

// SelfModelLimit sets an additional limit without overriding the grantor limit.
// @Summary Set personal limit on an assigned model
// @Tags model-aliases
// @Security BearerAuth
// @Param request body SelfModelLimitRequest true "Assigned model; zero removes personal cap only"
// @Success 200 {object} OKResponse
// @Failure 400,401,403,404,503 {object} responseapi.ErrorResponse
// @Router /admin/model-limits [post]
func (a *Admin) SelfModelLimit(c fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	if a.OrgModels == nil {
		return orgModelError(c, errors.New("unavailable"))
	}
	var input SelfModelLimitRequest
	if err := c.Bind().Body(&input); err != nil {
		return orgModelError(c, orgmodel.ErrInvalid)
	}
	if err := a.OrgModels.SetSelfLimit(c.Context(), principalFromSession(SessionFrom(c)), input.Model, input.WeeklyLimitUSD); err != nil {
		return orgModelError(c, err)
	}
	return responseapi.For(c).Response().Status(200).Data(OKResponse{OK: true}).Send()
}
