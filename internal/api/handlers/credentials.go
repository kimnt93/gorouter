package handlers

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	responseapi "github.com/kimnt93/gorouter/internal/api"
	"github.com/kimnt93/gorouter/internal/platform/llm"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/identity"
	"github.com/kimnt93/gorouter/pkg/modelroute"
	providerpkg "github.com/kimnt93/gorouter/pkg/provider"
	"github.com/kimnt93/gorouter/pkg/providerquota"
	"github.com/kimnt93/gorouter/pkg/usage"
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
	Usage       *usage.Service
	Identities  identity.Repository
}

// Quota returns the cached provider quota or refreshes it on explicit POST.
// @Summary Get or refresh credential quota
// @Description Returns the latest provider-account quota snapshot, optionally refreshing it from the provider.
// @Tags credentials
// @Security BearerAuth
// @Param id path string true "Credential ID"
// @Success 200 {object} providerquota.Snapshot
// @Failure 401,403,404,500,502 {object} responseapi.ErrorResponse
// @Router /admin/credentials/{id}/quota [get]
// @Router /admin/credentials/{id}/quota [post]
func (h *CredentialConnectivity) Quota(c fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	if !h.authorize(c) {
		return responseapi.For(c).NotFound("credential not found").Send()
	}
	if h.Quotas == nil {
		return responseapi.For(c).InternalError("provider quota service is unavailable").Send()
	}
	id := c.Params("id")
	if c.Method() == fiber.MethodGet {
		if snapshot, ok := h.Quotas.Cached(id); ok {
			return responseapi.For(c).Response().Status(fiber.StatusOK).Data(snapshot).Send()
		}
		runtime, err := h.Credentials.Runtime(c.Context(), id)
		if err != nil {
			return responseapi.For(c).NotFound("credential not found").Send()
		}
		return responseapi.For(c).Response().Status(fiber.StatusOK).Data(providerquota.Snapshot{CredentialID: id, Provider: runtime.Provider, Account: "connected account", Available: true, Windows: []providerquota.Window{}, Message: "Click reload to fetch quota"}).Send()
	}
	snapshot, err := h.Quotas.Refresh(c.Context(), id)
	if err != nil {
		return responseapi.For(c).Error(fiber.StatusBadGateway, err.Error(), "upstream_error", "quota_refresh_failed").Send()
	}
	return responseapi.For(c).Response().Status(fiber.StatusOK).Data(snapshot).Send()
}

// ImportModels imports selected upstream models into ownership-aware routes.
// @Summary Import provider models
// @Description Imports selected models discovered from a provider credential into model route definitions.
// @Tags credentials
// @Security BearerAuth
// @Param id path string true "Credential ID"
// @Param request body ImportModelsRequest true "Models to import"
// @Success 200 {object} ImportModelsResponse
// @Failure 400,401,403,404,500,502 {object} responseapi.ErrorResponse
// @Router /admin/credentials/{id}/models/import [post]
func (h *CredentialConnectivity) ImportModels(c fiber.Ctx) error {
	if sess := SessionFrom(c); sess == nil || !sess.IsMaster() {
		return responseapi.For(c).Forbidden("only the master session can import model routes").Send()
	}
	if !h.authorize(c) {
		return responseapi.For(c).NotFound("credential not found").Send()
	}
	if h.ModelRoutes == nil {
		return responseapi.For(c).InternalError("model service is unavailable").Send()
	}
	var input ImportModelsRequest
	if err := c.Bind().Body(&input); err != nil || len(input.Models) == 0 {
		return responseapi.For(c).BadRequest("at least one model is required").Send()
	}
	runtime, err := h.Credentials.Runtime(c.Context(), c.Params("id"))
	if err != nil {
		return responseapi.For(c).NotFound("credential not found").Send()
	}
	_, discoverer, _ := h.adapter(runtime.Provider)
	discovered, err := h.Credentials.DiscoverModels(c.Context(), runtime.ID, discoverer)
	if err != nil {
		return responseapi.For(c).Error(fiber.StatusBadGateway, "provider model discovery failed", "upstream_error", "").Send()
	}
	byUpstream := make(map[string]credential.ProviderModel, len(discovered))
	for _, model := range discovered {
		byUpstream[model.ID] = model
	}
	metadata, err := h.credential(c, runtime.ID)
	if err != nil {
		return responseapi.For(c).NotFound("credential not found").Send()
	}
	organizationName := ""
	if metadata.OwnerTenantID != nil {
		if h.Identities == nil {
			return responseapi.For(c).InternalError("identity repository is unavailable").Send()
		}
		organization, organizationErr := h.Identities.OrganizationByID(c.Context(), *metadata.OwnerTenantID)
		if organizationErr != nil || organization.Status != entities.StatusActive {
			return responseapi.For(c).BadRequest("credential organization is unavailable").Send()
		}
		organizationName = organization.Name
	}
	existing, err := h.ModelRoutes.List(c.Context())
	if err != nil {
		return responseapi.For(c).InternalError("failed to load models").Send()
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
		discoveredModel, exists := byUpstream[upstream]
		if !exists {
			return responseapi.For(c).BadRequest(fmt.Sprintf("model %s was not reported by the provider", upstream)).Send()
		}
		name := providerpkg.PublicModelID(runtime.Provider, upstream)
		if organizationName != "" {
			name = providerpkg.OrganizationModelID(organizationName, runtime.Provider, upstream)
		}
		model, ok := byName[name]
		if !ok {
			model = entities.ModelDef{Name: name, Strategy: "priority", UpstreamModel: upstream, Enabled: true, Routes: []entities.ModelRoute{}}
		}
		hasRoute := false
		nextPriority := 0
		for _, route := range model.Routes {
			if route.CredentialID == runtime.ID && (route.UpstreamModel == "" || route.UpstreamModel == upstream) {
				hasRoute = true
				break
			}
			if route.Priority <= nextPriority {
				nextPriority = route.Priority - 1
			}
		}
		if !hasRoute {
			model.Routes = append(model.Routes, entities.ModelRoute{CredentialID: runtime.ID, UpstreamModel: upstream, Priority: nextPriority, Weight: 1, Enabled: true})
		}
		model.Metadata = credential.MetadataSnapshot(runtime.Provider, runtime.ID, discoveredModel, time.Now())
		if err := h.ModelRoutes.Upsert(c.Context(), model); err != nil {
			return responseapi.For(c).BadRequest(fmt.Sprintf("import %s: %v", upstream, err)).Send()
		}
		imported = append(imported, name)
	}
	return responseapi.For(c).Response().Status(fiber.StatusOK).Data(ImportModelsResponse{OK: true, Imported: imported}).Send()
}

// RefreshModelMetadata refreshes provider-reported metadata for configured models.
// @Summary Refresh imported model metadata
// @Description Discovers the provider catalog and refreshes metadata for existing model routes that use this credential. It does not import newly discovered models.
// @Tags credentials
// @Security BearerAuth
// @Param id path string true "Credential ID"
// @Success 200 {object} RefreshModelMetadataResponse
// @Failure 401,403,404,500,502 {object} responseapi.ErrorResponse
// @Router /admin/credentials/{id}/models/refresh [post]
func (h *CredentialConnectivity) RefreshModelMetadata(c fiber.Ctx) error {
	if sess := SessionFrom(c); sess == nil || !sess.IsMaster() {
		return responseapi.For(c).Forbidden("only the master session can refresh model metadata").Send()
	}
	if !h.authorize(c) {
		return responseapi.For(c).NotFound("credential not found").Send()
	}
	if h.ModelRoutes == nil {
		return responseapi.For(c).InternalError("model service is unavailable").Send()
	}
	runtime, err := h.Credentials.Runtime(c.Context(), c.Params("id"))
	if err != nil {
		return responseapi.For(c).NotFound("credential not found").Send()
	}
	_, discoverer, _ := h.adapter(runtime.Provider)
	discovered, err := h.Credentials.DiscoverModels(c.Context(), runtime.ID, discoverer)
	if err != nil {
		return responseapi.For(c).Error(fiber.StatusBadGateway, "provider model discovery failed", "upstream_error", "").Send()
	}
	byID := make(map[string]credential.ProviderModel, len(discovered))
	for _, model := range discovered {
		byID[model.ID] = model
	}
	models, err := h.ModelRoutes.List(c.Context())
	if err != nil {
		return responseapi.For(c).InternalError("failed to load models").Send()
	}
	result := RefreshModelMetadataResponse{OK: true, Refreshed: []string{}, Missing: []string{}}
	now := time.Now()
	for _, model := range models {
		usesCredential := false
		for _, route := range model.Routes {
			if route.CredentialID == runtime.ID {
				usesCredential = true
				break
			}
		}
		if !usesCredential {
			continue
		}
		reported, ok := byID[model.UpstreamModel]
		if !ok {
			result.Missing = append(result.Missing, model.Name)
			continue
		}
		model.Metadata = credential.MetadataSnapshot(runtime.Provider, runtime.ID, reported, now)
		if err := h.ModelRoutes.Upsert(c.Context(), model); err != nil {
			return responseapi.For(c).InternalError("failed to refresh model metadata").Send()
		}
		result.Refreshed = append(result.Refreshed, model.Name)
	}
	return responseapi.For(c).Response().Status(fiber.StatusOK).Data(result).Send()
}

func (h *CredentialConnectivity) credential(c fiber.Ctx, id string) (*entities.Credential, error) {
	credentials, err := h.Credentials.List(c.Context())
	if err != nil {
		return nil, err
	}
	for index := range credentials {
		if credentials[index].ID == id {
			return &credentials[index], nil
		}
	}
	return nil, entities.ErrNotFound
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
		discoverer := credential.ResolveModelDiscoverer(providerID, h.Providers, modelDiscoverer(h.OpenAI), modelDiscoverer(h.Anthropic), modelDiscoverer(h.Codex))
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

func modelDiscoverer(value credential.ConnectivityProber) credential.ModelDiscoverer {
	discoverer, _ := value.(credential.ModelDiscoverer)
	return discoverer
}

// Test probes a credential without exposing its secret.
// @Summary Test credential connectivity
// @Description Probes a credential without exposing its secret or provider error body.
// @Tags credentials
// @Security BearerAuth
// @Param id path string true "Credential ID"
// @Success 200 {object} credential.ConnectivityResult
// @Failure 400,401,403,404,500 {object} responseapi.ErrorResponse
// @Router /admin/credentials/{id}/test [post]
func (h *CredentialConnectivity) Test(c fiber.Ctx) error {
	if !h.authorize(c) {
		return responseapi.For(c).NotFound("credential not found").Send()
	}
	runtime, err := h.Credentials.Runtime(c.Context(), c.Params("id"))
	if err != nil {
		return responseapi.For(c).NotFound("credential not found").Send()
	}
	probe, _, _ := h.adapter(runtime.Provider)
	result, err := h.Credentials.TestConnectivity(c.Context(), c.Params("id"), map[string]credential.ConnectivityProber{runtime.Provider: probe})
	if errors.Is(err, entities.ErrNotFound) {
		return responseapi.For(c).NotFound("credential not found").Send()
	}
	if errors.Is(err, credential.ErrUnsupportedProvider) {
		return responseapi.For(c).BadRequest("unsupported credential provider").Send()
	}
	if err != nil {
		return responseapi.For(c).Response().Status(fiber.StatusOK).Data(credential.ConnectivityResult{OK: false}).Send()
	}
	return responseapi.For(c).Response().Status(fiber.StatusOK).Data(result).Send()
}

// Models discovers safe model metadata through a credential.
// @Summary Discover provider models
// @Description Discovers safe model metadata through a provider credential.
// @Tags credentials
// @Security BearerAuth
// @Param id path string true "Credential ID"
// @Success 200 {object} ProviderModelsResponse
// @Failure 401,403,404,500,502 {object} responseapi.ErrorResponse
// @Router /admin/credentials/{id}/models [get]
func (h *CredentialConnectivity) Models(c fiber.Ctx) error {
	if !h.authorize(c) {
		return responseapi.For(c).NotFound("credential not found").Send()
	}
	runtime, err := h.Credentials.Runtime(c.Context(), c.Params("id"))
	if errors.Is(err, entities.ErrNotFound) {
		return responseapi.For(c).NotFound("credential not found").Send()
	}
	if err != nil {
		return responseapi.For(c).InternalError("failed to load credential").Send()
	}
	_, discoverer, _ := h.adapter(runtime.Provider)
	models, err := h.Credentials.DiscoverModels(c.Context(), c.Params("id"), discoverer)
	if err != nil {
		return responseapi.For(c).Error(fiber.StatusBadGateway, "provider model discovery failed", "upstream_error", "").Send()
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
			Name:               model.Name, Description: model.Description, MaxContextWindow: model.MaxContextWindow,
			DefaultReasoningLevel: model.DefaultReasoningLevel, SupportedReasoningLevels: model.SupportedReasoningLevels,
			SupportsOriginalImage: model.SupportsOriginalImage, SupportsReasoningSummary: model.SupportsReasoningSummary,
			SupportsParallelTools: model.SupportsParallelTools, SupportsVerbosity: model.SupportsVerbosity, DefaultVerbosity: model.DefaultVerbosity,
		})
	}
	return responseapi.For(c).Response().Status(fiber.StatusOK).Data(out).Send()
}

// Chat streams a bounded connectivity test through one credential.
// @Summary Run credential chat test
// @Description Runs a bounded streaming chat test through one credential and records the provider attempt in usage logs.
// @Tags credentials
// @Security BearerAuth
// @Param id path string true "Credential ID"
// @Param request body CredentialChatTestRequest true "Test prompt"
// @Success 200 {string} string "Server-sent events"
// @Failure 400,401,403,404,500,502 {object} responseapi.ErrorResponse
// @Router /admin/credentials/{id}/chat-tests [post]
func (h *CredentialConnectivity) Chat(c fiber.Ctx) error {
	started := time.Now()
	if !h.authorize(c) {
		return responseapi.For(c).NotFound("credential not found").Send()
	}
	var input CredentialChatTestRequest
	if err := c.Bind().Body(&input); err != nil || strings.TrimSpace(input.Model) == "" || strings.TrimSpace(input.Prompt) == "" {
		return responseapi.For(c).BadRequest("model and prompt are required").Send()
	}
	runtime, err := h.Credentials.Runtime(c.Context(), c.Params("id"))
	if err != nil {
		return responseapi.For(c).NotFound("credential not found").Send()
	}
	credentialRecord, err := h.credential(c, runtime.ID)
	if err != nil {
		return responseapi.For(c).NotFound("credential not found").Send()
	}
	logContext := newChatTestLogContext(SessionFrom(c), credentialRecord, runtime, input.Model)
	_, _, upstream := h.adapter(runtime.Provider)
	if upstream == nil {
		return responseapi.For(c).BadRequest("provider chat test is not supported").Send()
	}
	maxTokens := int64(128)
	req := llm.ChatRequest{Model: input.Model, Messages: []llm.Message{{Role: "user", Content: json.RawMessage(fmt.Sprintf("%q", input.Prompt))}}, Stream: true, MaxTokens: &maxTokens}
	body, _ := json.Marshal(req)
	result, err := upstream.Send(c.Context(), runtime, input.Model, body)
	if err != nil {
		h.recordChatTest(logContext, llm.Usage{PromptTokens: req.EstimatePromptTokens()}, fiber.StatusBadGateway, started, "provider chat test failed")
		return responseapi.For(c).Error(fiber.StatusBadGateway, "provider chat test failed", "upstream_error", "").Send()
	}
	if result.StatusCode < 200 || result.StatusCode >= 300 {
		defer result.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(result.Body, 1<<20))
		h.recordChatTest(logContext, llm.Usage{PromptTokens: req.EstimatePromptTokens()}, result.StatusCode, started, "provider rejected chat test")
		return responseapi.For(c).Error(fiber.StatusBadGateway, fmt.Sprintf("provider returned HTTP %d", result.StatusCode), "upstream_error", "").Send()
	}
	c.Set(fiber.HeaderContentType, "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("X-Accel-Buffering", "no")
	return c.SendStreamWriter(func(w *bufio.Writer) {
		defer result.Body.Close()
		streamStatus := fiber.StatusOK
		var collected llm.Usage
		var completion strings.Builder
		if providerpkg.UsesAnthropicWire(runtime.Provider) {
			converter := llm.NewAnthropicStreamConverter(providerpkg.PublicModelID(runtime.Provider, input.Model))
			scanErr := llm.ScanSSE(result.Body, func(event llm.SSEEvent) error {
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
			collected = converter.UsageCollected()
			completion.WriteString(converter.ContentCollected())
			if scanErr != nil {
				streamStatus = fiber.StatusBadGateway
			}
			_, _ = w.WriteString("data: [DONE]\n\n")
			_ = w.Flush()
		} else {
			scanErr := llm.ScanSSE(result.Body, func(event llm.SSEEvent) error {
				payload := string(event.Data)
				if payload != "[DONE]" {
					collected = llm.MergeUsage(payload, collected)
					completion.WriteString(llm.ContentDelta(payload))
				}
				if _, writeErr := w.WriteString("data: " + payload + "\n\n"); writeErr != nil {
					return writeErr
				}
				return w.Flush()
			})
			if scanErr != nil {
				streamStatus = fiber.StatusBadGateway
			}
		}
		if collected.PromptTokens == 0 {
			collected.PromptTokens = req.EstimatePromptTokens()
		}
		if collected.CompletionTokens == 0 {
			collected.CompletionTokens = llm.EstimateTextTokens(completion.String())
		}
		summary := ""
		if streamStatus != fiber.StatusOK {
			summary = "provider chat test stream failed"
		}
		h.recordChatTest(logContext, collected, streamStatus, started, summary)
	})
}

type chatTestLogContext struct {
	TenantID      string
	APIKeyID      string
	CredentialID  string
	Provider      string
	Model         string
	UpstreamModel string
	Actor         entities.UsageActor
}

func newChatTestLogContext(session *entities.Session, credentialRecord *entities.Credential, runtime *entities.CredentialRuntime, model string) chatTestLogContext {
	context := chatTestLogContext{CredentialID: runtime.ID, Provider: runtime.Provider, Model: providerpkg.PublicModelID(runtime.Provider, model), UpstreamModel: model, Actor: entities.UsageActor{Type: entities.ActorLegacy}}
	if session != nil {
		context.APIKeyID = session.KeyID
		context.Actor = entities.UsageActor{UserID: session.UserID, Username: session.Username, OrganizationID: session.OrganizationID}
		switch {
		case session.IsMaster():
			context.APIKeyID = ""
			context.Actor.Type = entities.ActorMaster
			context.Actor.Username = "master"
		case session.PrincipalType == entities.PrincipalUser:
			context.Actor.Type = entities.ActorUser
		default:
			context.Actor.Type = entities.ActorOrganization
		}
	}
	if credentialRecord.OwnerTenantID != nil {
		context.TenantID = *credentialRecord.OwnerTenantID
		context.Actor.OrganizationID = *credentialRecord.OwnerTenantID
	} else {
		// Personal and global connection tests must not acquire organization
		// attribution merely because the operator is currently viewing an org.
		context.Actor.OrganizationID = ""
	}
	return context
}

func (h *CredentialConnectivity) recordChatTest(logContext chatTestLogContext, tokenUsage llm.Usage, status int, started time.Time, summary string) {
	if h.Usage == nil {
		return
	}
	h.Usage.Record(entities.UsageEvent{
		TS: time.Now(), TenantID: logContext.TenantID, ApiKeyID: logContext.APIKeyID, CredentialID: logContext.CredentialID,
		Provider: logContext.Provider, Model: logContext.Model, UpstreamModel: logContext.UpstreamModel,
		PromptTokens: tokenUsage.PromptTokens, CompletionTokens: tokenUsage.CompletionTokens,
		CacheReadTokens: tokenUsage.CacheReadTokens, CacheWriteTokens: tokenUsage.CacheWriteTokens,
		Priced: true, StatusCode: status, DurationMS: time.Since(started).Milliseconds(), Error: summary,
		ActorType: logContext.Actor.Type, UserID: logContext.Actor.UserID, Username: logContext.Actor.Username, OrganizationID: logContext.Actor.OrganizationID,
	})
}

// CodexResetCredits lists or consumes banked rate-limit reset credits for one authorized Codex account.
// @Summary Manage Codex reset credits
// @Description Lists available banked reset credits or consumes one selected credit for the credential account.
// @Tags credentials
// @Security BearerAuth
// @Param id path string true "Credential ID"
// @Param request body CodexResetCreditRequest false "Selected reset credit"
// @Success 200 {object} providerquota.ResetCreditList
// @Failure 400,401,403,404,409,500,502 {object} responseapi.ErrorResponse
// @Router /admin/credentials/{id}/reset-credits [get]
// @Router /admin/credentials/{id}/reset-credits [post]
func (h *CredentialConnectivity) CodexResetCredits(c fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	if !h.authorize(c) {
		return responseapi.For(c).NotFound("credential not found").Send()
	}
	if h.Quotas == nil {
		return responseapi.For(c).InternalError("provider quota service is unavailable").Send()
	}
	if c.Method() == fiber.MethodGet {
		result, err := h.Quotas.ListCodexResetCredits(c.Context(), c.Params("id"))
		if err != nil {
			return responseapi.For(c).Error(fiber.StatusBadGateway, "failed to load Codex reset credits", "upstream_error", "reset_credit_list_failed").Send()
		}
		return responseapi.For(c).Response().Status(fiber.StatusOK).Data(result).Send()
	}
	var input CodexResetCreditRequest
	if err := c.Bind().Body(&input); err != nil || strings.TrimSpace(input.SelectionToken) == "" || strings.TrimSpace(input.RequestID) == "" {
		return responseapi.For(c).BadRequest("selected reset credit and request ID are required").Send()
	}
	result, err := h.Quotas.ConsumeCodexResetCredit(c.Context(), c.Params("id"), input.SelectionToken, input.RequestID)
	if err != nil {
		return responseapi.For(c).Error(fiber.StatusBadGateway, "failed to redeem Codex reset credit", "upstream_error", "reset_credit_failed").Send()
	}
	return responseapi.For(c).Response().Status(fiber.StatusOK).Data(result).Send()
}
