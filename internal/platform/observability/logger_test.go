package observability

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	otelfiber "github.com/gofiber/contrib/v3/otel"
	"github.com/gofiber/fiber/v3"
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
	log.Logger = NewLogger(&output, "gorouter", "test", "rfc3339")
	t.Cleanup(func() { log.Logger = previousLogger })

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
	if entry["trace_id"] == "00000000000000000000000000000000" || entry["span_id"] == "0000000000000000" {
		t.Fatalf("missing trace correlation: %+v", entry)
	}
}
