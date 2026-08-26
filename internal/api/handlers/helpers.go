package handlers

import "github.com/gofiber/fiber/v3"

func renderString(c fiber.Ctx, status int, html string) error {
	c.Status(status)
	c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
	return c.SendString(html)
}
