package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kimnt93/gorouter/pkg/entities"
)

func TestDevinCloudProbeAndStaticCapabilityCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/self" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer cog_synthetic" {
			t.Error("missing bearer credential")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"principal_type":"service_user","service_user_id":"synthetic","service_user_name":"test"}`))
	}))
	defer server.Close()

	a := &DevinCloudAdapter{HTTP: server.Client()}
	cr := &entities.CredentialRuntime{Provider: "devin", APIKey: "cog_synthetic", BaseURL: server.URL}
	if status, err := a.Probe(context.Background(), cr); err != nil || status != http.StatusOK {
		t.Fatalf("status=%d err=%v", status, err)
	}
	models, err := a.DiscoverModels(context.Background(), cr)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "devin" || models[0].APIFormat != "agent/sessions" {
		t.Fatalf("catalog = %+v", models)
	}
}

func TestDevinCloudProbeClassifiesAuthAndDoesNotExposeBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"detail":"SENSITIVE upstream diagnostic"}`))
	}))
	defer server.Close()
	a := &DevinCloudAdapter{HTTP: server.Client()}
	status, err := a.Probe(context.Background(), &entities.CredentialRuntime{APIKey: "cog_synthetic", BaseURL: server.URL})
	if status != http.StatusUnauthorized || err == nil || err.Error() != "Devin Cloud authentication failed" {
		t.Fatalf("status=%d err=%v", status, err)
	}
	if status, err = a.Probe(context.Background(), &entities.CredentialRuntime{APIKey: "apk_user_synthetic"}); status != http.StatusBadRequest || err == nil {
		t.Fatalf("legacy status=%d err=%v", status, err)
	}
}
