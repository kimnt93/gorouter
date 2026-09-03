package providerquota

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
)

type quotaCredentialRepo struct {
	runtime     *entities.CredentialRuntime
	credentials []entities.Credential
}

type quotaStore struct {
	loaded   []Snapshot
	saved    []Snapshot
	inUseIDs []string
}

func (s *quotaStore) LoadAll(context.Context) ([]Snapshot, error) { return s.loaded, nil }
func (s *quotaStore) Save(_ context.Context, snapshot Snapshot) error {
	s.saved = append(s.saved, snapshot)
	return nil
}
func (s *quotaStore) SetInUse(_ context.Context, id, _ string) error {
	s.inUseIDs = append(s.inUseIDs, id)
	return nil
}

func (r quotaCredentialRepo) Create(context.Context, entities.CredentialInput, entities.SecretBox) (*entities.Credential, error) {
	return nil, nil
}
func (r quotaCredentialRepo) List(context.Context) ([]entities.Credential, error) {
	return append([]entities.Credential(nil), r.credentials...), nil
}
func (r quotaCredentialRepo) Update(context.Context, entities.SecretBox, string, entities.CredentialUpdate) (*entities.Credential, error) {
	return nil, nil
}
func (r quotaCredentialRepo) Delete(context.Context, string) error { return nil }
func (r quotaCredentialRepo) Runtime(context.Context, entities.SecretBox, string) (*entities.CredentialRuntime, error) {
	return r.runtime, nil
}
func (r quotaCredentialRepo) UpdateOAuthTokens(context.Context, entities.SecretBox, string, string, string) error {
	return nil
}
func (r quotaCredentialRepo) RoutesForModel(context.Context, string) ([]entities.RouteCandidate, error) {
	return nil, nil
}

func TestSyncAccountRingsSortsPerProviderBySetupName(t *testing.T) {
	repo := quotaCredentialRepo{credentials: []entities.Credential{
		{ID: "c5", Name: "c5", Provider: "codex", Status: entities.StatusActive},
		{ID: "c1", Name: "c1", Provider: "codex", Status: entities.StatusActive},
		{ID: "c4", Name: "c4", Provider: "codex", Status: entities.StatusActive},
		{ID: "c2", Name: "c2", Provider: "codex", Status: entities.StatusActive},
		{ID: "c3", Name: "c3", Provider: "codex", Status: entities.StatusActive},
		{ID: "c5", Name: "c5", Provider: "opencode-zen", Status: entities.StatusActive},
		{ID: "c1", Name: "c1", Provider: "opencode-zen", Status: entities.StatusActive},
		{ID: "c2", Name: "c2", Provider: "opencode-zen", Status: entities.StatusActive},
		{ID: "disabled", Name: "c0", Provider: "codex", Status: entities.StatusDisabled},
	}}
	service := New(nil, credential.NewService(repo, nil))
	if err := service.SyncAccountRings(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(service.rings["codex"], ","); got != "c1,c2,c3,c4,c5" {
		t.Fatalf("codex ring=%s", got)
	}
	if got := strings.Join(service.rings["opencode-zen"], ","); got != "c1,c2,c5" {
		t.Fatalf("opencode ring=%s", got)
	}
}

func TestPercentAndUtilizationWindows(t *testing.T) {
	now := time.Now()
	percent := parsePercentWindow("Session", map[string]any{"used_percent": 25.5, "reset_after_seconds": 60.0})
	if percent.RemainingPercent != 74.5 || percent.ResetAt == nil || percent.ResetAt.Before(now.Add(55*time.Second)) {
		t.Fatalf("percent window = %+v", percent)
	}
	ratio := parseUtilizationWindow("Weekly", map[string]any{"utilization": 0.42, "resets_at": "2026-08-27T00:00:00Z"})
	if ratio.UsedPercent != 42 || ratio.RemainingPercent != 58 || ratio.ResetAt == nil {
		t.Fatalf("utilization window = %+v", ratio)
	}
}

func TestAnthropicUtilizationWindowsIncludeModelSpecificLimits(t *testing.T) {
	payload := map[string]any{
		"five_hour":            map[string]any{"utilization": 0.25},
		"seven_day":            map[string]any{"utilization": 0.50},
		"seven_day_opus":       map[string]any{"utilization": 1.0},
		"seven_day_sonnet":     map[string]any{"utilization": 0.10},
		"seven_day_oauth_apps": map[string]any{"utilization": 0.20},
	}
	windows := compact([]Window{
		parseUtilizationWindow("Session (5h)", object(payload, "five_hour")),
		parseUtilizationWindow("Weekly (7d)", object(payload, "seven_day")),
		parseUtilizationWindow("Weekly Opus", object(payload, "seven_day_opus")),
		parseUtilizationWindow("Weekly Sonnet", object(payload, "seven_day_sonnet")),
		parseUtilizationWindow("Weekly OAuth apps", object(payload, "seven_day_oauth_apps")),
	})
	if len(windows) != 5 || windows[2].Name != "Weekly Opus" || windows[2].RemainingPercent != 0 || windows[3].RemainingPercent != 90 {
		t.Fatalf("Anthropic windows = %+v", windows)
	}
	if missing := parseUtilizationWindow("Missing", nil); missing.Name != "" {
		t.Fatalf("missing utilization window = %+v", missing)
	}
}

func TestFetchUsesBearerAndExtraHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret-token" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("chatgpt-account-id"); got != "account-1" {
			t.Errorf("chatgpt-account-id = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"rate_limit":{"primary_window":{"used_percent":20}}}`))
	}))
	defer server.Close()

	service := New(server.Client(), nil)
	payload, err := service.fetch(context.Background(), &entities.CredentialRuntime{OAuthAccess: "secret-token"}, http.MethodGet, server.URL, nil, map[string]string{"chatgpt-account-id": "account-1"})
	if err != nil || len(object(payload, "rate_limit")) == 0 {
		t.Fatalf("fetch() payload=%v err=%v", payload, err)
	}
}

func TestAvailabilityExpiresExhaustionCooldown(t *testing.T) {
	service := New(nil, nil)
	service.snapshots["available"] = Snapshot{Windows: []Window{{Name: "Weekly", RemainingPercent: 50}}}
	service.snapshots["empty"] = Snapshot{Windows: []Window{{Name: "Weekly", RemainingPercent: 0}}}
	service.snapshots["reset"] = Snapshot{Windows: []Window{{Name: "Session", RemainingPercent: 0, ResetAt: timePointer(time.Now().Add(-time.Minute))}}}
	service.exhausted["cooldown"] = time.Now().Add(time.Minute)
	service.exhausted["expired"] = time.Now().Add(-time.Minute)

	for id, want := range map[string]bool{"unknown": true, "available": true, "empty": false, "reset": true, "cooldown": false, "expired": true} {
		if got := service.Available(id); got != want {
			t.Errorf("Available(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestRestoreAndMarkInUsePersistOnlyOnAccountTransition(t *testing.T) {
	runtime := &entities.CredentialRuntime{ID: "cred-b", Provider: "codex", OAuthMeta: entities.OAuthMetadata{Email: "person@example.test"}}
	credentials := credential.NewService(quotaCredentialRepo{runtime: runtime}, nil)
	store := &quotaStore{loaded: []Snapshot{{CredentialID: "cred-a", Provider: "codex", Account: "first", Available: true, InUse: true, Windows: []Window{}}}}
	service := New(nil, credentials)
	service.SetStore(store)
	if err := service.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	if snapshot, ok := service.Cached("cred-a"); !ok || !snapshot.InUse {
		t.Fatalf("restored snapshot = %+v, %v", snapshot, ok)
	}

	service.MarkInUse("cred-b")
	service.MarkInUse("cred-b")
	if len(store.saved) != 1 || len(store.inUseIDs) != 1 || store.inUseIDs[0] != "cred-b" {
		t.Fatalf("persistence calls saved=%+v inUse=%+v", store.saved, store.inUseIDs)
	}
	if first, _ := service.Cached("cred-a"); first.InUse {
		t.Fatal("previous account remained active")
	}
	if second, _ := service.Cached("cred-b"); !second.InUse || second.Account != "pe****@example.test" {
		t.Fatalf("active account = %+v", second)
	}
}

func TestActiveCredentialRestoresOrderedFailoverCursor(t *testing.T) {
	store := &quotaStore{loaded: []Snapshot{{CredentialID: "cred-c", Provider: "codex", InUse: true}}}
	service := New(nil, nil)
	service.SetStore(store)
	if err := service.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := service.ActiveCredential("codex"); got != "cred-c" {
		t.Fatalf("active credential = %q", got)
	}
}

func TestMaskAccountNeverReturnsToken(t *testing.T) {
	runtime := &entities.CredentialRuntime{APIKey: "top-secret", OAuthMeta: entities.OAuthMetadata{Email: "person@example.test"}}
	if got := maskAccount(runtime); got != "pe****@example.test" {
		t.Fatalf("maskAccount() = %q", got)
	}
	runtime.OAuthMeta = entities.OAuthMetadata{}
	if got := maskAccount(runtime); got != "connected account" {
		t.Fatalf("fallback mask = %q", got)
	}
}

func TestFailedReloadPreservesLastKnownQuotaAndExhaustion(t *testing.T) {
	status := http.StatusOK
	reset := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = w.Write([]byte(`{"quota":{"window_weekly":{"used":100,"limit":100,"reset_at":"` + reset + `"}}}`))
		}
	}))
	defer server.Close()

	runtime := &entities.CredentialRuntime{ID: "cred-1", Provider: "opencode-go", BaseURL: server.URL, APIKey: "secret"}
	credentials := credential.NewService(quotaCredentialRepo{runtime: runtime}, nil)
	service := New(server.Client(), credentials)
	first, err := service.Refresh(context.Background(), runtime.ID)
	if err != nil || first.Available || len(first.Windows) != 1 {
		t.Fatalf("initial refresh = %+v, err=%v", first, err)
	}

	status = http.StatusInternalServerError
	failed, err := service.Refresh(context.Background(), runtime.ID)
	if err != nil || failed.Available || len(failed.Windows) != 1 || failed.Message != "quota endpoint returned HTTP 500" {
		t.Fatalf("failed refresh = %+v, err=%v", failed, err)
	}
	if service.Available(runtime.ID) {
		t.Fatal("failed reload made an exhausted credential available")
	}
}

func timePointer(value time.Time) *time.Time { return &value }

type quotaOAuthRefresher struct{ calls int }

func (r *quotaOAuthRefresher) Refresh(_ context.Context, runtime *entities.CredentialRuntime) error {
	r.calls++
	runtime.OAuthAccess = "fresh-token"
	return nil
}

type quotaRewriteTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (transport quotaRewriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.URL.Scheme, clone.URL.Host = transport.target.Scheme, transport.target.Host
	return transport.base.RoundTrip(clone)
}

func TestCodexResetCreditsRefreshAuthValidateSelectionConsumeAndRefreshQuota(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Header.Get("chatgpt-account-id") != "account-1" || r.Header.Get("originator") != "codex_cli_rs" {
			t.Fatalf("headers=%v", r.Header)
		}
		switch r.URL.Path {
		case "/backend-api/wham/rate-limit-reset-credits":
			if r.Header.Get("Authorization") == "Bearer stale-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"credits": []any{
				map[string]any{"id": "later", "expires_at": "2099-08-01T00:00:00Z"},
				map[string]any{"id": "chosen", "expires_at": "2099-07-01T00:00:00Z"},
			}})
		case "/backend-api/wham/rate-limit-reset-credits/consume":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["credit_id"] != "chosen" || body["redeem_request_id"] != "request-1" {
				t.Fatalf("body=%v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"code": "alreadyRedeemed"})
		case "/backend-api/wham/usage":
			_ = json.NewEncoder(w).Encode(map[string]any{"rate_limit": map[string]any{"primary_window": map[string]any{"used_percent": 0.0}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	client := &http.Client{Transport: quotaRewriteTransport{target: target, base: http.DefaultTransport}}
	runtime := &entities.CredentialRuntime{ID: "cred-1", Provider: "codex", Kind: entities.KindOAuth, OAuthAccess: "stale-token", OAuthRefreh: "refresh-token", OAuthAccount: "account-1"}
	service := New(client, credential.NewService(quotaCredentialRepo{runtime: runtime}, nil))
	refresher := &quotaOAuthRefresher{}
	service.SetCodexOAuth(refresher)
	listed, err := service.ListCodexResetCredits(context.Background(), runtime.ID)
	if err != nil || refresher.calls != 1 || len(listed.Credits) != 2 || listed.Credits[0].SelectionToken != "chosen" {
		t.Fatalf("listed=%+v refreshes=%d err=%v", listed, refresher.calls, err)
	}
	result, err := service.ConsumeCodexResetCredit(context.Background(), runtime.ID, "chosen", "request-1")
	if err != nil || result.Outcome != "alreadyredeemed" || len(result.Quota.Windows) != 2 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if strings.Join(calls, ",") != "GET /backend-api/wham/rate-limit-reset-credits,GET /backend-api/wham/rate-limit-reset-credits,GET /backend-api/wham/rate-limit-reset-credits,POST /backend-api/wham/rate-limit-reset-credits/consume,GET /backend-api/wham/usage" {
		t.Fatalf("calls=%v", calls)
	}
}

func TestParseNestedCodexResetCreditsAndRejectsUnavailable(t *testing.T) {
	result := parseResetCreditList(map[string]any{"rate_limit_reset_credits": map[string]any{
		"available_count": 2.0,
		"credits": []any{
			map[string]any{"credit_id": "available", "available": true},
			map[string]any{"credit_id": "hidden", "available": false},
		},
	}})
	if result.AvailableCount != 2 || len(result.Credits) != 1 || result.Credits[0].SelectionToken != "available" {
		t.Fatalf("result=%+v", result)
	}
}
