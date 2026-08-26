package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
)

// OpenCodeZenAdapter selects the wire protocol advertised by OpenCode Zen.
// Zen exposes a single model catalog, but models are served by different
// endpoints. In particular GPT, Grok, and Muse models use Responses while
// DeepSeek and other OpenAI-compatible models use Chat Completions.
type OpenCodeZenAdapter struct{ HTTP *http.Client }

func (a *OpenCodeZenAdapter) client() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return NewHTTPClient()
}

func openCodeZenUsesResponses(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(model, "gpt-") ||
		strings.HasPrefix(model, "grok-") ||
		strings.HasPrefix(model, "muse-spark-")
}

func (a *OpenCodeZenAdapter) Send(ctx context.Context, cr *entities.CredentialRuntime, model string, raw []byte) (*entities.UpstreamResult, error) {
	if !openCodeZenUsesResponses(model) {
		return (&OpenAIAdapter{HTTP: a.client()}).Send(ctx, cr, model, raw)
	}
	var input ChatRequest
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, fmt.Errorf("parse OpenCode Zen request: %w", err)
	}
	payload := toCodexRequest(&input, model)
	payload.Stream = true // collectors convert the Responses stream for either client mode
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	headers := map[string]string{
		"Accept":        "text/event-stream",
		"Authorization": "Bearer " + cr.APIKey,
	}
	result, err := postJSON(ctx, a.client(), openAIEndpoint(cr.BaseURL, "responses"), headers, encoded)
	if err != nil || result.StatusCode < 200 || result.StatusCode >= 300 {
		return result, err
	}
	if input.Stream {
		reader, writer := io.Pipe()
		upstream := result.Body
		go func() {
			defer upstream.Close()
			_ = writer.CloseWithError(transformCodexStream(upstream, writer, model))
		}()
		result.Body = reader
		result.Header = http.Header{"Content-Type": []string{"text/event-stream"}}
		return result, nil
	}
	response, err := collectCodexResponse(result.Body, model)
	result.Body.Close()
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(response)
	result.Body = io.NopCloser(bytes.NewReader(body))
	result.Header = http.Header{"Content-Type": []string{"application/json"}}
	return result, nil
}

func (a *OpenCodeZenAdapter) Probe(ctx context.Context, cr *entities.CredentialRuntime) (int, error) {
	return (&OpenAIAdapter{HTTP: a.client()}).Probe(ctx, cr)
}

func (a *OpenCodeZenAdapter) DiscoverModels(ctx context.Context, cr *entities.CredentialRuntime) ([]credential.ProviderModel, error) {
	models, err := (&OpenAIAdapter{HTTP: a.client()}).DiscoverModels(ctx, cr)
	if err != nil {
		return nil, err
	}
	for index := range models {
		if openCodeZenUsesResponses(models[index].ID) {
			models[index].APIFormat = "responses"
			models[index].SupportedEndpoints = []string{"responses"}
		} else if models[index].APIFormat == "" {
			models[index].APIFormat = "chat/completions"
			models[index].SupportedEndpoints = []string{"chat/completions"}
		}
	}
	return models, nil
}

var _ credential.ConnectivityProber = (*OpenCodeZenAdapter)(nil)
var _ credential.ModelDiscoverer = (*OpenCodeZenAdapter)(nil)
var _ entities.Upstream = (*OpenCodeZenAdapter)(nil)
