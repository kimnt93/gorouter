package routes_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/kimnt93/gorouter/internal/api/routes"
	"github.com/kimnt93/gorouter/pkg/auth"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/modelroute"
	"github.com/kimnt93/gorouter/pkg/usage"
)

type connectivityRouteCredentialRepo struct {
	credentials []entities.Credential
	runtimes    map[string]*entities.CredentialRuntime
}

func (*connectivityRouteCredentialRepo) Create(context.Context, entities.CredentialInput, entities.SecretBox) (*entities.Credential, error) {
	return nil, nil
}

func (r *connectivityRouteCredentialRepo) List(context.Context) ([]entities.Credential, error) {
	return append([]entities.Credential(nil), r.credentials...), nil
}

func (*connectivityRouteCredentialRepo) Update(context.Context, entities.SecretBox, string, entities.CredentialUpdate) (*entities.Credential, error) {
	return nil, entities.ErrNotFound
}

func (*connectivityRouteCredentialRepo) Delete(context.Context, string) error { return nil }

func (r *connectivityRouteCredentialRepo) Runtime(_ context.Context, _ entities.SecretBox, id string) (*entities.CredentialRuntime, error) {
	runtime, ok := r.runtimes[id]
	if !ok {
		return nil, entities.ErrNotFound
	}
	copy := *runtime
	return &copy, nil
}

func (*connectivityRouteCredentialRepo) UpdateOAuthTokens(context.Context, entities.SecretBox, string, string, string) error {
	return nil
}

func (*connectivityRouteCredentialRepo) RoutesForModel(context.Context, string) ([]entities.RouteCandidate, error) {
	return nil, nil
}

type connectivityRouteModelRepo struct {
	upserts []entities.ModelDef
}

func (r *connectivityRouteModelRepo) Upsert(_ context.Context, model entities.ModelDef) error {
	r.upserts = append(r.upserts, model)
	return nil
}

func (*connectivityRouteModelRepo) Delete(context.Context, string) error { return nil }

func (r *connectivityRouteModelRepo) List(context.Context) ([]entities.ModelDef, error) {
	if len(r.upserts) == 0 {
		return nil, nil
	}
	return append([]entities.ModelDef(nil), r.upserts...), nil
}

func (*connectivityRouteModelRepo) SetPrice(context.Context, string, entities.Price) error {
	return nil
}

func (*connectivityRouteModelRepo) DeletePrice(context.Context, string) error { return nil }

func (*connectivityRouteModelRepo) ListPrices(context.Context) (map[string]entities.Price, error) {
	return nil, nil
}

type connectivityRouteProvider struct{}

func (connectivityRouteProvider) Probe(context.Context, *entities.CredentialRuntime) (int, error) {
	return http.StatusOK, nil
}

func (connectivityRouteProvider) DiscoverModels(context.Context, *entities.CredentialRuntime) ([]credential.ProviderModel, error) {
	return []credential.ProviderModel{{ID: "provider-model", OwnedBy: "test-provider", Name: "Provider Model", ContextLength: 200000, InputModalities: []string{"text", "image"}, DefaultReasoningLevel: "high", SupportedReasoningLevels: []entities.ModelReasoningLevel{{Effort: "high"}}}}, nil
}

func (connectivityRouteProvider) Send(context.Context, *entities.CredentialRuntime, string, []byte) (*entities.UpstreamResult, error) {
	body := "data: {\"id\":\"chunk-1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\n" +
		"data: {\"id\":\"chunk-1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2}}\n\n" +
		"data: [DONE]\n\n"
	return &entities.UpstreamResult{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}

type rejectedConnectivityProvider struct{ status int }

func (p rejectedConnectivityProvider) Probe(context.Context, *entities.CredentialRuntime) (int, error) {
	return p.status, nil
}
func (p rejectedConnectivityProvider) Send(context.Context, *entities.CredentialRuntime, string, []byte) (*entities.UpstreamResult, error) {
	return &entities.UpstreamResult{StatusCode: p.status, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"sensitive upstream detail"}}`))}, nil
}

type connectivityUsageRepo struct {
	mu     sync.Mutex
	events []entities.UsageEvent
}

func (*connectivityUsageRepo) SpendForKeySince(context.Context, string, time.Time) (float64, error) {
	return 0, nil
}
func (*connectivityUsageRepo) Summary(context.Context, time.Time) (*entities.UsageSummary, error) {
	return &entities.UsageSummary{}, nil
}
func (*connectivityUsageRepo) Recent(context.Context, int) ([]entities.RecentEvent, error) {
	return nil, nil
}
func (*connectivityUsageRepo) SummaryForTenant(context.Context, string, time.Time) (*entities.UsageSummary, error) {
	return &entities.UsageSummary{}, nil
}
func (*connectivityUsageRepo) RecentForTenant(context.Context, string, int) ([]entities.RecentEvent, error) {
	return nil, nil
}
func (r *connectivityUsageRepo) InsertBatch(_ context.Context, events []entities.UsageEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, events...)
	return nil
}

func TestCredentialConnectivityRoutesEnforceTenantOwnership(t *testing.T) {
	tenantA := "tenant-a"
	tenantB := "tenant-b"
	repo := &connectivityRouteCredentialRepo{
		credentials: []entities.Credential{
			{ID: "own", OwnerTenantID: &tenantA},
			{ID: "foreign", OwnerTenantID: &tenantB},
			{ID: "shared", OwnerTenantID: nil},
		},
		runtimes: map[string]*entities.CredentialRuntime{
			"own":     {ID: "own", Provider: entities.ProviderOpenAICompatible, Kind: entities.KindAPIKey},
			"foreign": {ID: "foreign", Provider: entities.ProviderOpenAICompatible, Kind: entities.KindAPIKey},
			"shared":  {ID: "shared", Provider: entities.ProviderOpenAICompatible, Kind: entities.KindAPIKey},
		},
	}
	models := &connectivityRouteModelRepo{}
	provider := connectivityRouteProvider{}
	usageRepo := &connectivityUsageRepo{}
	usageService := usage.NewService(usageRepo, 16, nil)
	t.Cleanup(usageService.Close)
	keys := oauthRouteKeyLookup{
		"tenant-key": {
			ID:       "key-a",
			TenantID: tenantA,
			Enabled:  true,
			Scopes:   []string{entities.ScopeCredentialsManage, entities.ScopeModelsManage},
		},
	}
	app := routes.New(routes.Dependencies{
		Auth:        auth.NewService("master-secret", "session-secret", keys),
		Credentials: credential.NewService(repo, oauthRouteBox{}),
		Models:      modelroute.NewService(models),
		Usage:       usageService,
		OpenAI:      provider,
	})

	connectivityEndpoints := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{name: "health test", method: http.MethodPost, path: "/admin/credentials/%s/test"},
		{name: "model discovery", method: http.MethodGet, path: "/admin/credentials/%s/models"},
		{name: "streaming chat test", method: http.MethodPost, path: "/admin/credentials/%s/chat-tests", body: map[string]string{"model": "provider-model", "prompt": "hello"}},
	}
	for _, endpoint := range connectivityEndpoints {
		t.Run(endpoint.name, func(t *testing.T) {
			assertConnectivityRouteStatus(t, app, endpoint.method, endpoint.path, "own", "tenant-key", http.StatusOK, endpoint.body)
			assertConnectivityRouteStatus(t, app, endpoint.method, endpoint.path, "foreign", "tenant-key", http.StatusNotFound, endpoint.body)
			assertConnectivityRouteStatus(t, app, endpoint.method, endpoint.path, "shared", "tenant-key", http.StatusNotFound, endpoint.body)
			assertConnectivityRouteStatus(t, app, endpoint.method, endpoint.path, "foreign", "master-secret", http.StatusOK, endpoint.body)
			assertConnectivityRouteStatus(t, app, endpoint.method, endpoint.path, "shared", "master-secret", http.StatusOK, endpoint.body)
		})
	}
	t.Run("model discovery marks first model as default", func(t *testing.T) {
		response := oauthFiberRequest(t, app, http.MethodGet, "/admin/credentials/own/models", "master-secret", nil)
		defer response.Body.Close()
		var payload struct {
			DefaultModel string `json:"default_model"`
			Data         []struct {
				ID      string `json:"id"`
				Object  string `json:"object"`
				Default bool   `json:"default"`
			} `json:"data"`
		}
		if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK || payload.DefaultModel != "provider-model" || len(payload.Data) != 1 || payload.Data[0].Object != "model" || !payload.Data[0].Default {
			t.Fatalf("model default payload = %+v status=%d", payload, response.StatusCode)
		}
	})

	response := oauthFiberRequest(t, app, http.MethodPost, "/admin/credentials/own/chat-tests", "tenant-key", map[string]string{"model": "provider-model", "prompt": "hello"})
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	usageService.Close()
	if len(usageRepo.events) == 0 {
		t.Fatal("provider chat test was not recorded in usage logs")
	}
	event := usageRepo.events[len(usageRepo.events)-1]
	if event.CredentialID != "own" || event.Provider != entities.ProviderOpenAICompatible || event.Model != "custom/provider-model" || event.UpstreamModel != "provider-model" || event.StatusCode != http.StatusOK || event.PromptTokens != 5 || event.CompletionTokens != 2 || event.ApiKeyID != "key-a" {
		t.Fatalf("chat test usage event = %+v", event)
	}

	importBody := map[string]any{"models": []string{"provider-model"}}
	assertConnectivityRouteStatus(t, app, http.MethodPost, "/admin/credentials/%s/models/import", "own", "tenant-key", http.StatusForbidden, importBody)
	if len(models.upserts) != 0 {
		t.Fatalf("non-master import mutated model routes: %+v", models.upserts)
	}
	assertConnectivityRouteStatus(t, app, http.MethodPost, "/admin/credentials/%s/models/import", "shared", "master-secret", http.StatusOK, importBody)
	if len(models.upserts) != 1 || models.upserts[0].Name != "custom/provider-model" || len(models.upserts[0].Routes) != 1 || models.upserts[0].Routes[0].CredentialID != "shared" || models.upserts[0].Metadata == nil || models.upserts[0].Metadata.DisplayName != "Provider Model" || strings.Join(models.upserts[0].Metadata.InputModalities, ",") != "text,image" {
		t.Fatalf("master import did not create the expected model route: %+v", models.upserts)
	}
	assertConnectivityRouteStatus(t, app, http.MethodPost, "/admin/credentials/%s/models/refresh", "shared", "master-secret", http.StatusOK, nil)
	if len(models.upserts) != 2 || models.upserts[1].Metadata == nil || models.upserts[1].Metadata.SourceCredentialID != "shared" {
		t.Fatalf("refresh did not update existing model metadata: %+v", models.upserts)
	}
}

func assertConnectivityRouteStatus(t *testing.T, app *fiber.App, method, path, credentialID, bearer string, want int, body any) {
	t.Helper()
	response := oauthFiberRequest(t, app, method, strings.Replace(path, "%s", credentialID, 1), bearer, body)
	defer response.Body.Close()
	if response.StatusCode != want {
		responseBody, _ := io.ReadAll(response.Body)
		t.Fatalf("%s %s as %q: status=%d, want=%d body=%s", method, path, bearer, response.StatusCode, want, responseBody)
	}
}

func TestCredentialChatPreservesSafeUpstreamClientStatus(t *testing.T) {
	tenantID := "tenant-a"
	repo := &connectivityRouteCredentialRepo{
		credentials: []entities.Credential{{ID: "own", OwnerTenantID: &tenantID}},
		runtimes:    map[string]*entities.CredentialRuntime{"own": {ID: "own", Provider: entities.ProviderOpenAICompatible, Kind: entities.KindAPIKey}},
	}
	provider := rejectedConnectivityProvider{status: http.StatusForbidden}
	app := routes.New(routes.Dependencies{
		Auth:        auth.NewService("master-secret", "session-secret", oauthRouteKeyLookup{}),
		Credentials: credential.NewService(repo, oauthRouteBox{}), OpenAI: provider,
	})
	response := oauthFiberRequest(t, app, http.MethodPost, "/admin/credentials/own/chat-tests", "master-secret", map[string]string{"model": "provider-model", "prompt": "hello"})
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusForbidden || !strings.Contains(string(body), "provider returned HTTP 403") || strings.Contains(string(body), "sensitive upstream detail") {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
}
