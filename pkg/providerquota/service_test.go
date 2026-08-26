package providerquota

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

func TestPercentAndUtilizationWindows(t *testing.T) {
	now := time.Now()
	percent := parsePercentWindow("Session", map[string]any{"used_percent": 25.5, "reset_after_seconds": 60.0})
	if percent.RemainingPercent != 74.5 || percent.ResetAt == nil || percent.ResetAt.Before(now.Add(55*time.Second)) {
		t.Fatalf("percent window = %+v", percent)
	}
	ratio := parseUtilizationWindow("Weekly", map[string]any{"utilization": 0.42, "resets_at": "2026-08-27T00:00:00Z"})
	if ratio.UsedPercent != 42 || ratio.RemainingPercent != 58 || ratio.ResetAt == nil {
		t.Fatalf("utilization window = %+v", ratio)
	}
}

func TestFetchUsesBearerAndExtraHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret-token" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("chatgpt-account-id"); got != "account-1" {
			t.Errorf("chatgpt-account-id = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rate_limit":{"primary_window":{"used_percent":20}}}`))
	}))
	defer server.Close()

	service := New(server.Client(), nil)
	payload, err := service.fetch(context.Background(), &entities.CredentialRuntime{OAuthAccess: "secret-token"}, http.MethodGet, server.URL, nil, map[string]string{"chatgpt-account-id": "account-1"})
	if err != nil || len(object(payload, "rate_limit")) == 0 {
		t.Fatalf("fetch() payload=%v err=%v", payload, err)
	}
}

func TestAvailabilityExpiresExhaustionCooldown(t *testing.T) {
	service := New(nil, nil)
	service.snapshots["available"] = Snapshot{Windows: []Window{{Name: "Weekly", RemainingPercent: 50}}}
	service.snapshots["empty"] = Snapshot{Windows: []Window{{Name: "Weekly", RemainingPercent: 0}}}
	service.snapshots["reset"] = Snapshot{Windows: []Window{{Name: "Session", RemainingPercent: 0, ResetAt: timePointer(time.Now().Add(-time.Minute))}}}
	service.exhausted["cooldown"] = time.Now().Add(time.Minute)
	service.exhausted["expired"] = time.Now().Add(-time.Minute)

	for id, want := range map[string]bool{"unknown": true, "available": true, "empty": false, "reset": true, "cooldown": false, "expired": true} {
		if got := service.Available(id); got != want {
			t.Errorf("Available(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestMaskAccountNeverReturnsToken(t *testing.T) {
	runtime := &entities.CredentialRuntime{APIKey: "top-secret", OAuthMeta: entities.OAuthMetadata{Email: "person@example.test"}}
	if got := maskAccount(runtime); got != "pe****@example.test" {
		t.Fatalf("maskAccount() = %q", got)
	}
	runtime.OAuthMeta = entities.OAuthMetadata{}
	if got := maskAccount(runtime); got != "connected account" {
		t.Fatalf("fallback mask = %q", got)
	}
}

func timePointer(value time.Time) *time.Time { return &value }
