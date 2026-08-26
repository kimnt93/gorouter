package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/kimnt93/gorouter/pkg/apikey"
	"github.com/kimnt93/gorouter/pkg/chat"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/modelroute"
	"github.com/kimnt93/gorouter/platform/llm"
)

func TestRetryableStatus(t *testing.T) {
	for _, status := range []int{408, 429, 500, 502, 503, 504, 529} {
		if !retryableStatus(status) {
			t.Errorf("status %d should retry", status)
		}
	}
	for _, status := range []int{400, 401, 403, 404, 409, 422, 501} {
		if retryableStatus(status) {
			t.Errorf("status %d must not retry", status)
		}
	}
}

func TestGatewayRejectsUnknownProvider(t *testing.T) {
	if adapter, ok := (&Gateway{}).adapter("unknown"); ok || adapter != nil {
		t.Fatalf("unknown provider resolved to %#v", adapter)
	}
}

type gatewayKeyRepo struct{ key *entities.ApiKey }

func (r gatewayKeyRepo) Create(context.Context, string, string, []string, []string, *float64, *int) (*entities.ApiKey, error) {
	return nil, nil
}
func (r gatewayKeyRepo) GetBySecret(context.Context, string) (*entities.ApiKey, error) {
	return r.key, nil
}
func (r gatewayKeyRepo) GetByID(context.Context, string) (*entities.ApiKey, error) { return r.key, nil }
func (r gatewayKeyRepo) GetByIDForTenant(context.Context, string, string) (*entities.ApiKey, error) {
	return r.key, nil
}
func (r gatewayKeyRepo) List(context.Context) ([]entities.ApiKey, error) { return nil, nil }
func (r gatewayKeyRepo) ListByTenant(context.Context, string) ([]entities.ApiKey, error) {
	return nil, nil
}
func (r gatewayKeyRepo) Patch(context.Context, string, *bool, *[]string, *[]string, **float64, **int) error {
	return nil
}
func (r gatewayKeyRepo) PatchForTenant(context.Context, string, string, *bool, *[]string, *[]string, **float64, **int) error {
	return nil
}
func (r gatewayKeyRepo) Delete(context.Context, string) error                  { return nil }
func (r gatewayKeyRepo) DeleteForTenant(context.Context, string, string) error { return nil }

type gatewayCredRepo struct {
	routes   []entities.RouteCandidate
	runtimes map[string]*entities.CredentialRuntime
}

func (r gatewayCredRepo) Create(context.Context, entities.CredentialInput, entities.SecretBox) (*entities.Credential, error) {
	return nil, nil
}
func (r gatewayCredRepo) List(context.Context) ([]entities.Credential, error) { return nil, nil }
func (r gatewayCredRepo) Update(context.Context, entities.SecretBox, string, entities.CredentialUpdate) (*entities.Credential, error) {
	return nil, nil
}
func (r gatewayCredRepo) Delete(context.Context, string) error { return nil }
func (r gatewayCredRepo) Runtime(_ context.Context, _ entities.SecretBox, id string) (*entities.CredentialRuntime, error) {
	return r.runtimes[id], nil
}
func (r gatewayCredRepo) UpdateOAuthTokens(context.Context, entities.SecretBox, string, string, string) error {
	return nil
}
func (r gatewayCredRepo) RoutesForModel(context.Context, string) ([]entities.RouteCandidate, error) {
	return r.routes, nil
}

type gatewayModelRepo struct{ model entities.ModelDef }

func (r gatewayModelRepo) Upsert(context.Context, entities.ModelDef) error { return nil }
func (r gatewayModelRepo) Delete(context.Context, string) error            { return nil }
func (r gatewayModelRepo) List(context.Context) ([]entities.ModelDef, error) {
	return []entities.ModelDef{r.model}, nil
}
func (r gatewayModelRepo) SetPrice(context.Context, string, entities.Price) error { return nil }
func (r gatewayModelRepo) DeletePrice(context.Context, string) error              { return nil }
func (r gatewayModelRepo) ListPrices(context.Context) (map[string]entities.Price, error) {
	return map[string]entities.Price{}, nil
}

type gatewayUpstream struct {
	statuses map[string]int
	calls    []string
}

type gatewayStreamUpstream struct{}

func (gatewayStreamUpstream) Send(context.Context, *entities.CredentialRuntime, string, []byte) (*entities.UpstreamResult, error) {
	body := "data: {\"id\":\"stream-1\",\"object\":\"chat.completion.chunk\",\"model\":\"upstream-a\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"stream works\"},\"finish_reason\":\"\"}]}\n\n" +
		"data: {\"id\":\"stream-1\",\"object\":\"chat.completion.chunk\",\"model\":\"upstream-a\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":2}}\n\n" +
		"data: [DONE]\n\n"
	return &entities.UpstreamResult{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func TestGatewayDoesNotCloseStreamingBodyBeforeWriterRuns(t *testing.T) {
	key := &entities.ApiKey{ID: "key-1", TenantID: "tenant-1", Models: []string{"model-a"}, Scopes: []string{entities.ScopeChat}, Enabled: true}
	runtime := &entities.CredentialRuntime{ID: "cred-a", Provider: entities.ProviderOpenAICompatible, Kind: entities.KindAPIKey}
	gateway := &Gateway{
		Keys:   apikey.NewService(gatewayKeyRepo{key}, func(string) string { return "" }, func() string { return "" }),
		Creds:  credential.NewService(gatewayCredRepo{routes: []entities.RouteCandidate{{CredentialID: "cred-a"}}, runtimes: map[string]*entities.CredentialRuntime{"cred-a": runtime}}, nil),
		Models: modelroute.NewService(gatewayModelRepo{model: entities.ModelDef{Name: "model-a", UpstreamModel: "upstream-a", Strategy: chat.StrategyPriority, Enabled: true}}),
		OpenAI: gatewayStreamUpstream{}, Selector: &chat.Selector{}, Health: chat.NewHealth(),
	}
	app := fiber.New()
	app.Post("/v1/chat/completions", func(c fiber.Ctx) error {
		c.Locals(localSession, &entities.Session{Role: entities.RoleAPIKey, KeyID: key.ID, TenantID: key.TenantID, Scopes: []string{entities.ScopeChat}})
		return gateway.Chat(c)
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"model-a","messages":[{"role":"user","content":"hi"}],"stream":true,"temperature":0.7}`))
	response, err := app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(body), "stream works") || !strings.Contains(string(body), "data: [DONE]") {
		t.Fatalf("stream body = %s", body)
	}
}

type gatewayPriceResolver struct {
	price entities.Price
}

func (r gatewayPriceResolver) Resolve(_, _ string) (entities.Price, bool) { return r.price, true }
func (r gatewayPriceResolver) Estimates(_, _ string, prompt, completion int64) entities.PriceEstimates {
	return entities.EstimateCosts(&r.price, prompt, completion, r.price.CachedInputPerM > 0)
}

func TestGatewayUsesPriceResolverFallback(t *testing.T) {
	gateway := &Gateway{
		Models:  modelroute.NewService(gatewayModelRepo{}),
		Pricing: gatewayPriceResolver{price: entities.Price{InputPerM: 3}},
	}
	price, ok, err := gateway.resolvePrice(context.Background(), &entities.ModelDef{Name: "alias", UpstreamModel: "provider/model"})
	if err != nil || !ok || price.InputPerM != 3 {
		t.Fatalf("resolved price=%+v ok=%v err=%v", price, ok, err)
	}
}

func (u *gatewayUpstream) Send(_ context.Context, runtime *entities.CredentialRuntime, _ string, _ []byte) (*entities.UpstreamResult, error) {
	u.calls = append(u.calls, runtime.ID)
	status := u.statuses[runtime.ID]
	body := `{"id":"ok","object":"chat.completion","model":"model-a","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`
	return &entities.UpstreamResult{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func testGatewayApp(statuses map[string]int) (*fiber.App, *gatewayUpstream) {
	return testGatewayAppWithStrategy(statuses, chat.StrategyPriority)
}

func testGatewayAppWithStrategy(statuses map[string]int, strategy string) (*fiber.App, *gatewayUpstream) {
	key := &entities.ApiKey{ID: "key-1", TenantID: "tenant-1", Models: []string{"model-a"}, Scopes: []string{entities.ScopeChat}, Enabled: true}
	routes := []entities.RouteCandidate{{CredentialID: "cred-a", Priority: 10}, {CredentialID: "cred-b", Priority: 5}}
	runtimes := map[string]*entities.CredentialRuntime{
		"cred-a": {ID: "cred-a", Provider: entities.ProviderOpenAICompatible, Kind: entities.KindAPIKey},
		"cred-b": {ID: "cred-b", Provider: entities.ProviderOpenAICompatible, Kind: entities.KindAPIKey},
	}
	upstream := &gatewayUpstream{statuses: statuses}
	gateway := &Gateway{
		Keys:   apikey.NewService(gatewayKeyRepo{key}, func(string) string { return "" }, func() string { return "" }),
		Creds:  credential.NewService(gatewayCredRepo{routes: routes, runtimes: runtimes}, nil),
		Models: modelroute.NewService(gatewayModelRepo{model: entities.ModelDef{Name: "model-a", UpstreamModel: "upstream-a", Strategy: strategy, Enabled: true}}),
		OpenAI: upstream, Selector: &chat.Selector{}, Health: chat.NewHealth(),
	}
	app := fiber.New()
	app.Post("/v1/chat/completions", func(c fiber.Ctx) error {
		c.Locals(localSession, &entities.Session{Role: entities.RoleAPIKey, KeyID: key.ID, TenantID: key.TenantID, Scopes: []string{entities.ScopeChat}})
		return gateway.Chat(c)
	})
	return app, upstream
}

func TestGatewayRoundRobinDistributesRequests(t *testing.T) {
	app, upstream := testGatewayAppWithStrategy(map[string]int{"cred-a": http.StatusOK, "cred-b": http.StatusOK}, chat.StrategyRoundRobin)
	for i := 0; i < 4; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"model-a","messages":[{"role":"user","content":"hi"}],"temperature":0.7}`))
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("request %d status=%d", i, res.StatusCode)
		}
	}
	want := []string{"cred-a", "cred-b", "cred-a", "cred-b"}
	if len(upstream.calls) != len(want) {
		t.Fatalf("calls=%v", upstream.calls)
	}
	for i := range want {
		if upstream.calls[i] != want[i] {
			t.Fatalf("round-robin calls=%v want=%v", upstream.calls, want)
		}
	}
}

func TestGatewayDoesNotRetryOrdinary4xx(t *testing.T) {
	app, upstream := testGatewayApp(map[string]int{"cred-a": http.StatusBadRequest, "cred-b": http.StatusOK})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"model-a","messages":[{"role":"user","content":"hi"}],"temperature":0.7}`))
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest || len(upstream.calls) != 1 || upstream.calls[0] != "cred-a" {
		t.Fatalf("status=%d calls=%v", res.StatusCode, upstream.calls)
	}
}

func TestGatewayFailsOverRetryableStatus(t *testing.T) {
	app, upstream := testGatewayApp(map[string]int{"cred-a": http.StatusServiceUnavailable, "cred-b": http.StatusOK})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"model-a","messages":[{"role":"user","content":"hi"}],"temperature":0.7}`))
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || len(upstream.calls) != 2 {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("status=%d calls=%v body=%s", res.StatusCode, upstream.calls, body)
	}
}

func TestReplayStreamHasRequiredHeadersUsageAndDone(t *testing.T) {
	response := llm.Response{
		ID: "chatcmpl-cached", Object: "chat.completion", Model: "model-a",
		Choices: []llm.Choice{{Index: 0, Message: &llm.ResponseMessage{Role: "assistant", Content: "cached reply"}, FinishReason: "stop"}},
		Usage:   llm.Usage{PromptTokens: 7, CompletionTokens: 3},
	}
	body, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	entry := &chat.CacheEntry{Status: 200, ContentType: "application/json", Body: body, Stream: true, PromptTok: 7, Completion: 3}
	app := fiber.New()
	app.Get("/stream", func(c fiber.Ctx) error {
		c.Set("X-Cache", "hit")
		return (&Gateway{}).replayStream(c, entry)
	})
	res, err := app.Test(httptest.NewRequest("GET", "/stream", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	got, _ := io.ReadAll(res.Body)
	if res.Header.Get("Content-Type") != "text/event-stream" || res.Header.Get("Cache-Control") != "no-cache" || res.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("missing SSE headers: %v", res.Header)
	}
	text := string(got)
	if !strings.Contains(text, `"prompt_tokens":7`) || !strings.Contains(text, "data: [DONE]\n\n") {
		t.Fatalf("invalid replay stream: %s", text)
	}
}
