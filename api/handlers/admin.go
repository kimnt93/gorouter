package handlers

import (
	"errors"
	"net/url"
	"strconv"
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
	"github.com/kimnt93/gorouter/pkg/provider"
	"github.com/kimnt93/gorouter/pkg/tenant"
	"github.com/kimnt93/gorouter/pkg/usage"
)

type okResponse struct {
	OK bool `json:"ok"`
}

func (a *Admin) Providers(c fiber.Ctx) error {
	return c.JSON(struct {
		Data []provider.Definition `json:"data"`
	}{Data: provider.Catalog()})
}

type loginResponse struct {
	OK     bool     `json:"ok"`
	Role   string   `json:"role"`
	Scopes []string `json:"scopes"`
}

type createdAPIKeyResponse struct {
	ID          string   `json:"id"`
	TenantID    string   `json:"tenant_id"`
	Name        string   `json:"name"`
	KeyPrefix   string   `json:"key_prefix"`
	Models      []string `json:"models"`
	Scopes      []string `json:"scopes"`
	QuotaUSD    *float64 `json:"quota_usd"`
	QuotaPeriod string   `json:"quota_period"`
	RPM         *int     `json:"rpm"`
	Enabled     bool     `json:"enabled"`
	Plaintext   string   `json:"plaintext"`
}

type Admin struct {
	Auth      *auth.Service
	TenantSvc *tenant.Service
	CredsSvc  *credential.Service
	KeysSvc   *apikey.Service
	ModelsSvc *modelroute.Service
	UsageSvc  *usage.Service
	Cache     chat.PromptCache
	Pricing   PriceCatalog
}

type priceEstimateResponse struct {
	Model          string                  `json:"model"`
	UpstreamModel  string                  `json:"upstream_model,omitempty"`
	Price          *entities.Price         `json:"price,omitempty"`
	CacheSupported bool                    `json:"cache_supported"`
	Estimates      entities.PriceEstimates `json:"estimates"`
}

func (a *Admin) PricingEstimate(c fiber.Ctx) error {
	if a.Pricing == nil {
		return presenter.Err(c, fiber.StatusServiceUnavailable, "pricing catalog is unavailable", "service_unavailable", "pricing_unavailable")
	}
	model := strings.TrimSpace(c.Query("model"))
	upstream := strings.TrimSpace(c.Query("upstream_model"))
	if model == "" && upstream == "" {
		return presenter.BadRequest(c, "model or upstream_model is required")
	}
	prompt, err := nonNegativeInt64(c.Query("prompt_tokens", "0"))
	if err != nil {
		return presenter.BadRequest(c, "prompt_tokens must be a non-negative integer")
	}
	completion, err := nonNegativeInt64(c.Query("completion_tokens", "0"))
	if err != nil {
		return presenter.BadRequest(c, "completion_tokens must be a non-negative integer")
	}
	response := priceEstimateResponse{Model: model, UpstreamModel: upstream, Estimates: a.Pricing.Estimates(model, upstream, prompt, completion)}
	if price, ok := a.Pricing.Resolve(model, upstream); ok {
		response.Price = &price
	}
	if catalog, ok := a.Pricing.Catalog(model, upstream); ok {
		response.CacheSupported = catalog.CacheSupported
	} else if response.Price != nil {
		response.CacheSupported = response.Price.CachedInputPerM > 0 || response.Price.CacheWritePerM > 0
	}
	return c.JSON(response)
}

func (a *Admin) PricingCatalog(c fiber.Ctx) error {
	if a.Pricing == nil {
		return presenter.Err(c, fiber.StatusServiceUnavailable, "pricing catalog is unavailable", "service_unavailable", "pricing_unavailable")
	}
	items := a.Pricing.CatalogPrices()
	query := strings.ToLower(strings.TrimSpace(c.Query("q")))
	if query != "" {
		filtered := make([]entities.CatalogPrice, 0)
		for _, item := range items {
			if strings.Contains(strings.ToLower(item.Model+" "+item.Name+" "+item.Provider), query) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	total := len(items)
	limit := 100
	if value, err := strconv.Atoi(c.Query("limit", "100")); err == nil && value > 0 && value <= 500 {
		limit = value
	}
	offset := 0
	if value, err := strconv.Atoi(c.Query("offset", "0")); err == nil && value > 0 {
		offset = value
	}
	if offset > len(items) {
		offset = len(items)
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return c.JSON(struct {
		Data   []entities.CatalogPrice `json:"data"`
		Total  int                     `json:"total"`
		Offset int                     `json:"offset"`
		Limit  int                     `json:"limit"`
	}{Data: items[offset:end], Total: total, Offset: offset, Limit: limit})
}

func nonNegativeInt64(value string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return 0, errors.New("invalid non-negative integer")
	}
	return parsed, nil
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
	if c.Method() == fiber.MethodPost && strings.Contains(c.Get("Content-Type"), "application/x-www-form-urlencoded") {
		return c.Redirect().To("/")
	}
	return c.JSON(loginResponse{OK: true, Role: sess.Role, Scopes: sess.Scopes})
}

func (a *Admin) Logout(c fiber.Ctx) error {
	c.Cookie(&fiber.Cookie{Name: sessionCookie, Value: "", Expires: time.Unix(0, 0), MaxAge: -1, HTTPOnly: true, Path: "/"})
	return c.Redirect().To("/login")
}

func (a *Admin) Tenants(c fiber.Ctx) error {
	if c.Method() == fiber.MethodGet {
		v, err := a.TenantSvc.List(c.Context())
		if err != nil {
			return presenter.ServerError(c, "failed to load tenants")
		}
		if sess := SessionFrom(c); sess != nil && !sess.IsMaster() {
			v = filterTenants(v, sess.TenantID)
		}
		return c.JSON(v)
	}
	if sess := SessionFrom(c); sess == nil || !sess.IsMaster() {
		return presenter.Forbidden(c, "only the master session can create tenants")
	}
	var b struct {
		Name string `json:"name"`
	}
	if err := c.Bind().Body(&b); err != nil || strings.TrimSpace(b.Name) == "" {
		return presenter.BadRequest(c, "name required")
	}
	v, err := a.TenantSvc.Create(c.Context(), strings.TrimSpace(b.Name))
	if err != nil {
		return presenter.ServerError(c, "failed to create tenant")
	}
	return c.Status(201).JSON(v)
}

func (a *Admin) Credentials(c fiber.Ctx) error {
	if c.Method() == fiber.MethodGet {
		v, err := a.CredsSvc.List(c.Context())
		if err != nil {
			return presenter.ServerError(c, "failed to load credentials")
		}
		if sess := SessionFrom(c); sess != nil && !sess.IsMaster() {
			v = filterCredentials(v, sess.TenantID)
		}
		return c.JSON(v)
	}
	var b struct {
		Name         string  `json:"name"`
		Provider     string  `json:"provider"`
		Kind         string  `json:"kind"`
		BaseURL      string  `json:"base_url"`
		APIKey       string  `json:"api_key"`
		OAuthAccess  string  `json:"oauth_access"`
		OAuthRefresh string  `json:"oauth_refresh"`
		OwnerTenant  *string `json:"owner_tenant_id"`
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
	if sess := SessionFrom(c); sess != nil && !sess.IsMaster() {
		b.OwnerTenant = &sess.TenantID
	}
	v, err := a.CredsSvc.Create(c.Context(), entities.CredentialInput{Name: b.Name, Provider: b.Provider, Kind: b.Kind, BaseURL: b.BaseURL, APIKey: b.APIKey, OAuthAccess: b.OAuthAccess, OAuthRefresh: b.OAuthRefresh, OwnerTenant: b.OwnerTenant})
	if err != nil {
		return presenter.BadRequest(c, err.Error())
	}
	return c.Status(201).JSON(v)
}

func (a *Admin) CredentialByID(c fiber.Ctx) error {
	sess := SessionFrom(c)
	if sess != nil && !sess.IsMaster() && !a.tenantOwnsCredential(c, sess.TenantID, c.Params("id")) {
		return presenter.NotFound(c, "credential not found")
	}
	if c.Method() == fiber.MethodPut {
		var b struct {
			Name          string  `json:"name"`
			BaseURL       string  `json:"base_url"`
			Status        string  `json:"status"`
			APIKey        string  `json:"api_key"`
			OAuthAccess   string  `json:"oauth_access"`
			OAuthRefresh  string  `json:"oauth_refresh"`
			OwnerTenantID *string `json:"owner_tenant_id"`
		}
		if err := c.Bind().Body(&b); err != nil {
			return presenter.BadRequest(c, "invalid body")
		}
		if sess != nil && !sess.IsMaster() {
			b.OwnerTenantID = &sess.TenantID
		}
		updated, err := a.CredsSvc.Update(c.Context(), c.Params("id"), entities.CredentialUpdate{
			Name: b.Name, BaseURL: b.BaseURL, Status: b.Status, APIKey: b.APIKey,
			OAuthAccess: b.OAuthAccess, OAuthRefresh: b.OAuthRefresh, OwnerTenant: b.OwnerTenantID,
		})
		if errors.Is(err, entities.ErrNotFound) {
			return presenter.NotFound(c, "credential not found")
		}
		if err != nil {
			return presenter.BadRequest(c, err.Error())
		}
		return c.JSON(updated)
	}
	err := a.CredsSvc.Delete(c.Context(), c.Params("id"))
	if errors.Is(err, entities.ErrNotFound) {
		return presenter.NotFound(c, "credential not found")
	}
	if err != nil {
		return presenter.ServerError(c, "failed to delete credential")
	}
	return c.JSON(okResponse{OK: true})
}

func (a *Admin) KeysList(c fiber.Ctx) error {
	sess := SessionFrom(c)
	var v []entities.ApiKey
	var err error
	if sess != nil && !sess.IsMaster() {
		v, err = a.KeysSvc.ListByTenant(c.Context(), sess.TenantID)
	} else {
		v, err = a.KeysSvc.List(c.Context())
	}
	if err != nil {
		return presenter.ServerError(c, "failed to load API keys")
	}
	return c.JSON(v)
}
func (a *Admin) KeysCreate(c fiber.Ctx) error {
	var b struct {
		TenantID    string   `json:"tenant_id"`
		Name        string   `json:"name"`
		Models      []string `json:"models"`
		Scopes      []string `json:"scopes"`
		QuotaUSD    *float64 `json:"quota_usd"`
		QuotaPeriod string   `json:"quota_period"`
		RPM         *int     `json:"rpm"`
	}
	if err := c.Bind().Body(&b); err != nil {
		return presenter.BadRequest(c, "invalid body")
	}
	if b.TenantID == "" {
		b.TenantID = "tenant_default"
	}
	if len(b.Scopes) == 0 {
		b.Scopes = []string{entities.ScopeChat}
	}
	in := apikey.CreateInput{TenantID: b.TenantID, Name: b.Name, Models: b.Models, Scopes: b.Scopes, QuotaUSD: b.QuotaUSD, QuotaPeriod: b.QuotaPeriod, RPM: b.RPM}
	sess := SessionFrom(c)
	var v *entities.ApiKey
	var err error
	if sess != nil && !sess.IsMaster() {
		if !scopesAllowedBySession(sess, b.Scopes) {
			return presenter.Forbidden(c, "cannot grant scopes not held by the current session")
		}
		v, err = a.KeysSvc.CreateForTenant(c.Context(), sess.TenantID, in)
	} else {
		v, err = a.KeysSvc.Create(c.Context(), in)
	}
	if err != nil {
		return presenter.BadRequest(c, err.Error())
	}
	return c.Status(201).JSON(createdAPIKeyResponse{ID: v.ID, TenantID: v.TenantID, Name: v.Name, KeyPrefix: v.SecretPrefix, Models: v.Models, Scopes: v.Scopes, QuotaUSD: v.QuotaUSD, QuotaPeriod: v.QuotaPeriod, RPM: v.RPM, Enabled: v.Enabled, Plaintext: v.Plaintext})
}
func (a *Admin) KeysPatch(c fiber.Ctx) error {
	var b struct {
		Enabled     *bool     `json:"enabled"`
		Models      *[]string `json:"models"`
		Scopes      *[]string `json:"scopes"`
		QuotaUSD    **float64 `json:"quota_usd"`
		QuotaPeriod *string   `json:"quota_period"`
		RPM         **int     `json:"rpm"`
	}
	if err := c.Bind().Body(&b); err != nil {
		return presenter.BadRequest(c, "invalid body")
	}
	quotaValue := b.QuotaUSD
	period := b.QuotaPeriod
	sess := SessionFrom(c)
	var err error
	if sess != nil && !sess.IsMaster() {
		if b.Scopes != nil && !scopesAllowedBySession(sess, *b.Scopes) {
			return presenter.Forbidden(c, "cannot grant scopes not held by the current session")
		}
		err = a.KeysSvc.PatchQuotaForTenant(c.Context(), sess.TenantID, c.Params("id"), b.Enabled, b.Models, b.Scopes, quotaValue, period, b.RPM)
	} else {
		err = a.KeysSvc.PatchQuota(c.Context(), c.Params("id"), b.Enabled, b.Models, b.Scopes, quotaValue, period, b.RPM)
	}
	if err != nil {
		if errors.Is(err, entities.ErrNotFound) {
			return presenter.NotFound(c, "API key not found")
		}
		return presenter.BadRequest(c, err.Error())
	}
	return c.JSON(okResponse{OK: true})
}
func (a *Admin) KeysDelete(c fiber.Ctx) error {
	sess := SessionFrom(c)
	var err error
	if sess != nil && !sess.IsMaster() {
		err = a.KeysSvc.DeleteForTenant(c.Context(), sess.TenantID, c.Params("id"))
	} else {
		err = a.KeysSvc.Delete(c.Context(), c.Params("id"))
	}
	if err != nil {
		if errors.Is(err, entities.ErrNotFound) {
			return presenter.NotFound(c, "API key not found")
		}
		return presenter.ServerError(c, "failed to delete API key")
	}
	return c.JSON(okResponse{OK: true})
}

func (a *Admin) ModelsList(c fiber.Ctx) error {
	v, err := a.ModelsSvc.List(c.Context())
	if err != nil {
		return presenter.ServerError(c, "failed to load models")
	}
	if sess := SessionFrom(c); sess != nil && !sess.IsMaster() {
		credentials, listErr := a.CredsSvc.List(c.Context())
		if listErr != nil {
			return presenter.ServerError(c, "failed to filter model routes")
		}
		allowed := map[string]bool{}
		for _, cred := range filterCredentials(credentials, sess.TenantID) {
			allowed[cred.ID] = true
		}
		for i := range v {
			routes := v[i].Routes[:0]
			for _, route := range v[i].Routes {
				if allowed[route.CredentialID] {
					routes = append(routes, route)
				}
			}
			v[i].Routes = routes
		}
	}
	return c.JSON(v)
}
func (a *Admin) ModelUpsert(c fiber.Ctx) error {
	if sess := SessionFrom(c); sess == nil || !sess.IsMaster() {
		return presenter.Forbidden(c, "only the master session can change global model routes")
	}
	var m entities.ModelDef
	if err := c.Bind().Body(&m); err != nil {
		return presenter.BadRequest(c, "invalid body")
	}
	m.Name = decodedPathParam(c, "name")
	if err := a.ModelsSvc.Upsert(c.Context(), m); err != nil {
		return presenter.BadRequest(c, err.Error())
	}
	return c.JSON(okResponse{OK: true})
}
func (a *Admin) ModelDelete(c fiber.Ctx) error {
	if sess := SessionFrom(c); sess == nil || !sess.IsMaster() {
		return presenter.Forbidden(c, "only the master session can change global model routes")
	}
	if err := a.ModelsSvc.Delete(c.Context(), decodedPathParam(c, "name")); err != nil {
		return presenter.ServerError(c, "failed to delete model")
	}
	return c.JSON(okResponse{OK: true})
}
func (a *Admin) Price(c fiber.Ctx) error {
	if sess := SessionFrom(c); sess == nil || !sess.IsMaster() {
		return presenter.Forbidden(c, "only the master session can change global prices")
	}
	if c.Method() == fiber.MethodDelete {
		if err := a.ModelsSvc.DeletePrice(c.Context(), decodedPathParam(c, "model")); err != nil {
			if errors.Is(err, entities.ErrNotFound) {
				return presenter.NotFound(c, "price not found")
			}
			return presenter.ServerError(c, "failed to delete price")
		}
		return c.JSON(okResponse{OK: true})
	}
	var p entities.Price
	if err := c.Bind().Body(&p); err != nil {
		return presenter.BadRequest(c, "invalid body")
	}
	if err := a.ModelsSvc.SetPrice(c.Context(), decodedPathParam(c, "model"), p); err != nil {
		return presenter.BadRequest(c, err.Error())
	}
	return c.JSON(okResponse{OK: true})
}

func decodedPathParam(c fiber.Ctx, name string) string {
	value := c.Params(name)
	if decoded, err := url.PathUnescape(value); err == nil {
		return decoded
	}
	return value
}
func (a *Admin) Prices(c fiber.Ctx) error {
	v, err := a.ModelsSvc.Prices(c.Context())
	if err != nil {
		return presenter.ServerError(c, "failed to load prices")
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
	sess := SessionFrom(c)
	var v *entities.UsageSummary
	var err error
	if sess != nil && !sess.IsMaster() {
		v, err = a.UsageSvc.SummaryForTenant(c.Context(), sess.TenantID, since)
	} else {
		v, err = a.UsageSvc.Summary(c.Context(), since)
	}
	if err != nil {
		return presenter.ServerError(c, "failed to load usage summary")
	}
	return c.JSON(v)
}
func (a *Admin) UsageRecent(c fiber.Ctx) error {
	sess := SessionFrom(c)
	var v []entities.RecentEvent
	var err error
	if sess != nil && !sess.IsMaster() {
		v, err = a.UsageSvc.RecentForTenant(c.Context(), sess.TenantID, 100)
	} else {
		v, err = a.UsageSvc.Recent(c.Context(), 100)
	}
	if err != nil {
		return presenter.ServerError(c, "failed to load recent usage")
	}
	return c.JSON(v)
}
func (a *Admin) CacheStats(c fiber.Ctx) error {
	if a.Cache == nil {
		return c.JSON(chat.CacheStats{})
	}
	return c.JSON(a.Cache.Stats())
}
func (a *Admin) CacheFlush(c fiber.Ctx) error {
	if a.Cache != nil {
		a.Cache.Flush()
	}
	return c.JSON(okResponse{OK: true})
}

func (a *Admin) tenantOwnsCredential(c fiber.Ctx, tenantID, credentialID string) bool {
	credentials, err := a.CredsSvc.List(c.Context())
	if err != nil {
		return false
	}
	for _, cred := range credentials {
		if cred.ID == credentialID && cred.OwnerTenantID != nil && *cred.OwnerTenantID == tenantID {
			return true
		}
	}
	return false
}

func scopesAllowedBySession(session *entities.Session, scopes []string) bool {
	if session == nil {
		return false
	}
	for _, scope := range scopes {
		if !session.Has(scope) {
			return false
		}
	}
	return true
}
