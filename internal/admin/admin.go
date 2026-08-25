package admin

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kimnt93/gorouter/internal/cache"
	"github.com/kimnt93/gorouter/internal/cost"
	"github.com/kimnt93/gorouter/internal/cryptoseal"
	"github.com/kimnt93/gorouter/internal/llm"
	"github.com/kimnt93/gorouter/internal/store"
)

type Server struct {
	DB        *store.DB
	Sealer    *cryptoseal.Sealer
	Cache     *cache.Cache
	MasterKey string
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/auth/verify", s.handleVerify)
	mux.Handle("GET /admin/tenants", s.guard(http.HandlerFunc(s.handleListTenants)))
	mux.Handle("POST /admin/tenants", s.guard(http.HandlerFunc(s.handleCreateTenant)))
	mux.Handle("GET /admin/credentials", s.guard(http.HandlerFunc(s.handleListCredentials)))
	mux.Handle("POST /admin/credentials", s.guard(http.HandlerFunc(s.handleCreateCredential)))
	mux.Handle("DELETE /admin/credentials/{id}", s.guard(http.HandlerFunc(s.handleDeleteCredential)))
	mux.Handle("POST /admin/credentials/{id}/test", s.guard(http.HandlerFunc(s.handleTestCredential)))
	mux.Handle("GET /admin/api-keys", s.guard(http.HandlerFunc(s.handleListKeys)))
	mux.Handle("POST /admin/api-keys", s.guard(http.HandlerFunc(s.handleCreateKey)))
	mux.Handle("PATCH /admin/api-keys/{id}", s.guard(http.HandlerFunc(s.handlePatchKey)))
	mux.Handle("DELETE /admin/api-keys/{id}", s.guard(http.HandlerFunc(s.handleDeleteKey)))
	mux.Handle("GET /admin/models", s.guard(http.HandlerFunc(s.handleListModels)))
	mux.Handle("PUT /admin/models/{name}", s.guard(http.HandlerFunc(s.handleUpsertModel)))
	mux.Handle("DELETE /admin/models/{name}", s.guard(http.HandlerFunc(s.handleDeleteModel)))
	mux.Handle("GET /admin/prices", s.guard(http.HandlerFunc(s.handleListPrices)))
	mux.Handle("PUT /admin/prices/{model}", s.guard(http.HandlerFunc(s.handleSetPrice)))
	mux.Handle("GET /admin/usage/summary", s.guard(http.HandlerFunc(s.handleUsageSummary)))
	mux.Handle("GET /admin/usage/recent", s.guard(http.HandlerFunc(s.handleRecentUsage)))
	mux.Handle("GET /admin/cache/stats", s.guard(http.HandlerFunc(s.handleCacheStats)))
	mux.Handle("POST /admin/cache/flush", s.guard(http.HandlerFunc(s.handleCacheFlush)))
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

func decode(r *http.Request, v any) error {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}

func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		token := ""
		if strings.HasPrefix(h, "Bearer ") {
			token = strings.TrimSpace(h[len("Bearer "):])
		}
		if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.MasterKey)) != 1 {
			writeErr(w, 401, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleVerify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key string `json:"key"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	if subtle.ConstantTimeCompare([]byte(req.Key), []byte(s.MasterKey)) != 1 {
		writeErr(w, 401, "invalid master key")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleListTenants(w http.ResponseWriter, r *http.Request) {
	ts, err := s.DB.ListTenants(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, ts)
}

func (s *Server) handleCreateTenant(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decode(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeErr(w, 400, "name is required")
		return
	}
	t, err := s.DB.CreateTenant(r.Context(), strings.TrimSpace(req.Name))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, t)
}

func (s *Server) handleListCredentials(w http.ResponseWriter, r *http.Request) {
	cs, err := s.DB.ListCredentials(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if cs == nil {
		cs = []store.Credential{}
	}
	writeJSON(w, 200, cs)
}

type credReq struct {
	Name         string  `json:"name"`
	Provider     string  `json:"provider"`
	Kind         string  `json:"kind"`
	BaseURL      string  `json:"base_url"`
	APIKey       string  `json:"api_key"`
	OAuthAccess  string  `json:"oauth_access"`
	OAuthRefresh string  `json:"oauth_refresh"`
	OwnerTenant  *string `json:"owner_tenant_id"`
}

func (s *Server) handleCreateCredential(w http.ResponseWriter, r *http.Request) {
	var req credReq
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	req.Provider = strings.TrimSpace(req.Provider)
	req.Kind = strings.TrimSpace(req.Kind)
	if req.Name == "" || req.Provider == "" {
		writeErr(w, 400, "name and provider are required")
		return
	}
	if req.Kind == "" {
		if req.APIKey != "" {
			req.Kind = llm.KindAPIKey
		} else if req.OAuthRefresh != "" {
			req.Kind = llm.KindOAuth
		}
	}
	if req.Kind != llm.KindAPIKey && req.Kind != llm.KindOAuth {
		writeErr(w, 400, "kind must be api_key or oauth")
		return
	}
	if req.Provider != llm.ProviderOpenAICompatible && req.Provider != llm.ProviderAnthropic {
		writeErr(w, 400, "provider must be openai-compatible or anthropic")
		return
	}
	if req.Kind == llm.KindAPIKey && req.APIKey == "" {
		writeErr(w, 400, "api_key is required for kind api_key")
		return
	}
	if req.Kind == llm.KindOAuth && req.OAuthRefresh == "" {
		writeErr(w, 400, "oauth_refresh is required for kind oauth")
		return
	}
	c, err := s.DB.CreateCredential(r.Context(), s.Sealer, store.CredentialInput{
		Name: req.Name, Provider: req.Provider, Kind: req.Kind, BaseURL: req.BaseURL,
		APIKey: req.APIKey, OAuthAccess: req.OAuthAccess, OAuthRefresh: req.OAuthRefresh,
		OwnerTenant: req.OwnerTenant,
	})
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, c)
}

func (s *Server) handleDeleteCredential(w http.ResponseWriter, r *http.Request) {
	if err := s.DB.DeleteCredential(r.Context(), r.PathValue("id")); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, 404, "not found")
			return
		}
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleTestCredential(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rt, err := s.DB.GetCredentialRuntime(r.Context(), s.Sealer, id)
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	client := llm.NewHTTPClient()
	start := time.Now()
	var status int
	var snippet string
	switch rt.Provider {
	case llm.ProviderAnthropic:
		base := strings.TrimSuffix(rt.BaseURL, "/")
		if base == "" {
			base = "https://api.anthropic.com"
		}
		body := []byte(`{"model":"claude-3-5-haiku-latest","max_tokens":8,"messages":[{"role":"user","content":"ping"}]}`)
		headers := map[string]string{"anthropic-version": "2023-06-01"}
		if rt.Kind == llm.KindOAuth {
			headers["Authorization"] = "Bearer " + rt.OAuthAccess
			headers["anthropic-beta"] = "oauth-2025-04-20"
		} else {
			headers["x-api-key"] = rt.APIKey
		}
		res, terr := postJSON(client, base+"/v1/messages", headers, body)
		if terr != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "error": terr.Error()})
			return
		}
		defer res.Body.Close()
		status = res.StatusCode
		b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		snippet = string(b)
	default:
		base := strings.TrimSuffix(rt.BaseURL, "/")
		if base == "" {
			base = "https://api.openai.com/v1"
		}
		url := base
		if !strings.HasSuffix(base, "/v1") && !strings.Contains(base[len(base)-4:], "/v") {
			url = base + "/v1"
		}
		url += "/models"
		res, terr := getJSON(client, url, "Bearer "+rt.APIKey)
		if terr != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "error": terr.Error()})
			return
		}
		defer res.Body.Close()
		status = res.StatusCode
		b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		snippet = string(b)
	}
	ok := status >= 200 && status < 300
	writeJSON(w, 200, map[string]any{"ok": ok, "status": status, "latency_ms": time.Since(start).Milliseconds(), "response": truncate(snippet, 400)})
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func (s *Server) handleListKeys(w http.ResponseWriter, r *http.Request) {
	ks, err := s.DB.ListApiKeys(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if ks == nil {
		ks = []store.ApiKey{}
	}
	writeJSON(w, 200, ks)
}

func (s *Server) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TenantID        string   `json:"tenant_id"`
		Name            string   `json:"name"`
		Models          []string `json:"models"`
		MonthlyQuotaUSD *float64 `json:"monthly_quota_usd"`
		RPM             *int     `json:"rpm"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	if req.TenantID == "" {
		req.TenantID = "tenant_default"
	}
	if req.Name == "" {
		req.Name = "unnamed-key"
	}
	if len(req.Models) == 0 {
		writeErr(w, 400, "models list must not be empty (keys are denied all models by default)")
		return
	}
	k, err := s.DB.CreateApiKey(r.Context(), req.TenantID, req.Name, req.Models, req.MonthlyQuotaUSD, req.RPM)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	out := map[string]any{
		"id":                k.ID,
		"tenant_id":         k.TenantID,
		"name":              k.Name,
		"key_prefix":        k.KeyPrefix,
		"models":            k.Models,
		"monthly_quota_usd": k.MonthlyQuotaUSD,
		"rpm":               k.RPM,
		"enabled":           k.Enabled,
		"plaintext":         k.Plaintext,
	}
	writeJSON(w, 201, out)
}

func (s *Server) handlePatchKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled         *bool     `json:"enabled"`
		Models          *[]string `json:"models"`
		MonthlyQuotaUSD **float64 `json:"monthly_quota_usd"`
		RPM             **int     `json:"rpm"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	if err := s.DB.PatchApiKey(r.Context(), r.PathValue("id"), req.Enabled, req.Models, req.MonthlyQuotaUSD, req.RPM); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, 404, "not found")
			return
		}
		writeErr(w, 500, err.Error())
		return
	}
	k, _ := s.DB.GetApiKey(r.Context(), r.PathValue("id"))
	writeJSON(w, 200, k)
}

func (s *Server) handleDeleteKey(w http.ResponseWriter, r *http.Request) {
	if err := s.DB.DeleteApiKey(r.Context(), r.PathValue("id")); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, 404, "not found")
			return
		}
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	ms, err := s.DB.ListModels(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if ms == nil {
		ms = []store.ModelDef{}
	}
	writeJSON(w, 200, ms)
}

func (s *Server) handleUpsertModel(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var req struct {
		Strategy      string `json:"strategy"`
		UpstreamModel string `json:"upstream_model"`
		Enabled       *bool  `json:"enabled"`
		Routes        []struct {
			CredentialID string `json:"credential_id"`
			Priority     int    `json:"priority"`
			Weight       int    `json:"weight"`
			Enabled      bool   `json:"enabled"`
		} `json:"routes"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	if req.Strategy != "" && req.Strategy != "priority" && req.Strategy != "round_robin" {
		writeErr(w, 400, "strategy must be priority or round_robin")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	m := store.ModelDef{Name: name, Strategy: req.Strategy, UpstreamModel: req.UpstreamModel, Enabled: enabled}
	for _, rt := range req.Routes {
		m.Routes = append(m.Routes, store.ModelRoute{
			CredentialID: rt.CredentialID, Priority: rt.Priority, Weight: rt.Weight, Enabled: rt.Enabled,
		})
	}
	if err := s.DB.UpsertModel(r.Context(), m); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleDeleteModel(w http.ResponseWriter, r *http.Request) {
	if err := s.DB.DeleteModel(r.Context(), r.PathValue("name")); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, 404, "not found")
			return
		}
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) handleListPrices(w http.ResponseWriter, r *http.Request) {
	p, err := s.DB.ListPrices(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, p)
}

func (s *Server) handleSetPrice(w http.ResponseWriter, r *http.Request) {
	var p cost.Prices
	if err := decode(r, &p); err != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	if err := s.DB.SetPrice(r.Context(), r.PathValue("model"), p); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func sinceFromQuery(r *http.Request) time.Time {
	switch r.URL.Query().Get("range") {
	case "today":
		t := time.Now().Truncate(24 * time.Hour)
		return t
	case "7d":
		return time.Now().Add(-7 * 24 * time.Hour)
	case "30d":
		return time.Now().Add(-30 * 24 * time.Hour)
	default:
		return time.Now().Add(-24 * time.Hour)
	}
}

func (s *Server) handleUsageSummary(w http.ResponseWriter, r *http.Request) {
	since := sinceFromQuery(r)
	sum, err := s.DB.UsageSummary(r.Context(), since)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, sum)
}

func (s *Server) handleRecentUsage(w http.ResponseWriter, r *http.Request) {
	limit := 100
	fmt.Sscanf(r.URL.Query().Get("limit"), "%d", &limit)
	evs, err := s.DB.RecentUsage(r.Context(), limit)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if evs == nil {
		evs = []store.RecentEvent{}
	}
	writeJSON(w, 200, evs)
}

func (s *Server) handleCacheStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.Cache.Stats())
}

func (s *Server) handleCacheFlush(w http.ResponseWriter, r *http.Request) {
	s.Cache.Flush()
	writeJSON(w, 200, map[string]any{"ok": true})
}
