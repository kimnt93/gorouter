package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v3"
	"github.com/redis/go-redis/v9"

	"github.com/kimnt93/gorouter/internal/platform/llm"
	"github.com/kimnt93/gorouter/pkg/apikey"
	"github.com/kimnt93/gorouter/pkg/chat"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/modelroute"
	"github.com/kimnt93/gorouter/pkg/usage"
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
	items    []entities.Credential
}

func (r gatewayCredRepo) Create(context.Context, entities.CredentialInput, entities.SecretBox) (*entities.Credential, error) {
	return nil, nil
}
func (r gatewayCredRepo) List(context.Context) ([]entities.Credential, error) { return r.items, nil }
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
	models   []string
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

func TestMasterAccessContextHasNoStoredKeyAndListsAllModels(t *testing.T) {
	gateway := &Gateway{
		Creds: credential.NewService(gatewayCredRepo{items: []entities.Credential{{ID: "cred-a", Status: entities.StatusActive}}}, nil),
		Models: modelroute.NewService(gatewayModelRepo{model: entities.ModelDef{
			Name: "model-a", UpstreamModel: "openai/model-a", Enabled: true,
			Routes: []entities.ModelRoute{{CredentialID: "cred-a", Enabled: true}},
		}}),
		Pricing: gatewayPriceResolver{price: entities.Price{InputPerM: 0.2, OutputPerM: 1.2, CachedInputPerM: 0.02, CacheWritePerM: 0.25}},
	}
	app := fiber.New()
	app.Get("/v1/models", func(c fiber.Ctx) error {
		c.Locals(localSession, &entities.Session{Role: entities.RoleMaster, PrincipalType: entities.PrincipalMaster, Scopes: entities.AllScopes})
		access, err := gateway.accessForSession(c, SessionFrom(c))
		if err != nil || !access.Master || access.StoredKey != nil || access.Actor.Type != entities.ActorMaster {
			t.Fatalf("master access=%+v err=%v", access, err)
		}
		return gateway.ListModels(c)
	})
	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body llm.ModelList
	if err = json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || len(body.Data) != 1 || body.Data[0].ID != "model-a" || body.Data[0].UpstreamModel != "openai/model-a" || body.Data[0].Pricing == nil || body.Data[0].Pricing.InputPerM != 0.2 {
		t.Fatalf("status=%d body=%+v", response.StatusCode, body)
	}
	if len(body.Models) != 1 || body.Models[0].Slug != "model-a" || body.Models[0].DisplayName == "" || body.Models[0].Description == "" || len(body.Models[0].SupportedReasoningLevels) != 1 || body.Models[0].SupportedReasoningLevels[0].Effort != "medium" || body.Models[0].SupportedReasoningLevels[0].Description == "" {
		t.Fatalf("Codex models = %+v", body.Models)
	}
}

func TestListModelsHidesModelsWithoutActiveCredentialRoutes(t *testing.T) {
	for _, test := range []struct {
		name        string
		credentials []entities.Credential
	}{
		{name: "deleted credential"},
		{name: "inactive credential", credentials: []entities.Credential{{ID: "cred-a", Status: entities.StatusDisabled}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			gateway := &Gateway{
				Creds: credential.NewService(gatewayCredRepo{items: test.credentials}, nil),
				Models: modelroute.NewService(gatewayModelRepo{model: entities.ModelDef{
					Name: "cx/orphan", Enabled: true, Routes: []entities.ModelRoute{{CredentialID: "cred-a", Enabled: true}},
				}}),
			}
			app := fiber.New()
			app.Get("/v1/models", func(c fiber.Ctx) error {
				c.Locals(localSession, &entities.Session{Role: entities.RoleMaster, PrincipalType: entities.PrincipalMaster, Scopes: entities.AllScopes})
				return gateway.ListModels(c)
			})
			response, err := app.Test(httptest.NewRequest(http.MethodGet, "/v1/models", nil))
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			var body llm.ModelList
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != http.StatusOK || body.Data == nil || body.Models == nil || len(body.Data) != 0 || len(body.Models) != 0 {
				t.Fatalf("status=%d body=%+v", response.StatusCode, body)
			}
		})
	}
}

func TestListModelsKeepsPersonalCredentialRoutesPrivate(t *testing.T) {
	for _, test := range []struct {
		name          string
		ownerUserID   string
		expectedCount int
	}{
		{name: "own route", ownerUserID: "user-a", expectedCount: 1},
		{name: "foreign route", ownerUserID: "user-b", expectedCount: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			key := &entities.ApiKey{ID: "key-a", Models: []string{"cx/model"}, Scopes: []string{entities.ScopeChat}, Enabled: true}
			gateway := &Gateway{
				Keys:  apikey.NewService(gatewayKeyRepo{key: key}, func(string) string { return "" }, func() string { return "" }),
				Creds: credential.NewService(gatewayCredRepo{items: []entities.Credential{{ID: "cred-a", Status: entities.StatusActive, OwnerUserID: test.ownerUserID}}}, nil),
				Models: modelroute.NewService(gatewayModelRepo{model: entities.ModelDef{
					Name: "cx/model", Enabled: true, Routes: []entities.ModelRoute{{CredentialID: "cred-a", Enabled: true}},
				}}),
			}
			app := fiber.New()
			app.Get("/v1/models", func(c fiber.Ctx) error {
				c.Locals(localSession, &entities.Session{Role: entities.RoleAPIKey, PrincipalType: entities.PrincipalUser, KeyID: key.ID, UserID: "user-a", Scopes: []string{entities.ScopeChat}})
				return gateway.ListModels(c)
			})
			response, err := app.Test(httptest.NewRequest(http.MethodGet, "/v1/models", nil))
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			var body llm.ModelList
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body.Data) != test.expectedCount || len(body.Models) != test.expectedCount {
				t.Fatalf("body=%+v", body)
			}
		})
	}
}

func TestCodexModelInfoUsesPersistedProviderMetadata(t *testing.T) {
	model := entities.ModelDef{Name: "cx/future-model", UpstreamModel: "future-model", Metadata: &entities.ModelMetadata{
		DisplayName: "Future Model", Description: "Provider description", ContextWindow: 272000, MaxContextWindow: 872000,
		DefaultReasoningLevel: "high", SupportedReasoningLevels: []entities.ModelReasoningLevel{{Effort: "low", Description: "Fast"}, {Effort: "high", Description: "Deep"}},
		InputModalities: []string{"text", "image"}, SupportsOriginalImage: true, SupportsReasoningSummary: true, SupportsParallelTools: true, SupportsVerbosity: true, DefaultVerbosity: "medium",
	}}
	info := codexModelInfo(model)
	if info.Slug != model.Name || info.DisplayName != "Future Model" || info.Description != "Provider description" || info.DefaultReasoningLevel != "high" || info.ContextWindow != 272000 || info.MaxContextWindow != 872000 || strings.Join(info.InputModalities, ",") != "text,image" || !info.SupportsOriginalImage || !info.SupportsReasoningSummary || !info.SupportsParallelTools || !info.SupportVerbosity || info.DefaultVerbosity != "medium" {
		t.Fatalf("model info = %+v", info)
	}
	if len(info.SupportedReasoningLevels) != 2 || info.SupportedReasoningLevels[1].Effort != "high" || info.SupportedReasoningLevels[1].Description != "Deep" {
		t.Fatalf("reasoning levels = %+v", info.SupportedReasoningLevels)
	}
}

func TestCodexModelInfoFailsClosedWithoutMetadata(t *testing.T) {
	info := codexModelInfo(entities.ModelDef{Name: "custom/model", UpstreamModel: "unknown-model"})
	if strings.Join(info.InputModalities, ",") != "text" || info.SupportVerbosity || info.DefaultVerbosity != "medium" || len(info.SupportedReasoningLevels) != 1 || info.SupportedReasoningLevels[0].Effort != "medium" || info.SupportedReasoningLevels[0].Description == "" {
		t.Fatalf("fallback info = %+v", info)
	}
}

func TestCodexModelInfoFillsMissingReasoningDescriptions(t *testing.T) {
	info := codexModelInfo(entities.ModelDef{Name: "cx/model", Metadata: &entities.ModelMetadata{
		DefaultReasoningLevel: "high",
		SupportedReasoningLevels: []entities.ModelReasoningLevel{
			{Effort: ""},
			{Effort: "high"},
			{Effort: "custom", Description: "Provider description"},
		},
	}})
	if len(info.SupportedReasoningLevels) != 2 || info.SupportedReasoningLevels[0].Description == "" || info.SupportedReasoningLevels[1].Description != "Provider description" {
		t.Fatalf("reasoning levels = %+v", info.SupportedReasoningLevels)
	}
}

func TestCodexModelInfoUsesClientNeutralAgentInstructions(t *testing.T) {
	info := codexModelInfo(entities.ModelDef{Name: "apollo-guidance/gemini/gemini-2.5-flash", UpstreamModel: "gemini-2.5-flash"})
	instructions := info.ModelMessages.InstructionsTemplate
	if instructions != agentHarnessInstructions {
		t.Fatalf("instructions = %q, want %q", instructions, agentHarnessInstructions)
	}
	for _, clientSpecific := range []string{"codex", "coding agent"} {
		if strings.Contains(strings.ToLower(instructions), clientSpecific) {
			t.Fatalf("instructions must be client-neutral: %q", instructions)
		}
	}
}

func TestKeyModelOptionsIncludesOnlyCallableModelsWithEffectivePrice(t *testing.T) {
	price := entities.Price{InputPerM: 0.2, OutputPerM: 1.2, CachedInputPerM: 0.02, CacheWritePerM: 0.25}
	model := entities.ModelDef{Name: "cx/gpt-5.6-luna", UpstreamModel: "gpt-5.6-luna", Enabled: true, Price: &price, Routes: []entities.ModelRoute{{CredentialID: "cred-a", Enabled: true, Weight: 1}}}
	admin := &Admin{
		ModelsSvc: modelroute.NewService(gatewayModelRepo{model: model}),
		CredsSvc:  credential.NewService(gatewayCredRepo{items: []entities.Credential{{ID: "cred-a"}}}, nil),
	}
	app := fiber.New()
	app.Get("/admin/api-keys/models", func(c fiber.Ctx) error {
		c.Locals(localSession, &entities.Session{Role: entities.RoleMaster, PrincipalType: entities.PrincipalMaster, Scopes: entities.AllScopes})
		return admin.KeyModelOptions(c)
	})
	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/admin/api-keys/models?organization_id=org-1", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body APIKeyModelOptionsResponse
	if err = json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || len(body.Data) != 1 || body.Data[0].ID != model.Name || body.Data[0].Price.InputPerM != 0.2 || body.Data[0].Free {
		t.Fatalf("status=%d body=%+v", response.StatusCode, body)
	}
}

type captureUsageRepository struct {
	mu     sync.Mutex
	events []entities.UsageEvent
}

func TestGatewayRecordsNoHealthyCredentialFailure(t *testing.T) {
	key := &entities.ApiKey{ID: "key-1", TenantID: "org-1", Models: []string{"model-a"}, Scopes: []string{entities.ScopeChat}, Enabled: true}
	health := chat.NewHealth()
	for range 3 {
		health.Report("cred-a", false)
	}
	repository := &captureUsageRepository{}
	usageService := usage.NewService(repository, 16, nil)
	gateway := &Gateway{
		Keys:     apikey.NewService(gatewayKeyRepo{key}, func(string) string { return "" }, func() string { return "" }),
		Creds:    credential.NewService(gatewayCredRepo{routes: []entities.RouteCandidate{{CredentialID: "cred-a"}}, runtimes: map[string]*entities.CredentialRuntime{"cred-a": {ID: "cred-a", Provider: entities.ProviderOpenAICompatible}}}, nil),
		Models:   modelroute.NewService(gatewayModelRepo{model: entities.ModelDef{Name: "model-a", UpstreamModel: "upstream-a", Strategy: chat.StrategyPriority, Enabled: true}}),
		Selector: &chat.Selector{}, Health: health, Usage: usageService,
	}
	app := fiber.New()
	app.Post("/v1/chat/completions", func(c fiber.Ctx) error {
		c.Locals(localSession, &entities.Session{Role: entities.RoleAPIKey, KeyID: key.ID, TenantID: key.TenantID, OrganizationID: key.TenantID, PrincipalType: entities.PrincipalUser, UserID: "user-1", Username: "user@example.com", Scopes: key.Scopes})
		return gateway.Chat(c)
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"model-a","messages":[{"role":"user","content":"hello"}]}`))
	request.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
	response, err := app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	usageService.Close()
	if response.StatusCode != http.StatusServiceUnavailable || len(repository.events) != 1 || repository.events[0].StatusCode != http.StatusServiceUnavailable || repository.events[0].Error != "no healthy credentials available" {
		t.Fatalf("status=%d events=%+v", response.StatusCode, repository.events)
	}
}

func (*captureUsageRepository) SpendForKeySince(context.Context, string, time.Time) (float64, error) {
	return 0, nil
}
func (*captureUsageRepository) Summary(context.Context, time.Time) (*entities.UsageSummary, error) {
	return &entities.UsageSummary{}, nil
}
func (*captureUsageRepository) Recent(context.Context, int) ([]entities.RecentEvent, error) {
	return nil, nil
}
func (*captureUsageRepository) SummaryForTenant(context.Context, string, time.Time) (*entities.UsageSummary, error) {
	return &entities.UsageSummary{}, nil
}
func (*captureUsageRepository) RecentForTenant(context.Context, string, int) ([]entities.RecentEvent, error) {
	return nil, nil
}
func (r *captureUsageRepository) InsertBatch(_ context.Context, events []entities.UsageEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, events...)
	return nil
}

func TestGatewayRecordsPrincipalAttributionForSuccessCacheStreamAndError(t *testing.T) {
	cases := []struct {
		name      string
		access    GatewayAccessContext
		cacheHit  bool
		status    int
		errorText string
		wantKeyID string
		wantActor string
		wantUser  string
		wantLabel string
		wantOrg   string
	}{
		{name: "personal non-stream", access: GatewayAccessContext{ApiKey: &entities.ApiKey{ID: "key-personal"}, StoredKey: &entities.ApiKey{ID: "key-personal"}, Actor: entities.UsageActor{Type: entities.ActorUser, UserID: "usr-1", Username: "person@example.com"}}, status: 200, wantKeyID: "key-personal", wantActor: entities.ActorUser, wantUser: "usr-1", wantLabel: "person@example.com"},
		{name: "scoped user stream", access: GatewayAccessContext{ApiKey: &entities.ApiKey{ID: "key-scoped", TenantID: "org-1"}, StoredKey: &entities.ApiKey{ID: "key-scoped"}, Actor: entities.UsageActor{Type: entities.ActorUser, UserID: "usr-1", Username: "person@example.com", OrganizationID: "org-1"}}, status: 200, wantKeyID: "key-scoped", wantActor: entities.ActorUser, wantUser: "usr-1", wantLabel: "person@example.com", wantOrg: "org-1"},
		{name: "organization cache hit", access: GatewayAccessContext{ApiKey: &entities.ApiKey{ID: "key-org", TenantID: "org-1"}, StoredKey: &entities.ApiKey{ID: "key-org"}, Actor: entities.UsageActor{Type: entities.ActorOrganization, Username: "org:Acme", OrganizationID: "org-1"}}, cacheHit: true, status: 200, wantKeyID: "key-org", wantActor: entities.ActorOrganization, wantLabel: "org:Acme", wantOrg: "org-1"},
		{name: "master error", access: GatewayAccessContext{ApiKey: &entities.ApiKey{ID: "master"}, Actor: entities.UsageActor{Type: entities.ActorMaster, Username: "master"}, Master: true}, status: 502, errorText: "safe upstream failure", wantActor: entities.ActorMaster, wantLabel: "master"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			repository := &captureUsageRepository{}
			service := usage.NewService(repository, 16, nil)
			gateway := &Gateway{Usage: service}
			gateway.recordCostError(&test.access, &entities.ModelDef{Name: "model-a", UpstreamModel: "upstream-a"}, "cred-1", llm.Usage{PromptTokens: 2, CompletionTokens: 3}, test.cacheHit, test.status, time.Now().Add(-time.Millisecond), entities.Cost{USD: 0.01, Priced: true}, test.errorText)
			service.Close()
			if len(repository.events) != 1 {
				t.Fatalf("events=%+v", repository.events)
			}
			event := repository.events[0]
			if event.ApiKeyID != test.wantKeyID || event.ActorType != test.wantActor || event.UserID != test.wantUser || event.Username != test.wantLabel || event.OrganizationID != test.wantOrg || event.CacheHit != test.cacheHit || event.StatusCode != test.status || event.Error != test.errorText {
				t.Fatalf("event=%+v", event)
			}
			if test.name == "personal non-stream" && event.OrganizationID != "" {
				t.Fatal("personal usage acquired organization context")
			}
		})
	}
}

func TestGatewayCapturesBoundedConversationAndMarksFreeCostPriced(t *testing.T) {
	repository := &captureUsageRepository{}
	service := usage.NewService(repository, 16, nil)
	gateway := &Gateway{Usage: service}
	access := &GatewayAccessContext{ApiKey: &entities.ApiKey{ID: "master"}, Actor: entities.UsageActor{Type: entities.ActorMaster, Username: "master"}, Master: true}
	request := []byte(`{"model":"free-model","messages":[{"role":"user","content":"hello"}]}`)
	response := []byte(`{"choices":[{"message":{"role":"assistant","content":"hi"}}]}`)
	gateway.recordCostConversation(access, &entities.ModelDef{Name: "free-model"}, "cred-1", llm.Usage{PromptTokens: 2, CompletionTokens: 1}, false, 200, time.Now(), entities.Cost{USD: 0, Priced: true}, request, response)
	service.Close()
	if len(repository.events) != 1 {
		t.Fatalf("events=%+v", repository.events)
	}
	event := repository.events[0]
	if !event.Priced || event.CostUSD != 0 || event.RequestBody != string(request) || event.ResponseBody != string(response) || event.ContentTruncated {
		t.Fatalf("captured event=%+v", event)
	}
}

func (u *gatewayUpstream) Send(_ context.Context, runtime *entities.CredentialRuntime, upstreamModel string, _ []byte) (*entities.UpstreamResult, error) {
	u.calls = append(u.calls, runtime.ID)
	u.models = append(u.models, upstreamModel)
	status := u.statuses[runtime.ID]
	body := `{"id":"ok","object":"chat.completion","model":"model-a","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`
	return &entities.UpstreamResult{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func TestGatewayBlendRoutesDifferentUpstreamModels(t *testing.T) {
	key := &entities.ApiKey{ID: "key-1", Models: []string{"blend"}, Scopes: []string{entities.ScopeChat}, Enabled: true}
	runtime := &entities.CredentialRuntime{ID: "cred-a", Provider: entities.ProviderOpenAICompatible, Kind: entities.KindAPIKey}
	upstream := &gatewayUpstream{statuses: map[string]int{"cred-a": http.StatusOK}}
	gateway := &Gateway{Keys: apikey.NewService(gatewayKeyRepo{key}, func(string) string { return "" }, func() string { return "" }), Creds: credential.NewService(gatewayCredRepo{routes: []entities.RouteCandidate{{CredentialID: "cred-a", UpstreamModel: "provider-model-b", Priority: 1}}, runtimes: map[string]*entities.CredentialRuntime{"cred-a": runtime}}, nil), Models: modelroute.NewService(gatewayModelRepo{model: entities.ModelDef{Name: "blend", UpstreamModel: "provider-model-a", Strategy: chat.StrategyPriority, Enabled: true}}), OpenAI: upstream, Selector: &chat.Selector{}, Health: chat.NewHealth()}
	app := fiber.New()
	app.Post("/", func(c fiber.Ctx) error {
		c.Locals(localSession, &entities.Session{Role: entities.RoleAPIKey, KeyID: key.ID, Scopes: []string{entities.ScopeChat}})
		return gateway.Chat(c)
	})
	response, err := app.Test(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"model":"blend","messages":[{"role":"user","content":"hi"}]}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || len(upstream.models) != 1 || upstream.models[0] != "provider-model-b" {
		t.Fatalf("status=%d models=%v", response.StatusCode, upstream.models)
	}
}

func testGatewayApp(statuses map[string]int) (*fiber.App, *gatewayUpstream) {
	return testGatewayAppWithStrategy(statuses, chat.StrategyPriority)
}

func testGatewayAppWithStrategy(statuses map[string]int, strategy string) (*fiber.App, *gatewayUpstream) {
	return testGatewayAppWithSelector(statuses, strategy, &chat.Selector{})
}

func testGatewayAppWithSelector(statuses map[string]int, strategy string, selector *chat.Selector) (*fiber.App, *gatewayUpstream) {
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
		OpenAI: upstream, Selector: selector, Health: chat.NewHealth(),
	}
	app := fiber.New()
	app.Post("/v1/chat/completions", func(c fiber.Ctx) error {
		c.Locals(localSession, &entities.Session{Role: entities.RoleAPIKey, KeyID: key.ID, TenantID: key.TenantID, Scopes: []string{entities.ScopeChat}})
		return gateway.Chat(c)
	})
	return app, upstream
}

func TestGatewayRoundRobinPreservesExplicitCacheAffinityAcrossReplicas(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	firstSelector, secondSelector := &chat.Selector{}, &chat.Selector{}
	firstSelector.SetRedis(client)
	secondSelector.SetRedis(client)
	firstApp, firstUpstream := testGatewayAppWithSelector(map[string]int{"cred-a": http.StatusOK, "cred-b": http.StatusOK}, chat.StrategyRoundRobin, firstSelector)
	secondApp, secondUpstream := testGatewayAppWithSelector(map[string]int{"cred-a": http.StatusOK, "cred-b": http.StatusOK}, chat.StrategyRoundRobin, secondSelector)

	request := func(app *fiber.App, body string, header string) {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		if header != "" {
			req.Header.Set("X-Codex-Session-Id", header)
		}
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status=%d", res.StatusCode)
		}
	}
	request(firstApp, `{"model":"model-a","messages":[{"role":"user","content":"one"}],"prompt_cache_key":"session-a","temperature":0.7}`, "")
	request(secondApp, `{"model":"model-a","messages":[{"role":"user","content":"two"}],"prompt_cache_key":"session-a","temperature":0.7}`, "")
	request(secondApp, `{"model":"model-a","messages":[{"role":"user","content":"three"}],"temperature":0.7}`, "session-b")
	if got := strings.Join(firstUpstream.calls, ","); got != "cred-a" {
		t.Fatalf("first replica calls=%s", got)
	}
	if got := strings.Join(secondUpstream.calls, ","); got != "cred-a,cred-b" {
		t.Fatalf("second replica calls=%s", got)
	}
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

type gatewayProviderQuota struct {
	available map[string]bool
	marked    []string
}

func (q *gatewayProviderQuota) Available(id string) bool {
	available, ok := q.available[id]
	return !ok || available
}

func (q *gatewayProviderQuota) MarkExhausted(id string) {
	q.available[id] = false
	q.marked = append(q.marked, id)
}

func (q *gatewayProviderQuota) MarkInUse(string) {}

func TestGatewayQuotaFillFirstAndExhaustionFailover(t *testing.T) {
	app, upstream := testGatewayApp(map[string]int{"cred-a": http.StatusTooManyRequests, "cred-b": http.StatusOK})
	quotaState := &gatewayProviderQuota{available: map[string]bool{"cred-a": true, "cred-b": true}}

	// Install the state on the gateway captured by a dedicated test route.
	key := &entities.ApiKey{ID: "key-1", TenantID: "tenant-1", Models: []string{"model-a"}, Scopes: []string{entities.ScopeChat}, Enabled: true}
	routes := []entities.RouteCandidate{{CredentialID: "cred-a", Priority: 10}, {CredentialID: "cred-b", Priority: 5}}
	runtimes := map[string]*entities.CredentialRuntime{
		"cred-a": {ID: "cred-a", Provider: entities.ProviderOpenAICompatible, Kind: entities.KindAPIKey},
		"cred-b": {ID: "cred-b", Provider: entities.ProviderOpenAICompatible, Kind: entities.KindAPIKey},
	}
	gateway := &Gateway{
		Keys:           apikey.NewService(gatewayKeyRepo{key}, func(string) string { return "" }, func() string { return "" }),
		Creds:          credential.NewService(gatewayCredRepo{routes: routes, runtimes: runtimes}, nil),
		Models:         modelroute.NewService(gatewayModelRepo{model: entities.ModelDef{Name: "model-a", UpstreamModel: "upstream-a", Strategy: chat.StrategyPriority, Enabled: true}}),
		OpenAI:         upstream,
		Selector:       &chat.Selector{},
		Health:         chat.NewHealth(),
		ProviderQuotas: quotaState,
	}
	app = fiber.New()
	app.Post("/v1/chat/completions", func(c fiber.Ctx) error {
		c.Locals(localSession, &entities.Session{Role: entities.RoleAPIKey, KeyID: key.ID, TenantID: key.TenantID, Scopes: []string{entities.ScopeChat}})
		return gateway.Chat(c)
	})

	for i := 0; i < 2; i++ {
		request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"model-a","messages":[{"role":"user","content":"hi"}],"temperature":0.7}`))
		response, err := app.Test(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("request %d status=%d", i, response.StatusCode)
		}
	}
	if got, want := strings.Join(upstream.calls, ","), "cred-a,cred-b,cred-b"; got != want {
		t.Fatalf("calls=%s want=%s", got, want)
	}
	if len(quotaState.marked) != 1 || quotaState.marked[0] != "cred-a" {
		t.Fatalf("marked=%v", quotaState.marked)
	}
}

func TestGatewayQuotaProviderUsesFillFirstEvenForRoundRobinBlend(t *testing.T) {
	key := &entities.ApiKey{ID: "key-1", TenantID: "tenant-1", Models: []string{"model-a"}, Scopes: []string{entities.ScopeChat}, Enabled: true}
	routes := []entities.RouteCandidate{{CredentialID: "cred-a", Priority: 10}, {CredentialID: "cred-b", Priority: 5}}
	runtimes := map[string]*entities.CredentialRuntime{
		"cred-a": {ID: "cred-a", Provider: "opencode-zen", Kind: entities.KindAPIKey},
		"cred-b": {ID: "cred-b", Provider: "opencode-zen", Kind: entities.KindAPIKey},
	}
	upstream := &gatewayUpstream{statuses: map[string]int{"cred-a": http.StatusOK, "cred-b": http.StatusOK}}
	gateway := &Gateway{
		Keys:      apikey.NewService(gatewayKeyRepo{key}, func(string) string { return "" }, func() string { return "" }),
		Creds:     credential.NewService(gatewayCredRepo{routes: routes, runtimes: runtimes}, nil),
		Models:    modelroute.NewService(gatewayModelRepo{model: entities.ModelDef{Name: "model-a", UpstreamModel: "upstream-a", Strategy: chat.StrategyRoundRobin, Enabled: true}}),
		Providers: map[string]entities.Upstream{"opencode-zen": upstream},
		Selector:  &chat.Selector{},
		Health:    chat.NewHealth(),
	}
	app := fiber.New()
	app.Post("/v1/chat/completions", func(c fiber.Ctx) error {
		c.Locals(localSession, &entities.Session{Role: entities.RoleAPIKey, KeyID: key.ID, TenantID: key.TenantID, Scopes: []string{entities.ScopeChat}})
		return gateway.Chat(c)
	})
	for i := 0; i < 3; i++ {
		request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"model-a","messages":[{"role":"user","content":"hi"}],"temperature":0.7}`))
		response, err := app.Test(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
	}
	if got, want := strings.Join(upstream.calls, ","), "cred-a,cred-a,cred-a"; got != want {
		t.Fatalf("calls=%s want=%s", got, want)
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
