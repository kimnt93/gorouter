package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	responseapi "github.com/kimnt93/gorouter/internal/api"
	"github.com/kimnt93/gorouter/internal/api/presenter"
	"github.com/kimnt93/gorouter/internal/platform/llm"
	"github.com/kimnt93/gorouter/pkg/apikey"
	"github.com/kimnt93/gorouter/pkg/chat"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/modelroute"
	"github.com/kimnt93/gorouter/pkg/policy"
	providerpkg "github.com/kimnt93/gorouter/pkg/provider"
	"github.com/kimnt93/gorouter/pkg/quota"
	"github.com/kimnt93/gorouter/pkg/usage"
)

type Gateway struct {
	Keys           *apikey.Service
	Creds          *credential.Service
	Models         *modelroute.Service
	Usage          *usage.Service
	Cache          chat.PromptCache
	OpenAI         entities.Upstream
	Anthropic      entities.Upstream
	Codex          entities.Upstream
	Providers      map[string]entities.Upstream
	Selector       *chat.Selector
	Health         *chat.Health
	Quota          quota.Coordinator
	Pricing        PriceResolver
	ProviderQuotas ProviderQuotaRouter
}

type ProviderQuotaRouter interface {
	Available(credentialID string) bool
	MarkExhausted(credentialID string)
}

// GatewayAccessContext separates a stored API key (absent for master) from
// request policy, cache isolation, credential visibility, and actor snapshot.
type GatewayAccessContext struct {
	*entities.ApiKey
	StoredKey *entities.ApiKey
	Actor     entities.UsageActor
	Master    bool
}

type PriceResolver interface {
	Resolve(model, upstreamModel string) (entities.Price, bool)
	Estimates(model, upstreamModel string, promptTokens, completionTokens int64) entities.PriceEstimates
}

type PriceCatalog interface {
	PriceResolver
	Catalog(model, upstreamModel string) (entities.CatalogPrice, bool)
	CatalogPrices() []entities.CatalogPrice
}

// Chat proxies an OpenAI-compatible chat completion with principal policy and attribution.
// @Summary Create a chat completion
// @Tags gateway
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param request body llm.ChatRequest true "Chat request"
// @Success 200 {object} llm.Response
// @Failure 400,401,403,404,429,500,502,503 {object} presenter.Error
// @Router /v1/chat/completions [post]
func (g *Gateway) Chat(c fiber.Ctx) error {
	started := time.Now()
	sess := SessionFrom(c)
	if sess == nil || !sess.Has(entities.ScopeChat) {
		return presenter.Unauthorized(c, "chat access required")
	}
	raw := append([]byte(nil), c.Body()...)
	req, err := llm.ParseRequest(raw)
	if err != nil || req.Model == "" || len(req.Messages) == 0 {
		return presenter.BadRequest(c, "model and messages are required")
	}
	key, err := g.accessForSession(c, sess)
	if err != nil {
		return presenter.Unauthorized(c, "API key required")
	}
	if !key.Master && !contains(key.Models, req.Model) {
		return presenter.Forbidden(c, "model is not allowed for this API key")
	}
	models, err := g.Models.List(c.Context())
	if err != nil {
		return presenter.ServerError(c, "failed to load model")
	}
	var model *entities.ModelDef
	for i := range models {
		if models[i].Name == req.Model && models[i].Enabled {
			model = &models[i]
			break
		}
	}
	if model == nil {
		return presenter.NotFound(c, "unknown model")
	}
	if key.RPM != nil {
		if g.Quota == nil {
			return presenter.Err(c, fiber.StatusServiceUnavailable, "rate-limit coordination is unavailable", "service_unavailable", "redis_unavailable")
		}
		allowed, limitErr := g.Quota.AllowRPM(c.Context(), key.ID, *key.RPM, started)
		if errors.Is(limitErr, quota.ErrUnavailable) {
			return presenter.Err(c, fiber.StatusServiceUnavailable, "rate-limit coordination is unavailable", "service_unavailable", "redis_unavailable")
		}
		if limitErr != nil {
			return presenter.ServerError(c, "failed to enforce rate limit")
		}
		if !allowed {
			return presenter.Err(c, fiber.StatusTooManyRequests, "requests-per-minute limit exceeded", "rate_limit_error", "rate_limit_exceeded")
		}
	}
	deterministic := llm.IsDeterministic(req)
	cacheEnabled := g.cacheEnabled()
	if cacheEnabled && deterministic {
		if cached, ok := g.Cache.Lookup(key.ID, key.TenantID, model.Name, raw); ok {
			usage := llm.Usage{PromptTokens: cached.PromptTok, CompletionTokens: cached.Completion}
			g.recordConversation(key, model, "", usage, true, cached.Status, started, raw, cached.Body)
			c.Set("X-Cache", "hit")
			if req.Stream {
				return g.replayStream(c, cached)
			}
			c.Set("Content-Type", contentTypeOrJSON(cached.ContentType))
			return c.Send(cached.Body)
		}
	}
	price, priced, priceErr := g.resolvePrice(c.Context(), model)
	if priceErr != nil {
		return presenter.ServerError(c, "failed to load model price")
	}
	var pricePtr *entities.Price
	if priced {
		pricePtr = &price
	}
	var reservation *quota.Reservation
	streamOwnsReservation := false
	defer func() {
		if reservation != nil && !streamOwnsReservation && g.Quota != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = g.Quota.Release(ctx, reservation)
		}
	}()
	quotaLimit := key.QuotaUSD
	quotaPeriod := key.QuotaPeriod
	if quotaLimit != nil && quotaPeriod != entities.QuotaPeriodNone {
		if g.Quota == nil {
			return presenter.Err(c, fiber.StatusServiceUnavailable, "quota coordination is unavailable", "service_unavailable", "redis_unavailable")
		}
		if g.Usage == nil {
			return presenter.Err(c, fiber.StatusServiceUnavailable, "quota usage is unavailable", "service_unavailable", "usage_unavailable")
		}
		windowStart, _, _, windowErr := quota.Window(quotaPeriod, started)
		if windowErr != nil {
			return presenter.ServerError(c, "invalid API-key quota period")
		}
		estimate := entities.CalculateCost(pricePtr, entities.TokenUsage{PromptTokens: req.EstimatePromptTokens(), CompletionTokens: req.EstimateOutputTokens()})
		if g.Pricing != nil {
			estimate = g.Pricing.Estimates(model.Name, model.UpstreamModel, req.EstimatePromptTokens(), req.EstimateOutputTokens()).WithoutCache
		}
		spent, spendErr := g.Usage.SpendForKeySince(c.Context(), key.ID, windowStart)
		if spendErr != nil {
			return presenter.ServerError(c, "failed to load quota usage")
		}
		if periodQuota, ok := g.Quota.(quota.PeriodCoordinator); ok {
			reservation, err = periodQuota.ReserveForPeriod(c.Context(), key.ID, *quotaLimit, spent, estimate.USD, quotaPeriod, started)
		} else if quotaPeriod == entities.QuotaPeriodWeek || quotaPeriod == "" {
			reservation, err = g.Quota.Reserve(c.Context(), key.ID, *quotaLimit, spent, estimate.USD, started)
		} else {
			return presenter.Err(c, fiber.StatusServiceUnavailable, "quota coordination does not support this period", "service_unavailable", "quota_period_unavailable")
		}
		if errors.Is(err, quota.ErrExceeded) {
			return presenter.Err(c, fiber.StatusTooManyRequests, "quota exceeded", "insufficient_quota", "quota_exceeded")
		}
		if errors.Is(err, quota.ErrUnavailable) {
			return presenter.Err(c, fiber.StatusServiceUnavailable, "quota coordination is unavailable", "service_unavailable", "redis_unavailable")
		}
		if err != nil {
			return presenter.ServerError(c, "failed to reserve quota")
		}
	}
	routes, err := g.Creds.Routes(c.Context(), model.Name)
	if err != nil || len(routes) == 0 {
		return presenter.Err(c, fiber.StatusServiceUnavailable, "no credentials available", "service_unavailable", "no_credentials")
	}
	candidates := make([]chat.Candidate, 0, len(routes))
	for _, route := range routes {
		if !policy.CredentialVisible(key.Master, key.TenantID, route.OwnerTenant) {
			continue
		}
		quotaAvailable := g.ProviderQuotas == nil || g.ProviderQuotas.Available(route.CredentialID)
		if quotaAvailable && g.Health.Available(route.CredentialID) {
			candidates = append(candidates, chat.Candidate{ID: route.CredentialID, Priority: route.Priority, Weight: route.Weight})
		}
	}
	candidates = g.Selector.Order(model.Strategy, candidates)
	if len(candidates) == 0 {
		return presenter.Err(c, fiber.StatusServiceUnavailable, "no healthy credentials available", "service_unavailable", "no_credentials")
	}
	upstreamModel := model.UpstreamModel
	if upstreamModel == "" {
		upstreamModel = model.Name
	}
	lastStatus := fiber.StatusBadGateway
	lastCredential := ""
	for _, candidate := range candidates {
		lastCredential = candidate.ID
		runtime, rerr := g.Creds.Runtime(c.Context(), candidate.ID)
		if rerr != nil {
			g.Health.Report(candidate.ID, false)
			continue
		}
		adapter, ok := g.adapter(runtime.Provider)
		if !ok || adapter == nil {
			g.Health.Report(candidate.ID, false)
			continue
		}
		result, rerr := adapter.Send(c.Context(), runtime, upstreamModel, raw)
		if rerr != nil {
			g.Health.Report(candidate.ID, false)
			continue
		}
		if result.StatusCode == fiber.StatusTooManyRequests || result.StatusCode == fiber.StatusPaymentRequired {
			lastStatus = result.StatusCode
			drainAndClose(result.Body)
			if g.ProviderQuotas != nil {
				g.ProviderQuotas.MarkExhausted(candidate.ID)
			}
			continue
		}
		if (result.StatusCode < 200 || result.StatusCode >= 300) && !retryableStatus(result.StatusCode) {
			status := result.StatusCode
			drainAndClose(result.Body)
			g.recordError(key, model, runtime.ID, status, started, "upstream rejected request")
			c.Set("X-Cache", "bypass")
			return presenter.Err(c, status, "upstream rejected the request", "upstream_error", "upstream_rejected")
		}
		if result.StatusCode < 200 || result.StatusCode >= 300 {
			lastStatus = result.StatusCode
			drainAndClose(result.Body)
			g.Health.Report(candidate.ID, false)
			continue
		}
		g.Health.Report(candidate.ID, true)
		if req.Stream {
			streamOwnsReservation = true
			return g.stream(c, key, model, runtime, result, raw, deterministic, started, pricePtr, reservation)
		}
		return g.nonStream(c, key, model, runtime, result, raw, deterministic, started, pricePtr, reservation)
	}
	c.Set("X-Cache", "bypass")
	g.recordError(key, model, lastCredential, lastStatus, started, "all credentials failed")
	responseStatus := fiber.StatusBadGateway
	if lastStatus >= fiber.StatusBadRequest && lastStatus < fiber.StatusInternalServerError {
		responseStatus = lastStatus
	}
	return presenter.Err(c, responseStatus, "all credentials failed", "upstream_error", "upstream_unavailable")
}

func (g *Gateway) adapter(provider string) (entities.Upstream, bool) {
	if g.Providers != nil {
		if value := g.Providers[provider]; value != nil {
			return value, true
		}
	}
	switch providerpkg.ProtocolFor(provider) {
	case providerpkg.ProtocolOpenAI:
		return g.OpenAI, true
	case providerpkg.ProtocolAnthropic:
		return g.Anthropic, true
	case providerpkg.ProtocolCodex:
		return g.Codex, true
	default:
		return nil, false
	}
}

func retryableStatus(status int) bool {
	switch status {
	case fiber.StatusRequestTimeout, fiber.StatusTooManyRequests, fiber.StatusInternalServerError,
		fiber.StatusBadGateway, fiber.StatusServiceUnavailable, fiber.StatusGatewayTimeout, 529:
		return true
	default:
		return false
	}
}

func drainAndClose(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 1<<20))
	_ = body.Close()
}

// ListModels returns enabled models allowed by the access context.
// @Summary List available models
// @Tags gateway
// @Security BearerAuth
// @Success 200 {object} llm.ModelList
// @Failure 401,403,500 {object} presenter.Error
// @Router /v1/models [get]
func (g *Gateway) ListModels(c fiber.Ctx) error {
	sess := SessionFrom(c)
	key, err := g.accessForSession(c, sess)
	if err != nil {
		return presenter.Unauthorized(c, "API key required")
	}
	models, err := g.Models.List(c.Context())
	if err != nil {
		return presenter.ServerError(c, "failed to load models")
	}
	out := llm.ModelList{Object: "list", Data: []llm.ModelInfo{}}
	for _, model := range models {
		if model.Enabled && (key.Master || contains(key.Models, model.Name)) {
			var price *entities.Price
			if resolved, ok, resolveErr := g.resolvePrice(c.Context(), &model); resolveErr == nil && ok {
				price = &resolved
			}
			out.Data = append(out.Data, llm.ModelInfo{ID: model.Name, Object: "model", OwnedBy: "gorouter", UpstreamModel: model.UpstreamModel, Pricing: price})
		}
	}
	return responseapi.JSON(c, out)
}

func (g *Gateway) accessForSession(c fiber.Ctx, sess *entities.Session) (*GatewayAccessContext, error) {
	if sess == nil {
		return nil, fmt.Errorf("session required")
	}
	if sess.IsMaster() {
		return &GatewayAccessContext{ApiKey: &entities.ApiKey{ID: "master", Scopes: entities.AllScopes, Enabled: true}, Actor: entities.UsageActor{Type: entities.ActorMaster, Username: "master"}, Master: true}, nil
	}
	key, err := g.Keys.GetByID(c.Context(), sess.KeyID)
	if err != nil {
		return nil, err
	}
	actor := entities.UsageActor{UserID: sess.UserID, Username: sess.Username, OrganizationID: sess.OrganizationID}
	if sess.PrincipalType == entities.PrincipalUser {
		actor.Type = entities.ActorUser
	} else {
		actor.Type = entities.ActorOrganization
		if actor.Username == "" {
			actor.Username = "org:" + key.TenantName
		}
	}
	return &GatewayAccessContext{ApiKey: key, StoredKey: key, Actor: actor}, nil
}

func (g *Gateway) nonStream(c fiber.Ctx, key *GatewayAccessContext, model *entities.ModelDef, runtime *entities.CredentialRuntime, result *entities.UpstreamResult, raw []byte, deterministic bool, started time.Time, price *entities.Price, reservation *quota.Reservation) error {
	defer result.Body.Close()
	body, err := io.ReadAll(io.LimitReader(result.Body, 32<<20))
	if err != nil {
		g.recordError(key, model, runtime.ID, fiber.StatusBadGateway, started, "upstream read failed")
		return presenter.Err(c, 502, "upstream read failed", "upstream_error", "")
	}
	usage := llm.ExtractUsage(body)
	if providerpkg.UsesAnthropicWire(runtime.Provider) {
		resp, err := llm.FromAnthropic(body, model.Name)
		if err != nil {
			g.recordError(key, model, runtime.ID, fiber.StatusBadGateway, started, "response translation failed")
			return presenter.Err(c, 502, "response translation failed", "upstream_error", "")
		}
		body, _ = json.Marshal(resp)
		usage = resp.Usage
	}
	if usage.PromptTokens == 0 {
		usage.PromptTokens = llm.EstimatePromptTokensFromBody(raw)
	}
	if usage.CompletionTokens == 0 {
		usage.CompletionTokens = estimateResponseTokens(body)
	}
	cost := entities.CalculateCost(price, usage.TokenUsage())
	if err := g.settle(c.Context(), reservation, cost.USD); err != nil {
		g.recordCostConversation(key, model, runtime.ID, usage, false, fiber.StatusServiceUnavailable, started, cost, raw, body)
		return presenter.Err(c, fiber.StatusServiceUnavailable, "quota settlement is unavailable", "service_unavailable", "redis_unavailable")
	}
	g.recordCostConversation(key, model, runtime.ID, usage, false, result.StatusCode, started, cost, raw, body)
	cacheStatus := "off"
	if deterministic && g.cacheEnabled() {
		g.Cache.Store(key.ID, key.TenantID, model.Name, raw, &chat.CacheEntry{Status: 200, ContentType: "application/json", Body: body, PromptTok: usage.PromptTokens, Completion: usage.CompletionTokens})
		cacheStatus = "miss"
	}
	c.Set("X-Cache", cacheStatus)
	c.Set("X-Upstream-Credential", runtime.ID)
	c.Set("Content-Type", "application/json")
	return c.Send(body)
}

func (g *Gateway) stream(c fiber.Ctx, key *GatewayAccessContext, model *entities.ModelDef, runtime *entities.CredentialRuntime, result *entities.UpstreamResult, raw []byte, deterministic bool, started time.Time, price *entities.Price, reservation *quota.Reservation) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("X-Accel-Buffering", "no")
	c.Set("X-Upstream-Credential", runtime.ID)
	if deterministic && g.cacheEnabled() {
		c.Set("X-Cache", "miss")
	} else {
		c.Set("X-Cache", "off")
	}
	var usage llm.Usage
	var content strings.Builder
	finishReason := "stop"
	return c.SendStreamWriter(func(w *bufio.Writer) {
		defer result.Body.Close()
		streamStatus := fiber.StatusOK
		if providerpkg.UsesAnthropicWire(runtime.Provider) {
			conv := llm.NewAnthropicStreamConverter(model.Name)
			err := llm.ScanSSE(result.Body, func(ev llm.SSEEvent) error {
				chunks, _, err := conv.Feed(ev.Event, ev.Data)
				if err != nil {
					return err
				}
				for _, chunk := range chunks {
					_, _ = w.WriteString("data: " + string(chunk) + "\n\n")
					_ = w.Flush()
				}
				return nil
			})
			usage = conv.UsageCollected()
			content.WriteString(conv.ContentCollected())
			finishReason = conv.FinishReason()
			if err != nil {
				streamStatus = fiber.StatusBadGateway
			}
			_, _ = w.WriteString("data: [DONE]\n\n")
			_ = w.Flush()
		} else {
			done := false
			err := llm.ScanSSE(result.Body, func(ev llm.SSEEvent) error {
				payload := string(ev.Data)
				if payload == "[DONE]" {
					done = true
				} else {
					usage = llm.MergeUsage(payload, usage)
					content.WriteString(llm.ContentDelta(payload))
					if reason := llm.FinishReason(payload); reason != "" {
						finishReason = reason
					}
				}
				_, werr := w.WriteString("data: " + payload + "\n\n")
				if werr == nil {
					werr = w.Flush()
				}
				return werr
			})
			if err != nil {
				streamStatus = fiber.StatusBadGateway
			}
			if !done {
				_, _ = w.WriteString("data: [DONE]\n\n")
				_ = w.Flush()
			}
		}
		if usage.PromptTokens == 0 {
			usage.PromptTokens = llm.EstimatePromptTokensFromBody(raw)
		}
		if usage.CompletionTokens == 0 {
			usage.CompletionTokens = llm.EstimateTextTokens(content.String())
		}
		cost := entities.CalculateCost(price, usage.TokenUsage())
		if streamStatus == fiber.StatusOK {
			if err := g.settle(context.Background(), reservation, cost.USD); err != nil {
				streamStatus = fiber.StatusServiceUnavailable
			}
		} else if reservation != nil && g.Quota != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = g.Quota.Release(ctx, reservation)
			cancel()
		}
		conversationResponse, _ := json.Marshal(llm.Response{Model: model.Name, Choices: []llm.Choice{{Index: 0, Message: &llm.ResponseMessage{Role: "assistant", Content: content.String()}, FinishReason: finishReason}}, Usage: usage})
		g.recordCostConversation(key, model, runtime.ID, usage, false, streamStatus, started, cost, raw, conversationResponse)
		if streamStatus == fiber.StatusOK && deterministic && g.cacheEnabled() && content.Len() > 0 {
			full := llm.Response{
				ID: "chatcmpl-cache", Object: "chat.completion", Created: time.Now().Unix(), Model: model.Name,
				Choices: []llm.Choice{{Index: 0, Message: &llm.ResponseMessage{Role: "assistant", Content: content.String()}, FinishReason: finishReason}},
				Usage:   usage,
			}
			body, _ := json.Marshal(full)
			g.Cache.Store(key.ID, key.TenantID, model.Name, raw, &chat.CacheEntry{Status: 200, ContentType: "application/json", Body: body, Stream: true, PromptTok: usage.PromptTokens, Completion: usage.CompletionTokens})
		}
	})
}

func (g *Gateway) replayStream(c fiber.Ctx, e *chat.CacheEntry) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("X-Accel-Buffering", "no")
	return c.SendStreamWriter(func(w *bufio.Writer) {
		var resp llm.Response
		if json.Unmarshal(e.Body, &resp) != nil || len(resp.Choices) == 0 || resp.Choices[0].Message == nil {
			return
		}
		content := resp.Choices[0].Message.Content
		first, _ := json.Marshal(llm.Chunk{ID: resp.ID, Object: "chat.completion.chunk", Created: time.Now().Unix(), Model: resp.Model, Choices: []llm.ChunkChoice{{Index: 0, Delta: llm.Delta{Role: "assistant", Content: content}}}})
		last, _ := json.Marshal(llm.Chunk{ID: resp.ID, Object: "chat.completion.chunk", Created: time.Now().Unix(), Model: resp.Model, Choices: []llm.ChunkChoice{{Index: 0, Delta: llm.Delta{}, FinishReason: resp.Choices[0].FinishReason}}, Usage: &resp.Usage})
		_, _ = w.WriteString("data: " + string(first) + "\n\n")
		_, _ = w.WriteString("data: " + string(last) + "\n\n")
		_, _ = w.WriteString("data: [DONE]\n\n")
		_ = w.Flush()
	})
}

func (g *Gateway) record(key *GatewayAccessContext, model *entities.ModelDef, cred string, u llm.Usage, hit bool, status int, started time.Time) {
	g.recordConversation(key, model, cred, u, hit, status, started, nil, nil)
}

func (g *Gateway) recordConversation(key *GatewayAccessContext, model *entities.ModelDef, cred string, u llm.Usage, hit bool, status int, started time.Time, requestBody, responseBody []byte) {
	if g.Usage == nil {
		return
	}
	p, _, _ := g.resolvePrice(context.Background(), model)
	cost := entities.CalculateCost(&p, u.TokenUsage())
	if hit {
		cost = entities.Cost{USD: 0, Priced: true}
	}
	g.recordCostConversation(key, model, cred, u, hit, status, started, cost, requestBody, responseBody)
}

func (g *Gateway) resolvePrice(ctx context.Context, model *entities.ModelDef) (entities.Price, bool, error) {
	if g.Pricing != nil {
		price, ok := g.Pricing.Resolve(model.Name, model.UpstreamModel)
		return price, ok, nil
	}
	prices, err := g.Models.Prices(ctx)
	if err != nil {
		return entities.Price{}, false, err
	}
	price, ok := prices[model.Name]
	if !ok {
		return entities.Price{}, true, nil
	}
	return price, true, nil
}

func (g *Gateway) recordCost(key *GatewayAccessContext, model *entities.ModelDef, cred string, u llm.Usage, hit bool, status int, started time.Time, cost entities.Cost) {
	g.recordCostConversation(key, model, cred, u, hit, status, started, cost, nil, nil)
}

func (g *Gateway) recordCostConversation(key *GatewayAccessContext, model *entities.ModelDef, cred string, u llm.Usage, hit bool, status int, started time.Time, cost entities.Cost, requestBody, responseBody []byte) {
	g.recordCostErrorConversation(key, model, cred, u, hit, status, started, cost, "", requestBody, responseBody)
}

func (g *Gateway) recordError(key *GatewayAccessContext, model *entities.ModelDef, cred string, status int, started time.Time, summary string) {
	g.recordCostError(key, model, cred, llm.Usage{}, false, status, started, entities.Cost{Priced: true}, summary)
}

func (g *Gateway) recordCostError(key *GatewayAccessContext, model *entities.ModelDef, cred string, u llm.Usage, hit bool, status int, started time.Time, cost entities.Cost, summary string) {
	g.recordCostErrorConversation(key, model, cred, u, hit, status, started, cost, summary, nil, nil)
}

const maxStoredConversationBytes = 8 << 20

func storedConversationBody(body []byte) (string, bool) {
	if len(body) <= maxStoredConversationBytes {
		return string(body), false
	}
	return string(body[:maxStoredConversationBytes]), true
}

func (g *Gateway) recordCostErrorConversation(key *GatewayAccessContext, model *entities.ModelDef, cred string, u llm.Usage, hit bool, status int, started time.Time, cost entities.Cost, summary string, requestBody, responseBody []byte) {
	if g.Usage == nil {
		return
	}
	apiKeyID := ""
	if key.StoredKey != nil {
		apiKeyID = key.StoredKey.ID
	}
	requestText, requestTruncated := storedConversationBody(requestBody)
	responseText, responseTruncated := storedConversationBody(responseBody)
	g.Usage.Record(entities.UsageEvent{TS: time.Now(), TenantID: key.TenantID, ApiKeyID: apiKeyID, CredentialID: cred, Model: model.Name, UpstreamModel: model.UpstreamModel, PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens, CacheReadTokens: u.CacheReadTokens, CacheWriteTokens: u.CacheWriteTokens, CostUSD: cost.USD, Priced: cost.Priced, CacheHit: hit, StatusCode: status, DurationMS: time.Since(started).Milliseconds(), Error: summary, ActorType: key.Actor.Type, UserID: key.Actor.UserID, Username: key.Actor.Username, OrganizationID: key.Actor.OrganizationID, RequestBody: requestText, ResponseBody: responseText, ContentTruncated: requestTruncated || responseTruncated})
}

func (g *Gateway) settle(ctx context.Context, reservation *quota.Reservation, actualUSD float64) error {
	if reservation == nil || g.Quota == nil {
		return nil
	}
	settleCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return g.Quota.Settle(settleCtx, reservation, actualUSD)
}

func (g *Gateway) cacheEnabled() bool {
	if g.Cache == nil {
		return false
	}
	if status, ok := g.Cache.(interface{ Enabled() bool }); ok {
		return status.Enabled()
	}
	return true
}

func contentTypeOrJSON(contentType string) string {
	if contentType == "" {
		return "application/json"
	}
	return contentType
}

func estimateResponseTokens(body []byte) int64 {
	var resp llm.Response
	if json.Unmarshal(body, &resp) != nil || len(resp.Choices) == 0 || resp.Choices[0].Message == nil {
		return 0
	}
	return llm.EstimateTextTokens(resp.Choices[0].Message.Content)
}

func contains(xs []string, value string) bool {
	for _, x := range xs {
		if x == value {
			return true
		}
	}
	return false
}
