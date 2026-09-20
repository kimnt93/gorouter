package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
)

const devinCloudDefaultBaseURL = "https://api.devin.ai"

// DevinCloudAdapter validates Cognition cog_ credentials against the Devin v3
// API. Devin Cloud is an asynchronous agent-session API, not an OpenAI chat
// completion endpoint, so its catalog contains one capability identifier rather
// than pretending that the service exposes selectable foundation models.
type DevinCloudAdapter struct {
	HTTP *http.Client
}

type devinCloudProblem struct {
	Detail string `json:"detail"`
	Title  string `json:"title"`
}

func (a *DevinCloudAdapter) client() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return http.DefaultClient
}

func devinCloudBaseURL(value string) string {
	if value = strings.TrimRight(strings.TrimSpace(value), "/"); value != "" {
		return value
	}
	return devinCloudDefaultBaseURL
}

func validateDevinCloudCredential(cr *entities.CredentialRuntime) error {
	if cr == nil {
		return errors.New("missing Devin Cloud credential")
	}
	key := strings.TrimSpace(cr.APIKey)
	if !strings.HasPrefix(key, "cog_") || key == "cog_" {
		return errors.New("Devin Cloud requires a cog_ personal access token or service-user API key")
	}
	return nil
}

func (a *DevinCloudAdapter) Probe(ctx context.Context, cr *entities.CredentialRuntime) (int, error) {
	if err := validateDevinCloudCredential(cr); err != nil {
		return http.StatusBadRequest, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, devinCloudBaseURL(cr.BaseURL)+"/v3/self", nil)
	if err != nil {
		return 0, errors.New("failed to create Devin Cloud health request")
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cr.APIKey))
	req.Header.Set("Accept", "application/json")
	resp, err := a.client().Do(req)
	if err != nil {
		return 0, errors.New("Devin Cloud health request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return resp.StatusCode, nil
	}
	var problem devinCloudProblem
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&problem)
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return resp.StatusCode, errors.New("Devin Cloud authentication failed")
	case http.StatusForbidden:
		return resp.StatusCode, errors.New("Devin Cloud credential lacks API permission")
	case http.StatusTooManyRequests:
		return resp.StatusCode, errors.New("Devin Cloud rate limit reached")
	default:
		return resp.StatusCode, fmt.Errorf("Devin Cloud health check returned status %d", resp.StatusCode)
	}
}

func (a *DevinCloudAdapter) DiscoverModels(ctx context.Context, cr *entities.CredentialRuntime) ([]credential.ProviderModel, error) {
	status, err := a.Probe(ctx, cr)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, errors.New("Devin Cloud authentication failed")
	}
	// The documented v3 API offers asynchronous Devin sessions and modes, but
	// no foundation-model listing or per-session model selector. Keep this
	// stable capability record honest and do not fabricate provider model IDs.
	return []credential.ProviderModel{{
		ID:                 "devin",
		Name:               "Devin Cloud",
		Description:        "Asynchronous Cognition Devin agent sessions (model selection is managed by Devin)",
		Root:               "devin",
		Object:             "model",
		OwnedBy:            "devin",
		APIFormat:          "agent/sessions",
		SupportedEndpoints: []string{"agent/sessions"},
		InputModalities:    []string{"text"},
		OutputModalities:   []string{"text"},
	}}, nil
}
