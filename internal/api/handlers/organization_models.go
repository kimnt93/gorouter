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
// @Failure 400,401,403,404,503 {object} responseapi.ErrorResponse
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
// @Success 201 {object} entities.OrganizationModelGrant
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
	record, err := a.OrgModels.Assign(ctx, actor, org, input.Model, input.UserID, input.WeeklyLimitUSD, input.Enabled)
	if err != nil {
		return orgModelError(c, err)
	}
	return responseapi.For(c).Response().Status(201).Data(record).Send()
}
func orgModelError(c fiber.Ctx, err error) error {
	switch {
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
