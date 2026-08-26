package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

type responseFixture struct {
	ID string `json:"id"`
}

func TestResponseBuilderPreservesTypedPayloadAndStatus(t *testing.T) {
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		return Response().Status(fiber.StatusCreated).Data(responseFixture{ID: "item_1"}).Send(c)
	})
	response, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != fiber.StatusCreated {
		t.Fatalf("status=%d", response.StatusCode)
	}
	var body responseFixture
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil || body.ID != "item_1" {
		t.Fatalf("body=%+v err=%v", body, err)
	}
}

func TestResponseBuilderUsesTypedErrorEnvelope(t *testing.T) {
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error { return Response().Forbidden("denied").Send(c) })
	response, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	var body ErrorResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != fiber.StatusForbidden || body.Error.Type != "permission_error" {
		t.Fatalf("status=%d body=%+v", response.StatusCode, body)
	}
}
