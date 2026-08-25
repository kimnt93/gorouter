package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAdminAuth(t *testing.T) {
	status, _ := adminCall(t, adminReq{"GET", "/admin/api-keys", nil})
	if status != 200 {
		t.Fatalf("valid master key rejected: %d", status)
	}
	req, _ := httpNewRequest("GET", adSrv.URL+"/admin/api-keys", nil)
	res, err := httpDo(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 401 {
		t.Fatalf("expected 401 without key, got %d", res.StatusCode)
	}
}

func TestNonStreamPassthroughUsageAndCost(t *testing.T) {
	up := openAIMock(t)
	cred := createCred(t, "openai-main", "openai-compatible", up.URL)
	createModel(t, "mock-model", "priority", map[string]int{cred.ID: 0})
	key := createKey(t, "tenant_default", []string{"mock-model"}, nil)

	res, data := chat(t, gwSrv.URL, key.Plaintext, map[string]any{
		"model":    "mock-model",
		"messages": []map[string]any{{"role": "user", "content": "hello world"}},
	})
	if res.StatusCode != 200 {
		t.Fatalf("status %d: %s", res.StatusCode, data)
	}
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == "" {
		t.Fatalf("empty response: %s", data)
	}
	if resp.Usage.PromptTokens != 100 || resp.Usage.CompletionTokens != 20 {
		t.Fatalf("usage wrong: %+v", resp.Usage)
	}

	waitFlush()
	sum := usageSummary(t)
	if sum.Requests < 1 || sum.CostUSD <= 0 {
		t.Fatalf("usage not recorded: %+v", sum)
	}
	want := (100*3.0 + 20*15.0) / 1e6
	mu := sum.ByModel["mock-model"]
	if mu.Requests != 1 {
		t.Fatalf("mock-model should have exactly 1 request in this summary window, got %+v", mu)
	}
	if diff := mu.CostUSD - want; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("cost %f want %f", mu.CostUSD, want)
	}
}

func TestStreaming(t *testing.T) {
	up := openAIMock(t)
	cred := createCred(t, "stream-cred", "openai-compatible", up.URL)
	createModel(t, "stream-model", "priority", map[string]int{cred.ID: 0})
	key := createKey(t, "tenant_default", []string{"stream-model"}, nil)

	payload := map[string]any{
		"model":    "stream-model",
		"messages": []map[string]any{{"role": "user", "content": "tell me a story"}},
		"stream":   true,
	}
	b, _ := json.Marshal(payload)
	req, _ := httpNewRequest("POST", gwSrv.URL+"/v1/chat/completions", bytesReader(string(b)))
	req.Header.Set("Authorization", "Bearer "+key.Plaintext)
	res, err := httpDo(req)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64<<10)
	var sb strings.Builder
	for {
		n, rerr := res.Body.Read(buf)
		sb.Write(buf[:n])
		if rerr != nil {
			break
		}
	}
	body := sb.String()
	if !strings.Contains(body, "chat.completion.chunk") || !strings.Contains(body, "[DONE]") {
		t.Fatalf("bad stream body: %.300s", body)
	}
	if !strings.Contains(body, `"prompt_tokens":100`) {
		t.Fatalf("stream usage chunk missing: tail %.300s", body[len(body)-400:])
	}
	waitFlush()
	sum := usageSummary(t)
	if sum.PromptTok < 100 {
		t.Fatalf("stream usage not logged: %+v", sum)
	}
}

func TestRoundRobinDistribution(t *testing.T) {
	upA := openAIMock(t)
	upB := openAIMock(t)
	credA := createCred(t, "rr-a", "openai-compatible", upA.URL)
	credB := createCred(t, "rr-b", "openai-compatible", upB.URL)
	createModel(t, "rr-model", "round_robin", map[string]int{credA.ID: 0, credB.ID: 0})
	key := createKey(t, "tenant_default", []string{"rr-model"}, nil)

	for i := 0; i < 6; i++ {
		res, data := chat(t, gwSrv.URL, key.Plaintext, map[string]any{
			"model": "rr-model", "messages": []map[string]any{{"role": "user", "content": fmt.Sprint(i)}},
		})
		if res.StatusCode != 200 {
			t.Fatalf("req %d failed: %d %s", i, res.StatusCode, data)
		}
	}
	waitFlush()
	aHits := countRecentCred(t, credA.ID)
	bHits := countRecentCred(t, credB.ID)
	if aHits != 3 || bHits != 3 {
		t.Fatalf("round robin uneven: A=%d B=%d (want 3/3)", aHits, bHits)
	}
}

func TestPriorityFailover(t *testing.T) {
	upBad := openAIMock(t, 100000)
	upGood := openAIMock(t)
	credBad := createCred(t, "prio-bad", "openai-compatible", upBad.URL)
	credGood := createCred(t, "prio-good", "openai-compatible", upGood.URL)
	createModel(t, "failover-model", "priority", map[string]int{credBad.ID: 10, credGood.ID: 5})
	key := createKey(t, "tenant_default", []string{"failover-model"}, nil)

	res, data := chat(t, gwSrv.URL, key.Plaintext, map[string]any{
		"model": "failover-model", "messages": []map[string]any{{"role": "user", "content": "x"}},
	})
	if res.StatusCode != 200 {
		t.Fatalf("failover did not succeed: %d %s", res.StatusCode, data)
	}
	waitFlush()
	if got := countRecentCred(t, credGood.ID); got < 1 {
		t.Fatalf("expected fallback credential usage, got %d", got)
	}
}

func TestQuotaEnforcement(t *testing.T) {
	up := openAIMock(t)
	cred := createCred(t, "quota-cred", "openai-compatible", up.URL)
	createModel(t, "quota-model", "priority", map[string]int{cred.ID: 0})
	quota := 0.001
	key := createKey(t, "tenant_default", []string{"quota-model"}, &quota)

	res, data := chat(t, gwSrv.URL, key.Plaintext, map[string]any{
		"model": "quota-model", "messages": []map[string]any{{"role": "user", "content": strings.Repeat("a", 8000)}},
	})
	if res.StatusCode != 429 {
		t.Fatalf("expected 429 quota rejection on first oversized request, got %d %s", res.StatusCode, data)
	}
	if !strings.Contains(string(data), "quota") && !strings.Contains(string(data), "exceeded") {
		t.Fatalf("error body should mention quota: %s", data)
	}
}

func TestQuotaSettlesAfterSpend(t *testing.T) {
	up := openAIMock(t)
	cred := createCred(t, "qs-cred", "openai-compatible", up.URL)
	createModel(t, "qs-model", "priority", map[string]int{cred.ID: 0})
	quota := 0.02
	key := createKey(t, "tenant_default", []string{"qs-model"}, &quota)

	ok := false
	for i := 0; i < 50; i++ {
		res, _ := chat(t, gwSrv.URL, key.Plaintext, map[string]any{
			"model":    "qs-model",
			"messages": []map[string]any{{"role": "user", "content": fmt.Sprintf("%s-%d", strings.Repeat("b", 400), i)}},
		})
		if res.StatusCode == 429 {
			ok = true
			break
		}
		if res.StatusCode != 200 {
			t.Fatalf("unexpected status %d at iter %d", res.StatusCode, i)
		}
	}
	if !ok {
		t.Fatal("quota never blocked despite repeated spend")
	}
	waitFlush()
	spent := monthSpend(t, key.ID)
	if spent <= 0 {
		t.Fatalf("spend should be positive, got %f", spent)
	}
}

func TestPromptCacheMultiTenantIsolation(t *testing.T) {
	up := openAIMock(t)
	cred := createCred(t, "cache-cred", "openai-compatible", up.URL)
	createModel(t, "cache-model", "priority", map[string]int{cred.ID: 0})
	keyA := createKey(t, "tenant_default", []string{"cache-model"}, nil)
	tenantID, _ := createTenant(t, "cache-tenant")
	keyB := createKey(t, tenantID, []string{"cache-model"}, nil)

	payload := func(msg string) map[string]any {
		return map[string]any{
			"model":       "cache-model",
			"temperature": 0,
			"messages":    []map[string]any{{"role": "user", "content": msg}},
		}
	}

	res1, d1 := chat(t, gwSrv.URL, keyA.Plaintext, payload("cached hello"))
	if res1.Header.Get("X-Cache") != "miss" {
		t.Fatalf("first request should be miss, got %q (%s)", res1.Header.Get("X-Cache"), d1)
	}
	res2, _ := chat(t, gwSrv.URL, keyA.Plaintext, payload("cached hello"))
	if res2.Header.Get("X-Cache") != "hit" {
		t.Fatalf("second identical request should hit cache")
	}
	res3, _ := chat(t, gwSrv.URL, keyB.Plaintext, payload("cached hello"))
	if res3.Header.Get("X-Cache") != "miss" {
		t.Fatalf("cross-key request must NOT hit cache (multi-tenant isolation)")
	}
	res4, _ := chat(t, gwSrv.URL, keyA.Plaintext, payload("different message"))
	if res4.Header.Get("X-Cache") != "miss" {
		t.Fatal("different message must miss")
	}

	st := cacheStats(t)
	if st.Hits < 1 {
		t.Fatalf("cache stats should record hits: %+v", st)
	}
}

func TestCacheDisabledForNondeterministicRequests(t *testing.T) {
	up := openAIMock(t)
	cred := createCred(t, "nc-cred", "openai-compatible", up.URL)
	createModel(t, "nc-model", "priority", map[string]int{cred.ID: 0})
	key := createKey(t, "tenant_default", []string{"nc-model"}, nil)

	payload := map[string]any{
		"model":       "nc-model",
		"temperature": 0.7,
		"messages":    []map[string]any{{"role": "user", "content": "creative"}},
	}
	res1, _ := chat(t, gwSrv.URL, key.Plaintext, payload)
	res2, _ := chat(t, gwSrv.URL, key.Plaintext, payload)
	if res1.Header.Get("X-Cache") == "hit" || res2.Header.Get("X-Cache") == "hit" {
		t.Fatal("nondeterministic requests must not be cached")
	}
	if res2.Header.Get("X-Cache") != "off" {
		t.Fatalf("expected X-Cache off, got %q", res2.Header.Get("X-Cache"))
	}
}

func TestModelNotAllowedForKey(t *testing.T) {
	up := openAIMock(t)
	cred := createCred(t, "na-cred", "openai-compatible", up.URL)
	createModel(t, "secret-model", "priority", map[string]int{cred.ID: 0})
	key := createKey(t, "tenant_default", []string{"other-model-exists"}, nil)

	createModel(t, "other-model-exists", "priority", map[string]int{cred.ID: 0})

	res, _ := chat(t, gwSrv.URL, key.Plaintext, map[string]any{
		"model": "secret-model", "messages": []map[string]any{{"role": "user", "content": "hi"}},
	})
	if res.StatusCode != 403 {
		t.Fatalf("expected 403 for unauthorized model, got %d", res.StatusCode)
	}
}

func TestAnthropicTranslationAndOAuthRefresh(t *testing.T) {
	up := anthropicMock(t)
	status, data := adminCall(t, adminReq{"POST", "/admin/credentials", map[string]any{
		"name": "claude-oauth", "provider": "anthropic", "kind": "oauth",
		"base_url": up.URL, "oauth_access": "expired-access-token", "oauth_refresh": "refresh-token-1",
	}})
	if status != 201 {
		t.Fatalf("oauth credential create failed: %d %s", status, data)
	}
	var cred credOut
	must(json.Unmarshal(data, &cred))
	createModel(t, "claude-via-oauth", "priority", map[string]int{cred.ID: 0})
	key := createKey(t, "tenant_default", []string{"claude-via-oauth"}, nil)

	res, raw := chat(t, gwSrv.URL, key.Plaintext, map[string]any{
		"model":    "claude-via-oauth",
		"messages": []map[string]any{{"role": "system", "content": "be terse"}, {"role": "user", "content": "hi"}},
	})
	if res.StatusCode != 200 {
		t.Fatalf("anthropic flow failed: %d %s", res.StatusCode, raw)
	}
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
			CacheReadTokens  int64 `json:"cache_read_tokens"`
			CacheWriteTokens int64 `json:"cache_write_tokens"`
		} `json:"usage"`
	}
	must(json.Unmarshal(raw, &resp))
	if resp.Choices[0].Message.Content != "anthropic says hi" {
		t.Fatalf("translation wrong: %s", raw)
	}
	if resp.Usage.CacheReadTokens != 5 || resp.Usage.CacheWriteTokens != 9 {
		t.Fatalf("cache token mapping wrong: %+v", resp.Usage)
	}
	var stored string
	must(db.Pool.QueryRow(testCtx(), `SELECT encode(oauth_blob_enc,'escape') FROM credentials WHERE id=$1`, cred.ID).Scan(&stored))
	if strings.Contains(stored, "refreshed-access-token") {
		t.Fatal("oauth tokens must be stored encrypted")
	}
}

func TestCredentialSealedAtRest(t *testing.T) {
	up := httptestNewServer(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	defer up.Close()
	cred := createCred(t, "sealed-check", "openai-compatible", up.URL)
	var blob []byte
	must(db.Pool.QueryRow(testCtx(), `SELECT api_key_enc FROM credentials WHERE id=$1`, cred.ID).Scan(&blob))
	if len(blob) == 0 {
		t.Fatal("api key not persisted")
	}
	if containsStr(blob, "sk-test-sealed-check") {
		t.Fatal("credential stored in plaintext!")
	}
	plain, err := sealer.Open(blob)
	if err != nil || string(plain) != "sk-test-sealed-check" {
		t.Fatalf("seal/open mismatch: %v %q", err, plain)
	}
}

// --- small helpers ---

var ctxBg = contextBackground()

func waitFlush() {
	time.Sleep(600 * time.Millisecond)
}

func usageSummary(t *testing.T) summaryShape {
	t.Helper()
	status, data := adminCall(t, adminReq{"GET", "/admin/usage/summary?range=7d", nil})
	if status != 200 {
		t.Fatalf("summary: %d %s", status, data)
	}
	var s summaryShape
	must(json.Unmarshal(data, &s))
	return s
}

type summaryShape struct {
	Requests     int64   `json:"requests"`
	CacheHits    int64   `json:"cache_hits"`
	CostUSD      float64 `json:"cost_usd"`
	PromptTok    int64   `json:"prompt_tokens"`
	CompletionTo int64   `json:"completion_tokens"`
	CacheReadTok int64   `json:"cache_read_tokens"`
	ByModel      map[string]struct {
		Requests int64   `json:"requests"`
		CostUSD  float64 `json:"cost_usd"`
	} `json:"by_model"`
	ByKey map[string]struct {
		Requests int64 `json:"requests"`
	} `json:"by_key"`
}

type cacheShape struct {
	Hits uint64 `json:"hits"`
}

func cacheStats(t *testing.T) cacheShape {
	t.Helper()
	status, data := adminCall(t, adminReq{"GET", "/admin/cache/stats", nil})
	if status != 200 {
		t.Fatalf("cache stats: %d %s", status, data)
	}
	var s cacheShape
	must(json.Unmarshal(data, &s))
	return s
}

func countRecentCred(t *testing.T, credID string) int {
	t.Helper()
	status, data := adminCall(t, adminReq{"GET", "/admin/usage/recent?limit=500", nil})
	if status != 200 {
		t.Fatalf("recent: %d %s", status, data)
	}
	var evs []struct {
		CredentialID string `json:"credential_id"`
	}
	must(json.Unmarshal(data, &evs))
	n := 0
	for _, e := range evs {
		if e.CredentialID == credID {
			n++
		}
	}
	return n
}

func monthSpend(t *testing.T, keyID string) float64 {
	t.Helper()
	var spent float64
	must(db.Pool.QueryRow(testCtx(), `SELECT COALESCE(SUM(cost_usd),0) FROM usage_events WHERE api_key_id=$1 AND ts >= date_trunc('month', now())`, keyID).Scan(&spent))
	return spent
}

func createTenant(t *testing.T, name string) (string, error) {
	status, data := adminCall(t, adminReq{"POST", "/admin/tenants", map[string]any{"name": name}})
	if status != 201 {
		return "", fmt.Errorf("create tenant: %d %s", status, data)
	}
	var out struct {
		ID string `json:"id"`
	}
	must(json.Unmarshal(data, &out))
	return out.ID, nil
}
