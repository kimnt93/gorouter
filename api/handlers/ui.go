package handlers

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/kimnt93/gorouter/api/presenter"
	"github.com/kimnt93/gorouter/api/views"
	"github.com/kimnt93/gorouter/pkg/apikey"
	"github.com/kimnt93/gorouter/pkg/chat"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/modelroute"
	"github.com/kimnt93/gorouter/pkg/tenant"
	"github.com/kimnt93/gorouter/pkg/usage"
)

func renderTemplate(c fiber.Ctx, name string, data any) error {
	var b bytes.Buffer
	if err := views.Render(&b, name, data); err != nil {
		return presenter.ServerError(c, "template rendering failed")
	}
	c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
	return c.Send(b.Bytes())
}

func LoginPage(c fiber.Ctx) error { return renderTemplate(c, "login.html", nil) }

type UI struct {
	Cache       chat.PromptCache
	Usage       *usage.Service
	Keys        *apikey.Service
	Tenants     *tenant.Service
	Credentials *credential.Service
	Models      *modelroute.Service
}

type pageData struct {
	Title           string
	Session         *entities.Session
	CanUsage        bool
	CanKeys         bool
	CanCredentials  bool
	CanModels       bool
	CanCache        bool
	CanManageGlobal bool
	Cache           chat.CacheStats
	Keys            []entities.ApiKey
	Tenants         []entities.Tenant
	Credentials     []entities.Credential
	Models          []entities.ModelDef
	Summary         *entities.UsageSummary
	Recent          []entities.RecentEvent
	CreatedSecret   string
}

func (u *UI) page(c fiber.Ctx, title string) pageData {
	sess := SessionFrom(c)
	data := pageData{Title: title, Session: sess}
	if sess != nil {
		data.CanUsage = sess.Has(entities.ScopeUsageRead)
		data.CanKeys = sess.Has(entities.ScopeKeysManage)
		data.CanCredentials = sess.Has(entities.ScopeCredentialsManage)
		data.CanModels = sess.Has(entities.ScopeModelsManage)
		data.CanCache = sess.Has(entities.ScopeCachePurge)
		data.CanManageGlobal = sess.IsMaster()
	}
	if u.Cache != nil {
		data.Cache = u.Cache.Stats()
	}
	return data
}

func (u *UI) DashboardPage(c fiber.Ctx) error {
	return renderTemplate(c, "dashboard.html", u.page(c, "Dashboard"))
}

func (u *UI) CacheFragment(c fiber.Ctx) error {
	if u.Cache == nil {
		return renderTemplate(c, "cache.html", chat.CacheStats{})
	}
	return renderTemplate(c, "cache.html", u.Cache.Stats())
}

func (u *UI) KeysPage(c fiber.Ctx) error {
	data, err := u.loadKeys(c, "")
	if err != nil {
		return presenter.ServerError(c, "failed to load API keys")
	}
	return renderTemplate(c, "keys.html", data)
}

func (u *UI) KeysCreate(c fiber.Ctx) error {
	sess := SessionFrom(c)
	quota, err := optionalFloat(c.FormValue("quota"))
	if err != nil {
		return presenter.BadRequest(c, "quota must be a non-negative number")
	}
	rpm, err := optionalInt(c.FormValue("rpm"))
	if err != nil {
		return presenter.BadRequest(c, "RPM must be a positive integer")
	}
	in := apikey.CreateInput{TenantID: c.FormValue("tenant_id"), Name: c.FormValue("name"), Models: splitCSV(c.FormValue("models")), Scopes: splitCSV(c.FormValue("scopes")), MonthlyQuotaUSD: quota, RPM: rpm}
	var key *entities.ApiKey
	if sess != nil && !sess.IsMaster() {
		if !scopesAllowedBySession(sess, in.Scopes) {
			return presenter.Forbidden(c, "cannot grant scopes not held by the current session")
		}
		in.TenantID = sess.TenantID
		key, err = u.Keys.CreateForTenant(c.Context(), sess.TenantID, in)
	} else {
		key, err = u.Keys.Create(c.Context(), in)
	}
	if err != nil {
		return presenter.BadRequest(c, err.Error())
	}
	data, loadErr := u.loadKeys(c, key.Plaintext)
	if loadErr != nil {
		return presenter.ServerError(c, "key created but list refresh failed")
	}
	return renderTemplate(c, "keys.html", data)
}

func (u *UI) KeyToggle(c fiber.Ctx) error {
	enabled, err := strconv.ParseBool(c.FormValue("enabled"))
	if err != nil {
		return presenter.BadRequest(c, "enabled must be true or false")
	}
	sess := SessionFrom(c)
	if sess != nil && !sess.IsMaster() {
		err = u.Keys.PatchForTenant(c.Context(), sess.TenantID, c.Params("id"), &enabled, nil, nil, nil, nil)
	} else {
		err = u.Keys.Patch(c.Context(), c.Params("id"), &enabled, nil, nil, nil, nil)
	}
	if err != nil {
		return presenter.BadRequest(c, err.Error())
	}
	return u.redirectOrRefresh(c, "/ui/keys", u.KeysPage)
}

func (u *UI) KeyDelete(c fiber.Ctx) error {
	sess := SessionFrom(c)
	var err error
	if sess != nil && !sess.IsMaster() {
		err = u.Keys.DeleteForTenant(c.Context(), sess.TenantID, c.Params("id"))
	} else {
		err = u.Keys.Delete(c.Context(), c.Params("id"))
	}
	if err != nil {
		return presenter.BadRequest(c, err.Error())
	}
	return u.redirectOrRefresh(c, "/ui/keys", u.KeysPage)
}

func (u *UI) loadKeys(c fiber.Ctx, created string) (pageData, error) {
	data := u.page(c, "API keys")
	sess := SessionFrom(c)
	var err error
	if sess != nil && !sess.IsMaster() {
		data.Keys, err = u.Keys.ListByTenant(c.Context(), sess.TenantID)
	} else {
		data.Keys, err = u.Keys.List(c.Context())
	}
	if err != nil {
		return data, err
	}
	data.Tenants, err = u.Tenants.List(c.Context())
	if sess != nil && !sess.IsMaster() {
		data.Tenants = filterTenants(data.Tenants, sess.TenantID)
	}
	data.CreatedSecret = created
	return data, err
}

func (u *UI) CredentialsPage(c fiber.Ctx) error {
	data, err := u.loadCredentials(c)
	if err != nil {
		return presenter.ServerError(c, "failed to load credentials")
	}
	return renderTemplate(c, "credentials.html", data)
}

func (u *UI) CredentialsCreate(c fiber.Ctx) error {
	sess := SessionFrom(c)
	owner := optionalString(c.FormValue("owner_tenant_id"))
	if sess != nil && !sess.IsMaster() {
		owner = &sess.TenantID
	}
	_, err := u.Credentials.Create(c.Context(), entities.CredentialInput{Name: c.FormValue("name"), Provider: c.FormValue("provider"), Kind: c.FormValue("kind"), BaseURL: c.FormValue("base_url"), APIKey: c.FormValue("api_key"), OAuthAccess: c.FormValue("oauth_access"), OAuthRefresh: c.FormValue("oauth_refresh"), OwnerTenant: owner})
	if err != nil {
		return presenter.BadRequest(c, err.Error())
	}
	return u.redirectOrRefresh(c, "/ui/credentials", u.CredentialsPage)
}

func (u *UI) CredentialDelete(c fiber.Ctx) error {
	sess := SessionFrom(c)
	if sess != nil && !sess.IsMaster() && !u.tenantOwnsCredential(c, sess.TenantID, c.Params("id")) {
		return presenter.NotFound(c, "credential not found")
	}
	if err := u.Credentials.Delete(c.Context(), c.Params("id")); err != nil {
		if errors.Is(err, entities.ErrNotFound) {
			return presenter.NotFound(c, "credential not found")
		}
		return presenter.ServerError(c, "failed to delete credential")
	}
	return u.redirectOrRefresh(c, "/ui/credentials", u.CredentialsPage)
}

func (u *UI) loadCredentials(c fiber.Ctx) (pageData, error) {
	data := u.page(c, "Credentials")
	var err error
	data.Credentials, err = u.Credentials.List(c.Context())
	if err != nil {
		return data, err
	}
	sess := SessionFrom(c)
	if sess != nil && !sess.IsMaster() {
		data.Credentials = filterCredentials(data.Credentials, sess.TenantID)
	}
	data.Tenants, err = u.Tenants.List(c.Context())
	if sess != nil && !sess.IsMaster() {
		data.Tenants = filterTenants(data.Tenants, sess.TenantID)
	}
	return data, err
}

func (u *UI) ModelsPage(c fiber.Ctx) error {
	data := u.page(c, "Models and routes")
	var err error
	data.Models, err = u.Models.List(c.Context())
	if err != nil {
		return presenter.ServerError(c, "failed to load models")
	}
	data.Credentials, err = u.Credentials.List(c.Context())
	if err != nil {
		return presenter.ServerError(c, "failed to load credentials")
	}
	if sess := SessionFrom(c); sess != nil && !sess.IsMaster() {
		data.Credentials = filterCredentials(data.Credentials, sess.TenantID)
		allowed := make(map[string]bool, len(data.Credentials))
		for _, cred := range data.Credentials {
			allowed[cred.ID] = true
		}
		for i := range data.Models {
			routes := data.Models[i].Routes[:0]
			for _, route := range data.Models[i].Routes {
				if allowed[route.CredentialID] {
					routes = append(routes, route)
				}
			}
			data.Models[i].Routes = routes
		}
	}
	return renderTemplate(c, "models.html", data)
}

func (u *UI) ModelsCreate(c fiber.Ctx) error {
	if sess := SessionFrom(c); sess == nil || !sess.IsMaster() {
		return presenter.Forbidden(c, "only the master session can change global model routes")
	}
	priority, _ := strconv.Atoi(c.FormValue("priority"))
	weight, _ := strconv.Atoi(c.FormValue("weight"))
	if weight <= 0 {
		weight = 1
	}
	credentialID := strings.TrimSpace(c.FormValue("credential_id"))
	routes := []entities.ModelRoute{}
	if credentialID != "" {
		routes = append(routes, entities.ModelRoute{CredentialID: credentialID, Priority: priority, Weight: weight, Enabled: true})
	}
	err := u.Models.Upsert(c.Context(), entities.ModelDef{Name: strings.TrimSpace(c.FormValue("name")), Strategy: c.FormValue("strategy"), UpstreamModel: strings.TrimSpace(c.FormValue("upstream_model")), Enabled: true, Routes: routes})
	if err != nil {
		return presenter.BadRequest(c, err.Error())
	}
	return u.redirectOrRefresh(c, "/ui/models", u.ModelsPage)
}

func (u *UI) ModelDelete(c fiber.Ctx) error {
	if sess := SessionFrom(c); sess == nil || !sess.IsMaster() {
		return presenter.Forbidden(c, "only the master session can change global model routes")
	}
	if err := u.Models.Delete(c.Context(), c.Params("name")); err != nil {
		if errors.Is(err, entities.ErrNotFound) {
			return presenter.NotFound(c, "model not found")
		}
		return presenter.ServerError(c, "failed to delete model")
	}
	return u.redirectOrRefresh(c, "/ui/models", u.ModelsPage)
}

func (u *UI) UsagePage(c fiber.Ctx) error {
	data := u.page(c, "Usage")
	var err error
	since := time.Now().Add(-30 * 24 * time.Hour)
	sess := SessionFrom(c)
	if sess != nil && !sess.IsMaster() {
		data.Summary, err = u.Usage.SummaryForTenant(c.Context(), sess.TenantID, since)
	} else {
		data.Summary, err = u.Usage.Summary(c.Context(), since)
	}
	if err != nil {
		return presenter.ServerError(c, "failed to load usage summary")
	}
	if sess != nil && !sess.IsMaster() {
		data.Recent, err = u.Usage.RecentForTenant(c.Context(), sess.TenantID, 100)
	} else {
		data.Recent, err = u.Usage.Recent(c.Context(), 100)
	}
	if err != nil {
		return presenter.ServerError(c, "failed to load recent usage")
	}
	return renderTemplate(c, "usage.html", data)
}

func (u *UI) CachePage(c fiber.Ctx) error {
	return renderTemplate(c, "cache_page.html", u.page(c, "Prompt cache"))
}

func (u *UI) CacheFlush(c fiber.Ctx) error {
	if u.Cache != nil {
		u.Cache.Flush()
	}
	return u.redirectOrRefresh(c, "/ui/cache-page", u.CachePage)
}

func (u *UI) redirectOrRefresh(c fiber.Ctx, location string, refresh fiber.Handler) error {
	if c.Get("HX-Request") == "true" {
		return refresh(c)
	}
	return c.Redirect().To(location)
}

func (u *UI) tenantOwnsCredential(c fiber.Ctx, tenantID, credentialID string) bool {
	credentials, err := u.Credentials.List(c.Context())
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

func filterTenants(in []entities.Tenant, tenantID string) []entities.Tenant {
	out := make([]entities.Tenant, 0, 1)
	for _, item := range in {
		if item.ID == tenantID {
			out = append(out, item)
		}
	}
	return out
}

func filterCredentials(in []entities.Credential, tenantID string) []entities.Credential {
	out := make([]entities.Credential, 0, len(in))
	for _, item := range in {
		if item.OwnerTenantID != nil && *item.OwnerTenantID == tenantID {
			out = append(out, item)
		}
	}
	return out
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func optionalFloat(value string) (*float64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	n, err := strconv.ParseFloat(value, 64)
	if err != nil || n < 0 {
		return nil, strconv.ErrSyntax
	}
	return &n, nil
}

func optionalInt(value string) (*int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return nil, strconv.ErrSyntax
	}
	return &n, nil
}
