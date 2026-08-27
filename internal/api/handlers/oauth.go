package handlers

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"

	responseapi "github.com/kimnt93/gorouter/internal/api"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/identity"
	oauthpkg "github.com/kimnt93/gorouter/pkg/oauth"
)

type OAuthConnector struct {
	Service    *oauthpkg.Service
	Identities identity.Repository
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
// @Description Starts a provider-specific OAuth or device authorization flow bound to the current session.
// @Tags oauth
// @Security BearerAuth
// @Param provider path string true "Provider ID"
// @Success 200 {object} OAuthStartResponse
// @Failure 400,401,403,500 {object} responseapi.ErrorResponse
// @Router /admin/oauth/{provider}/start [post]
func (h *OAuthConnector) Start(c fiber.Ctx) error {
	if h.Service == nil {
		return responseapi.For(c).InternalError("OAuth connector is unavailable").Send()
	}
	result, err := h.Service.StartContext(c.Context(), strings.ToLower(c.Params("provider")), oauthSessionBinding(SessionFrom(c)))
	if errors.Is(err, oauthpkg.ErrInvalidFlow) {
		return responseapi.For(c).BadRequest("unsupported OAuth provider").Send()
	}
	if err != nil {
		return responseapi.For(c).InternalError("failed to start OAuth flow").Send()
	}
	return responseapi.For(c).Response().Status(fiber.StatusOK).Data(result).Send()
}

// Complete exchanges an OAuth callback/device result and creates a credential.
// @Summary Complete OAuth connection
// @Description Completes a pending OAuth flow and persists the resulting credential with encrypted tokens.
// @Tags oauth
// @Security BearerAuth
// @Param provider path string true "Provider ID"
// @Param request body OAuthCompleteRequest true "OAuth completion"
// @Success 201 {object} OAuthCompleteResponse
// @Success 202 {object} OAuthPendingResponse
// @Failure 400,401,403,500,502 {object} responseapi.ErrorResponse
// @Router /admin/oauth/{provider}/complete [post]
func (h *OAuthConnector) Complete(c fiber.Ctx) error {
	if h.Service == nil {
		return responseapi.For(c).InternalError("OAuth connector is unavailable").Send()
	}
	var body oauthCompleteBody
	if err := c.Bind().Body(&body); err != nil || strings.TrimSpace(body.FlowID) == "" {
		return responseapi.For(c).BadRequest("flow_id is required").Send()
	}
	session := SessionFrom(c)
	ownerTenant, ownerUserID, ownerErr := credentialOwner(c.Context(), session, body.OwnerType, "", body.OwnerOrganizationID, h.Identities)
	if ownerErr != nil {
		if session != nil && !session.IsMaster() {
			return responseapi.For(c).Forbidden(ownerErr.Error()).Send()
		}
		return responseapi.For(c).BadRequest(ownerErr.Error()).Send()
	}
	created, err := h.Service.Complete(c.Context(), oauthpkg.CompleteInput{
		Provider: strings.ToLower(c.Params("provider")), FlowID: body.FlowID,
		Callback: body.Callback, Name: body.Name, OwnerTenant: ownerTenant, OwnerUserID: ownerUserID,
		SessionBinding: oauthSessionBinding(session),
	})
	if errors.Is(err, oauthpkg.ErrInvalidFlow) || errors.Is(err, oauthpkg.ErrBadCallback) {
		return responseapi.For(c).BadRequest("OAuth flow expired or callback did not match").Send()
	}
	if errors.Is(err, oauthpkg.ErrAuthorizationPending) {
		return responseapi.For(c).Response().Status(fiber.StatusAccepted).Data(OAuthPendingResponse{Status: "authorization_pending"}).Send()
	}
	if errors.Is(err, oauthpkg.ErrAccessDenied) {
		return responseapi.For(c).BadRequest("OAuth authorization was denied").Send()
	}
	if err != nil {
		return responseapi.For(c).Error(fiber.StatusBadGateway, "OAuth token exchange failed", "upstream_error", "oauth_exchange_failed").Send()
	}
	return responseapi.For(c).Response().Status(fiber.StatusCreated).Data(OAuthCompleteResponse{ID: created.ID, Provider: created.Provider, Name: created.Name}).Send()
}
