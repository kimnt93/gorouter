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
	MarkInUse(credentialID string)
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
// @Description Accepts an OpenAI-compatible chat request, applies authentication, model policy, quota, cache, routing, and usage accounting, then returns a provider response or stream.
// @Tags gateway
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param request body llm.ChatRequest true "Chat request"
// @Success 200 {object} llm.Response
// @Failure 400,401,403,404,429,500,502,503 {object} responseapi.ErrorResponse
// @Router /v1/chat/completions [post]
func (g *Gateway) Chat(c fiber.Ctx) error {
	started := time.Now()
	sess := SessionFrom(c)
	if sess == nil || !sess.Has(entities.ScopeChat) {
		return responseapi.For(c).Unauthorized("chat access required").Send()
	}
	raw := append([]byte(nil), c.Body()...)
	req, err := llm.ParseRequest(raw)
	if err != nil || req.Model == "" || len(req.Messages) == 0 {
		return responseapi.For(c).BadRequest("model and messages are required").Send()
	}
	key, err := g.accessForSession(c, sess)
	if err != nil {
		return responseapi.For(c).Unauthorized("API key required").Send()
	}
	if !key.Master && !contains(key.Models, req.Model) {
		return responseapi.For(c).Forbidden("model is not allowed for this API key").Send()
	}
	models, err := g.Models.List(c.Context())
	if err != nil {
		return responseapi.For(c).InternalError("failed to load model").Send()
	}
	var model *entities.ModelDef
	for i := range models {
		if models[i].Name == req.Model && models[i].Enabled {
			model = &models[i]
			break
		}
	}
	if model == nil {
		return responseapi.For(c).NotFound("unknown model").Send()
	}
	if key.RPM != nil {
		if g.Quota == nil {
			return responseapi.For(c).Error(fiber.StatusServiceUnavailable, "rate-limit coordination is unavailable", "service_unavailable", "redis_unavailable").Send()
		}
		allowed, limitErr := g.Quota.AllowRPM(c.Context(), key.ID, *key.RPM, started)
		if errors.Is(limitErr, quota.ErrUnavailable) {
			return responseapi.For(c).Error(fiber.StatusServiceUnavailable, "rate-limit coordination is unavailable", "service_unavailable", "redis_unavailable").Send()
		}
		if limitErr != nil {
			return responseapi.For(c).InternalError("failed to enforce rate limit").Send()
		}
		if !allowed {
			return responseapi.For(c).Error(fiber.StatusTooManyRequests, "requests-per-minute limit exceeded", "rate_limit_error", "rate_limit_exceeded").Send()
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
		return responseapi.For(c).InternalError("failed to load model price").Send()
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
			return responseapi.For(c).Error(fiber.StatusServiceUnavailable, "quota coordination is unavailable", "service_unavailable", "redis_unavailable").Send()
		}
		if g.Usage == nil {
			return responseapi.For(c).Error(fiber.StatusServiceUnavailable, "quota usage is unavailable", "service_unavailable", "usage_unavailable").Send()
		}
		windowStart, _, _, windowErr := quota.Window(quotaPeriod, started)
		if windowErr != nil {
			return responseapi.For(c).InternalError("invalid API-key quota period").Send()
		}
		estimate := entities.CalculateCost(pricePtr, entities.TokenUsage{PromptTokens: req.EstimatePromptTokens(), CompletionTokens: req.EstimateOutputTokens()})
		if g.Pricing != nil {
			estimate = g.Pricing.Estimates(model.Name, model.UpstreamModel, req.EstimatePromptTokens(), req.EstimateOutputTokens()).WithoutCache
		}
		spent, spendErr := g.Usage.SpendForKeySince(c.Context(), key.ID, windowStart)
		if spendErr != nil {
			return responseapi.For(c).InternalError("failed to load quota usage").Send()
		}
		if periodQuota, ok := g.Quota.(quota.PeriodCoordinator); ok {
			reservation, err = periodQuota.ReserveForPeriod(c.Context(), key.ID, *quotaLimit, spent, estimate.USD, quotaPeriod, started)
		} else if quotaPeriod == entities.QuotaPeriodWeek || quotaPeriod == "" {
			reservation, err = g.Quota.Reserve(c.Context(), key.ID, *quotaLimit, spent, estimate.USD, started)
		} else {
			return responseapi.For(c).Error(fiber.StatusServiceUnavailable, "quota coordination does not support this period", "service_unavailable", "quota_period_unavailable").Send()
		}
		if errors.Is(err, quota.ErrExceeded) {
			return responseapi.For(c).Error(fiber.StatusTooManyRequests, "quota exceeded", "insufficient_quota", "quota_exceeded").Send()
		}
		if errors.Is(err, quota.ErrUnavailable) {
			return responseapi.For(c).Error(fiber.StatusServiceUnavailable, "quota coordination is unavailable", "service_unavailable", "redis_unavailable").Send()
		}
		if err != nil {
			return responseapi.For(c).InternalError("failed to reserve quota").Send()
		}
	}
	routes, err := g.Creds.Routes(c.Context(), model.Name)
	if err != nil || len(routes) == 0 {
		g.recordError(key, model, "", fiber.StatusServiceUnavailable, started, "no credentials available")
		return responseapi.For(c).Error(fiber.StatusServiceUnavailable, "no credentials available", "service_unavailable", "no_credentials").Send()
	}
	candidates := make([]chat.Candidate, 0, len(routes))
	runtimes := make(map[string]*entities.CredentialRuntime, len(routes))
	fillFirstProvider := ""
	fillFirst := true
	for _, route := range routes {
		if route.OwnerUserID != "" && !key.Master && route.OwnerUserID != key.Actor.UserID {
			continue
		}
		if !policy.CredentialVisible(key.Master, key.TenantID, route.OwnerTenant) {
			continue
		}
		quotaAvailable := g.ProviderQuotas == nil || g.ProviderQuotas.Available(route.CredentialID)
		if !quotaAvailable || !g.Health.Available(route.CredentialID) {
			continue
		}
		runtime, runtimeErr := g.Creds.Runtime(c.Context(), route.CredentialID)
		if runtimeErr != nil {
			g.Health.Report(route.CredentialID, false)
			continue
		}
		runtimes[route.CredentialID] = runtime
		definition, knownProvider := providerpkg.Lookup(runtime.Provider)
		if !knownProvider || !definition.QuotaSupported {
			fillFirst = false
		} else if fillFirstProvider == "" {
			fillFirstProvider = runtime.Provider
		} else if fillFirstProvider != runtime.Provider {
			fillFirst = false
		}
		candidates = append(candidates, chat.Candidate{ID: route.CredentialID, Priority: route.Priority, Weight: route.Weight})
	}
	strategy := model.Strategy
	if fillFirst && fillFirstProvider != "" {
		strategy = chat.StrategyPriority
	}
	affinityValue := req.ExplicitRouteAffinity()
	if affinityValue == "" {
		for _, header := range []string{"X-Codex-Session-Id", "X-Session-Id", "X-OpenCode-Session", "Session-Id"} {
			if value := strings.TrimSpace(c.Get(header)); value != "" && len(value) <= 512 {
				affinityValue = value
				break
			}
		}
	}
	routeAffinity := chat.RouteAffinity{ScopeID: key.ID, TenantID: key.TenantID, Model: model.Name, Value: affinityValue}
	candidates = g.Selector.OrderWithAffinity(c.Context(), strategy, candidates, routeAffinity)
	if len(candidates) == 0 {
		g.recordError(key, model, "", fiber.StatusServiceUnavailable, started, "no healthy credentials available")
		return responseapi.For(c).Error(fiber.StatusServiceUnavailable, "no healthy credentials available", "service_unavailable", "no_credentials").Send()
	}
	upstreamModel := model.UpstreamModel
	if upstreamModel == "" {
		upstreamModel = model.Name
	}
	lastStatus := fiber.StatusBadGateway
	lastCredential := ""
	for _, candidate := range candidates {
		lastCredential = candidate.ID
		runtime := runtimes[candidate.ID]
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
			return responseapi.For(c).Error(status, "upstream rejected the request", "upstream_error", "upstream_rejected").Send()
		}
		if result.StatusCode < 200 || result.StatusCode >= 300 {
			lastStatus = result.StatusCode
			drainAndClose(result.Body)
			g.Health.Report(candidate.ID, false)
			continue
		}
		g.Health.Report(candidate.ID, true)
		if strategy == chat.StrategyRoundRobin {
			g.Selector.BindAffinity(c.Context(), routeAffinity, candidate.ID)
		}
		if g.ProviderQuotas != nil {
			g.ProviderQuotas.MarkInUse(candidate.ID)
		}
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
	return responseapi.For(c).Error(responseStatus, "all credentials failed", "upstream_error", "upstream_unavailable").Send()
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
// @Description Lists the public models available to the authenticated principal and API key.
// @Tags gateway
// @Security BearerAuth
// @Success 200 {object} llm.ModelList
// @Failure 401,403,500 {object} responseapi.ErrorResponse
// @Router /v1/models [get]
func (g *Gateway) ListModels(c fiber.Ctx) error {
	sess := SessionFrom(c)
	key, err := g.accessForSession(c, sess)
	if err != nil {
		return responseapi.For(c).Unauthorized("API key required").Send()
	}
	models, err := g.Models.List(c.Context())
	if err != nil {
		return responseapi.For(c).InternalError("failed to load models").Send()
	}
	out := llm.ModelList{Object: "list", Data: []llm.ModelInfo{}}
	for _, model := range models {
		if model.Enabled && (key.Master || contains(key.Models, model.Name)) {
			var price *entities.Price
			if resolved, ok, resolveErr := g.resolvePrice(c.Context(), &model); resolveErr == nil && ok {
				price = &resolved
			}
			out.Data = append(out.Data, llm.ModelInfo{ID: model.Name, Object: "model", OwnedBy: "gorouter", UpstreamModel: model.UpstreamModel, Pricing: price})
			out.Models = append(out.Models, codexModelInfo(model))
		}
	}
	return responseapi.For(c).Response().Status(fiber.StatusOK).Data(out).Send()
}

// agentHarnessInstructions is exposed through the Codex-compatible model
// catalog as the client's base instruction template. Keep it independent of a
// specific client, provider, or task type because GoRouter models are also used
// by general-purpose agent harnesses.
const agentHarnessInstructions = "Follow the user's instructions and use the available tools to complete the task."

func codexModelInfo(model entities.ModelDef) llm.CodexModelInfo {
	displayName := model.Name
	description := "Model routed through GoRouter"
	contextWindow := int64(128000)
	maxContextWindow := int64(128000)
	defaultReasoning := "medium"
	reasoning := []llm.ReasoningLevel{{Effort: "medium", Description: codexReasoningDescription("medium")}}
	inputModalities := []string{"text"}
	supportsOriginalImage := false
	supportsReasoningSummary := false
	supportsParallelTools := false
	supportVerbosity := false
	defaultVerbosity := "medium"
	if metadata := model.Metadata; metadata != nil {
		if metadata.DisplayName != "" {
			displayName = metadata.DisplayName
		}
		if metadata.Description != "" {
			description = metadata.Description
		}
		if metadata.ContextWindow > 0 {
			contextWindow = metadata.ContextWindow
		}
		if metadata.MaxContextWindow > 0 {
			maxContextWindow = metadata.MaxContextWindow
		} else {
			maxContextWindow = contextWindow
		}
		if metadata.DefaultReasoningLevel != "" {
			defaultReasoning = metadata.DefaultReasoningLevel
		}
		if len(metadata.SupportedReasoningLevels) > 0 {
			reasoning = make([]llm.ReasoningLevel, 0, len(metadata.SupportedReasoningLevels))
			for _, level := range metadata.SupportedReasoningLevels {
				effort := strings.TrimSpace(level.Effort)
				if effort == "" {
					continue
				}
				description := strings.TrimSpace(level.Description)
				if description == "" {
					description = codexReasoningDescription(effort)
				}
				reasoning = append(reasoning, llm.ReasoningLevel{Effort: effort, Description: description})
			}
			if len(reasoning) == 0 {
				reasoning = []llm.ReasoningLevel{{Effort: defaultReasoning, Description: codexReasoningDescription(defaultReasoning)}}
			}
		}
		if len(metadata.InputModalities) > 0 {
			inputModalities = append([]string(nil), metadata.InputModalities...)
		}
		supportsOriginalImage = metadata.SupportsOriginalImage
		supportsReasoningSummary = metadata.SupportsReasoningSummary
		supportsParallelTools = metadata.SupportsParallelTools
		supportVerbosity = metadata.SupportsVerbosity
		switch metadata.DefaultVerbosity {
		case "low", "medium", "high":
			defaultVerbosity = metadata.DefaultVerbosity
		}
	}
	return llm.CodexModelInfo{
		Slug: model.Name, DisplayName: displayName, Description: description,
		ModelMessages:         llm.CodexModelMessages{InstructionsTemplate: agentHarnessInstructions},
		DefaultReasoningLevel: defaultReasoning, SupportedReasoningLevels: reasoning,
		ShellType: "unified_exec", Visibility: "list", SupportedInAPI: true,
		ContextWindow: int(contextWindow), MaxContextWindow: int(maxContextWindow),
		DefaultReasoningSummary: "none", ApplyPatchToolType: "freeform", WebSearchToolType: "text",
		TruncationPolicy: llm.TruncationPolicy{Mode: "tokens", Limit: 10000}, SupportsOriginalImage: supportsOriginalImage, EffectiveContextPercent: 95,
		ExperimentalTools: []string{}, InputModalities: inputModalities, NodeReplDisabled: true,
		SupportsReasoningSummary: supportsReasoningSummary, SupportsParallelTools: supportsParallelTools,
		SupportVerbosity: supportVerbosity, DefaultVerbosity: defaultVerbosity,
	}
}

func codexReasoningDescription(effort string) string {
	switch effort {
	case "none":
		return "No additional reasoning"
	case "minimal":
		return "Minimal reasoning for the fastest response"
	case "low":
		return "Fast responses with lighter reasoning"
	case "medium":
		return "Balanced speed and reasoning depth"
	case "high":
		return "Greater reasoning depth for complex problems"
	case "xhigh":
		return "Extra reasoning depth for harder problems"
	case "max":
		return "Maximum reasoning depth"
	case "ultra":
		return "Maximum reasoning with automatic delegation"
	default:
		return effort + " reasoning effort"
	}
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
		return responseapi.For(c).Error(502, "upstream read failed", "upstream_error", "").Send()
	}
	usage := llm.ExtractUsage(body)
	if providerpkg.UsesAnthropicWire(runtime.Provider) {
		resp, err := llm.FromAnthropic(body, model.Name)
		if err != nil {
			g.recordError(key, model, runtime.ID, fiber.StatusBadGateway, started, "response translation failed")
			return responseapi.For(c).Error(502, "response translation failed", "upstream_error", "").Send()
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
		return responseapi.For(c).Error(fiber.StatusServiceUnavailable, "quota settlement is unavailable", "service_unavailable", "redis_unavailable").Send()
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
	responsesMode, _ := c.Locals("responses_mode").(bool)
	messagesMode, _ := c.Locals("messages_mode").(bool)
	return c.SendStreamWriter(func(w *bufio.Writer) {
		defer result.Body.Close()
		streamStatus := fiber.StatusOK
		var responses *responsesStreamEmitter
		var messages *messagesStreamEmitter
		if responsesMode {
			responses = newResponsesStreamEmitter(model.Name)
			_ = responses.Created(w)
		} else if messagesMode {
			messages = newMessagesStreamEmitter(model.Name)
			_ = messages.Created(w)
		}
		if providerpkg.UsesAnthropicWire(runtime.Provider) {
			conv := llm.NewAnthropicStreamConverter(model.Name)
			err := llm.ScanSSE(result.Body, func(ev llm.SSEEvent) error {
				chunks, _, err := conv.Feed(ev.Event, ev.Data)
				if err != nil {
					return err
				}
				for _, chunk := range chunks {
					if responses != nil {
						if err := responses.ChatChunk(w, chunk); err != nil {
							return err
						}
					} else if messages != nil {
						if err := messages.ChatChunk(w, chunk); err != nil {
							return err
						}
					} else {
						_, _ = w.WriteString("data: " + string(chunk) + "\n\n")
						_ = w.Flush()
					}
				}
				return nil
			})
			usage = conv.UsageCollected()
			content.WriteString(conv.ContentCollected())
			finishReason = conv.FinishReason()
			if err != nil {
				streamStatus = fiber.StatusBadGateway
			}
			if responses == nil && messages == nil {
				_, _ = w.WriteString("data: [DONE]\n\n")
				_ = w.Flush()
			}
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
				if responses != nil {
					if payload == "[DONE]" {
						return nil
					}
					return responses.ChatChunk(w, ev.Data)
				}
				if messages != nil {
					if payload == "[DONE]" {
						return nil
					}
					return messages.ChatChunk(w, ev.Data)
				}
				if payload == "" {
					return nil
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
			if !done && responses == nil && messages == nil {
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
		if responses != nil {
			if err := responses.Completed(w, usage); err != nil {
				streamStatus = fiber.StatusBadGateway
			}
		} else if messages != nil {
			if err := messages.Completed(w, usage); err != nil {
				streamStatus = fiber.StatusBadGateway
			}
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
	messagesMode, _ := c.Locals("messages_mode").(bool)
	responsesMode, _ := c.Locals("responses_mode").(bool)
	return c.SendStreamWriter(func(w *bufio.Writer) {
		var resp llm.Response
		if json.Unmarshal(e.Body, &resp) != nil || len(resp.Choices) == 0 || resp.Choices[0].Message == nil {
			return
		}
		content := resp.Choices[0].Message.Content
		first, _ := json.Marshal(llm.Chunk{ID: resp.ID, Object: "chat.completion.chunk", Created: time.Now().Unix(), Model: resp.Model, Choices: []llm.ChunkChoice{{Index: 0, Delta: llm.Delta{Role: "assistant", Content: content}}}})
		last, _ := json.Marshal(llm.Chunk{ID: resp.ID, Object: "chat.completion.chunk", Created: time.Now().Unix(), Model: resp.Model, Choices: []llm.ChunkChoice{{Index: 0, Delta: llm.Delta{}, FinishReason: resp.Choices[0].FinishReason}}, Usage: &resp.Usage})
		if messagesMode {
			emitter := newMessagesStreamEmitter(resp.Model)
			_ = emitter.Created(w)
			_ = emitter.ChatChunk(w, first)
			_ = emitter.ChatChunk(w, last)
			_ = emitter.Completed(w, resp.Usage)
			return
		}
		if responsesMode {
			emitter := newResponsesStreamEmitter(resp.Model)
			_ = emitter.Created(w)
			_ = emitter.ChatChunk(w, first)
			_ = emitter.ChatChunk(w, last)
			_ = emitter.Completed(w, resp.Usage)
			return
		}
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
	g.Usage.Record(entities.UsageEvent{TS: time.Now(), TenantID: key.TenantID, ApiKeyID: apiKeyID, CredentialID: cred, Model: model.Name, UpstreamModel: model.UpstreamModel, PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens, CacheReadTokens: u.CacheReadTokens, CacheWriteTokens: u.CacheWriteTokens, CostUSD: cost.USD, InputCostUSD: cost.InputUSD, OutputCostUSD: cost.OutputUSD, CacheReadCostUSD: cost.CacheReadUSD, CacheWriteCostUSD: cost.CacheWriteUSD, Priced: cost.Priced, CacheHit: hit, StatusCode: status, DurationMS: time.Since(started).Milliseconds(), Error: summary, ActorType: key.Actor.Type, UserID: key.Actor.UserID, Username: key.Actor.Username, OrganizationID: key.Actor.OrganizationID, RequestBody: requestText, ResponseBody: responseText, ContentTruncated: requestTruncated || responseTruncated})
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
