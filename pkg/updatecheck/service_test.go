package updatecheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckVersionAndSafeMetadata(t *testing.T) {
	old := Version
	Version = "v0.2.1"
	defer func() { Version = old }()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v0.2.2","html_url":"https://github.com/kimnt93/gorouter/releases/tag/v0.2.2","published_at":"2026-09-17T00:00:00Z"}`))
	}))
	defer server.Close()
	status, err := (Service{Client: server.Client(), URL: server.URL}).Check(context.Background())
	if err != nil || !status.UpdateAvailable || status.Image != "ghcr.io/kimnt93/gorouter:v0.2.2" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	Version = ""
	status, err = (Service{Client: server.Client(), URL: server.URL}).Check(context.Background())
	if err != nil || status.UpdateAvailable || status.Installed != "unknown" {
		t.Fatalf("unknown version=%+v err=%v", status, err)
	}
}
func TestRejectsInvalidReleaseTag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"latest;rm -rf /","html_url":"https://example.org"}`))
	}))
	defer server.Close()
	if _, err := (Service{Client: server.Client(), URL: server.URL}).Check(context.Background()); err == nil {
		t.Fatal("unsafe release accepted")
	}
}
