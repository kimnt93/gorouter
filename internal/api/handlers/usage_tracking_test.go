package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/kimnt93/gorouter/internal/platform/database"
	"github.com/kimnt93/gorouter/internal/platform/llm"
	"github.com/kimnt93/gorouter/internal/repositories/local"
	"github.com/kimnt93/gorouter/pkg/apikey"
	"github.com/kimnt93/gorouter/pkg/chat"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/modelroute"
	"github.com/kimnt93/gorouter/pkg/usage"
)

type trackingUpstream struct {
	calls int
	fail  bool
}

func (u *trackingUpstream) Send(_ context.Context, _ *entities.CredentialRuntime, model string, raw []byte) (*entities.UpstreamResult, error) {
	u.calls++
	if strings.Contains(string(raw), "trace-marker") || strings.Contains(string(raw), "request-marker") {
		return nil, errors.New("internal tracking leaked upstream")
	}
	if u.fail && u.calls == 1 {
		return nil, errors.New("synthetic connection failure")
	}
	var request llm.ChatRequest
	_ = json.Unmarshal(raw, &request)
	if request.Stream {
		return &entities.UpstreamResult{StatusCode: 200, Body: io.NopCloser(strings.NewReader("data: {\"id\":\"completion\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"cache_read_tokens\":3}}\n\ndata: [DONE]\n\n"))}, nil
	}
	return &entities.UpstreamResult{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":"completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"cache_read_tokens":3}}`))}, nil
}
func TestTrackingAcrossInferenceProtocols(t *testing.T) {
	for _, protocol := range []string{"chat", "responses", "messages"} {
		for _, mode := range []string{"success", "cache", "failed_stream", "fallback", "unbound"} {
			for _, stream := range []bool{false, true} {
				if mode == "failed_stream" && !stream {
					continue
				}
				t.Run(mode+"/"+protocol+map[bool]string{true: "_stream", false: "_nonstream"}[stream], func(t *testing.T) {
					key := &entities.ApiKey{ID: "key", OwnerType: entities.OwnerUser, OwnerUserID: "user-1", Models: []string{"model-a"}, Scopes: []string{entities.ScopeChat}, Enabled: true, Workload: entities.WorkloadBinding{Application: "test-app", WorkspaceID: "workspace", AgentID: "agent"}}
					if mode == "unbound" {
						key.Workload = entities.WorkloadBinding{}
					}
					repo := &captureUsageRepository{}
					svc := usage.NewService(repo, 16, nil)
					upstream := &trackingUpstream{}
					if mode == "fallback" {
						upstream.fail = true
					}
					gateway := &Gateway{Keys: apikey.NewService(gatewayKeyRepo{key}, func(string) string { return "" }, func() string { return "" }), Creds: credential.NewService(gatewayCredRepo{routes: []entities.RouteCandidate{{CredentialID: "cred"}}, runtimes: map[string]*entities.CredentialRuntime{"cred": {ID: "cred", Kind: entities.KindAPIKey, Provider: entities.ProviderOpenAICompatible}}}, nil), Models: modelroute.NewService(gatewayModelRepo{model: entities.ModelDef{Name: "model-a", UpstreamModel: "upstream", Strategy: chat.StrategyPriority, Enabled: true}}), OpenAI: upstream, Selector: &chat.Selector{}, Health: chat.NewHealth(), Usage: svc}
					if mode == "cache" {
						gateway.Cache = trackingCache{}
					}
					if mode == "failed_stream" {
						gateway.OpenAI = failingGatewayStreamUpstream{}
					}
					if mode == "fallback" {
						gateway.RouteRetries = 1
					}
					app := fiber.New()
					app.Post("/test", func(c fiber.Ctx) error {
						c.Locals(localSession, &entities.Session{Role: entities.RoleAPIKey, PrincipalType: entities.PrincipalUser, KeyID: key.ID, UserID: "user-1", Scopes: key.Scopes})
						switch protocol {
						case "messages":
							return gateway.Messages(c)
						case "responses":
							return gateway.Responses(c)
						default:
							return gateway.Chat(c)
						}
					})
					body := `{"model":"model-a","messages":[{"role":"user","content":"synthetic"}],"max_tokens":10`
					if protocol == "responses" {
						body = `{"model":"model-a","input":"synthetic"`
					}
					if stream {
						body += `,"stream":true`
					}
					body += "}"
					request := httptest.NewRequest("POST", "/test", strings.NewReader(body))
					request.Header.Set("Content-Type", "application/json")
					for name, value := range map[string]string{headerAgentID: "agent", headerConversationID: "conversation", headerRunID: "run", headerParentRunID: "parent", headerLogicalRequestID: "request-marker", headerTraceID: "trace-marker", "X-Agent-ID": "forged", "X-User-ID": "foreign"} {
						request.Header.Set(name, value)
					}
					res, err := app.Test(request)
					if err != nil {
						t.Fatal(err)
					}
					_, _ = io.Copy(io.Discard, res.Body)
					res.Body.Close()
					svc.Close()
					wantCalls := 1
					wantStatus := 200
					if mode == "cache" || mode == "failed_stream" {
						wantCalls = 0
					}
					if mode == "fallback" {
						wantCalls = 2
					}
					if res.StatusCode != wantStatus || upstream.calls != wantCalls || len(repo.events) != 1 {
						t.Fatalf("status=%d calls=%d events=%d", res.StatusCode, upstream.calls, len(repo.events))
					}
					ev := repo.events[0]
					if mode == "cache" && (!ev.CacheHit || ev.CostUSD != 0) {
						t.Fatal("router cache attribution lost")
					}
					if mode == "failed_stream" && ev.StatusCode < 400 {
						t.Fatal("failed stream not recorded")
					}
					if ev.TraceID != "trace-marker" || ev.ParentRunID != "parent" || ev.ConversationID != "conversation" || ev.RunID != "run" || ev.LogicalRequestID != "request-marker" || ev.AgentID != "agent" || ev.UserID != "user-1" {
						t.Fatal("incorrect attribution")
					}
					if len(ev.ConversationEnc) != 0 {
						t.Fatal("accounting enabled content capture")
					}
					if res.Header.Get(headerLogicalRequestID) != "request-marker" {
						t.Fatal("request header not echoed")
					}
				})
			}
		}
	}

}

func TestCorrelationValidationAndDefaults(t *testing.T) {
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		v, err := correlationFromRequest(c)
		if err != nil {
			return c.SendStatus(400)
		}
		c.Set(headerLogicalRequestID, v.LogicalRequestID)
		return c.SendStatus(200)
	})
	previous := ""
	for i := 0; i < 2; i++ {
		res, err := app.Test(httptest.NewRequest("GET", "/", nil))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		id := res.Header.Get(headerLogicalRequestID)
		if id == "" || id == previous {
			t.Fatal("default request ID not unique")
		}
		previous = id
	}
	for _, bad := range []string{"invalid value", strings.Repeat("a", 129), "a,b"} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set(headerTraceID, bad)
		res, err := app.Test(r)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 400 {
			t.Fatal("invalid trace accepted")
		}
	}
}

func TestTrackingQueryAuthorizationAndMultiSelect(t *testing.T) {
	ctx := context.Background()
	db, err := database.ConnectSQLite(ctx, t.TempDir()+"/usage.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo := local.NewUsageRepo(local.New(db.DB))
	svc := usage.NewService(repo, 16, nil)
	defer svc.Close()
	now := time.Now().UTC()
	events := []entities.UsageEvent{}
	for _, v := range []struct{ id, user, org, agent string }{{"one", "u1", "org1", "a"}, {"two", "u1", "org1", "b"}, {"foreign", "u2", "org2", "a"}, {"private", "u1", "", "a"}} {
		events = append(events, entities.UsageEvent{ID: v.id, TS: now, AccountingTS: now, ActorType: entities.ActorUser, UserID: v.user, OrganizationID: v.org, Application: "app", WorkspaceID: "ws", AgentID: v.agent, TraceID: "trace-" + v.id, ParentRunID: "parent", RunID: "run", LogicalRequestID: "req", ConversationID: "session", CostUSD: 1, Priced: true})
	}
	if err = repo.InsertBatch(ctx, events); err != nil {
		t.Fatal(err)
	}
	bound := &entities.ApiKey{ID: "key", OwnerType: entities.OwnerUser, OwnerUserID: "u1", Workload: entities.WorkloadBinding{Application: "app", WorkspaceID: "ws", AgentID: "a"}}
	for _, endpoint := range []string{"recent", "summary", "activity"} {
		for _, tc := range []struct {
			name   string
			sess   entities.Session
			query  string
			status int
			count  int64
			bound  bool
		}{
			{"master_all", entities.Session{Role: entities.RoleMaster, PrincipalType: entities.PrincipalMaster}, "", 200, 4, false},
			{"user_all", entities.Session{PrincipalType: entities.PrincipalUser, UserID: "u1", Scopes: []string{entities.ScopeUsageRead}}, "", 200, 3, false},
			{"member_context", entities.Session{PrincipalType: entities.PrincipalUser, UserID: "u1", OrganizationID: "org1", Scopes: []string{entities.ScopeUsageRead}}, "", 200, 2, false},
			{"org_admin", entities.Session{PrincipalType: entities.PrincipalUser, UserID: "u1", OrganizationID: "org1", MembershipRole: entities.MembershipAdmin, Scopes: []string{entities.ScopeUsageRead}}, "", 200, 2, false},
			{"multi_or", entities.Session{Role: entities.RoleMaster, PrincipalType: entities.PrincipalMaster}, "&agent_id=a&agent_id=b&trace_id=trace-one,trace-two&parent_run_id=parent&run_id=run&logical_request_id=req&conversation_id=session", 200, 2, false},
			{"foreign_filter", entities.Session{PrincipalType: entities.PrincipalUser, UserID: "u1", Scopes: []string{entities.ScopeUsageRead}}, "&user_id=u2", 200, 0, false},
			{"no_scope", entities.Session{PrincipalType: entities.PrincipalUser, UserID: "u1"}, "", 403, 0, false},
			{"bound_all", entities.Session{KeyID: "key", PrincipalType: entities.PrincipalUser, UserID: "u1", Scopes: []string{entities.ScopeUsageRead}}, "", 200, 3, true},
			{"bound_forged", entities.Session{KeyID: "key", PrincipalType: entities.PrincipalUser, UserID: "u1", Scopes: []string{entities.ScopeUsageRead}}, "&agent_id=b", 200, 1, true},
			{"invalid_filter", entities.Session{Role: entities.RoleMaster, PrincipalType: entities.PrincipalMaster}, "&trace_id=a,,b", 400, 0, false},
			{"oversized_filter", entities.Session{Role: entities.RoleMaster, PrincipalType: entities.PrincipalMaster}, "&agent_id=" + strings.Repeat("a,", 100) + "a", 400, 0, false},
		} {
			t.Run(endpoint+"/"+tc.name, func(t *testing.T) {
				admin := &Admin{UsageSvc: svc}
				if tc.bound {
					admin.KeysSvc = apikey.NewService(gatewayKeyRepo{bound}, nil, nil)
				}
				app := fiber.New()
				app.Get("/", func(c fiber.Ctx) error {
					c.Locals(localSession, &tc.sess)
					switch endpoint {
					case "recent":
						return admin.UsageRecent(c)
					case "summary":
						return admin.UsageSummary(c)
					default:
						return admin.UsageActivity(c)
					}
				})
				res, err := app.Test(httptest.NewRequest("GET", "/?range=all"+tc.query, nil))
				if err != nil {
					t.Fatal(err)
				}
				defer res.Body.Close()
				if res.StatusCode != tc.status {
					t.Fatalf("status=%d want %d", res.StatusCode, tc.status)
				}
				if tc.status != 200 {
					return
				}
				var count int64
				switch endpoint {
				case "recent":
					var body entities.UsagePage
					if err = json.NewDecoder(res.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					count = int64(len(body.Data))
					for _, e := range body.Data {
						if e.TraceID == "" {
							t.Fatal("trace missing from response")
						}
					}
				case "summary":
					var body entities.UsageSummary
					_ = json.NewDecoder(res.Body).Decode(&body)
					count = body.Requests
				default:
					var body UsageActivityResponse
					_ = json.NewDecoder(res.Body).Decode(&body)
					count = body.Summary.Requests
				}
				if count != tc.count {
					t.Fatalf("count=%d want %d", count, tc.count)
				}
			})
		}
	}
}

type trackingCache struct{}

func (trackingCache) Lookup(string, string, string, []byte) (*chat.CacheEntry, bool) {
	return &chat.CacheEntry{Status: 200, ContentType: "application/json", Body: []byte(`{"choices":[{"message":{"role":"assistant","content":"cached"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)}, true
}
func (trackingCache) Store(string, string, string, []byte, *chat.CacheEntry) bool { return true }
func (trackingCache) Flush()                                                      {}
func (trackingCache) Stats() chat.CacheStats                                      { return chat.CacheStats{} }
func (trackingCache) Close()                                                      {}

func TestWeeklyTrackingFiltersRespectBinding(t *testing.T) {
	ctx := context.Background()
	db, err := database.ConnectSQLite(ctx, t.TempDir()+"/weekly.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo := local.NewUsageRepo(local.New(db.DB))
	svc := usage.NewService(repo, 16, nil)
	defer svc.Close()
	key := &entities.ApiKey{ID: "key", OwnerType: entities.OwnerUser, OwnerUserID: "user", Workload: entities.WorkloadBinding{Application: "app", WorkspaceID: "ws", AgentID: "a"}}
	now := time.Now().UTC()
	for _, trace := range []string{"t1", "t2"} {
		if err = repo.InsertBatch(ctx, []entities.UsageEvent{{ID: entities.NewID("usage"), TS: now, AccountingTS: now, UserID: "user", ActorType: entities.ActorUser, Application: "app", WorkspaceID: "ws", AgentID: "a", TraceID: trace, CostUSD: 1, Priced: true}}); err != nil {
			t.Fatal(err)
		}
	}
	admin := &Admin{UsageSvc: svc, KeysSvc: apikey.NewService(gatewayKeyRepo{key}, nil, nil)}
	for _, tc := range []struct {
		query  string
		count  int64
		status int
	}{{"", 2, 200}, {"?trace_id=t1", 1, 200}, {"?trace_id=t1&trace_id=t2", 2, 200}, {"?trace_id=missing", 0, 200}, {"?agent_id=other&agent_id=a", 2, 200}, {"?application=foreign&application=app", 2, 200}, {"?environment=foreign", 0, 200}, {"?trace_id=a,,b", 0, 400}} {
		t.Run(tc.query, func(t *testing.T) {
			app := fiber.New()
			app.Get("/", func(c fiber.Ctx) error {
				c.Locals(localSession, &entities.Session{Role: entities.RoleAPIKey, KeyID: key.ID, PrincipalType: entities.PrincipalUser, UserID: "user", Scopes: []string{entities.ScopeUsageRead}})
				return admin.WorkloadWeeklyUsage(c)
			})
			res, err := app.Test(httptest.NewRequest("GET", "/"+tc.query, nil))
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if res.StatusCode != tc.status {
				t.Fatalf("status=%d want %d", res.StatusCode, tc.status)
			}
			if tc.status != 200 {
				return
			}
			var body WorkloadWeeklyUsageResponse
			if err = json.NewDecoder(res.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Summary.Requests != tc.count {
				t.Fatalf("requests=%d want %d", body.Summary.Requests, tc.count)
			}
		})
	}
	// Closing a backend must not turn an unavailable aggregate into successful zero.
	db.Close()
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		c.Locals(localSession, &entities.Session{Role: entities.RoleAPIKey, KeyID: key.ID, PrincipalType: entities.PrincipalUser, UserID: "user", Scopes: []string{entities.ScopeUsageRead}})
		return admin.WorkloadWeeklyUsage(c)
	})
	res, err := app.Test(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 503 {
		t.Fatalf("outage status=%d", res.StatusCode)
	}
}
