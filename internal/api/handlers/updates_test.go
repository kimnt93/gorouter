package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/updatecheck"
)

type releaseCheckStub struct{ called bool }

func (s *releaseCheckStub) Check(context.Context) (updatecheck.Status, error) {
	s.called = true
	return updatecheck.Status{Latest: "v0.2.2"}, nil
}
func TestUpdateCheckRejectsNonMasterBeforeNetwork(t *testing.T) {
	stub := &releaseCheckStub{}
	app := fiber.New()
	app.Get("/check", func(c fiber.Ctx) error {
		c.Locals(localSession, &entities.Session{PrincipalType: entities.PrincipalUser})
		return (Updates{Checker: stub}).CheckRelease(c)
	})
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/check", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 403 || stub.called {
		t.Fatalf("status=%d network=%v", resp.StatusCode, stub.called)
	}
}
