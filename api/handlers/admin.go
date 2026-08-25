package handlers

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/kimnt93/gorouter/api/presenter"
	"github.com/kimnt93/gorouter/pkg/apikey"
	"github.com/kimnt93/gorouter/pkg/auth"
	"github.com/kimnt93/gorouter/pkg/chat"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/modelroute"
	"github.com/kimnt93/gorouter/pkg/tenant"
	"github.com/kimnt93/gorouter/pkg/usage"
)

type Admin struct {
	Auth      *auth.Service
	TenantSvc *tenant.Service
	CredsSvc  *credential.Service
	KeysSvc   *apikey.Service
	ModelsSvc *modelroute.Service
	UsageSvc  *usage.Service
	Cache     chat.PromptCache
}

func (a *Admin) Verify(c fiber.Ctx) error {
	var body struct {
		Key string `json:"key"`
	}
	if err := c.Bind().Body(&body); err != nil || body.Key == "" {
		body.Key = c.FormValue("key")
	}
	sess, err := a.Auth.Login(c.Context(), body.Key)
	if err != nil {
		return presenter.Unauthorized(c, "invalid key")
	}
	token, err := a.Auth.IssueToken(sess)
	if err != nil {
		return presenter.ServerError(c, "failed to issue session")
	}
	c.Cookie(&fiber.Cookie{Name: sessionCookie, Value: token, HTTPOnly: true, SameSite: "Lax", MaxAge: int(auth.SessionTTL.Seconds()), Path: "/"})
	if c.Get("HX-Request") == "true" {
		c.Set("HX-Redirect", "/")
		return c.SendStatus(200)
	}
	if c.Method() == fiber.MethodPost && c.Get("Content-Type") == "application/x-www-form-urlencoded" {
		return c.Redirect().To("/")
	}
	return c.JSON(map[string]any{"ok": true, "role": sess.Role, "scopes": sess.Scopes})
}

func (a *Admin) Logout(c fiber.Ctx) error {
	c.Cookie(&fiber.Cookie{Name: sessionCookie, Value: "", Expires: time.Unix(0, 0), MaxAge: -1, HTTPOnly: true, Path: "/"})
	return c.Redirect().To("/login")
}

func (a *Admin) Tenants(c fiber.Ctx) error {
	if c.Method() == fiber.MethodGet {
		v, err := a.TenantSvc.List(c.Context())
		if err != nil {
			return presenter.ServerError(c, err.Error())
		}
		return c.JSON(v)
	}
	var b struct {
		Name string `json:"name"`
	}
	if err := c.Bind().Body(&b); err != nil || strings.TrimSpace(b.Name) == "" {
		return presenter.BadRequest(c, "name required")
	}
	v, err := a.TenantSvc.Create(c.Context(), strings.TrimSpace(b.Name))
	if err != nil {
		return presenter.ServerError(c, err.Error())
	}
	return c.Status(201).JSON(v)
}

func (a *Admin) Credentials(c fiber.Ctx) error {
	if c.Method() == fiber.MethodGet {
		v, err := a.CredsSvc.List(c.Context())
		if err != nil {
			return presenter.ServerError(c, err.Error())
		}
		return c.JSON(v)
	}
	var b struct {
		Name, Provider, Kind, BaseURL, APIKey, OAuthAccess, OAuthRefresh string
		OwnerTenant                                                      *string `json:"owner_tenant_id"`
	}
	if err := c.Bind().Body(&b); err != nil {
		return presenter.BadRequest(c, "invalid body")
	}
	if b.Kind == "" {
		b.Kind = entities.KindAPIKey
	}
	if b.Provider == "" {
		b.Provider = entities.ProviderOpenAICompatible
	}
	v, err := a.CredsSvc.Create(c.Context(), entities.CredentialInput{Name: b.Name, Provider: b.Provider, Kind: b.Kind, BaseURL: b.BaseURL, APIKey: b.APIKey, OAuthAccess: b.OAuthAccess, OAuthRefresh: b.OAuthRefresh, OwnerTenant: b.OwnerTenant})
	if err != nil {
		return presenter.ServerError(c, err.Error())
	}
	return c.Status(201).JSON(v)
}

func (a *Admin) CredentialByID(c fiber.Ctx) error {
	err := a.CredsSvc.Delete(c.Context(), c.Params("id"))
	if errors.Is(err, entities.ErrNotFound) {
		return presenter.NotFound(c, "credential not found")
	}
	if err != nil {
		return presenter.ServerError(c, err.Error())
	}
	return c.JSON(map[string]any{"ok": true})
}

func (a *Admin) KeysList(c fiber.Ctx) error {
	v, err := a.KeysSvc.List(c.Context())
	if err != nil {
		return presenter.ServerError(c, err.Error())
	}
	return c.JSON(v)
}
func (a *Admin) KeysCreate(c fiber.Ctx) error {
	var b struct {
		TenantID, Name  string
		Models, Scopes  []string
		MonthlyQuotaUSD *float64 `json:"monthly_quota_usd"`
		RPM             *int     `json:"rpm"`
	}
	if err := c.Bind().Body(&b); err != nil {
		return presenter.BadRequest(c, "invalid body")
	}
	if b.TenantID == "" {
		b.TenantID = "tenant_default"
	}
	if len(b.Models) == 0 {
		return presenter.BadRequest(c, "models must not be empty")
	}
	if len(b.Scopes) == 0 {
		b.Scopes = []string{entities.ScopeChat}
	}
	v, err := a.KeysSvc.Create(c.Context(), apikey.CreateInput{TenantID: b.TenantID, Name: b.Name, Models: b.Models, Scopes: b.Scopes, MonthlyQuotaUSD: b.MonthlyQuotaUSD, RPM: b.RPM})
	if err != nil {
		return presenter.ServerError(c, err.Error())
	}
	return c.Status(201).JSON(map[string]any{"id": v.ID, "tenant_id": v.TenantID, "name": v.Name, "key_prefix": v.SecretPrefix, "models": v.Models, "scopes": v.Scopes, "monthly_quota_usd": v.MonthlyQuotaUSD, "rpm": v.RPM, "enabled": v.Enabled, "plaintext": v.Plaintext})
}
func (a *Admin) KeysPatch(c fiber.Ctx) error {
	var b struct {
		Enabled *bool     `json:"enabled"`
		Models  *[]string `json:"models"`
		Scopes  *[]string `json:"scopes"`
		Quota   **float64 `json:"monthly_quota_usd"`
		RPM     **int     `json:"rpm"`
	}
	if err := c.Bind().Body(&b); err != nil {
		return presenter.BadRequest(c, "invalid body")
	}
	if err := a.KeysSvc.Patch(c.Context(), c.Params("id"), b.Enabled, b.Models, b.Scopes, b.Quota, b.RPM); err != nil {
		return presenter.ServerError(c, err.Error())
	}
	return c.JSON(map[string]any{"ok": true})
}
func (a *Admin) KeysDelete(c fiber.Ctx) error {
	if err := a.KeysSvc.Delete(c.Context(), c.Params("id")); err != nil {
		return presenter.ServerError(c, err.Error())
	}
	return c.JSON(map[string]any{"ok": true})
}

func (a *Admin) ModelsList(c fiber.Ctx) error {
	v, err := a.ModelsSvc.List(c.Context())
	if err != nil {
		return presenter.ServerError(c, err.Error())
	}
	return c.JSON(v)
}
func (a *Admin) ModelUpsert(c fiber.Ctx) error {
	var m entities.ModelDef
	if err := c.Bind().Body(&m); err != nil {
		return presenter.BadRequest(c, "invalid body")
	}
	m.Name = c.Params("name")
	if err := a.ModelsSvc.Upsert(c.Context(), m); err != nil {
		return presenter.ServerError(c, err.Error())
	}
	return c.JSON(map[string]any{"ok": true})
}
func (a *Admin) ModelDelete(c fiber.Ctx) error {
	if err := a.ModelsSvc.Delete(c.Context(), c.Params("name")); err != nil {
		return presenter.ServerError(c, err.Error())
	}
	return c.JSON(map[string]any{"ok": true})
}
func (a *Admin) Price(c fiber.Ctx) error {
	var p entities.Price
	if err := c.Bind().Body(&p); err != nil {
		return presenter.BadRequest(c, "invalid body")
	}
	if err := a.ModelsSvc.SetPrice(c.Context(), c.Params("model"), p); err != nil {
		return presenter.ServerError(c, err.Error())
	}
	return c.JSON(map[string]any{"ok": true})
}
func (a *Admin) Prices(c fiber.Ctx) error {
	v, err := a.ModelsSvc.Prices(c.Context())
	if err != nil {
		return presenter.ServerError(c, err.Error())
	}
	return c.JSON(v)
}
func (a *Admin) UsageSummary(c fiber.Ctx) error {
	since := time.Now().Add(-24 * time.Hour)
	switch c.Query("range") {
	case "7d":
		since = time.Now().Add(-7 * 24 * time.Hour)
	case "30d":
		since = time.Now().Add(-30 * 24 * time.Hour)
	}
	v, err := a.UsageSvc.Summary(c.Context(), since)
	if err != nil {
		return presenter.ServerError(c, err.Error())
	}
	return c.JSON(v)
}
func (a *Admin) UsageRecent(c fiber.Ctx) error {
	v, err := a.UsageSvc.Recent(c.Context(), 100)
	if err != nil {
		return presenter.ServerError(c, err.Error())
	}
	return c.JSON(v)
}
func (a *Admin) CacheStats(c fiber.Ctx) error { return c.JSON(a.Cache.Stats()) }
func (a *Admin) CacheFlush(c fiber.Ctx) error {
	a.Cache.Flush()
	return c.JSON(map[string]any{"ok": true})
}
