package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kimnt93/gorouter/internal/cache"
	"github.com/kimnt93/gorouter/internal/cost"
	"github.com/kimnt93/gorouter/internal/cryptoseal"
	"github.com/kimnt93/gorouter/internal/llm"
	"github.com/kimnt93/gorouter/internal/routing"
	"github.com/kimnt93/gorouter/internal/store"
)

type Config struct {
	Cache        cache.Config
	RequestLimit int64
}

type Server struct {
	DB       *store.DB
	Sealer   *cryptoseal.Sealer
	Cache    *cache.Cache
	Usage    *store.UsageWriter
	Selector *routing.Selector
	Health   *routing.Health
	OpenAI   llm.Adapter
	Anthro   llm.Adapter
	cfg      Config

	mu      sync.Mutex
	pending map[string]float64
	rpmWin  map[string]*rpmWindow
}

type rpmWindow struct {
	minute int64
	count  int
}

func NewServer(db *store.DB, sealer *cryptoseal.Sealer, c *cache.Cache, uw *store.UsageWriter, openAI llm.Adapter, anthro llm.Adapter, cfg Config) *Server {
	return &Server{
		DB: db, Sealer: sealer, Cache: c, Usage: uw,
		Selector: &routing.Selector{}, Health: routing.NewHealth(),
		OpenAI: openAI, Anthro: anthro, cfg: cfg,
		pending: map[string]float64{}, rpmWin: map[string]*rpmWindow{},
	}
}

func writeErr(w http.ResponseWriter, status int, msg, typ, code string) {
	resp := map[string]any{
		"error": map[string]any{"message": msg, "type": typ, "code": code},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", s.handleChat)
	mux.HandleFunc("GET /v1/models", s.handleModels)
	return s.recoverMiddleware(mux)
}

func (s *Server) recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("gateway panic: %v", rec)
				writeErr(w, 500, "internal error", "server_error", "")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(h[len("Bearer "):])
}

var errRateLimited = errors.New("rate limited")
var errAuthFailed = errors.New("auth failed")

func (s *Server) authKey(r *http.Request) (*store.ApiKey, error) {
	token := bearerToken(r)
	if token == "" {
		return nil, errAuthFailed
	}
	k, err := s.DB.GetApiKeyByHash(r.Context(), token)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, errAuthFailed
		}
		return nil, fmt.Errorf("key lookup: %w", err)
	}
	if !k.Enabled {
		return nil, errAuthFailed
	}
	if k.RPM != nil && *k.RPM > 0 && !s.allowRPM(k.ID, *k.RPM) {
		return nil, errRateLimited
	}
	return k, nil
}

func (s *Server) allowRPM(keyID string, rpm int) bool {
	now := time.Now().Unix() / 60
	s.mu.Lock()
	defer s.mu.Unlock()
	win := s.rpmWin[keyID]
	if win == nil || win.minute != now {
		win = &rpmWindow{minute: now}
		s.rpmWin[keyID] = win
		if len(s.rpmWin) > 100_000 {
			for k, v := range s.rpmWin {
				if v.minute < now-1 {
					delete(s.rpmWin, k)
				}
			}
		}
	}
	if win.count >= rpm {
		return false
	}
	win.count++
	return true
}

func (s *Server) addPending(keyID string, v float64) {
	s.mu.Lock()
	s.pending[keyID] += v
	s.mu.Unlock()
}

type chatContext struct {
	Key     *store.ApiKey
	Model   *store.ModelDef
	RawBody []byte
	Parsed  *llm.ChatRequest
	Start   time.Time
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	key, err := s.authKey(r)
	if err != nil {
		if errors.Is(err, errRateLimited) {
			writeErr(w, 429, "rate limit exceeded", "rate_limit_error", "rate_limit_exceeded")
		} else if errors.Is(err, errAuthFailed) {
			writeErr(w, 401, "invalid API key", "authentication_error", "invalid_api_key")
		} else {
			writeErr(w, 500, "lookup failed", "server_error", "")
		}
		return
	}
	models, err := s.DB.ListModels(r.Context())
	if err != nil {
		writeErr(w, 500, "failed to load models", "server_error", "")
		return
	}
	out := llm.ModelList{Object: "list"}
	for _, m := range models {
		if !m.Enabled {
			continue
		}
		for _, allowed := range key.Models {
			if allowed == m.Name {
				out.Data = append(out.Data, llm.ModelInfo{ID: m.Name, Object: "model", OwnedBy: "gorouter"})
				break
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	key, err := s.authKey(r)
	if err != nil {
		switch {
		case errors.Is(err, errRateLimited):
			writeErr(w, 429, "rate limit exceeded (requests per minute)", "rate_limit_error", "rate_limit_exceeded")
		case errors.Is(err, errAuthFailed):
			writeErr(w, 401, "invalid API key", "authentication_error", "invalid_api_key")
		default:
			writeErr(w, 500, "key lookup failed", "server_error", "")
		}
		return
	}
	if s.cfg.RequestLimit > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, s.cfg.RequestLimit)
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, 413, "request body too large", "invalid_request_error", "")
		return
	}
	var req llm.ChatRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		writeErr(w, 400, "invalid JSON body: "+err.Error(), "invalid_request_error", "")
		return
	}
	if req.Model == "" || len(req.Messages) == 0 {
		writeErr(w, 400, "model and messages are required", "invalid_request_error", "")
		return
	}
	modelDef, status, emsg := s.resolveModel(key, req.Model)
	if modelDef == nil {
		typ := "invalid_request_error"
		code := "model_not_found"
		if status == 403 {
			typ = "permission_error"
			code = "model_not_allowed"
		}
		writeErr(w, status, emsg, typ, code)
		return
	}
	cc := &chatContext{Key: key, Model: modelDef, RawBody: raw, Parsed: &req, Start: start}

	prices, _ := s.DB.ListPrices(r.Context())
	price := prices[modelDef.Name]

	if cached, ok := s.Cache.Lookup(key.ID, key.TenantID, modelDef.Name, raw); ok {
		s.serveCached(w, cc, cached)
		return
	}

	if quotaMsg := s.checkQuota(r.Context(), key, &price, &req); quotaMsg != "" {
		writeErr(w, 429, quotaMsg, "insufficient_quota", "quota_exceeded")
		return
	}

	candidates, err := s.candidatesFor(r.Context(), key, modelDef)
	if err != nil {
		writeErr(w, 500, "failed to resolve credentials", "server_error", "")
		return
	}
	if len(candidates) == 0 {
		writeErr(w, 503, "no healthy credential available for model "+modelDef.Name, "service_unavailable", "no_credentials")
		return
	}

	upstreamModel := modelDef.UpstreamModel
	if upstreamModel == "" {
		upstreamModel = modelDef.Name
	}

	var lastErrMsg string
	lastStatus := 0
	for _, cand := range candidates {
		res, rt, aerr := s.tryCandidate(r.Context(), cand, upstreamModel, raw, &req)
		if aerr != nil {
			s.Health.Report(cand.ID, false)
			lastErrMsg = aerr.Error()
			continue
		}
		if res.StatusCode >= 400 && !retryable(res.StatusCode) {
			s.relayUpstreamError(w, cc, rt.ID, upstreamModel, res)
			return
		}
		if res.StatusCode >= 400 {
			io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
			res.Body.Close()
			s.Health.Report(cand.ID, false)
			lastStatus = res.StatusCode
			lastErrMsg = fmt.Sprintf("credential %s returned HTTP %d", rt.ID, res.StatusCode)
			continue
		}
		s.Health.Report(cand.ID, true)
		if req.Stream {
			s.streamUpstream(w, cc, rt, upstreamModel, res)
		} else {
			s.nonStreamUpstream(w, cc, rt, upstreamModel, res)
		}
		return
	}
	code := 502
	if lastStatus == http.StatusTooManyRequests {
		code = 429
	}
	if lastErrMsg == "" {
		lastErrMsg = "all credentials failed"
	}
	writeErr(w, code, lastErrMsg, "upstream_error", "")
}

func (s *Server) relayUpstreamError(w http.ResponseWriter, cc *chatContext, credID, upstreamModel string, res *llm.Result) {
	defer res.Body.Close()
	bodyBytes, _ := io.ReadAll(io.LimitReader(res.Body, 256<<10))
	ev := s.event(cc, credID, upstreamModel, llm.Usage{}, time.Since(cc.Start).Milliseconds(), res.StatusCode, fmt.Sprintf("upstream HTTP %d", res.StatusCode))
	s.Usage.Submit(ev)
	w.Header().Set("X-Cache", "bypass")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(res.StatusCode)
	_, _ = w.Write(bodyBytes)
}

func (s *Server) tryCandidate(ctx context.Context, cand routing.Candidate, upstreamModel string, raw []byte, req *llm.ChatRequest) (*llm.Result, *llm.CredentialRuntime, error) {
	rt, err := s.DB.GetCredentialRuntime(ctx, s.Sealer, cand.ID)
	if err != nil {
		return nil, &llm.CredentialRuntime{ID: cand.ID}, err
	}
	var adapter llm.Adapter
	if rt.Provider == llm.ProviderAnthropic {
		adapter = s.Anthro
	} else {
		adapter = s.OpenAI
	}
	res, err := adapter.Send(ctx, rt, upstreamModel, raw, req)
	if err != nil {
		return nil, rt, err
	}
	return res, rt, nil
}

func retryable(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

func (s *Server) resolveModel(key *store.ApiKey, name string) (*store.ModelDef, int, string) {
	models, err := s.DB.ListModels(context.Background())
	if err != nil {
		return nil, 500, "failed to load models"
	}
	var found *store.ModelDef
	for i := range models {
		if models[i].Name == name {
			found = &models[i]
			break
		}
	}
	if found == nil || !found.Enabled {
		return nil, 404, "unknown model: " + name
	}
	for _, m := range key.Models {
		if m == name {
			return found, 0, ""
		}
	}
	return nil, 403, "api key is not authorized for model " + name
}

func (s *Server) candidatesFor(ctx context.Context, key *store.ApiKey, m *store.ModelDef) ([]routing.Candidate, error) {
	rows, err := s.DB.RoutesForModel(ctx, m.Name)
	if err != nil {
		return nil, err
	}
	var cands []routing.Candidate
	for _, rc := range rows {
		if rc.OwnerTenant != nil && *rc.OwnerTenant != key.TenantID {
			continue
		}
		if !s.Health.Available(rc.CredentialID) {
			continue
		}
		cands = append(cands, routing.Candidate{ID: rc.CredentialID, Priority: rc.Priority, Weight: rc.Weight, Enabled: true})
	}
	return s.Selector.Order(m.Strategy, cands), nil
}

func (s *Server) checkQuota(ctx context.Context, key *store.ApiKey, price *cost.Prices, req *llm.ChatRequest) string {
	if key.MonthlyQuotaUSD == nil {
		return ""
	}
	est := cost.Compute(price, cost.Usage{
		PromptTokens:     req.EstimatePromptTokens(),
		CompletionTokens: req.EstimateOutputTokens(),
	})
	spent, err := s.spentForKey(ctx, key)
	if err != nil {
		return ""
	}
	if spent+est > *key.MonthlyQuotaUSD {
		return fmt.Sprintf("monthly quota of $%.2f exceeded (spent $%.4f, estimated next $%.4f)", *key.MonthlyQuotaUSD, spent, est)
	}
	return ""
}

func (s *Server) spentForKey(ctx context.Context, k *store.ApiKey) (float64, error) {
	dbSpent, err := s.DB.MonthSpendForKey(ctx, k.ID)
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return dbSpent + s.pending[k.ID], nil
}

func (s *Server) event(cc *chatContext, credID, upstreamModel string, u llm.Usage, durationMS int64, status int, errMsg string) store.UsageEvent {
	prices, _ := s.DB.ListPrices(context.Background())
	var p *cost.Prices
	if pr, ok := prices[cc.Model.Name]; ok {
		p = &pr
	}
	c := cost.Compute(p, cost.Usage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		CacheReadTokens:  u.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens,
	})
	return store.UsageEvent{
		TS: time.Now(), TenantID: cc.Key.TenantID, ApiKeyID: cc.Key.ID, CredentialID: credID,
		Model: cc.Model.Name, UpstreamModel: upstreamModel,
		PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens,
		CacheReadTokens: u.CacheReadTokens, CacheWriteTokens: u.CacheWriteTokens,
		CostUSD: c, StatusCode: status, DurationMS: durationMS, Error: errMsg,
	}
}
