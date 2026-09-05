package handlers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/kimnt93/gorouter/pkg/auth"
	"github.com/kimnt93/gorouter/pkg/entities"
)

type optionalAuthKeys struct{}

func (optionalAuthKeys) GetBySecret(_ context.Context, secret string) (*entities.ApiKey, error) {
	if secret != "valid-key" {
		return nil, entities.ErrNotFound
	}
	return &entities.ApiKey{ID: "key-1", Enabled: true, Scopes: []string{entities.ScopeChat}}, nil
}

func TestOptionalAuthenticationAllowsAnonymousAndRejectsInvalidBearer(t *testing.T) {
	authService := auth.NewService("master-key", "session-secret", optionalAuthKeys{})
	app := fiber.New()
	app.Get("/v1/models", Optional(authService, entities.ScopeChat), func(c fiber.Ctx) error {
		if SessionFrom(c) == nil {
			return c.SendString("anonymous")
		}
		return c.SendString("authenticated")
	})

	for _, test := range []struct {
		name   string
		bearer string
		status int
		body   string
	}{
		{name: "anonymous", status: http.StatusOK, body: "anonymous"},
		{name: "valid API key", bearer: "valid-key", status: http.StatusOK, body: "authenticated"},
		{name: "invalid API key", bearer: "invalid-key", status: http.StatusUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			if test.bearer != "" {
				request.Header.Set(fiber.HeaderAuthorization, "Bearer "+test.bearer)
			}
			response, err := app.Test(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != test.status {
				t.Fatalf("status=%d want=%d", response.StatusCode, test.status)
			}
			if test.body != "" {
				buffer, readErr := io.ReadAll(response.Body)
				if readErr != nil || string(buffer) != test.body {
					t.Fatalf("body=%q err=%v", buffer, readErr)
				}
			}
		})
	}
}
