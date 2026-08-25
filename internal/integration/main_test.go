package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/kimnt93/gorouter/internal/admin"
	"github.com/kimnt93/gorouter/internal/cache"
	"github.com/kimnt93/gorouter/internal/cost"
	"github.com/kimnt93/gorouter/internal/cryptoseal"
	"github.com/kimnt93/gorouter/internal/gateway"
	"github.com/kimnt93/gorouter/internal/llm"
	"github.com/kimnt93/gorouter/internal/store"
)

const testDBURL = "postgres://gorouter:change-me-postgres-password@127.0.0.1:54329/gorouter"

var (
	sealer      *cryptoseal.Sealer
	db          *store.DB
	usageWriter *store.UsageWriter
	gwSrv       *httptest.Server
	adSrv       *httptest.Server
	masterKey   = "test-master-key-123"
)

func TestMain(m *testing.M) {
	if os.Getenv("TEST_DATABASE_URL") == "" && !pgReachable() {
		fmt.Println("SKIP: no test database")
		os.Exit(0)
	}
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = testDBURL
	}
	ctx := context.Background()
	resetDB(ctx, url)
	var err error
	db, err = store.Open(ctx, url)
	must(err)
	must(db.Migrate(ctx))
	must(db.EnsureDefaultTenants(ctx))
	sealer, err = cryptoseal.New("integration-test-key")
	must(err)
	usageWriter = store.NewUsageWriter(db, 256)

	promptCache := cache.New(cache.DefaultConfig())
	httpClient := llm.NewHTTPClient()
	anthroAdapter := &llm.AnthropicAdapter{HTTP: httpClient, OAuthClientID: "mock-client-id"}
	anthroAdapter.Refresh = func(rctx context.Context, rt *llm.CredentialRuntime) error {
		rt.OAuthAccess = "refreshed-access-token"
		return db.UpdateOAuthTokens(rctx, sealer, rt.ID, rt.OAuthAccess, rt.OAuthRefreh)
	}
	gwServer := gateway.NewServer(db, sealer, promptCache, usageWriter,
		&llm.OpenAIAdapter{HTTP: httpClient}, anthroAdapter,
		gateway.Config{Cache: cache.DefaultConfig(), RequestLimit: 20 << 20})
	gwSrv = httptest.NewServer(gwServer.Handler())
	adSrv = httptest.NewServer((&admin.Server{DB: db, Sealer: sealer, Cache: promptCache, MasterKey: masterKey}).Handler())

	code := m.Run()
	usageWriter.Close()
	promptCache.Close()
	gwSrv.Close()
	adSrv.Close()
	db.Close()
	os.Exit(code)
}

func pgReachable() bool {
	conn, err := pgx.Connect(context.Background(), testDBURL)
	if err != nil {
		return false
	}
	_ = conn.Close(context.Background())
	return true
}

func resetDB(ctx context.Context, url string) {
	conn, err := pgx.Connect(ctx, url)
	must(err)
	defer conn.Close(ctx)
	_, err = conn.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`)
	must(err)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

type adminReq struct {
	method string
	path   string
	body   any
}

func adminCall(t *testing.T, r adminReq) (int, []byte) {
	t.Helper()
	var body io.Reader
	if r.body != nil {
		b, err := json.Marshal(r.body)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(r.method, adSrv.URL+r.path, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+masterKey)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res.StatusCode, data
}

type credOut struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func createCred(t *testing.T, name, provider, baseURL string) credOut {
	status, data := adminCall(t, adminReq{"POST", "/admin/credentials", map[string]any{
		"name": name, "provider": provider, "kind": "api_key", "base_url": baseURL, "api_key": "sk-test-" + name,
	}})
	if status != 201 {
		t.Fatalf("create credential %s: %d %s", name, status, data)
	}
	var out credOut
	must(json.Unmarshal(data, &out))
	return out
}

func createModel(t *testing.T, name, strategy string, routes map[string]int) {
	var rs []map[string]any
	for cid, prio := range routes {
		rs = append(rs, map[string]any{"credential_id": cid, "priority": prio, "weight": 1, "enabled": true})
	}
	status, data := adminCall(t, adminReq{"PUT", "/admin/models/" + name, map[string]any{
		"strategy": strategy, "routes": rs,
	}})
	if status != 200 {
		t.Fatalf("upsert model %s: %d %s", name, status, data)
	}
	setPrice(t, name, cost.Prices{InputPerM: 3, OutputPerM: 15, CachedInputPerM: 0.3, CacheWritePerM: 3.75})
}

func setPrice(t *testing.T, model string, p cost.Prices) {
	status, data := adminCall(t, adminReq{"PUT", "/admin/prices/" + model, p})
	if status != 200 {
		t.Fatalf("set price: %d %s", status, data)
	}
}

type keyOut struct {
	ID        string   `json:"id"`
	Plaintext string   `json:"plaintext"`
	Models    []string `json:"models"`
}

func createKey(t *testing.T, tenantID string, models []string, quota *float64) keyOut {
	status, data := adminCall(t, adminReq{"POST", "/admin/api-keys", map[string]any{
		"tenant_id": tenantID, "name": "test-key", "models": models, "monthly_quota_usd": quota,
	}})
	if status != 201 {
		t.Fatalf("create key: %d %s", status, data)
	}
	var out keyOut
	must(json.Unmarshal(data, &out))
	return out
}

func chat(t *testing.T, gwURL, apiKey string, payload map[string]any) (*http.Response, []byte) {
	t.Helper()
	b, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", gwURL+"/v1/chat/completions", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(res.Body)
	res.Body.Close()
	return res, data
}

// --- mock upstreams ---

func openAIMock(t *testing.T, opts ...any) *httptest.Server {
	t.Helper()
	failFirst := 0
	for _, o := range opts {
		if n, ok := o.(int); ok {
			failFirst = n
		}
	}
	var count int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomicAdd(&count)
		if failFirst > 0 && n <= int64(failFirst) {
			w.WriteHeader(500)
			_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			var req struct {
				Model  string `json:"model"`
				Stream bool   `json:"stream"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			content := fmt.Sprintf("reply from %s via %s", req.Model, r.Host)
			in, out := int64(100), int64(20)
			if !req.Stream {
				writeJSONT(w, map[string]any{
					"id": "chatcmpl-x", "object": "chat.completion", "model": req.Model,
					"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": content}, "finish_reason": "stop"}},
					"usage":   map[string]any{"prompt_tokens": in, "completion_tokens": out, "total_tokens": in + out},
				})
				return
			}
			fl := w.(http.Flusher)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			send := func(v any) { b, _ := json.Marshal(v); fmt.Fprintf(w, "data: %s\n\n", b); fl.Flush() }
			send(map[string]any{"id": "c1", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant"}}}})
			for _, word := range strings.Fields(content) {
				send(map[string]any{"id": "c1", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": word + " "}}}})
			}
			send(map[string]any{"id": "c1", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
				"usage": map[string]any{"prompt_tokens": in, "completion_tokens": out}})
			fmt.Fprint(w, "data: [DONE]\n\n")
			fl.Flush()
		case strings.HasSuffix(r.URL.Path, "/models"):
			writeJSONT(w, map[string]any{"object": "list", "data": []any{}})
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func anthropicMock(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/messages") {
			w.WriteHeader(404)
			return
		}
		authz := r.Header.Get("Authorization")
		apikey := r.Header.Get("x-api-key")
		if authz == "" && apikey == "" {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":"no auth"}`))
			return
		}
		if apikey != "" && !strings.HasPrefix(apikey, "sk-test-") {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":"bad key"}`))
			return
		}
		var req struct {
			Model     string `json:"model"`
			System    any    `json:"system"`
			Messages  []any  `json:"messages"`
			MaxTokens any    `json:"max_tokens"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.System == nil || len(req.Messages) == 0 {
			w.WriteHeader(400)
			writeJSONT(w, map[string]any{"error": "missing system or messages"})
			return
		}
		writeJSONT(w, map[string]any{
			"id": "msg_mock", "type": "message", "role": "assistant", "model": req.Model,
			"content":     []any{map[string]any{"type": "text", "text": "anthropic says hi"}},
			"usage":       map[string]any{"input_tokens": 42, "output_tokens": 7, "cache_read_input_tokens": 5, "cache_creation_input_tokens": 9},
			"stop_reason": "end_turn",
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeJSONT(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
