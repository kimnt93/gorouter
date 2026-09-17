package handlers

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v3"
	responseapi "github.com/kimnt93/gorouter/internal/api"
	"github.com/kimnt93/gorouter/pkg/updatecheck"
)

type ReleaseChecker interface {
	Check(context.Context) (updatecheck.Status, error)
}
type Updates struct{ Checker ReleaseChecker }

// CheckRelease checks the latest published GoRouter image version on demand.
// @Summary Check for a published GoRouter update
// @Tags updates
// @Security BearerAuth
// @Produce json
// @Success 200 {object} updatecheck.Status
// @Failure 401,403,503 {object} responseapi.ErrorResponse
// @Router /admin/updates/check [get]
func (u Updates) CheckRelease(c fiber.Ctx) error {
	api := responseapi.For(c)
	c.Set(fiber.HeaderCacheControl, "no-store")
	if sess := SessionFrom(c); sess == nil || !sess.IsMaster() {
		return api.Forbidden("master access required").Send()
	}
	if u.Checker == nil {
		return api.Error(503, "release check unavailable", "service_unavailable", "release_unavailable").Send()
	}
	ctx, cancel := context.WithTimeout(c.Context(), 9*time.Second)
	defer cancel()
	status, err := u.Checker.Check(ctx)
	if err != nil {
		return api.Error(503, "release check unavailable", "service_unavailable", "release_unavailable").Send()
	}
	return api.Response().Status(200).Data(status).Send()
}
