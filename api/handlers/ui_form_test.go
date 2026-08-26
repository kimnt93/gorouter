package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestFormValuesReadsRepeatedModelCheckboxes(t *testing.T) {
	app := fiber.New()
	app.Post("/keys", func(c fiber.Ctx) error {
		return c.JSON(formValues(c, "models"))
	})

	form := url.Values{}
	form.Add("models", "cx/gpt-5.5")
	form.Add("models", "groq/llama-3.1-8b-instant")
	request := httptest.NewRequest("POST", "/keys", strings.NewReader(form.Encode()))
	request.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationForm)
	response, err := app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var models []string
	if err := json.NewDecoder(response.Body).Decode(&models); err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0] != "cx/gpt-5.5" || models[1] != "groq/llama-3.1-8b-instant" {
		t.Fatalf("models = %#v", models)
	}
}

func TestFormValuesKeepsLegacyCSVCompatibility(t *testing.T) {
	app := fiber.New()
	app.Post("/keys", func(c fiber.Ctx) error {
		return c.JSON(formValues(c, "models"))
	})
	request := httptest.NewRequest("POST", "/keys", strings.NewReader("models=openai%2Fgpt-mini%2C+anthropic%2Fhaiku"))
	request.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationForm)
	response, err := app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var models []string
	if err := json.NewDecoder(response.Body).Decode(&models); err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0] != "openai/gpt-mini" || models[1] != "anthropic/haiku" {
		t.Fatalf("models = %#v", models)
	}
}
