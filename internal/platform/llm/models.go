package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
)

const maxModelCatalogBytes = 4 << 20

type upstreamModelList struct {
	Data   []upstreamModel `json:"data"`
	Models []upstreamModel `json:"models"`
}

type upstreamModel struct {
	ID                       string                         `json:"id"`
	Object                   string                         `json:"object"`
	Created                  int64                          `json:"created"`
	Model                    string                         `json:"model"`
	Name                     string                         `json:"name"`
	DisplayName              string                         `json:"display_name"`
	OwnedBy                  string                         `json:"owned_by"`
	Provider                 string                         `json:"provider"`
	Permission               []any                          `json:"permission"`
	Root                     string                         `json:"root"`
	Parent                   *string                        `json:"parent"`
	APIFormat                string                         `json:"api_format"`
	ContextLength            int64                          `json:"context_length"`
	MaxOutputTokens          int64                          `json:"max_output_tokens"`
	SupportedEndpoints       []string                       `json:"supported_endpoints"`
	Capabilities             map[string]any                 `json:"capabilities"`
	InputModalities          []string                       `json:"input_modalities"`
	OutputModalities         []string                       `json:"output_modalities"`
	MaxInputTokens           int64                          `json:"max_input_tokens"`
	MaxInputTokensCamel      int64                          `json:"maxInputTokens"`
	MaxContextWindow         int64                          `json:"max_context_window"`
	MaxContextWindowCamel    int64                          `json:"maxContextWindow"`
	Description              string                         `json:"description"`
	DefaultReasoningLevel    string                         `json:"default_reasoning_level"`
	DefaultReasoningCamel    string                         `json:"defaultReasoningLevel"`
	SupportedReasoningLevels []entities.ModelReasoningLevel `json:"supported_reasoning_levels"`
	ReasoningLevelsCamel     []entities.ModelReasoningLevel `json:"supportedReasoningLevels"`
	SupportsOriginalImage    bool                           `json:"supports_image_detail_original"`
	SupportsReasoningSummary bool                           `json:"supports_reasoning_summary_parameter"`
	SupportsParallelTools    bool                           `json:"supports_parallel_tool_calls"`
	SupportsVerbosity        bool                           `json:"support_verbosity"`
	DefaultVerbosity         string                         `json:"default_verbosity"`
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func decodeProviderModels(body io.Reader) ([]credential.ProviderModel, error) {
	limited := &io.LimitedReader{R: body, N: maxModelCatalogBytes + 1}
	payload, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read provider model list: %w", err)
	}
	if limited.N <= 0 {
		return nil, errors.New("provider model list exceeds 4 MiB")
	}
	var document upstreamModelList
	if err := json.Unmarshal(payload, &document); err != nil {
		var items []upstreamModel
		if arrayErr := json.Unmarshal(payload, &items); arrayErr != nil {
			return nil, fmt.Errorf("decode provider model list: %w", err)
		}
		document.Data = items
	}
	if document.Data == nil && document.Models != nil {
		document.Data = document.Models
	}
	if document.Data == nil {
		document.Data = []upstreamModel{}
	}
	if len(document.Data) == 0 && len(payload) == 0 {
		return nil, fmt.Errorf("decode provider model list: %w", err)
	}
	seen := make(map[string]credential.ProviderModel, len(document.Data))
	for _, model := range document.Data {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			id = strings.TrimSpace(model.Model)
		}
		if id == "" {
			id = strings.TrimSpace(model.Name)
		}
		if id == "" {
			continue
		}
		ownedBy := strings.TrimSpace(model.OwnedBy)
		if ownedBy == "" {
			ownedBy = strings.TrimSpace(model.Provider)
		}
		object := strings.TrimSpace(model.Object)
		if object == "" {
			object = "model"
		}
		maxInput := model.MaxInputTokens
		if maxInput == 0 {
			maxInput = model.MaxInputTokensCamel
		}
		maxContext := model.MaxContextWindow
		if maxContext == 0 {
			maxContext = model.MaxContextWindowCamel
		}
		defaultReasoning := model.DefaultReasoningLevel
		if defaultReasoning == "" {
			defaultReasoning = model.DefaultReasoningCamel
		}
		reasoningLevels := model.SupportedReasoningLevels
		if len(reasoningLevels) == 0 {
			reasoningLevels = model.ReasoningLevelsCamel
		}
		seen[id] = credential.ProviderModel{
			ID:                 id,
			Object:             object,
			Created:            model.Created,
			OwnedBy:            ownedBy,
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
			MaxInputTokens:     maxInput,
			MaxContextWindow:   maxContext,
			Name:               firstNonEmpty(model.DisplayName, model.Name),
			Description:        model.Description, DefaultReasoningLevel: defaultReasoning, SupportedReasoningLevels: reasoningLevels,
			SupportsOriginalImage: model.SupportsOriginalImage, SupportsReasoningSummary: model.SupportsReasoningSummary,
			SupportsParallelTools: model.SupportsParallelTools, SupportsVerbosity: model.SupportsVerbosity, DefaultVerbosity: model.DefaultVerbosity,
		}
	}
	models := make([]credential.ProviderModel, 0, len(seen))
	for _, model := range seen {
		models = append(models, model)
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models, nil
}

func (a *OpenAIAdapter) DiscoverModels(ctx context.Context, cr *entities.CredentialRuntime) ([]credential.ProviderModel, error) {
	token := cr.APIKey
	if cr.Kind == entities.KindOAuth {
		token = cr.OAuthAccess
	}
	headers := map[string]string{"Accept": "application/json", "Authorization": "Bearer " + token}
	applyOAuthProviderHeaders(headers, cr)
	load := func() (*entities.UpstreamResult, error) {
		return get(ctx, a.httpClient(), openAIEndpoint(cr.BaseURL, "models"), headers)
	}
	result, err := load()
	if err != nil {
		return nil, err
	}
	if result.StatusCode == http.StatusUnauthorized && cr.Kind == entities.KindOAuth && a.Refresh != nil {
		result.Body.Close()
		if err := a.Refresh(ctx, cr); err != nil {
			return nil, err
		}
		headers["Authorization"] = "Bearer " + cr.OAuthAccess
		result, err = load()
		if err != nil {
			return nil, err
		}
	}
	defer result.Body.Close()
	if result.StatusCode < http.StatusOK || result.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(result.Body, 64<<10))
		return nil, fmt.Errorf("provider model endpoint returned HTTP %d", result.StatusCode)
	}
	models, err := decodeProviderModels(result.Body)
	if err != nil {
		return nil, err
	}
	// Google's native model catalog prefixes resource names with "models/",
	// while its OpenAI-compatible chat endpoint expects the bare model ID.
	if cr.Provider == "gemini" {
		for i := range models {
			models[i].ID = strings.TrimPrefix(models[i].ID, "models/")
			models[i].Root = strings.TrimPrefix(models[i].Root, "models/")
		}
	}
	return models, nil
}

func (a *AnthropicAdapter) DiscoverModels(ctx context.Context, cr *entities.CredentialRuntime) ([]credential.ProviderModel, error) {
	if cr.Provider == "kimi-code" {
		return modelsFor("kimi-code", "k3", "kimi-for-coding", "kimi-for-coding-highspeed"), nil
	}
	base := anthropicBase(cr.BaseURL)
	headers, err := anthropicHeaders(cr)
	if err != nil {
		return nil, err
	}
	result, err := get(ctx, a.httpClient(), base+"/v1/models", headers)
	if err != nil {
		return nil, err
	}
	defer result.Body.Close()
	if result.StatusCode < http.StatusOK || result.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(result.Body, 64<<10))
		return nil, fmt.Errorf("provider model endpoint returned HTTP %d", result.StatusCode)
	}
	return decodeProviderModels(result.Body)
}

var _ credential.ModelDiscoverer = (*OpenAIAdapter)(nil)
var _ credential.ModelDiscoverer = (*AnthropicAdapter)(nil)
