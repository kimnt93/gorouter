package observability

import (
	"io"
	"os"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/trace"
)

func SetupLogger(serviceName, environment, timeFormat string) {
	log.Logger = NewLogger(os.Stdout, serviceName, environment, timeFormat)
}

func NewLogger(output io.Writer, serviceName, environment, timeFormat string) zerolog.Logger {
	zerolog.TimeFieldFormat = TimeFormat(timeFormat)
	return zerolog.New(output).With().
		Timestamp().
		Caller().
		Str("service_name", serviceName).
		Str("development_environment", environment).
		Logger()
}

func TimeFormat(value string) string {
	if value == "rfc3339nano" {
		return time.RFC3339Nano
	}
	return time.RFC3339
}

func RequestLoggingMiddleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		started := time.Now()
		err := c.Next()
		spanContext := trace.SpanFromContext(c.Context()).SpanContext()

		event := log.Info()
		if err != nil {
			event = log.Error().Err(err)
		}
		event.
			Str("method", c.Method()).
			Str("path", c.Path()).
			Int("status", c.Response().StatusCode()).
			Dur("latency", time.Since(started)).
			Str("client_ip", c.IP()).
			Str("trace_id", spanContext.TraceID().String()).
			Str("span_id", spanContext.SpanID().String()).
			Msg("Request processed")
		return err
	}
}
