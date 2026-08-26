package handlers

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/kimnt93/gorouter/api/presenter"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/modelroute"
	providerpkg "github.com/kimnt93/gorouter/pkg/provider"
	"github.com/kimnt93/gorouter/platform/llm"
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
}

func (h *CredentialConnectivity) ImportModels(c fiber.Ctx) error {
	if sess := SessionFrom(c); sess == nil || !sess.IsMaster() {
		return presenter.Forbidden(c, "only the master session can import global model routes")
	}
	if h.ModelRoutes == nil {
		return presenter.ServerError(c, "model service is unavailable")
	}
	var input struct {
		Models []string `json:"models"`
	}
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
		for _, route := range model.Routes {
			if route.CredentialID == runtime.ID {
				hasRoute = true
				break
			}
		}
		if !hasRoute {
			model.Routes = append(model.Routes, entities.ModelRoute{CredentialID: runtime.ID, Weight: 1, Enabled: true})
		}
		if err := h.ModelRoutes.Upsert(c.Context(), model); err != nil {
			return presenter.BadRequest(c, fmt.Sprintf("import %s: %v", upstream, err))
		}
		imported = append(imported, name)
	}
	return c.JSON(struct {
		OK       bool     `json:"ok"`
		Imported []string `json:"imported"`
	}{OK: true, Imported: imported})
}

type providerModelResponse struct {
	ID                 string         `json:"id"`
	Object             string         `json:"object"`
	PublicID           string         `json:"public_id"`
	Default            bool           `json:"default,omitempty"`
	Created            int64          `json:"created,omitempty"`
	OwnedBy            string         `json:"owned_by,omitempty"`
	Permission         []any          `json:"permission"`
	Root               string         `json:"root,omitempty"`
	Parent             *string        `json:"parent"`
	APIFormat          string         `json:"api_format,omitempty"`
	ContextLength      int64          `json:"context_length,omitempty"`
	MaxOutputTokens    int64          `json:"max_output_tokens,omitempty"`
	SupportedEndpoints []string       `json:"supported_endpoints,omitempty"`
	Capabilities       map[string]any `json:"capabilities,omitempty"`
	InputModalities    []string       `json:"input_modalities,omitempty"`
	OutputModalities   []string       `json:"output_modalities,omitempty"`
	MaxInputTokens     int64          `json:"max_input_tokens,omitempty"`
	Name               string         `json:"name,omitempty"`
}

type providerModelsResponse struct {
	Object       string                  `json:"object"`
	Provider     string                  `json:"provider"`
	DefaultModel string                  `json:"default_model,omitempty"`
	Data         []providerModelResponse `json:"data"`
}

func (h *CredentialConnectivity) authorize(c fiber.Ctx) bool {
	if sess := SessionFrom(c); sess != nil && !sess.IsMaster() {
		credentials, err := h.Credentials.List(c.Context())
		if err != nil {
			return false
		}
		for _, candidate := range credentials {
			if candidate.ID == c.Params("id") && candidate.OwnerTenantID != nil && *candidate.OwnerTenantID == sess.TenantID {
				return true
			}
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
		return c.JSON(credential.ConnectivityResult{OK: false})
	}
	return c.JSON(result)
}

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
	out := providerModelsResponse{Object: "list", Provider: runtime.Provider, DefaultModel: defaultModel, Data: make([]providerModelResponse, 0, len(models))}
	for _, model := range models {
		object := model.Object
		if object == "" {
			object = "model"
		}
		out.Data = append(out.Data, providerModelResponse{
			ID:                 model.ID,
			Object:             object,
			PublicID:           providerpkg.PublicModelID(runtime.Provider, model.ID),
			Default:            model.ID == defaultModel,
			Created:            model.Created,
			OwnedBy:            model.OwnedBy,
			Permission:         model.Permission,
			Root:               model.Root,
			Parent:             model.Parent,
			APIFormat:          model.APIFormat,
			ContextLength:      model.ContextLength,
			MaxOutputTokens:    model.MaxOutputTokens,
			SupportedEndpoints: model.SupportedEndpoints,
			Capabilities:       model.Capabilities,
			InputModalities:    model.InputModalities,
			OutputModalities:   model.OutputModalities,
			MaxInputTokens:     model.MaxInputTokens,
			Name:               model.Name,
		})
	}
	return c.JSON(out)
}

func (h *CredentialConnectivity) Chat(c fiber.Ctx) error {
	if !h.authorize(c) {
		return presenter.NotFound(c, "credential not found")
	}
	var input struct {
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
	}
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
