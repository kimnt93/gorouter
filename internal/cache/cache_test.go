package cache

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/kimnt93/gorouter/internal/llm"
)

func body(model, msg string) []byte {
	return []byte(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":%q}]}`, model, msg))
}

func TestKeyScopeIsolation(t *testing.T) {
	c := New(DefaultConfig())
	defer c.Close()
	raw := body("gpt-4o", "hello")
	c.Store("keyA", "tenant1", "gpt-4o", raw, &Entry{Status: 200, ContentType: "application/json", Body: []byte(`{"x":1}`)})
	if _, ok := c.Lookup("keyB", "tenant1", "gpt-4o", raw); ok {
		t.Fatal("different API key must not share cache")
	}
	if _, ok := c.Lookup("keyA", "tenantX", "gpt-4o", raw); !ok {
		t.Fatal("in key scope, tenant is irrelevant for same key id")
	}
	if _, ok := c.Lookup("keyA", "tenant1", "gpt-4o", raw); !ok {
		t.Fatal("same key should hit")
	}
}

func TestTenantScopeSharesAcrossKeys(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Scope = ScopeTenant
	c := New(cfg)
	defer c.Close()
	raw := body("gpt-4o", "hi")
	c.Store("keyA", "t1", "gpt-4o", raw, &Entry{Status: 200, Body: []byte(`{}`)})
	if _, ok := c.Lookup("keyB", "t1", "gpt-4o", raw); !ok {
		t.Fatal("tenant scope should share across keys of same tenant")
	}
	if _, ok := c.Lookup("keyC", "t2", "gpt-4o", raw); ok {
		t.Fatal("cross-tenant leak detected")
	}
}

func TestGlobalScope(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Scope = ScopeGlobal
	c := New(cfg)
	defer c.Close()
	raw := body("m", "q")
	c.Store("k1", "t1", "m", raw, &Entry{Status: 200, Body: []byte(`{}`)})
	if _, ok := c.Lookup("other", "other", "m", raw); !ok {
		t.Fatal("global scope should hit for anyone")
	}
}

func TestModelIsPartOfKey(t *testing.T) {
	c := New(DefaultConfig())
	defer c.Close()
	raw := body("gpt-4o", "q")
	c.Store("k", "t", "gpt-4o", raw, &Entry{Status: 200, Body: []byte(`{}`)})
	if _, ok := c.Lookup("k", "t", "claude-3", raw); ok {
		t.Fatal("model must be part of the cache key")
	}
}

func TestTTLExpiry(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TTL = 50 * time.Millisecond
	c := New(cfg)
	defer c.Close()
	raw := body("m", "q")
	c.Store("k", "t", "m", raw, &Entry{Status: 200, Body: []byte(`{}`)})
	time.Sleep(80 * time.Millisecond)
	if _, ok := c.Lookup("k", "t", "m", raw); ok {
		t.Fatal("expired entry must not be served")
	}
}

func TestLRUEviction(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxTotalBytes = 100
	c := New(cfg)
	defer c.Close()
	for i := 0; i < 20; i++ {
		c.Store("k", "t", "m", body("m", fmt.Sprint(i)), &Entry{Status: 200, Body: make([]byte, 60)})
	}
	st := c.Stats()
	perEntry := int64(60 + 128)
	if st.Entries > 2 || st.Bytes > perEntry*int64(st.Entries)+8 {
		t.Fatalf("eviction failed: %+v", st)
	}
	if st.Evictions == 0 {
		t.Fatal("expected evictions to be counted")
	}
}

func TestOversizedEntryRejected(t *testing.T) {
	cfg := DefaultConfig()
	c := New(cfg)
	defer c.Close()
	ok := c.Store("k", "t", "m", body("m", "q"), &Entry{Status: 200, Body: make([]byte, cfg.MaxEntryBytes+1)})
	if ok {
		t.Fatal("oversized entry must be rejected")
	}
}

func TestDeterministicGate(t *testing.T) {
	cases := []struct {
		name string
		req  string
		want bool
	}{
		{"default params", `{"model":"m","messages":[]}`, true},
		{"temp zero", `{"model":"m","temperature":0,"messages":[]}`, true},
		{"top_p one", `{"model":"m","top_p":1,"messages":[]}`, true},
		{"temp nonzero", `{"model":"m","temperature":0.7,"messages":[]}`, false},
		{"top_p partial", `{"model":"m","top_p":0.9,"messages":[]}`, false},
		{"n two", `{"model":"m","n":2,"messages":[]}`, false},
		{"with tools", `{"model":"m","tools":[{"type":"function","function":{"name":"f"}}],"messages":[]}`, false},
		{"penalty", `{"model":"m","frequency_penalty":0.5,"messages":[]}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var r llm.ChatRequest
			if err := jsonUnmarshal(tc.req, &r); err != nil {
				t.Fatal(err)
			}
			if got := Deterministic(&r); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func jsonUnmarshal(s string, v any) error {
	return json.Unmarshal([]byte(s), v)
}
