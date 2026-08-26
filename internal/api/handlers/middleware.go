package handlers

import (
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/kimnt93/gorouter/internal/api/presenter"
	"github.com/kimnt93/gorouter/pkg/auth"
	"github.com/kimnt93/gorouter/pkg/entities"
)

const (
	localSession = "nr_session"

	sessionCookie = "nr_session"
)

// authenticate resolves a request into a session: either an Authorization
// bearer secret (master key or API key) or a signed session cookie from the UI.
func authenticate(c fiber.Ctx, authSvc *auth.Service) *entities.Session {
	if h := c.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		secret := strings.TrimSpace(h[len("Bearer "):])
		if secret != "" {
			if sess, err := authSvc.Login(c.Context(), secret); err == nil {
				return sess
			}
			return nil
		}
	}
	if tok := c.Cookies(sessionCookie); tok != "" {
		if sess, err := authSvc.VerifyAndRevalidate(c.Context(), tok); err == nil {
			return sess
		}
	}
	return nil
}

// Require authenticates the request and enforces an access-control scope.
// The master key set during setup passes every scope.
func Require(authSvc *auth.Service, scope string) fiber.Handler {
	return func(c fiber.Ctx) error {
		sess := authenticate(c, authSvc)
		if sess == nil {
			switch {
			case c.Get("HX-Request") == "true":
				c.Set("HX-Redirect", "/login")
				return c.SendStatus(fiber.StatusUnauthorized)
			case c.Path() == "/" || strings.HasPrefix(c.Path(), "/ui"):
				return c.Redirect().To("/login")
			default:
				return presenter.Unauthorized(c, "authentication required")
			}
		}
		if scope != "" && !sess.Has(scope) {
			if c.Get("HX-Request") == "true" {
				c.Status(fiber.StatusForbidden)
				return renderString(c, fiber.StatusForbidden, `<div class="p-4 text-red-400">Forbidden: missing <code>`+scope+`</code> access for this key.</div>`)
			}
			return presenter.Err(c, fiber.StatusForbidden, "missing required access: "+scope, "permission_error", "scope_denied")
		}
		c.Locals(localSession, sess)
		return c.Next()
	}
}

func SessionFrom(c fiber.Ctx) *entities.Session {
	sess, _ := c.Locals(localSession).(*entities.Session)
	return sess
}
