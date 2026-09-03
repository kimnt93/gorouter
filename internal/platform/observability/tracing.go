package observability

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"

	"github.com/kimnt93/gorouter/pkg/config"
)

func InitTracer(ctx context.Context, serviceName, environment string, cfg config.TelemetryConfig) (func(context.Context) error, error) {
	if !cfg.Enabled {
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := traceExporter(ctx, cfg)
	if err != nil {
		return nil, err
	}
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(serviceName),
		attribute.String("deployment.environment.name", environment),
	)
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	return provider.Shutdown, nil
}

func traceExporter(ctx context.Context, cfg config.TelemetryConfig) (sdktrace.SpanExporter, error) {
	endpointURL := strings.Contains(cfg.Endpoint, "://")
	switch cfg.Protocol {
	case "grpc":
		options := []otlptracegrpc.Option{}
		if endpointURL {
			options = append(options, otlptracegrpc.WithEndpointURL(cfg.Endpoint))
		} else {
			options = append(options, otlptracegrpc.WithEndpoint(cfg.Endpoint), otlptracegrpc.WithInsecure())
		}
		exporter, err := otlptracegrpc.New(ctx, options...)
		if err != nil {
			return nil, fmt.Errorf("create OTLP gRPC trace exporter: %w", err)
		}
		return exporter, nil
	case "http":
		options := []otlptracehttp.Option{}
		if endpointURL {
			options = append(options, otlptracehttp.WithEndpointURL(cfg.Endpoint))
		} else {
			options = append(options, otlptracehttp.WithEndpoint(cfg.Endpoint), otlptracehttp.WithInsecure())
		}
		exporter, err := otlptracehttp.New(ctx, options...)
		if err != nil {
			return nil, fmt.Errorf("create OTLP HTTP trace exporter: %w", err)
		}
		return exporter, nil
	default:
		return nil, fmt.Errorf("unsupported OTLP protocol %q", cfg.Protocol)
	}
}
