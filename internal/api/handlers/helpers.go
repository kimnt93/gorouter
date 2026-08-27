package handlers

import (
	"github.com/gofiber/fiber/v3"

	responseapi "github.com/kimnt93/gorouter/internal/api"
)

// Health reports process liveness.
// @Summary Health check
// @Tags system
// @Success 200 {object} HealthResponse
// @Router /healthz [get]
func Health(c fiber.Ctx) error {
	return responseapi.For(c).Response().
		Status(fiber.StatusOK).
		Data(HealthResponse{OK: true}).
		Send()
}

func renderString(c fiber.Ctx, status int, html string) error {
	c.Status(status)
	c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
	return c.SendString(html)
}
