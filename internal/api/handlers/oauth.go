package handlers

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"

	responseapi "github.com/kimnt93/gorouter/internal/api"
	"github.com/kimnt93/gorouter/internal/api/presenter"
	"github.com/kimnt93/gorouter/pkg/entities"
	oauthpkg "github.com/kimnt93/gorouter/pkg/oauth"
)

type OAuthConnector struct {
	Service *oauthpkg.Service
}

type oauthCompleteBody = OAuthCompleteRequest

func oauthSessionBinding(session *entities.Session) string {
	if session == nil {
		return ""
	}
	return session.Role + ":" + session.KeyID + ":" + session.TenantID
}

// Start begins a provider OAuth flow bound to the current session.
// @Summary Start OAuth connection
// @Tags oauth
// @Security BearerAuth
// @Param provider path string true "Provider ID"
// @Success 200 {object} OAuthStartResponse
// @Failure 400,401,403,500 {object} presenter.Error
// @Router /admin/oauth/{provider}/start [post]
func (h *OAuthConnector) Start(c fiber.Ctx) error {
	if h.Service == nil {
		return presenter.ServerError(c, "OAuth connector is unavailable")
	}
	result, err := h.Service.StartContext(c.Context(), strings.ToLower(c.Params("provider")), oauthSessionBinding(SessionFrom(c)))
	if errors.Is(err, oauthpkg.ErrInvalidFlow) {
		return presenter.BadRequest(c, "unsupported OAuth provider")
	}
	if err != nil {
		return presenter.ServerError(c, "failed to start OAuth flow")
	}
	return responseapi.JSON(c, result)
}

// Complete exchanges an OAuth callback/device result and creates a credential.
// @Summary Complete OAuth connection
// @Tags oauth
// @Security BearerAuth
// @Param provider path string true "Provider ID"
// @Param request body OAuthCompleteRequest true "OAuth completion"
// @Success 201 {object} OAuthCompleteResponse
// @Success 202 {object} OAuthPendingResponse
// @Failure 400,401,403,500,502 {object} presenter.Error
// @Router /admin/oauth/{provider}/complete [post]
func (h *OAuthConnector) Complete(c fiber.Ctx) error {
	if h.Service == nil {
		return presenter.ServerError(c, "OAuth connector is unavailable")
	}
	var body oauthCompleteBody
	if err := c.Bind().Body(&body); err != nil || strings.TrimSpace(body.FlowID) == "" {
		return presenter.BadRequest(c, "flow_id is required")
	}
	session := SessionFrom(c)
	owner := body.OwnerTenant
	if session != nil && !session.IsMaster() {
		owner = &session.TenantID
	}
	created, err := h.Service.Complete(c.Context(), oauthpkg.CompleteInput{
		Provider: strings.ToLower(c.Params("provider")), FlowID: body.FlowID,
		Callback: body.Callback, Name: body.Name, OwnerTenant: owner,
		SessionBinding: oauthSessionBinding(session),
	})
	if errors.Is(err, oauthpkg.ErrInvalidFlow) || errors.Is(err, oauthpkg.ErrBadCallback) {
		return presenter.BadRequest(c, "OAuth flow expired or callback did not match")
	}
	if errors.Is(err, oauthpkg.ErrAuthorizationPending) {
		return responseapi.JSONStatus(c, fiber.StatusAccepted, OAuthPendingResponse{Status: "authorization_pending"})
	}
	if errors.Is(err, oauthpkg.ErrAccessDenied) {
		return presenter.BadRequest(c, "OAuth authorization was denied")
	}
	if err != nil {
		return presenter.Err(c, fiber.StatusBadGateway, "OAuth token exchange failed", "upstream_error", "oauth_exchange_failed")
	}
	return responseapi.JSONStatus(c, fiber.StatusCreated, OAuthCompleteResponse{ID: created.ID, Provider: created.Provider, Name: created.Name})
}
