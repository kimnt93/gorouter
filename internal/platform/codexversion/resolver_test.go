package codexversion

import (
	"context"
	"encoding/json"
	"github.com/kimnt93/gorouter/internal/platform/modeldiscovery"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestResolverUsesOfficialStableReleaseAndSharedCache(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Accept") != "application/vnd.github+json" {
			t.Fatal("accept")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "rust-v0.157.2", "html_url": "synthetic", "draft": false, "prerelease": false})
	}))
	defer server.Close()
	cache := modeldiscovery.NewMemorySnapshots(8)
	one := New(server.Client(), cache, time.Hour)
	one.URL = server.URL
	if got := one.Resolve(context.Background()); got != "0.157.2" {
		t.Fatalf("version=%s", got)
	}
	two := New(&http.Client{Transport: neverRoundTrip{t}}, cache, time.Hour)
	if got := two.Resolve(context.Background()); got != "0.157.2" || calls != 1 {
		t.Fatalf("shared=%s calls=%d", got, calls)
	}
}

type neverRoundTrip struct{ t *testing.T }

func (n neverRoundTrip) RoundTrip(*http.Request) (*http.Response, error) {
	n.t.Fatal("cache miss")
	return nil, nil
}
func TestResolverFailsClosedToBaseline(t *testing.T) {
	for _, payload := range []string{`{"tag_name":"rust-v0.155.0"}`, `{"tag_name":"rust-v9.9.9","prerelease":true}`, `{"tag_name":"main"}`, `{"tag_name":"rust-v1.0.0"}`, `{`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(payload)) }))
		r := New(server.Client(), nil, time.Hour)
		r.URL = server.URL
		if got := r.Resolve(context.Background()); got != Baseline {
			t.Fatalf("payload=%s got=%s", payload, got)
		}
		server.Close()
	}
}
func TestVersionOrdering(t *testing.T) {
	if maxVersion("0.156.0", "0.155.99") != "0.156.0" || maxVersion("0.156.0", "0.156.1") != "0.156.1" || valid("0.156") {
		t.Fatal("ordering")
	}
}

func TestResolverCoalescesConcurrentChecks(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		time.Sleep(20 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "rust-v0.157.0"})
	}))
	defer server.Close()
	r := New(server.Client(), nil, time.Hour)
	r.URL = server.URL
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if got := r.Resolve(context.Background()); got != "0.157.0" {
				t.Errorf("got=%s", got)
			}
		})
	}
	wg.Wait()
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestStartChecksImmediatelyAndStops(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "rust-v0.157.0"})
	}))
	defer server.Close()
	r := New(server.Client(), nil, time.Hour)
	r.URL = server.URL
	ctx, cancel := context.WithCancel(context.Background())
	values := make(chan string, 1)
	r.Start(ctx, time.Hour, func(v string) { values <- v })
	select {
	case v := <-values:
		if v != "0.157.0" {
			t.Fatal(v)
		}
	case <-time.After(time.Second):
		t.Fatal("no startup check")
	}
	cancel()
}
