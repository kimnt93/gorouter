package observability

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.opentelemetry.io/otel"

	"github.com/kimnt93/gorouter/pkg/config"
)

func TestInitTracerDisabledDoesNotRequireEndpoint(t *testing.T) {
	shutdown, err := InitTracer(t.Context(), "gorouter", "test", config.TelemetryConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPTraceExporterSendsSpans(t *testing.T) {
	requests := make(chan []byte, 1)
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/traces" {
			t.Errorf("path = %q", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read export: %v", err)
		}
		requests <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer collector.Close()

	shutdown, err := InitTracer(t.Context(), "gorouter", "test", config.TelemetryConfig{
		Enabled: true, Protocol: "http", Endpoint: collector.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, span := otel.Tracer("gorouter-test").Start(t.Context(), "test-span")
	span.End()
	shutdownCtx, cancel := timeContext(t, 5*time.Second)
	defer cancel()
	if err := shutdown(shutdownCtx); err != nil {
		t.Fatal(err)
	}

	select {
	case body := <-requests:
		if len(body) == 0 {
			t.Fatal("collector received an empty trace payload")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("collector did not receive a trace export")
	}
}

func timeContext(t *testing.T, timeout time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(t.Context(), timeout)
}
