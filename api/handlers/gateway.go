package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/kimnt93/gorouter/api/presenter"
	"github.com/kimnt93/gorouter/pkg/apikey"
	"github.com/kimnt93/gorouter/pkg/auth"
	"github.com/kimnt93/gorouter/pkg/chat"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/modelroute"
	"github.com/kimnt93/gorouter/pkg/usage"
	"github.com/kimnt93/gorouter/platform/llm"
)

type Gateway struct {
	Keys      *apikey.Service
	Creds     *credential.Service
	Models    *modelroute.Service
	Usage     *usage.Service
	Cache     chat.PromptCache
	Auth      *auth.Service
	OpenAI    entities.Upstream
	Anthropic entities.Upstream
	Selector  *chat.Selector
	Health    *chat.Health
}

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
	key, err := g.keyForSession(c, sess)
	if err != nil {
		return presenter.Unauthorized(c, "API key required")
	}
	if !contains(key.Models, req.Model) {
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
	deterministic := llm.IsDeterministic(req)
	if g.Cache != nil && deterministic {
		if cached, ok := g.Cache.Lookup(key.ID, key.TenantID, model.Name, raw); ok {
			usage := llm.Usage{PromptTokens: cached.PromptTok, CompletionTokens: cached.Completion}
			g.record(key, model, "", usage, true, cached.Status, started)
			c.Set("X-Cache", "hit")
			if req.Stream {
				return g.replayStream(c, cached)
			}
			c.Set("Content-Type", contentTypeOrJSON(cached.ContentType))
			return c.Send(cached.Body)
		}
	}
	prices, _ := g.Models.Prices(c.Context())
	price := prices[model.Name]
	if msg := g.quotaMessage(c, key, &price, req); msg != "" {
		return presenter.Err(c, fiber.StatusTooManyRequests, msg, "insufficient_quota", "quota_exceeded")
	}
	routes, err := g.Creds.Routes(c.Context(), model.Name)
	if err != nil || len(routes) == 0 {
		return presenter.Err(c, fiber.StatusServiceUnavailable, "no credentials available", "service_unavailable", "no_credentials")
	}
	candidates := make([]chat.Candidate, 0, len(routes))
	for _, route := range routes {
		if route.OwnerTenant != nil && *route.OwnerTenant != key.TenantID {
			continue
		}
		if g.Health.Available(route.CredentialID) {
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
		if (result.StatusCode < 200 || result.StatusCode >= 300) && !retryableStatus(result.StatusCode) {
			status := result.StatusCode
			drainAndClose(result.Body)
			g.record(key, model, runtime.ID, llm.Usage{}, false, status, started)
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
			return g.stream(c, key, model, runtime, result, raw, deterministic, started)
		}
		return g.nonStream(c, key, model, runtime, result, raw, deterministic, started)
	}
	c.Set("X-Cache", "bypass")
	g.record(key, model, lastCredential, llm.Usage{}, false, lastStatus, started)
	responseStatus := fiber.StatusBadGateway
	if lastStatus == fiber.StatusTooManyRequests {
		responseStatus = fiber.StatusTooManyRequests
	}
	return presenter.Err(c, responseStatus, "all credentials failed", "upstream_error", "upstream_unavailable")
}

func (g *Gateway) adapter(provider string) (entities.Upstream, bool) {
	switch provider {
	case entities.ProviderOpenAICompatible:
		return g.OpenAI, true
	case entities.ProviderAnthropic:
		return g.Anthropic, true
	default:
		return nil, false
	}
}

func retryableStatus(status int) bool {
	switch status {
	case fiber.StatusRequestTimeout, fiber.StatusTooManyRequests, fiber.StatusInternalServerError,
		fiber.StatusBadGateway, fiber.StatusServiceUnavailable, fiber.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func drainAndClose(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 1<<20))
	_ = body.Close()
}

func (g *Gateway) ListModels(c fiber.Ctx) error {
	sess := SessionFrom(c)
	key, err := g.keyForSession(c, sess)
	if err != nil {
		return presenter.Unauthorized(c, "API key required")
	}
	models, err := g.Models.List(c.Context())
	if err != nil {
		return presenter.ServerError(c, "failed to load models")
	}
	out := llm.ModelList{Object: "list", Data: []llm.ModelInfo{}}
	for _, model := range models {
		if model.Enabled && contains(key.Models, model.Name) {
			out.Data = append(out.Data, llm.ModelInfo{ID: model.Name, Object: "model", OwnedBy: "gorouter"})
		}
	}
	return c.JSON(out)
}

func (g *Gateway) keyForSession(c fiber.Ctx, sess *entities.Session) (*entities.ApiKey, error) {
	if sess == nil || sess.IsMaster() {
		return nil, fmt.Errorf("master session is not a client key")
	}
	return g.Keys.GetByID(c.Context(), sess.KeyID)
}

func (g *Gateway) quotaMessage(c fiber.Ctx, key *entities.ApiKey, price *entities.Price, req *llm.ChatRequest) string {
	if key.MonthlyQuotaUSD == nil {
		return ""
	}
	estimate := entities.ComputeCost(price, entities.TokenUsage{PromptTokens: req.EstimatePromptTokens(), CompletionTokens: req.EstimateOutputTokens()})
	spent, err := g.Usage.MonthSpendForKey(c.Context(), key.ID)
	if err == nil && spent+estimate > *key.MonthlyQuotaUSD {
		return fmt.Sprintf("monthly quota exceeded: spent $%.4f, estimated next $%.4f", spent, estimate)
	}
	return ""
}

func (g *Gateway) nonStream(c fiber.Ctx, key *entities.ApiKey, model *entities.ModelDef, runtime *entities.CredentialRuntime, result *entities.UpstreamResult, raw []byte, deterministic bool, started time.Time) error {
	defer result.Body.Close()
	body, err := io.ReadAll(io.LimitReader(result.Body, 32<<20))
	if err != nil {
		g.record(key, model, runtime.ID, llm.Usage{}, false, fiber.StatusBadGateway, started)
		return presenter.Err(c, 502, "upstream read failed", "upstream_error", "")
	}
	usage := llm.ExtractUsage(body)
	if runtime.Provider == entities.ProviderAnthropic {
		resp, err := llm.FromAnthropic(body, model.Name)
		if err != nil {
			g.record(key, model, runtime.ID, llm.Usage{}, false, fiber.StatusBadGateway, started)
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
	g.record(key, model, runtime.ID, usage, false, result.StatusCode, started)
	cacheStatus := "off"
	if deterministic && g.Cache != nil {
		g.Cache.Store(key.ID, key.TenantID, model.Name, raw, &chat.CacheEntry{Status: 200, ContentType: "application/json", Body: body, PromptTok: usage.PromptTokens, Completion: usage.CompletionTokens})
		cacheStatus = "miss"
	}
	c.Set("X-Cache", cacheStatus)
	c.Set("X-Upstream-Credential", runtime.ID)
	c.Set("Content-Type", "application/json")
	return c.Send(body)
}

func (g *Gateway) stream(c fiber.Ctx, key *entities.ApiKey, model *entities.ModelDef, runtime *entities.CredentialRuntime, result *entities.UpstreamResult, raw []byte, deterministic bool, started time.Time) error {
	defer result.Body.Close()
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("X-Accel-Buffering", "no")
	c.Set("X-Upstream-Credential", runtime.ID)
	if deterministic && g.Cache != nil {
		c.Set("X-Cache", "miss")
	} else {
		c.Set("X-Cache", "off")
	}
	var usage llm.Usage
	var content strings.Builder
	finishReason := "stop"
	return c.SendStreamWriter(func(w *bufio.Writer) {
		streamStatus := fiber.StatusOK
		if runtime.Provider == entities.ProviderAnthropic {
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
		g.record(key, model, runtime.ID, usage, false, streamStatus, started)
		if streamStatus == fiber.StatusOK && deterministic && g.Cache != nil && content.Len() > 0 {
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

func (g *Gateway) record(key *entities.ApiKey, model *entities.ModelDef, cred string, u llm.Usage, hit bool, status int, started time.Time) {
	if g.Usage == nil {
		return
	}
	prices, _ := g.Models.Prices(context.Background())
	p := prices[model.Name]
	cost := entities.ComputeCost(&p, u.TokenUsage())
	if hit {
		cost = 0
	}
	g.Usage.Record(entities.UsageEvent{TS: time.Now(), TenantID: key.TenantID, ApiKeyID: key.ID, CredentialID: cred, Model: model.Name, UpstreamModel: model.UpstreamModel, PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens, CacheReadTokens: u.CacheReadTokens, CacheWriteTokens: u.CacheWriteTokens, CostUSD: cost, CacheHit: hit, StatusCode: status, DurationMS: time.Since(started).Milliseconds()})
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
