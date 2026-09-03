package observability

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	otelfiber "github.com/gofiber/contrib/v3/otel"
	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestLoggerIncludesServiceEnvironmentAndRFC3339Time(t *testing.T) {
	var output bytes.Buffer
	logger := NewLogger(&output, "gorouter", "local", "rfc3339")
	logger.Info().Msg("ready")

	var entry map[string]any
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatalf("decode log entry: %v", err)
	}
	if entry["service_name"] != "gorouter" || entry["development_environment"] != "local" {
		t.Fatalf("identity fields: %+v", entry)
	}
	value, ok := entry["time"].(string)
	if !ok {
		t.Fatalf("time field: %#v", entry["time"])
	}
	if _, err := time.Parse(time.RFC3339, value); err != nil {
		t.Fatalf("time %q is not RFC3339: %v", value, err)
	}
	if entry["level"] != "info" || entry["message"] != "ready" || entry["caller"] == nil {
		t.Fatalf("structured fields: %+v", entry)
	}
}

func TestTimeFormat(t *testing.T) {
	if got := TimeFormat("rfc3339nano"); got != time.RFC3339Nano {
		t.Fatalf("nano format = %q", got)
	}
	if got := TimeFormat("rfc3339"); got != time.RFC3339 {
		t.Fatalf("default format = %q", got)
	}
}

func TestRequestLogIncludesTraceCorrelation(t *testing.T) {
	var output bytes.Buffer
	previousLogger := log.Logger
	previousLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	log.Logger = NewLogger(&output, "gorouter", "test", "rfc3339")
	t.Cleanup(func() {
		log.Logger = previousLogger
		zerolog.SetGlobalLevel(previousLevel)
	})

	provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })
	app := fiber.New()
	app.Use(otelfiber.Middleware(otelfiber.WithTracerProvider(provider), otelfiber.WithoutMetrics(true)))
	app.Use(RequestLoggingMiddleware())
	app.Get("/healthz", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	response, err := app.Test(httptest.NewRequest("GET", "/healthz", nil))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var entry map[string]any
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatalf("decode request log: %v", err)
	}
	if entry["method"] != "GET" || entry["path"] != "/healthz" || entry["status"] != float64(200) {
		t.Fatalf("request fields: %+v", entry)
	}
	if entry["level"] != "debug" {
		t.Fatalf("level = %#v, want debug", entry["level"])
	}
	if entry["trace_id"] == "00000000000000000000000000000000" || entry["span_id"] == "0000000000000000" {
		t.Fatalf("missing trace correlation: %+v", entry)
	}
}

func TestRequestLogLevelFollowsResponseClass(t *testing.T) {
	tests := []struct {
		name       string
		handler    fiber.Handler
		wantStatus int
		wantLevel  string
	}{
		{name: "success", handler: func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) }, wantStatus: 204, wantLevel: "debug"},
		{name: "client error", handler: func(fiber.Ctx) error { return fiber.ErrNotFound }, wantStatus: 404, wantLevel: "warn"},
		{name: "server error", handler: func(fiber.Ctx) error { return errors.New("synthetic failure") }, wantStatus: 500, wantLevel: "error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			previousLogger := log.Logger
			previousLevel := zerolog.GlobalLevel()
			zerolog.SetGlobalLevel(zerolog.DebugLevel)
			log.Logger = NewLogger(&output, "gorouter", "test", "rfc3339")
			t.Cleanup(func() {
				log.Logger = previousLogger
				zerolog.SetGlobalLevel(previousLevel)
			})

			app := fiber.New()
			app.Use(RequestLoggingMiddleware())
			app.Get("/resource", tt.handler)
			response, err := app.Test(httptest.NewRequest("GET", "/resource", nil))
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != tt.wantStatus {
				t.Fatalf("response status = %d, want %d", response.StatusCode, tt.wantStatus)
			}
			var entry map[string]any
			if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
				t.Fatalf("decode request log: %v", err)
			}
			if entry["level"] != tt.wantLevel || entry["status"] != float64(tt.wantStatus) {
				t.Fatalf("request log: %+v", entry)
			}
			if _, exists := entry["trace_id"]; exists {
				t.Fatalf("trace_id should be omitted without a valid span: %+v", entry)
			}
		})
	}
}

func TestSuccessfulRequestLogIsSuppressedAtInfoLevel(t *testing.T) {
	var output bytes.Buffer
	previousLogger := log.Logger
	previousLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	log.Logger = NewLogger(&output, "gorouter", "test", "rfc3339")
	t.Cleanup(func() {
		log.Logger = previousLogger
		zerolog.SetGlobalLevel(previousLevel)
	})

	app := fiber.New()
	app.Use(RequestLoggingMiddleware())
	app.Get("/healthz", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })
	if _, err := app.Test(httptest.NewRequest("GET", "/healthz", nil)); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatalf("successful request emitted at info level: %s", output.String())
	}
}
