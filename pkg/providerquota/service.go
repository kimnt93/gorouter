package providerquota

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
)

type Window struct {
	Name             string     `json:"name"`
	UsedPercent      float64    `json:"used_percent"`
	RemainingPercent float64    `json:"remaining_percent"`
	ResetAt          *time.Time `json:"reset_at,omitempty"`
}

type Snapshot struct {
	CredentialID string     `json:"credential_id"`
	Provider     string     `json:"provider"`
	Account      string     `json:"account"`
	Plan         string     `json:"plan,omitempty"`
	FetchedAt    *time.Time `json:"fetched_at,omitempty"`
	Available    bool       `json:"available"`
	Windows      []Window   `json:"windows"`
	Message      string     `json:"message,omitempty"`
	InUse        bool       `json:"in_use,omitempty"`
}

// Store persists only safe quota metadata. Implementations must never store
// credential tokens or upstream response bodies.
type Store interface {
	LoadAll(ctx context.Context) ([]Snapshot, error)
	Save(ctx context.Context, snapshot Snapshot) error
	SetInUse(ctx context.Context, credentialID, provider string) error
}

type Service struct {
	client      *http.Client
	credentials *credential.Service
	mu          sync.RWMutex
	snapshots   map[string]Snapshot
	exhausted   map[string]time.Time
	active      map[string]string
	store       Store
}

func New(client *http.Client, credentials *credential.Service) *Service {
	if client == nil {
		client = http.DefaultClient
	}
	return &Service{client: client, credentials: credentials, snapshots: map[string]Snapshot{}, exhausted: map[string]time.Time{}, active: map[string]string{}}
}

func (s *Service) SetStore(store Store) { s.store = store }

// Restore warms the read-only quota cache from durable snapshots. It does not
// contact any provider and is safe to call during startup.
func (s *Service) Restore(ctx context.Context) error {
	if s.store == nil {
		return nil
	}
	snapshots, err := s.store.LoadAll(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	for _, snapshot := range snapshots {
		s.snapshots[snapshot.CredentialID] = snapshot
		if snapshot.InUse {
			s.active[snapshot.Provider] = snapshot.CredentialID
		}
	}
	s.mu.Unlock()
	return nil
}

func Supported(provider string) bool {
	switch provider {
	case "codex", "claude", "kiro", "amazon-q", "opencode-go", "opencode-zen":
		return true
	default:
		return false
	}
}

func (s *Service) Cached(id string) (Snapshot, bool) {
	s.mu.RLock()
	snapshot, ok := s.snapshots[id]
	s.mu.RUnlock()
	if ok {
		snapshot.Available = s.Available(id)
	}
	return snapshot, ok
}

func (s *Service) Available(id string) bool {
	now := time.Now()
	s.mu.RLock()
	until := s.exhausted[id]
	snapshot, ok := s.snapshots[id]
	s.mu.RUnlock()
	if until.After(now) {
		return false
	}
	if !ok {
		return true
	}
	for _, window := range snapshot.Windows {
		if window.RemainingPercent <= 0 && (window.ResetAt == nil || window.ResetAt.After(now)) {
			return false
		}
	}
	return true
}

func (s *Service) MarkExhausted(id string) {
	now := time.Now()
	until := now.Add(5 * time.Minute)
	s.mu.Lock()
	if snapshot, ok := s.snapshots[id]; ok {
		for _, window := range snapshot.Windows {
			if window.ResetAt != nil && window.ResetAt.After(now) && (until.Equal(now.Add(5*time.Minute)) || window.ResetAt.Before(until)) {
				until = *window.ResetAt
			}
		}
	}
	s.exhausted[id] = until
	s.mu.Unlock()
}

// MarkInUse records the credential that most recently accepted a gateway
// request. Only one credential per provider is highlighted.
func (s *Service) MarkInUse(id string) {
	s.mu.RLock()
	target, ok := s.snapshots[id]
	alreadyActive := ok && s.active[target.Provider] == id
	s.mu.RUnlock()
	if alreadyActive {
		return
	}
	if !ok {
		runtime, err := s.credentials.Runtime(context.Background(), id)
		if err != nil {
			return
		}
		target = Snapshot{CredentialID: id, Provider: runtime.Provider, Account: maskAccount(runtime), Available: true, Windows: []Window{}}
	}
	s.mu.Lock()
	provider := target.Provider
	for key, snapshot := range s.snapshots {
		if key == id || provider == "" || snapshot.Provider == provider {
			snapshot.InUse = key == id
			s.snapshots[key] = snapshot
		}
	}
	target.InUse = true
	s.snapshots[id] = target
	s.active[provider] = id
	s.mu.Unlock()
	if s.store != nil {
		_ = s.store.Save(context.Background(), target)
		_ = s.store.SetInUse(context.Background(), id, provider)
	}
}

func (s *Service) Refresh(ctx context.Context, id string) (Snapshot, error) {
	runtime, err := s.credentials.Runtime(ctx, id)
	if err != nil {
		return Snapshot{}, err
	}
	if !Supported(runtime.Provider) {
		return Snapshot{}, fmt.Errorf("provider %s does not expose quota data", runtime.Provider)
	}
	fetchedAt := time.Now().UTC()
	snapshot := Snapshot{CredentialID: id, Provider: runtime.Provider, Account: maskAccount(runtime), FetchedAt: &fetchedAt, Available: true, Windows: []Window{}}
	var payload map[string]any
	switch runtime.Provider {
	case "codex":
		payload, err = s.fetch(ctx, runtime, http.MethodGet, "https://chatgpt.com/backend-api/wham/usage", nil, map[string]string{"chatgpt-account-id": runtime.OAuthAccount, "originator": "codex_cli_rs"})
		if err == nil {
			snapshot.Plan = textAny(payload, "plan_type", "plan", "subscription_plan")
			snapshot.Windows = append(snapshot.Windows, parsePercentWindow("Session (5h)", object(object(payload, "rate_limit"), "primary_window")), parsePercentWindow("Weekly (7d)", object(object(payload, "rate_limit"), "secondary_window")))
		}
	case "claude":
		payload, err = s.fetch(ctx, runtime, http.MethodGet, "https://api.anthropic.com/api/oauth/usage", nil, map[string]string{"anthropic-version": "2023-06-01", "anthropic-beta": "oauth-2025-04-20"})
		if err == nil {
			snapshot.Windows = append(snapshot.Windows, parseUtilizationWindow("Session (5h)", object(payload, "five_hour")), parseUtilizationWindow("Weekly (7d)", object(payload, "seven_day")))
		}
	case "kiro", "amazon-q":
		body := map[string]any{"origin": "AI_EDITOR", "resourceType": "AGENTIC_REQUEST"}
		if runtime.OAuthMeta.ProfileARN != "" {
			body["profileArn"] = runtime.OAuthMeta.ProfileARN
		}
		region := runtime.OAuthMeta.Region
		if region == "" {
			region = "us-east-1"
		}
		headers := map[string]string{"x-amz-target": "AmazonCodeWhispererService.GetUsageLimits", "Content-Type": "application/x-amz-json-1.0"}
		if runtime.OAuthMeta.AuthMethod == "api_key" {
			headers["TokenType"] = "API_KEY"
		} else if runtime.OAuthMeta.AuthMethod == "external_idp" || runtime.OAuthMeta.AuthMethod == "enterprise" {
			headers["TokenType"] = "EXTERNAL_IDP"
		}
		payload, err = s.fetch(ctx, runtime, http.MethodPost, "https://codewhisperer."+region+".amazonaws.com", body, headers)
		if err == nil {
			snapshot.Plan = text(object(payload, "subscriptionInfo"), "subscriptionTitle")
			reset := parseTime(payload["nextDateReset"])
			for _, item := range array(payload, "usageBreakdownList") {
				entry, _ := item.(map[string]any)
				used, total := number(entry["currentUsageWithPrecision"]), number(entry["usageLimitWithPrecision"])
				snapshot.Windows = append(snapshot.Windows, amountWindow(resourceLabel(text(entry, "resourceType")), used, total, reset))
			}
		}
	case "opencode-go", "opencode-zen":
		base := strings.TrimRight(runtime.BaseURL, "/")
		payload, err = s.fetch(ctx, runtime, http.MethodGet, base+"/quota", nil, nil)
		if err == nil {
			quota := objectAny(payload, "quota", "data", "usage")
			for _, spec := range []struct {
				name string
				keys []string
			}{{"Session (5h)", []string{"window_5h", "5h", "short"}}, {"Weekly", []string{"window_weekly", "weekly", "week"}}, {"Monthly", []string{"window_monthly", "monthly", "month"}}} {
				window := objectAny(quota, spec.keys...)
				if len(window) > 0 {
					snapshot.Windows = append(snapshot.Windows, amountWindow(spec.name, number(first(window, "used", "used_amount")), number(first(window, "limit", "limit_amount")), parseTime(first(window, "reset_at", "resetAt"))))
				}
			}
		}
	}
	snapshot.Windows = compact(snapshot.Windows)
	usableRefresh := err == nil && len(snapshot.Windows) > 0
	if err != nil {
		message := err.Error()
		if previous, ok := s.Cached(id); ok {
			snapshot = previous
			snapshot.Message = message
		} else {
			snapshot.Available = true
			snapshot.Message = message
		}
	} else if len(snapshot.Windows) == 0 {
		snapshot.Message = "Provider returned no quota windows"
	}
	for _, window := range snapshot.Windows {
		if window.RemainingPercent <= 0 && (window.ResetAt == nil || window.ResetAt.After(time.Now())) {
			snapshot.Available = false
		}
	}
	s.mu.Lock()
	snapshot.InUse = s.active[snapshot.Provider] == id
	s.snapshots[id] = snapshot
	if usableRefresh && snapshot.Available {
		delete(s.exhausted, id)
	}
	s.mu.Unlock()
	if s.store != nil {
		if saveErr := s.store.Save(ctx, snapshot); saveErr != nil {
			return Snapshot{}, fmt.Errorf("save quota snapshot: %w", saveErr)
		}
	}
	return snapshot, nil
}

func (s *Service) fetch(ctx context.Context, runtime *entities.CredentialRuntime, method, url string, body any, extra map[string]string) (map[string]any, error) {
	var requestBody *strings.Reader
	if body == nil {
		requestBody = strings.NewReader("")
	} else {
		encoded, _ := json.Marshal(body)
		requestBody = strings.NewReader(string(encoded))
	}
	request, err := http.NewRequestWithContext(ctx, method, url, requestBody)
	if err != nil {
		return nil, err
	}
	token := runtime.OAuthAccess
	if token == "" {
		token = runtime.APIKey
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	for key, value := range extra {
		if value != "" {
			request.Header.Set(key, value)
		}
	}
	response, err := s.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("quota endpoint returned HTTP %d", response.StatusCode)
	}
	var payload map[string]any
	if json.NewDecoder(response.Body).Decode(&payload) != nil {
		return nil, fmt.Errorf("quota endpoint returned invalid JSON")
	}
	return payload, nil
}

func parsePercentWindow(name string, value map[string]any) Window {
	used := number(first(value, "used_percent", "usedPercent"))
	reset := parseTime(first(value, "reset_at", "resetAt"))
	if reset == nil {
		if seconds := number(first(value, "reset_after_seconds", "resetAfterSeconds")); seconds > 0 {
			value := time.Now().UTC().Add(time.Duration(seconds * float64(time.Second)))
			reset = &value
		}
	}
	return percentWindow(name, used, reset)
}
func parseUtilizationWindow(name string, value map[string]any) Window {
	used := number(value["utilization"])
	// Anthropic reports utilization as a 0..1 ratio.
	if used >= 0 && used <= 1 {
		used *= 100
	}
	return percentWindow(name, used, parseTime(first(value, "resets_at", "resetsAt")))
}
func percentWindow(name string, used float64, reset *time.Time) Window {
	used = math.Max(0, math.Min(100, used))
	return Window{Name: name, UsedPercent: used, RemainingPercent: 100 - used, ResetAt: reset}
}
func amountWindow(name string, used, total float64, reset *time.Time) Window {
	if total <= 0 {
		return Window{}
	}
	return percentWindow(name, used/total*100, reset)
}
func compact(values []Window) []Window {
	out := values[:0]
	for _, value := range values {
		if value.Name != "" {
			out = append(out, value)
		}
	}
	return out
}

func resourceLabel(value string) string {
	parts := strings.Fields(strings.ReplaceAll(strings.ToLower(strings.TrimSpace(value)), "_", " "))
	for i := range parts {
		if len(parts[i]) > 0 {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	if len(parts) == 0 {
		return "Quota"
	}
	return strings.Join(parts, " ")
}

func object(value map[string]any, key string) map[string]any {
	result, _ := value[key].(map[string]any)
	return result
}
func objectAny(value map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		if result := object(value, key); len(result) > 0 {
			return result
		}
	}
	return map[string]any{}
}
func array(value map[string]any, key string) []any { result, _ := value[key].([]any); return result }
func first(value map[string]any, keys ...string) any {
	for _, key := range keys {
		if candidate, ok := value[key]; ok {
			return candidate
		}
	}
	return nil
}
func number(value any) float64 {
	switch item := value.(type) {
	case float64:
		return item
	case json.Number:
		result, _ := item.Float64()
		return result
	case string:
		result, _ := strconv.ParseFloat(item, 64)
		return result
	}
	return 0
}
func text(value map[string]any, key string) string {
	result, _ := value[key].(string)
	return strings.TrimSpace(result)
}
func textAny(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if result := text(value, key); result != "" {
			return result
		}
	}
	return ""
}
func parseTime(value any) *time.Time {
	switch item := value.(type) {
	case string:
		if parsed, err := time.Parse(time.RFC3339, item); err == nil {
			return &parsed
		}
	case float64:
		if item <= 0 {
			return nil
		}
		if item < 1e12 {
			item *= 1000
		}
		parsed := time.UnixMilli(int64(item)).UTC()
		return &parsed
	}
	return nil
}
func maskAccount(runtime *entities.CredentialRuntime) string {
	value := runtime.OAuthMeta.Email
	if value == "" {
		value = runtime.OAuthMeta.Login
	}
	if value == "" {
		value = runtime.OAuthAccount
	}
	if value == "" {
		value = runtime.OAuthMeta.AccountID
	}
	if value == "" {
		return "connected account"
	}
	if at := strings.Index(value, "@"); at > 1 {
		return value[:2] + strings.Repeat("*", min(8, at-2)) + value[at:]
	}
	if len(value) > 8 {
		return value[:4] + "…" + value[len(value)-3:]
	}
	return "••••"
}
