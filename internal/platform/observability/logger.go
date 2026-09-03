package observability

import (
	"errors"
	"io"
	"os"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/trace"
)

func SetupLogger(serviceName, environment, level, timeFormat string) {
	logLevel, err := zerolog.ParseLevel(level)
	if err != nil {
		logLevel = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(logLevel)
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
		status := requestStatus(c, err)

		var event *zerolog.Event
		switch {
		case status >= fiber.StatusInternalServerError:
			event = log.Error()
		case status >= fiber.StatusBadRequest:
			event = log.Warn()
		default:
			event = log.Debug()
		}
		if !event.Enabled() {
			return err
		}
		if err != nil {
			event = event.Err(err)
		}
		spanContext := trace.SpanFromContext(c.Context()).SpanContext()
		event.
			Str("method", c.Method()).
			Str("path", c.Path()).
			Int("status", status).
			Dur("latency", time.Since(started)).
			Str("client_ip", c.IP())
		if spanContext.IsValid() {
			event = event.
				Str("trace_id", spanContext.TraceID().String()).
				Str("span_id", spanContext.SpanID().String())
		}
		event.Msg("request completed")
		return err
	}
}

func requestStatus(c fiber.Ctx, err error) int {
	status := c.Response().StatusCode()
	if err == nil {
		return status
	}
	var fiberError *fiber.Error
	if errors.As(err, &fiberError) {
		return fiberError.Code
	}
	if status < fiber.StatusBadRequest {
		return fiber.StatusInternalServerError
	}
	return status
}
