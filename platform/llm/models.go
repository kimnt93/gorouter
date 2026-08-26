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
	ID            string `json:"id"`
	Model         string `json:"model"`
	Name          string `json:"name"`
	DisplayName   string `json:"display_name"`
	OwnedBy       string `json:"owned_by"`
	Provider      string `json:"provider"`
	ContextLength int64  `json:"context_length"`
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
		seen[id] = credential.ProviderModel{ID: id, OwnedBy: ownedBy, ContextLength: model.ContextLength}
	}
	models := make([]credential.ProviderModel, 0, len(seen))
	for _, model := range seen {
		models = append(models, model)
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models, nil
}

func (a *OpenAIAdapter) DiscoverModels(ctx context.Context, cr *entities.CredentialRuntime) ([]credential.ProviderModel, error) {
	result, err := get(ctx, a.httpClient(), openAIEndpoint(cr.BaseURL, "models"), map[string]string{
		"Accept":        "application/json",
		"Authorization": "Bearer " + cr.APIKey,
	})
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

func (a *AnthropicAdapter) DiscoverModels(ctx context.Context, cr *entities.CredentialRuntime) ([]credential.ProviderModel, error) {
	base := anthropicBase(cr.BaseURL)
	headers, err := anthropicHeaders(cr)
	if err != nil {
		return nil, err
	}
	result, err := get(ctx, a.HTTP, base+"/v1/models", headers)
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
