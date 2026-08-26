package handlers

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/kimnt93/gorouter/api/presenter"
	"github.com/kimnt93/gorouter/pkg/entities"
	oauthpkg "github.com/kimnt93/gorouter/pkg/oauth"
)

type OAuthConnector struct {
	Service *oauthpkg.Service
}

type oauthCompleteBody struct {
	FlowID      string  `json:"flow_id"`
	Callback    string  `json:"callback"`
	Name        string  `json:"name"`
	OwnerTenant *string `json:"owner_tenant_id"`
}

func oauthSessionBinding(session *entities.Session) string {
	if session == nil {
		return ""
	}
	return session.Role + ":" + session.KeyID + ":" + session.TenantID
}

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
	return c.JSON(result)
}

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
		return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"status": "authorization_pending"})
	}
	if errors.Is(err, oauthpkg.ErrAccessDenied) {
		return presenter.BadRequest(c, "OAuth authorization was denied")
	}
	if err != nil {
		return presenter.Err(c, fiber.StatusBadGateway, "OAuth token exchange failed", "upstream_error", "oauth_exchange_failed")
	}
	return c.Status(fiber.StatusCreated).JSON(struct {
		ID       string `json:"id"`
		Provider string `json:"provider"`
		Name     string `json:"name"`
	}{ID: created.ID, Provider: created.Provider, Name: created.Name})
}
