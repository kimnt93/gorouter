package handlers

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	"github.com/kimnt93/gorouter/api/presenter"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
)

// CredentialConnectivity is registered separately from Admin so provider
// adapters stay explicit dependencies at the HTTP boundary.
type CredentialConnectivity struct {
	Credentials *credential.Service
	OpenAI      credential.ConnectivityProber
	Anthropic   credential.ConnectivityProber
}

func (h *CredentialConnectivity) Test(c fiber.Ctx) error {
	if sess := SessionFrom(c); sess != nil && !sess.IsMaster() {
		credentials, listErr := h.Credentials.List(c.Context())
		if listErr != nil {
			return presenter.ServerError(c, "failed to authorize credential")
		}
		owned := false
		for _, candidate := range credentials {
			if candidate.ID == c.Params("id") && candidate.OwnerTenantID != nil && *candidate.OwnerTenantID == sess.TenantID {
				owned = true
				break
			}
		}
		if !owned {
			return presenter.NotFound(c, "credential not found")
		}
	}
	result, err := h.Credentials.TestConnectivity(c.Context(), c.Params("id"), map[string]credential.ConnectivityProber{
		entities.ProviderOpenAICompatible: h.OpenAI,
		entities.ProviderAnthropic:        h.Anthropic,
	})
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
