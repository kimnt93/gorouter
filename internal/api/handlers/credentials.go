package handlers

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/gofiber/fiber/v3"

	responseapi "github.com/kimnt93/gorouter/internal/api"
	"github.com/kimnt93/gorouter/internal/api/presenter"
	"github.com/kimnt93/gorouter/internal/platform/llm"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/modelroute"
	providerpkg "github.com/kimnt93/gorouter/pkg/provider"
	"github.com/kimnt93/gorouter/pkg/providerquota"
)

// CredentialConnectivity is registered separately from Admin so provider
// adapters stay explicit dependencies at the HTTP boundary.
type CredentialConnectivity struct {
	Credentials *credential.Service
	OpenAI      credential.ConnectivityProber
	Anthropic   credential.ConnectivityProber
	Codex       credential.ConnectivityProber
	Providers   map[string]credential.ConnectivityProber
	ModelRoutes *modelroute.Service
	Quotas      *providerquota.Service
}

// Quota returns the cached provider quota or refreshes it on explicit POST.
// @Summary Get or refresh credential quota
// @Tags credentials
// @Security BearerAuth
// @Param id path string true "Credential ID"
// @Success 200 {object} providerquota.Snapshot
// @Failure 401,403,404,500,502 {object} presenter.Error
// @Router /admin/credentials/{id}/quota [get]
// @Router /admin/credentials/{id}/quota [post]
func (h *CredentialConnectivity) Quota(c fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	if !h.authorize(c) {
		return presenter.NotFound(c, "credential not found")
	}
	if h.Quotas == nil {
		return presenter.ServerError(c, "provider quota service is unavailable")
	}
	id := c.Params("id")
	if c.Method() == fiber.MethodGet {
		if snapshot, ok := h.Quotas.Cached(id); ok {
			return responseapi.JSON(c, snapshot)
		}
		runtime, err := h.Credentials.Runtime(c.Context(), id)
		if err != nil {
			return presenter.NotFound(c, "credential not found")
		}
		return responseapi.JSON(c, providerquota.Snapshot{CredentialID: id, Provider: runtime.Provider, Account: "connected account", Available: true, Windows: []providerquota.Window{}, Message: "Click reload to fetch quota"})
	}
	snapshot, err := h.Quotas.Refresh(c.Context(), id)
	if err != nil {
		return presenter.Err(c, fiber.StatusBadGateway, err.Error(), "upstream_error", "quota_refresh_failed")
	}
	return responseapi.JSON(c, snapshot)
}

// ImportModels imports selected upstream models into global model routes.
// @Summary Import provider models
// @Tags credentials
// @Security BearerAuth
// @Param id path string true "Credential ID"
// @Param request body ImportModelsRequest true "Models to import"
// @Success 200 {object} ImportModelsResponse
// @Failure 400,401,403,404,500 {object} presenter.Error
// @Router /admin/credentials/{id}/models/import [post]
func (h *CredentialConnectivity) ImportModels(c fiber.Ctx) error {
	if sess := SessionFrom(c); sess == nil || !sess.IsMaster() {
		return presenter.Forbidden(c, "only the master session can import global model routes")
	}
	if h.ModelRoutes == nil {
		return presenter.ServerError(c, "model service is unavailable")
	}
	var input ImportModelsRequest
	if err := c.Bind().Body(&input); err != nil || len(input.Models) == 0 {
		return presenter.BadRequest(c, "at least one model is required")
	}
	runtime, err := h.Credentials.Runtime(c.Context(), c.Params("id"))
	if err != nil {
		return presenter.NotFound(c, "credential not found")
	}
	existing, err := h.ModelRoutes.List(c.Context())
	if err != nil {
		return presenter.ServerError(c, "failed to load models")
	}
	byName := make(map[string]entities.ModelDef, len(existing))
	for _, model := range existing {
		byName[model.Name] = model
	}
	imported := make([]string, 0, len(input.Models))
	seen := map[string]bool{}
	for _, upstream := range input.Models {
		upstream = strings.TrimSpace(upstream)
		if upstream == "" || seen[upstream] {
			continue
		}
		seen[upstream] = true
		name := providerpkg.PublicModelID(runtime.Provider, upstream)
		model, ok := byName[name]
		if !ok {
			model = entities.ModelDef{Name: name, Strategy: "priority", UpstreamModel: upstream, Enabled: true, Routes: []entities.ModelRoute{}}
		}
		hasRoute := false
		nextPriority := 0
		for _, route := range model.Routes {
			if route.CredentialID == runtime.ID {
				hasRoute = true
				break
			}
			if route.Priority <= nextPriority {
				nextPriority = route.Priority - 1
			}
		}
		if !hasRoute {
			model.Routes = append(model.Routes, entities.ModelRoute{CredentialID: runtime.ID, Priority: nextPriority, Weight: 1, Enabled: true})
		}
		if err := h.ModelRoutes.Upsert(c.Context(), model); err != nil {
			return presenter.BadRequest(c, fmt.Sprintf("import %s: %v", upstream, err))
		}
		imported = append(imported, name)
	}
	return responseapi.JSON(c, ImportModelsResponse{OK: true, Imported: imported})
}

func (h *CredentialConnectivity) authorize(c fiber.Ctx) bool {
	if sess := SessionFrom(c); sess != nil && !sess.IsMaster() {
		credentials, err := h.Credentials.List(c.Context())
		if err != nil {
			return false
		}
		for _, candidate := range credentials {
			if candidate.ID != c.Params("id") {
				continue
			}
			if candidate.OwnerUserID != "" {
				return candidate.OwnerUserID == sess.UserID
			}
			return candidate.OwnerTenantID != nil && *candidate.OwnerTenantID == sess.TenantID
		}
		return false
	}
	return true
}

func (h *CredentialConnectivity) adapter(providerID string) (credential.ConnectivityProber, credential.ModelDiscoverer, entities.Upstream) {
	var value credential.ConnectivityProber
	if h.Providers != nil {
		value = h.Providers[providerID]
	}
	if value != nil {
		discoverer, _ := value.(credential.ModelDiscoverer)
		upstream, _ := value.(entities.Upstream)
		return value, discoverer, upstream
	}
	switch providerpkg.ProtocolFor(providerID) {
	case providerpkg.ProtocolOpenAI:
		value = h.OpenAI
	case providerpkg.ProtocolAnthropic:
		value = h.Anthropic
	case providerpkg.ProtocolCodex:
		value = h.Codex
	default:
		return nil, nil, nil
	}
	discoverer, _ := value.(credential.ModelDiscoverer)
	upstream, _ := value.(entities.Upstream)
	return value, discoverer, upstream
}

// Test probes a credential without exposing its secret.
// @Summary Test credential connectivity
// @Tags credentials
// @Security BearerAuth
// @Param id path string true "Credential ID"
// @Success 200 {object} credential.ConnectivityResult
// @Failure 400,401,403,404,500 {object} presenter.Error
// @Router /admin/credentials/{id}/test [post]
func (h *CredentialConnectivity) Test(c fiber.Ctx) error {
	if !h.authorize(c) {
		return presenter.NotFound(c, "credential not found")
	}
	runtime, err := h.Credentials.Runtime(c.Context(), c.Params("id"))
	if err != nil {
		return presenter.NotFound(c, "credential not found")
	}
	probe, _, _ := h.adapter(runtime.Provider)
	result, err := h.Credentials.TestConnectivity(c.Context(), c.Params("id"), map[string]credential.ConnectivityProber{runtime.Provider: probe})
	if errors.Is(err, entities.ErrNotFound) {
		return presenter.NotFound(c, "credential not found")
	}
	if errors.Is(err, credential.ErrUnsupportedProvider) {
		return presenter.BadRequest(c, "unsupported credential provider")
	}
	if err != nil {
		return responseapi.JSON(c, credential.ConnectivityResult{OK: false})
	}
	return responseapi.JSON(c, result)
}

// Models discovers safe model metadata through a credential.
// @Summary Discover provider models
// @Tags credentials
// @Security BearerAuth
// @Param id path string true "Credential ID"
// @Success 200 {object} ProviderModelsResponse
// @Failure 401,403,404,500,502 {object} presenter.Error
// @Router /admin/credentials/{id}/models [get]
func (h *CredentialConnectivity) Models(c fiber.Ctx) error {
	if !h.authorize(c) {
		return presenter.NotFound(c, "credential not found")
	}
	runtime, err := h.Credentials.Runtime(c.Context(), c.Params("id"))
	if errors.Is(err, entities.ErrNotFound) {
		return presenter.NotFound(c, "credential not found")
	}
	if err != nil {
		return presenter.ServerError(c, "failed to load credential")
	}
	_, discoverer, _ := h.adapter(runtime.Provider)
	models, err := h.Credentials.DiscoverModels(c.Context(), c.Params("id"), discoverer)
	if err != nil {
		return presenter.Err(c, fiber.StatusBadGateway, "provider model discovery failed", "upstream_error", "")
	}
	defaultModel := ""
	if len(models) > 0 {
		// Discoverers return a deterministic order. Use the first available
		// model as the console's safe default; callers can still choose any
		// other discovered model explicitly.
		defaultModel = models[0].ID
	}
	out := ProviderModelsResponse{Object: "list", Provider: runtime.Provider, DefaultModel: defaultModel, Data: make([]ProviderModelResponse, 0, len(models))}
	for _, model := range models {
		object := model.Object
		if object == "" {
			object = "model"
		}
		permission := make([]json.RawMessage, 0, len(model.Permission))
		for _, item := range model.Permission {
			encoded, _ := json.Marshal(item)
			permission = append(permission, encoded)
		}
		capabilities, _ := json.Marshal(model.Capabilities)
		out.Data = append(out.Data, ProviderModelResponse{
			ID:                 model.ID,
			Object:             object,
			PublicID:           providerpkg.PublicModelID(runtime.Provider, model.ID),
			Default:            model.ID == defaultModel,
			Created:            model.Created,
			OwnedBy:            model.OwnedBy,
			Permission:         permission,
			Root:               model.Root,
			Parent:             model.Parent,
			APIFormat:          model.APIFormat,
			ContextLength:      model.ContextLength,
			MaxOutputTokens:    model.MaxOutputTokens,
			SupportedEndpoints: model.SupportedEndpoints,
			Capabilities:       capabilities,
			InputModalities:    model.InputModalities,
			OutputModalities:   model.OutputModalities,
			MaxInputTokens:     model.MaxInputTokens,
			Name:               model.Name,
		})
	}
	return responseapi.JSON(c, out)
}

// Chat streams a bounded connectivity test through one credential.
// @Summary Run credential chat test
// @Tags credentials
// @Security BearerAuth
// @Param id path string true "Credential ID"
// @Param request body CredentialChatTestRequest true "Test prompt"
// @Success 200 {string} string "Server-sent events"
// @Failure 400,401,403,404,500,502 {object} presenter.Error
// @Router /admin/credentials/{id}/chat-tests [post]
func (h *CredentialConnectivity) Chat(c fiber.Ctx) error {
	if !h.authorize(c) {
		return presenter.NotFound(c, "credential not found")
	}
	var input CredentialChatTestRequest
	if err := c.Bind().Body(&input); err != nil || strings.TrimSpace(input.Model) == "" || strings.TrimSpace(input.Prompt) == "" {
		return presenter.BadRequest(c, "model and prompt are required")
	}
	runtime, err := h.Credentials.Runtime(c.Context(), c.Params("id"))
	if err != nil {
		return presenter.NotFound(c, "credential not found")
	}
	_, _, upstream := h.adapter(runtime.Provider)
	if upstream == nil {
		return presenter.BadRequest(c, "provider chat test is not supported")
	}
	maxTokens := int64(128)
	req := llm.ChatRequest{Model: input.Model, Messages: []llm.Message{{Role: "user", Content: json.RawMessage(fmt.Sprintf("%q", input.Prompt))}}, Stream: true, MaxTokens: &maxTokens}
	body, _ := json.Marshal(req)
	result, err := upstream.Send(c.Context(), runtime, input.Model, body)
	if err != nil {
		return presenter.Err(c, fiber.StatusBadGateway, "provider chat test failed", "upstream_error", "")
	}
	if result.StatusCode < 200 || result.StatusCode >= 300 {
		defer result.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(result.Body, 1<<20))
		return presenter.Err(c, fiber.StatusBadGateway, fmt.Sprintf("provider returned HTTP %d", result.StatusCode), "upstream_error", "")
	}
	c.Set(fiber.HeaderContentType, "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("X-Accel-Buffering", "no")
	return c.SendStreamWriter(func(w *bufio.Writer) {
		defer result.Body.Close()
		if providerpkg.UsesAnthropicWire(runtime.Provider) {
			converter := llm.NewAnthropicStreamConverter(providerpkg.PublicModelID(runtime.Provider, input.Model))
			_ = llm.ScanSSE(result.Body, func(event llm.SSEEvent) error {
				chunks, _, feedErr := converter.Feed(event.Event, event.Data)
				if feedErr != nil {
					return feedErr
				}
				for _, chunk := range chunks {
					if _, writeErr := w.WriteString("data: " + string(chunk) + "\n\n"); writeErr != nil {
						return writeErr
					}
					if flushErr := w.Flush(); flushErr != nil {
						return flushErr
					}
				}
				return nil
			})
			_, _ = w.WriteString("data: [DONE]\n\n")
			_ = w.Flush()
			return
		}
		_ = llm.ScanSSE(result.Body, func(event llm.SSEEvent) error {
			if _, writeErr := w.WriteString("data: " + string(event.Data) + "\n\n"); writeErr != nil {
				return writeErr
			}
			return w.Flush()
		})
	})
}
