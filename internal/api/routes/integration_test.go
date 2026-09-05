package routes_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"

	"github.com/kimnt93/gorouter/internal/api/handlers"
	"github.com/kimnt93/gorouter/internal/api/routes"
	"github.com/kimnt93/gorouter/internal/platform/database"
	"github.com/kimnt93/gorouter/internal/platform/llm"
	"github.com/kimnt93/gorouter/internal/platform/promptcache"
	"github.com/kimnt93/gorouter/internal/repositories/postgres"
	"github.com/kimnt93/gorouter/pkg/apikey"
	"github.com/kimnt93/gorouter/pkg/auth"
	"github.com/kimnt93/gorouter/pkg/chat"
	"github.com/kimnt93/gorouter/pkg/config"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/identity"
	"github.com/kimnt93/gorouter/pkg/modelroute"
	"github.com/kimnt93/gorouter/pkg/quota"
	"github.com/kimnt93/gorouter/pkg/seal"
	"github.com/kimnt93/gorouter/pkg/tenant"
	"github.com/kimnt93/gorouter/pkg/usage"
)

type integrationHarness struct {
	app       *fiber.App
	db        *database.Postgres
	usage     *usage.Service
	cache     chat.PromptCache
	redisURL  string
	schema    string
	adminURL  string
	masterKey string
	keys      *apikey.Service
	creds     *credential.Service
	models    *modelroute.Service
}

func newIntegrationHarness(t *testing.T, upstreamURL string) *integrationHarness {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	redisURL := os.Getenv("TEST_REDIS_URL")
	if databaseURL == "" || redisURL == "" {
		t.Skip("TEST_DATABASE_URL and TEST_REDIS_URL are required for integration tests")
	}
	schema := fmt.Sprintf("gorouter_test_%d", time.Now().UnixNano())
	admin, err := pgx.Connect(context.Background(), databaseURL)
	if err != nil {
		t.Skipf("test PostgreSQL unavailable: %v", err)
	}
	if _, err := admin.Exec(context.Background(), "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		admin.Close(context.Background())
		t.Fatal(err)
	}
	admin.Close(context.Background())
	u, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	db, err := database.Connect(context.Background(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(context.Background()); err != nil {
		db.Close()
		t.Fatal(err)
	}
	box, err := seal.New("integration-encryption-key")
	if err != nil {
		t.Fatal(err)
	}
	repoDB := postgres.New(db.Pool)
	tenantSvc := tenant.NewService(postgres.NewTenantRepo(repoDB))
	if err := tenantSvc.EnsureDefault(context.Background()); err != nil {
		t.Fatal(err)
	}
	credSvc := credential.NewService(postgres.NewCredentialRepo(repoDB), box)
	keySvc := apikey.NewService(postgres.NewApiKeyRepo(repoDB), postgres.HashSecret, postgres.GenerateSecret)
	identityRepo := postgres.NewIdentityRepo(repoDB)
	auditRepo := postgres.NewAuditRepo(repoDB)
	identitySvc := identity.NewService(identityRepo, auditRepo)
	modelSvc := modelroute.NewService(postgres.NewModelRouteRepo(repoDB))
	usageSvc := usage.NewService(postgres.NewUsageRepo(repoDB), 32, usage.NewPending())
	cacheSvc, redisClient, err := promptcache.New(config.CacheConfig{Enabled: true, TTL: time.Minute, Scope: chat.ScopeKey, MaxEntryBytes: 1 << 20}, redisURL)
	if err != nil {
		t.Skipf("test Redis unavailable: %v", err)
	}
	policy, _ := quota.ParsePolicy("strict")
	quotaSvc, err := quota.NewRedis(redisClient, policy)
	if err != nil {
		t.Fatal(err)
	}
	client := llm.NewHTTPClient()
	openai := &llm.OpenAIAdapter{HTTP: client}
	anthropic := &llm.AnthropicAdapter{HTTP: client}
	refresher := &llm.AnthropicOAuthRefresher{HTTP: client, TokenURL: strings.TrimSuffix(upstreamURL, "/") + "/oauth/token", ClientID: "integration-client", Persister: credSvc}
	anthropic.Refresh = refresher.Refresh
	masterKey := "integration-master-key"
	authSvc := auth.NewServiceWithIdentity(masterKey, "integration-session-secret", keySvc, identityRepo)
	gw := &handlers.Gateway{Keys: keySvc, Creds: credSvc, Models: modelSvc, Usage: usageSvc, Cache: cacheSvc,
		OpenAI: openai, Anthropic: anthropic, Selector: &chat.Selector{}, Health: chat.NewHealth(), Quota: quotaSvc}
	app := routes.New(routes.Dependencies{Auth: authSvc, Tenants: tenantSvc, Credentials: credSvc, Keys: keySvc,
		Models: modelSvc, Usage: usageSvc, Cache: cacheSvc, Gateway: gw, OpenAI: openai, Anthropic: anthropic,
		Identity: identitySvc, IdentityRepo: identityRepo, Audit: auditRepo,
		BodyLimit: 2 << 20, ReadTimeout: time.Minute})
	h := &integrationHarness{app: app, db: db, usage: usageSvc, cache: cacheSvc, redisURL: redisURL, schema: schema,
		adminURL: upstreamURL, masterKey: masterKey, keys: keySvc, creds: credSvc, models: modelSvc}
	t.Cleanup(func() {
		usageSvc.Close()
		cacheSvc.Flush()
		cacheSvc.Close()
		redisClient.Close()
		db.Close()
		cleanup, cleanupErr := pgx.Connect(context.Background(), databaseURL)
		if cleanupErr == nil {
			_, _ = cleanup.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
			cleanup.Close(context.Background())
		}
	})
	return h
}

func (h *integrationHarness) request(t *testing.T, method, path, bearer string, body any) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	res, err := h.app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

type createCredentialRequest struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Kind     string `json:"kind"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
}

type createKeyRequest struct {
	TenantID    string   `json:"tenant_id"`
	Name        string   `json:"name"`
	Models      []string `json:"models"`
	Scopes      []string `json:"scopes"`
	QuotaUSD    *float64 `json:"quota_usd,omitempty"`
	QuotaPeriod string   `json:"quota_period,omitempty"`
	RPM         *int     `json:"rpm,omitempty"`
}

type createKeyResponse struct {
	ID        string `json:"id"`
	Plaintext string `json:"plaintext"`
}
type createUserAPIResponse struct {
	User       entities.User                   `json:"user"`
	InitialKey *handlers.CreatedAPIKeyResponse `json:"initial_key"`
}

func decodeResponse[T any](t *testing.T, res *http.Response) T {
	t.Helper()
	defer res.Body.Close()
	var out T
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func createConfiguredKey(t *testing.T, h *integrationHarness, model string, quotaUSD *float64, rpm *int) createKeyResponse {
	t.Helper()
	credRes := h.request(t, http.MethodPost, "/admin/credentials", h.masterKey, createCredentialRequest{
		Name: "mock", Provider: entities.ProviderOpenAICompatible, Kind: entities.KindAPIKey, BaseURL: h.adminURL, APIKey: "provider-secret-value",
	})
	if credRes.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(credRes.Body)
		t.Fatalf("create credential: %d %s", credRes.StatusCode, body)
	}
	cred := decodeResponse[entities.Credential](t, credRes)
	modelRes := h.request(t, http.MethodPut, "/admin/models/"+model, h.masterKey, entities.ModelDef{
		Name: model, Strategy: chat.StrategyPriority, UpstreamModel: "upstream-model", Enabled: true,
		Routes: []entities.ModelRoute{{CredentialID: cred.ID, Priority: 10, Weight: 1, Enabled: true}},
	})
	if modelRes.StatusCode != http.StatusOK {
		t.Fatalf("create model: %d", modelRes.StatusCode)
	}
	modelRes.Body.Close()
	priceRes := h.request(t, http.MethodPut, "/admin/prices/"+model, h.masterKey, entities.Price{InputPerM: 10, OutputPerM: 20})
	if priceRes.StatusCode != http.StatusOK {
		t.Fatalf("set price: %d", priceRes.StatusCode)
	}
	priceRes.Body.Close()
	period := entities.QuotaPeriodNone
	if quotaUSD != nil {
		period = entities.QuotaPeriodWeek
	}
	keyRes := h.request(t, http.MethodPost, "/admin/api-keys", h.masterKey, createKeyRequest{
		TenantID: "tenant_default", Name: model + " key", Models: []string{model}, Scopes: []string{entities.ScopeChat, entities.ScopeUsageRead}, QuotaUSD: quotaUSD, QuotaPeriod: period, RPM: rpm,
	})
	if keyRes.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(keyRes.Body)
		t.Fatalf("create key: %d %s", keyRes.StatusCode, body)
	}
	return decodeResponse[createKeyResponse](t, keyRes)
}

func mockOpenAIUpstream(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var req llm.ChatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			chunk, _ := json.Marshal(llm.Chunk{ID: "chunk-1", Object: "chat.completion.chunk", Model: req.Model,
				Choices: []llm.ChunkChoice{{Index: 0, Delta: llm.Delta{Content: "hello"}, FinishReason: "stop"}}, Usage: &llm.Usage{PromptTokens: 5, CompletionTokens: 2}})
			fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", chunk)
			return
		}
		_ = json.NewEncoder(w).Encode(llm.Response{ID: "chat-1", Object: "chat.completion", Model: req.Model,
			Choices: []llm.Choice{{Index: 0, Message: &llm.ResponseMessage{Role: "assistant", Content: "hello"}, FinishReason: "stop"}},
			Usage:   llm.Usage{PromptTokens: 5, CompletionTokens: 2}})
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

func chatBody(model string, stream bool, content string) llm.ChatRequest {
	temperature := float64(0)
	return llm.ChatRequest{Model: model, Stream: stream, Temperature: &temperature,
		Messages: []llm.Message{{Role: "user", Content: json.RawMessage(strconvQuote(content))}}}
}

func strconvQuote(value string) string {
	b, _ := json.Marshal(value)
	return string(b)
}

func TestFiberApplicationEndToEnd(t *testing.T) {
	upstream, calls := mockOpenAIUpstream(t)
	h := newIntegrationHarness(t, upstream.URL)
	key := createConfiguredKey(t, h, "integration-model", nil, nil)
	credentialList := readBody(t, h.request(t, http.MethodGet, "/admin/credentials", h.masterKey, nil))
	if strings.Contains(credentialList, "provider-secret-value") {
		t.Fatal("credential list leaked a provider secret")
	}
	var listedCredentials []entities.Credential
	if err := json.Unmarshal([]byte(credentialList), &listedCredentials); err != nil || len(listedCredentials) == 0 {
		t.Fatalf("decode credential list: credentials=%v err=%v", listedCredentials, err)
	}
	probe := h.request(t, http.MethodPost, "/admin/credentials/"+listedCredentials[0].ID+"/test", h.masterKey, nil)
	if probe.StatusCode != http.StatusOK || !decodeResponse[credential.ConnectivityResult](t, probe).OK {
		t.Fatal("credential connectivity probe failed")
	}
	keyList := readBody(t, h.request(t, http.MethodGet, "/admin/api-keys", h.masterKey, nil))
	if strings.Contains(keyList, key.Plaintext) {
		t.Fatal("API-key list repeated one-time plaintext")
	}

	masterLogin := h.request(t, http.MethodPost, "/login", "", struct {
		Key string `json:"key"`
	}{Key: h.masterKey})
	if masterLogin.StatusCode != http.StatusOK || len(masterLogin.Cookies()) == 0 || !masterLogin.Cookies()[0].HttpOnly {
		t.Fatalf("master login/cookie failed: status=%d cookies=%v", masterLogin.StatusCode, masterLogin.Cookies())
	}
	masterLogin.Body.Close()
	keyLogin := h.request(t, http.MethodPost, "/login", "", struct {
		Key string `json:"key"`
	}{Key: key.Plaintext})
	if keyLogin.StatusCode != http.StatusOK {
		t.Fatalf("API-key login failed: %d", keyLogin.StatusCode)
	}
	keyLogin.Body.Close()

	denied := h.request(t, http.MethodGet, "/admin/credentials", key.Plaintext, nil)
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("missing scope status=%d, want 403", denied.StatusCode)
	}
	denied.Body.Close()
	models := h.request(t, http.MethodGet, "/v1/models", key.Plaintext, nil)
	if models.StatusCode != http.StatusOK || !strings.Contains(readBody(t, models), "integration-model") {
		t.Fatal("authorized model was not listed")
	}
	anonymousModels := h.request(t, http.MethodGet, "/v1/models", "", nil)
	if anonymousModels.StatusCode != http.StatusOK || !strings.Contains(readBody(t, anonymousModels), "integration-model") {
		t.Fatal("anonymous current model catalog was not listed")
	}
	invalidModels := h.request(t, http.MethodGet, "/v1/models", "invalid-api-key", nil)
	if invalidModels.StatusCode != http.StatusUnauthorized {
		t.Fatalf("invalid model-catalog bearer status=%d, want 401", invalidModels.StatusCode)
	}
	invalidModels.Body.Close()

	callsBeforeChat := calls.Load()
	first := h.request(t, http.MethodPost, "/v1/chat/completions", key.Plaintext, chatBody("integration-model", false, "cache me"))
	if first.StatusCode != http.StatusOK || first.Header.Get("X-Cache") != "miss" {
		t.Fatalf("first chat status=%d cache=%q", first.StatusCode, first.Header.Get("X-Cache"))
	}
	readBody(t, first)
	second := h.request(t, http.MethodPost, "/v1/chat/completions", key.Plaintext, chatBody("integration-model", false, "cache me"))
	if second.StatusCode != http.StatusOK || second.Header.Get("X-Cache") != "hit" || calls.Load() != callsBeforeChat+1 {
		t.Fatalf("cached chat status=%d cache=%q upstream_calls=%d", second.StatusCode, second.Header.Get("X-Cache"), calls.Load())
	}
	readBody(t, second)
	stream := h.request(t, http.MethodPost, "/v1/chat/completions", key.Plaintext, chatBody("integration-model", true, "stream me"))
	streamBody := readBody(t, stream)
	if stream.StatusCode != http.StatusOK || stream.Header.Get("Content-Type") != "text/event-stream" || !strings.Contains(streamBody, "[DONE]") {
		t.Fatalf("bad stream: status=%d content-type=%q body=%s", stream.StatusCode, stream.Header.Get("Content-Type"), streamBody)
	}

	unknown := h.request(t, http.MethodPost, "/v1/chat/completions", key.Plaintext, chatBody("not-allowed", false, "no"))
	if unknown.StatusCode != http.StatusForbidden {
		t.Fatalf("model allowlist did not fail closed: %d", unknown.StatusCode)
	}
	unknown.Body.Close()
	oversizedBody, err := json.Marshal(chatBody("integration-model", false, strings.Repeat("x", 3<<20)))
	if err != nil {
		t.Fatal(err)
	}
	oversizedRequest := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(oversizedBody))
	oversizedRequest.Header.Set("Authorization", "Bearer "+key.Plaintext)
	oversizedRequest.Header.Set("Content-Type", "application/json")
	oversized, oversizedErr := h.app.Test(oversizedRequest)
	if oversizedErr != nil && !strings.Contains(oversizedErr.Error(), "body size") {
		t.Fatalf("unexpected oversized-request error: %v", oversizedErr)
	}
	if oversizedErr == nil && (oversized == nil || oversized.StatusCode != http.StatusRequestEntityTooLarge) {
		t.Fatalf("oversized request response=%v error=%v", oversized, oversizedErr)
	}
	if oversized != nil {
		oversized.Body.Close()
	}
	if _, err := h.db.Pool.Exec(context.Background(), `INSERT INTO tenants (id,name,created_at) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, "tenant_private", "private", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.Pool.Exec(context.Background(), `INSERT INTO organizations (id,name,normalized_name,status,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$5) ON CONFLICT DO NOTHING`, `tenant_private`, `private`, `private`, entities.StatusActive, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	privateTenant := "tenant_private"
	privateCredential, err := h.creds.Create(context.Background(), entities.CredentialInput{Name: "private", Provider: entities.ProviderOpenAICompatible,
		Kind: entities.KindAPIKey, BaseURL: h.adminURL, APIKey: "private-secret", OwnerTenant: &privateTenant})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.models.Upsert(context.Background(), entities.ModelDef{Name: "private-model", Strategy: chat.StrategyPriority, Enabled: true,
		Routes: []entities.ModelRoute{{CredentialID: privateCredential.ID, Weight: 1, Enabled: true}}}); err != nil {
		t.Fatal(err)
	}
	foreignKey, err := h.keys.Create(context.Background(), apikey.CreateInput{TenantID: "tenant_default", Name: "foreign", Models: []string{"private-model"}, Scopes: []string{entities.ScopeChat}})
	if err != nil {
		t.Fatal(err)
	}
	callsBeforePrivate := calls.Load()
	privateResponse := h.request(t, http.MethodPost, "/v1/chat/completions", foreignKey.Plaintext, chatBody("private-model", false, "must not route"))
	if privateResponse.StatusCode != http.StatusServiceUnavailable || calls.Load() != callsBeforePrivate {
		t.Fatalf("private credential routed cross-tenant: status=%d calls_before=%d calls_after=%d", privateResponse.StatusCode, callsBeforePrivate, calls.Load())
	}
	privateResponse.Body.Close()

	deadline := time.Now().Add(2 * time.Second)
	usageReady := false
	for time.Now().Before(deadline) {
		summary := h.request(t, http.MethodGet, "/admin/usage/summary", key.Plaintext, nil)
		decoded := decodeResponse[entities.UsageSummary](t, summary)
		if decoded.Requests >= 3 && decoded.CacheHits >= 1 && decoded.CostUSD > 0 {
			usageReady = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !usageReady {
		t.Fatal("usage events were not durably flushed with cost and cache-hit metadata")
	}
	var encrypted bool
	if err := h.db.Pool.QueryRow(context.Background(), `SELECT position(convert_to($1,'UTF8') in api_key_enc)=0 FROM credentials LIMIT 1`, "provider-secret-value").Scan(&encrypted); err != nil || !encrypted {
		t.Fatalf("credential was not encrypted at rest: encrypted=%v err=%v", encrypted, err)
	}
}

func TestIdentityOrganizationAPIAndRoleMatrix(t *testing.T) {
	upstream, _ := mockOpenAIUpstream(t)
	h := newIntegrationHarness(t, upstream.URL)
	configured := createConfiguredKey(t, h, "identity-model", nil, nil)
	_ = configured
	createUser := func(username string) createUserAPIResponse {
		response := h.request(t, http.MethodPost, "/admin/users", h.masterKey, handlers.UserCreateRequest{Username: username, GenerateInitialKey: true, InitialKey: handlers.InitialKeyRequest{Name: "login", Scopes: []string{entities.ScopeChat, entities.ScopeKeysManage, entities.ScopeMembersManage, entities.ScopeUsageRead}}})
		if response.StatusCode != http.StatusCreated {
			t.Fatalf("create user %s: %d %s", username, response.StatusCode, readBody(t, response))
		}
		return decodeResponse[createUserAPIResponse](t, response)
	}
	adminUser := createUser("admin@example.com")
	memberUser := createUser("member@example.com")
	foreignUser := createUser("foreign@example.com")
	organizationResponse := h.request(t, http.MethodPost, "/admin/organizations", h.masterKey, handlers.OrganizationCreateRequest{Name: "Acme"})
	if organizationResponse.StatusCode != http.StatusCreated {
		t.Fatalf("create organization=%d", organizationResponse.StatusCode)
	}
	organization := decodeResponse[entities.Organization](t, organizationResponse)
	for _, request := range []handlers.MembershipCreateRequest{{UserID: adminUser.User.ID, Role: entities.MembershipAdmin}, {UserID: memberUser.User.ID, Role: entities.MembershipMember}} {
		response := h.request(t, http.MethodPost, "/admin/organizations/"+organization.ID+"/members", h.masterKey, request)
		if response.StatusCode != http.StatusCreated {
			t.Fatalf("add member=%d %s", response.StatusCode, readBody(t, response))
		}
	}
	adminKeyResponse := h.request(t, http.MethodPost, "/admin/api-keys", h.masterKey, handlers.APIKeyCreateRequest{Name: "scoped admin", OwnerType: entities.OwnerUser, OwnerUserID: adminUser.User.ID, ContextOrganizationID: organization.ID, Models: []string{"identity-model"}, Scopes: []string{entities.ScopeChat, entities.ScopeKeysManage, entities.ScopeMembersManage, entities.ScopeUsageRead}})
	if adminKeyResponse.StatusCode != http.StatusCreated {
		t.Fatalf("admin key=%d %s", adminKeyResponse.StatusCode, readBody(t, adminKeyResponse))
	}
	adminKey := decodeResponse[handlers.CreatedAPIKeyResponse](t, adminKeyResponse)
	membersResponse := h.request(t, http.MethodGet, "/admin/organizations/"+organization.ID+"/members", adminKey.Plaintext, nil)
	if membersResponse.StatusCode != http.StatusOK {
		t.Fatalf("admin list members=%d %s", membersResponse.StatusCode, readBody(t, membersResponse))
	}
	members := decodeResponse[handlers.MembershipListResponse](t, membersResponse)
	if len(members.Data) != 2 {
		t.Fatalf("members=%+v", members.Data)
	}
	memberKeyResponse := h.request(t, http.MethodPost, "/admin/api-keys", h.masterKey, handlers.APIKeyCreateRequest{Name: "scoped member", OwnerType: entities.OwnerUser, OwnerUserID: memberUser.User.ID, ContextOrganizationID: organization.ID, Scopes: []string{entities.ScopeMembersManage, entities.ScopeUsageRead}})
	memberKey := decodeResponse[handlers.CreatedAPIKeyResponse](t, memberKeyResponse)
	denied := h.request(t, http.MethodGet, "/admin/organizations/"+organization.ID+"/members", memberKey.Plaintext, nil)
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("member list members=%d", denied.StatusCode)
	}
	denied.Body.Close()
	organizationKeyResponse := h.request(t, http.MethodPost, "/admin/api-keys", adminKey.Plaintext, handlers.APIKeyCreateRequest{Name: "shared", OwnerType: entities.OwnerOrganization, OwnerOrganizationID: organization.ID, ContextOrganizationID: organization.ID, Models: []string{"identity-model"}, Scopes: []string{entities.ScopeChat, entities.ScopeUsageRead}})
	if organizationKeyResponse.StatusCode != http.StatusCreated {
		t.Fatalf("organization key=%d %s", organizationKeyResponse.StatusCode, readBody(t, organizationKeyResponse))
	}
	organizationKey := decodeResponse[handlers.CreatedAPIKeyResponse](t, organizationKeyResponse)
	login := h.request(t, http.MethodPost, "/login", "", handlers.LoginRequest{Key: organizationKey.Plaintext})
	if login.StatusCode != http.StatusOK {
		t.Fatalf("organization login=%d", login.StatusCode)
	}
	loginResult := decodeResponse[handlers.LoginResponse](t, login)
	if loginResult.PrincipalType != entities.PrincipalOrganization || loginResult.OrganizationID != organization.ID {
		t.Fatalf("organization login=%+v", loginResult)
	}
	personalKeyResponse := h.request(t, http.MethodPost, "/admin/api-keys", h.masterKey, handlers.APIKeyCreateRequest{Name: "personal call", OwnerType: entities.OwnerUser, OwnerUserID: foreignUser.User.ID, Models: []string{"identity-model"}, Scopes: []string{entities.ScopeChat, entities.ScopeUsageRead}})
	personalKey := decodeResponse[handlers.CreatedAPIKeyResponse](t, personalKeyResponse)
	for label, secret := range map[string]string{"personal": personalKey.Plaintext, "scoped": adminKey.Plaintext, "organization": organizationKey.Plaintext, "master": h.masterKey} {
		response := h.request(t, http.MethodPost, "/v1/chat/completions", secret, chatBody("identity-model", false, label))
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s model call=%d %s", label, response.StatusCode, readBody(t, response))
		}
		response.Body.Close()
	}
	var attribution *entities.UsagePage
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		attribution, _ = h.usage.Query(context.Background(), entities.UsageQuery{Visibility: entities.UsageVisibility{PrincipalType: entities.PrincipalMaster}, Model: "identity-model", Limit: 20})
		if attribution != nil && len(attribution.Data) >= 4 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	wantActors := map[string]bool{entities.ActorMaster: false, entities.ActorOrganization: false, entities.ActorUser + ":personal": false, entities.ActorUser + ":organization": false}
	for _, event := range attribution.Data {
		switch {
		case event.ActorType == entities.ActorMaster && event.Username == "master" && event.KeyID == "":
			wantActors[entities.ActorMaster] = true
		case event.ActorType == entities.ActorOrganization && event.Username == "org:"+organization.Name && event.OrganizationID == organization.ID:
			wantActors[entities.ActorOrganization] = true
		case event.ActorType == entities.ActorUser && event.UserID == foreignUser.User.ID && event.OrganizationID == "":
			wantActors[entities.ActorUser+":personal"] = true
		case event.ActorType == entities.ActorUser && event.UserID == adminUser.User.ID && event.OrganizationID == organization.ID:
			wantActors[entities.ActorUser+":organization"] = true
		}
	}
	for actor, found := range wantActors {
		if !found {
			t.Fatalf("missing %s attribution in %+v", actor, attribution)
		}
	}
	organizationUsage := h.request(t, http.MethodGet, "/admin/usage/recent?model=identity-model", organizationKey.Plaintext, nil)
	organizationEvents := decodeResponse[handlers.UsageRecentResponse](t, organizationUsage)
	if len(organizationEvents.Data) != 2 {
		t.Fatalf("organization usage visibility=%+v", organizationEvents.Data)
	}
	personalUsage := h.request(t, http.MethodGet, "/admin/usage/recent?model=identity-model", personalKey.Plaintext, nil)
	personalEvents := decodeResponse[handlers.UsageRecentResponse](t, personalUsage)
	if len(personalEvents.Data) != 1 || personalEvents.Data[0].OrganizationID != "" {
		t.Fatalf("personal usage visibility=%+v", personalEvents.Data)
	}
	foreignKeyResponse := h.request(t, http.MethodPost, "/admin/api-keys", h.masterKey, handlers.APIKeyCreateRequest{Name: "foreign personal", OwnerType: entities.OwnerUser, OwnerUserID: foreignUser.User.ID, Scopes: []string{entities.ScopeKeysManage}})
	foreignKey := decodeResponse[handlers.CreatedAPIKeyResponse](t, foreignKeyResponse)
	foreignList := h.request(t, http.MethodGet, "/admin/api-keys", adminKey.Plaintext, nil)
	body := readBody(t, foreignList)
	if strings.Contains(body, foreignKey.ID) {
		t.Fatal("organization admin saw unrelated personal key")
	}
	disable := h.request(t, http.MethodPatch, "/admin/users/"+adminUser.User.ID, h.masterKey, handlers.UserStatusRequest{Status: entities.StatusDisabled})
	if disable.StatusCode != http.StatusOK {
		t.Fatalf("disable user=%d", disable.StatusCode)
	}
	disable.Body.Close()
	stale := h.request(t, http.MethodGet, "/admin/organizations", adminKey.Plaintext, nil)
	if stale.StatusCode != http.StatusUnauthorized {
		t.Fatalf("disabled user key status=%d", stale.StatusCode)
	}
	stale.Body.Close()
}

func TestDistributedQuotaAndRPM(t *testing.T) {
	upstream, _ := mockOpenAIUpstream(t)
	h := newIntegrationHarness(t, upstream.URL)
	zero := float64(0)
	quotaKey := createConfiguredKey(t, h, "quota-model", &zero, nil)
	quotaResponse := h.request(t, http.MethodPost, "/v1/chat/completions", quotaKey.Plaintext, chatBody("quota-model", false, "will be rejected"))
	if quotaResponse.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("quota status=%d, want 429", quotaResponse.StatusCode)
	}
	quotaResponse.Body.Close()
	one := 1
	rpmKey := createConfiguredKey(t, h, "rpm-model", nil, &one)
	first := h.request(t, http.MethodPost, "/v1/chat/completions", rpmKey.Plaintext, chatBody("rpm-model", false, "one"))
	readBody(t, first)
	second := h.request(t, http.MethodPost, "/v1/chat/completions", rpmKey.Plaintext, chatBody("rpm-model", false, "two"))
	if first.StatusCode != http.StatusOK || second.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("RPM statuses=%d,%d", first.StatusCode, second.StatusCode)
	}
	second.Body.Close()
}

type anthropicMockResponse struct {
	ID         string                 `json:"id"`
	Model      string                 `json:"model"`
	StopReason string                 `json:"stop_reason"`
	Content    []anthropicMockContent `json:"content"`
	Usage      anthropicMockUsage     `json:"usage"`
}

type anthropicMockContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicMockUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

type oauthMockRequest struct {
	GrantType    string `json:"grant_type"`
	RefreshToken string `json:"refresh_token"`
}

type oauthMockResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func TestAnthropicOAuthRefreshAndTranslationThroughFiber(t *testing.T) {
	var messageCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			var request oauthMockRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.GrantType != "refresh_token" || request.RefreshToken != "old-refresh" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(oauthMockResponse{AccessToken: "fresh-access", RefreshToken: "fresh-refresh"})
		case "/v1/messages":
			messageCalls.Add(1)
			if r.Header.Get("Authorization") == "Bearer expired-access" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if r.Header.Get("Authorization") != "Bearer fresh-access" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			var request llm.AnthropicRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Messages) != 1 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(anthropicMockResponse{ID: "message-1", Model: request.Model, StopReason: "end_turn",
				Content: []anthropicMockContent{{Type: "text", Text: "translated response"}}, Usage: anthropicMockUsage{InputTokens: 9, OutputTokens: 3}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	h := newIntegrationHarness(t, server.URL)
	credentialEntity, err := h.creds.Create(context.Background(), entities.CredentialInput{Name: "oauth-anthropic", Provider: entities.ProviderAnthropic,
		Kind: entities.KindOAuth, BaseURL: server.URL, OAuthAccess: "expired-access", OAuthRefresh: "old-refresh"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.models.Upsert(context.Background(), entities.ModelDef{Name: "anthropic-model", UpstreamModel: "claude-test", Strategy: chat.StrategyPriority, Enabled: true,
		Routes: []entities.ModelRoute{{CredentialID: credentialEntity.ID, Priority: 1, Weight: 1, Enabled: true}}}); err != nil {
		t.Fatal(err)
	}
	key, err := h.keys.Create(context.Background(), apikey.CreateInput{TenantID: "tenant_default", Name: "anthropic key", Models: []string{"anthropic-model"}, Scopes: []string{entities.ScopeChat}})
	if err != nil {
		t.Fatal(err)
	}
	response := h.request(t, http.MethodPost, "/v1/chat/completions", key.Plaintext, chatBody("anthropic-model", false, "hello"))
	if response.StatusCode != http.StatusOK {
		body := readBody(t, response)
		t.Fatalf("Anthropic call status=%d body=%s", response.StatusCode, body)
	}
	translated := decodeResponse[llm.Response](t, response)
	if len(translated.Choices) != 1 || translated.Choices[0].Message.Content != "translated response" || translated.Usage.PromptTokens != 9 || messageCalls.Load() != 2 {
		t.Fatalf("translated response=%+v calls=%d", translated, messageCalls.Load())
	}
	runtime, err := h.creds.Runtime(context.Background(), credentialEntity.ID)
	if err != nil || runtime.OAuthAccess != "fresh-access" || runtime.OAuthRefreh != "fresh-refresh" {
		t.Fatalf("rotated runtime=%+v err=%v", runtime, err)
	}
	var encrypted bool
	if err := h.db.Pool.QueryRow(context.Background(), `SELECT position(convert_to($1,'UTF8') in oauth_blob_enc)=0 FROM credentials WHERE id=$2`, "fresh-access", credentialEntity.ID).Scan(&encrypted); err != nil || !encrypted {
		t.Fatalf("rotated OAuth token was not encrypted: encrypted=%v err=%v", encrypted, err)
	}
}

func readBody(t *testing.T, res *http.Response) string {
	t.Helper()
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
