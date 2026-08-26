package presenter

import (
	"github.com/gofiber/fiber/v3"

	response "github.com/kimnt93/gorouter/internal/api"
)

type Error = response.ErrorResponse
type Detail = response.ErrorDetail

func Err(c fiber.Ctx, status int, msg, typ, code string) error {
	return response.Response().Error(status, msg, typ, code).Send(c)
}

func BadRequest(c fiber.Ctx, msg string) error {
	return Err(c, fiber.StatusBadRequest, msg, "invalid_request_error", "")
}
func Unauthorized(c fiber.Ctx, msg string) error {
	return Err(c, fiber.StatusUnauthorized, msg, "authentication_error", "invalid_api_key")
}
func Forbidden(c fiber.Ctx, msg string) error {
	return Err(c, fiber.StatusForbidden, msg, "permission_error", "")
}
func NotFound(c fiber.Ctx, msg string) error {
	return Err(c, fiber.StatusNotFound, msg, "invalid_request_error", "not_found")
}
func ServerError(c fiber.Ctx, msg string) error {
	return Err(c, fiber.StatusInternalServerError, msg, "server_error", "")
}

func OK(c fiber.Ctx, v any) error { return response.Response().Data(v).Send(c) }
