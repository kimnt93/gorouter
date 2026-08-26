package presenter

import (
	"github.com/gofiber/fiber/v3"
)

type Error struct {
	Error Detail `json:"error"`
}

type Detail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
}

func Err(c fiber.Ctx, status int, msg, typ, code string) error {
	c.Status(status)
	return c.JSON(Error{Error: Detail{Message: msg, Type: typ, Code: code}})
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

func OK(c fiber.Ctx, v any) error { return c.JSON(v) }
