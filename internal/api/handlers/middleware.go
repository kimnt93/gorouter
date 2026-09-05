package handlers

import (
	"strings"

	"github.com/gofiber/fiber/v3"

	responseapi "github.com/kimnt93/gorouter/internal/api"
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
			case c.Path() == "/" || strings.HasPrefix(c.Path(), "/ui") || strings.HasPrefix(c.Path(), "/dashboard"):
				return c.Redirect().To("/login")
			default:
				return responseapi.For(c).Unauthorized("authentication required").Send()
			}
		}
		if scope != "" && !sess.Has(scope) {
			if c.Get("HX-Request") == "true" {
				c.Status(fiber.StatusForbidden)
				return renderString(c, fiber.StatusForbidden, `<div class="p-4 text-red-400">Forbidden: missing <code>`+scope+`</code> access for this key.</div>`)
			}
			return responseapi.For(c).Error(fiber.StatusForbidden, "missing required access: "+scope, "permission_error", "scope_denied").Send()
		}
		c.Locals(localSession, sess)
		return c.Next()
	}
}

// Optional authenticates a presented bearer token or session cookie while
// allowing callers that present no credentials to continue anonymously.
// Invalid credentials still fail closed instead of being treated as absent.
func Optional(authSvc *auth.Service, scope string) fiber.Handler {
	return func(c fiber.Ctx) error {
		hasCredentials := strings.TrimSpace(c.Get(fiber.HeaderAuthorization)) != "" || c.Cookies(sessionCookie) != ""
		sess := authenticate(c, authSvc)
		if sess == nil {
			if hasCredentials {
				return responseapi.For(c).Unauthorized("authentication required").Send()
			}
			return c.Next()
		}
		if scope != "" && !sess.Has(scope) {
			return responseapi.For(c).Error(fiber.StatusForbidden, "missing required access: "+scope, "permission_error", "scope_denied").Send()
		}
		c.Locals(localSession, sess)
		return c.Next()
	}
}

func SessionFrom(c fiber.Ctx) *entities.Session {
	sess, _ := c.Locals(localSession).(*entities.Session)
	return sess
}
