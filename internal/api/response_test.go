package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v3"
)

type responseFixture struct {
	ID string `json:"id"`
}

func TestResponseBuilderPreservesTypedPayloadAndStatus(t *testing.T) {
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		api := For(c)
		return api.Response().Status(fiber.StatusCreated).Data(responseFixture{ID: "item_1"}).Send()
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
	app.Get("/", func(c fiber.Ctx) error {
		api := For(c)
		return api.Forbidden("denied").Send()
	})
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

func TestResponseBuilderAddsListCursorAndPaging(t *testing.T) {
	app := fiber.New()
	app.Get("/cursor", func(c fiber.Ctx) error {
		api := For(c)
		return api.Response().Object("list").Data([]responseFixture{{ID: "item_1"}}).Next("cursor_2").Send()
	})
	app.Get("/page", func(c fiber.Ctx) error {
		api := For(c)
		return api.Response().Data([]responseFixture{{ID: "item_1"}}).Paging(10, 0, 5).Send()
	})
	app.Get("/last-page", func(c fiber.Ctx) error {
		api := For(c)
		return api.Response().Data([]responseFixture{{ID: "item_1"}}).Next("").Send()
	})

	for _, test := range []struct {
		path string
		want map[string]any
	}{
		{path: "/cursor", want: map[string]any{"object": "list", "next_cursor": "cursor_2"}},
		{path: "/page", want: map[string]any{"total": float64(10), "offset": float64(0), "limit": float64(5)}},
		{path: "/last-page", want: map[string]any{}},
	} {
		response, err := app.Test(httptest.NewRequest(fiber.MethodGet, test.path, nil))
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if _, ok := body["data"]; !ok {
			t.Fatalf("%s missing data: %+v", test.path, body)
		}
		for key, value := range test.want {
			if body[key] != value {
				t.Fatalf("%s %s=%v want %v", test.path, key, body[key], value)
			}
		}
	}
}

func TestResponseBuilderRequiresRequestContext(t *testing.T) {
	err := (&ResponseBuilder{}).Send()
	if !errors.Is(err, ErrMissingContext) {
		t.Fatalf("error=%v want %v", err, ErrMissingContext)
	}
}

func TestResponderKeepsConcurrentRequestContextsIsolated(t *testing.T) {
	app := fiber.New()
	app.Get("/:id", func(c fiber.Ctx) error {
		api := For(c)
		return api.Response().Status(fiber.StatusOK).Data(responseFixture{ID: c.Params("id")}).Send()
	})

	const requests = 32
	errs := make(chan error, requests)
	var wg sync.WaitGroup
	for i := range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			want := fmt.Sprintf("item_%d", i)
			response, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/"+want, nil))
			if err != nil {
				errs <- err
				return
			}
			var body responseFixture
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				errs <- err
				return
			}
			if body.ID != want {
				errs <- fmt.Errorf("response ID %q, want %q", body.ID, want)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
