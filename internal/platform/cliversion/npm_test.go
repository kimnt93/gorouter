package cliversion

import (
	"context"
	"encoding/json"
	"github.com/kimnt93/gorouter/internal/platform/modeldiscovery"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNPMResolverCachesAndNeverDowngrades(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]string{"version": "2.1.280"})
	}))
	defer server.Close()
	cache := modeldiscovery.NewMemorySnapshots(4)
	one := NewNPM(server.Client(), cache, time.Hour, "@anthropic-ai/claude-code", "2.1.260")
	one.URL = server.URL
	if one.Resolve(context.Background()) != "2.1.280" {
		t.Fatal("update")
	}
	two := NewNPM(server.Client(), cache, time.Hour, "@anthropic-ai/claude-code", "2.1.260")
	if two.Resolve(context.Background()) != "2.1.280" || calls != 1 {
		t.Fatal("cache")
	}
}
func TestNPMResolverFailsClosed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"version":"bad"}`)) }))
	defer server.Close()
	r := NewNPM(server.Client(), nil, time.Hour, "pkg", "2.1.260")
	r.URL = server.URL
	if r.Resolve(context.Background()) != "2.1.260" {
		t.Fatal("baseline")
	}
}

func TestStartChecksImmediately(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"version": "2.1.280"})
	}))
	defer server.Close()
	r := NewNPM(server.Client(), nil, time.Hour, "pkg", "2.1.260")
	r.URL = server.URL
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	values := make(chan string, 1)
	r.Start(ctx, time.Hour, func(v string) { values <- v })
	select {
	case v := <-values:
		if v != "2.1.280" {
			t.Fatal(v)
		}
	case <-time.After(time.Second):
		t.Fatal("no startup check")
	}
}
