package handlers

import (
	"context"
	"encoding/base64"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	responseapi "github.com/kimnt93/gorouter/internal/api"
	"github.com/kimnt93/gorouter/pkg/apikey"
	"github.com/kimnt93/gorouter/pkg/auth"
	"github.com/kimnt93/gorouter/pkg/chat"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/identity"
	"github.com/kimnt93/gorouter/pkg/modelroute"
	"github.com/kimnt93/gorouter/pkg/policy"
	"github.com/kimnt93/gorouter/pkg/provider"
	"github.com/kimnt93/gorouter/pkg/tenant"
	"github.com/kimnt93/gorouter/pkg/usage"
)

type okResponse = OKResponse

// Providers returns the built-in provider catalog.
// @Summary List providers
// @Description Returns the static provider catalog, including supported protocols, authentication methods, and model prefixes.
// @Tags providers
// @Security BearerAuth
// @Success 200 {object} ProviderListResponse
// @Failure 401,403 {object} responseapi.ErrorResponse
// @Router /admin/providers [get]
func (a *Admin) Providers(c fiber.Ctx) error {
	catalog := provider.Catalog()
	if a.OAuthAvailable != nil {
		for i := range catalog {
			if catalog[i].Auth == provider.AuthOAuth {
				catalog[i].OAuthSupported = a.OAuthAvailable(catalog[i].ID)
			}
		}
	}
	return responseapi.For(c).Response().Status(fiber.StatusOK).Data(ProviderListResponse{Data: catalog}).Send()
}

type loginResponse = LoginResponse
type createdAPIKeyResponse = CreatedAPIKeyResponse

type Admin struct {
	Auth           *auth.Service
	TenantSvc      *tenant.Service
	CredsSvc       *credential.Service
	KeysSvc        *apikey.Service
	ModelsSvc      *modelroute.Service
	UsageSvc       *usage.Service
	Cache          chat.PromptCache
	Pricing        PriceCatalog
	IdentitySvc    *identity.Service
	IdentityRepo   identity.Repository
	AuditRepo      entities.AuditRepository
	OAuthAvailable func(string) bool
}

type priceEstimateResponse = PricingEstimateResponse

// PricingEstimate calculates typed cost estimates for token counts.
// @Summary Estimate model cost
// @Description Calculates an estimated cost from a public or upstream model and token counts using the effective price.
// @Tags pricing
// @Security BearerAuth
// @Param model query string false "Public model"
// @Param upstream_model query string false "Upstream model"
// @Param prompt_tokens query int false "Prompt tokens" minimum(0)
// @Param completion_tokens query int false "Completion tokens" minimum(0)
// @Success 200 {object} PricingEstimateResponse
// @Failure 400,401,403,503 {object} responseapi.ErrorResponse
// @Router /admin/pricing/estimate [get]
func (a *Admin) PricingEstimate(c fiber.Ctx) error {
	if a.Pricing == nil {
		return responseapi.For(c).Error(fiber.StatusServiceUnavailable, "pricing catalog is unavailable", "service_unavailable", "pricing_unavailable").Send()
	}
	model := strings.TrimSpace(c.Query("model"))
	upstream := strings.TrimSpace(c.Query("upstream_model"))
	if model == "" && upstream == "" {
		return responseapi.For(c).BadRequest("model or upstream_model is required").Send()
	}
	prompt, err := nonNegativeInt64(c.Query("prompt_tokens", "0"))
	if err != nil {
		return responseapi.For(c).BadRequest("prompt_tokens must be a non-negative integer").Send()
	}
	completion, err := nonNegativeInt64(c.Query("completion_tokens", "0"))
	if err != nil {
		return responseapi.For(c).BadRequest("completion_tokens must be a non-negative integer").Send()
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
	return responseapi.For(c).Response().Status(fiber.StatusOK).Data(response).Send()
}

// PricingCatalog returns the imported price catalog.
// @Summary List catalog prices
// @Description Returns imported catalog prices with optional text filtering and offset pagination.
// @Tags pricing
// @Security BearerAuth
// @Param q query string false "Search"
// @Param limit query int false "Page size" maximum(500)
// @Param offset query int false "Offset"
// @Success 200 {object} PricingCatalogResponse
// @Failure 401,403,503 {object} responseapi.ErrorResponse
// @Router /admin/pricing/catalog [get]
func (a *Admin) PricingCatalog(c fiber.Ctx) error {
	if a.Pricing == nil {
		return responseapi.For(c).Error(fiber.StatusServiceUnavailable, "pricing catalog is unavailable", "service_unavailable", "pricing_unavailable").Send()
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
	return responseapi.For(c).Response().
		Status(fiber.StatusOK).
		Data(items[offset:end]).
		Paging(total, offset, limit).
		Send()
}

func nonNegativeInt64(value string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return 0, errors.New("invalid non-negative integer")
	}
	return parsed, nil
}

// Verify authenticates a master or API key and issues a signed session cookie.
// @Summary Log in
// @Description Authenticates a master secret or API key and issues a signed browser session.
// @Tags authentication
// @Accept json
// @Produce json
// @Param request body LoginRequest true "Login key"
// @Success 200 {object} LoginResponse
// @Failure 400,401,500 {object} responseapi.ErrorResponse
// @Router /login [post]
func (a *Admin) Verify(c fiber.Ctx) error {
	var body LoginRequest
	if err := c.Bind().Body(&body); err != nil || body.Key == "" {
		body.Key = c.FormValue("key")
	}
	sess, err := a.Auth.Login(c.Context(), body.Key)
	if err != nil {
		return responseapi.For(c).Unauthorized("invalid key").Send()
	}
	token, err := a.Auth.IssueToken(sess)
	if err != nil {
		return responseapi.For(c).InternalError("failed to issue session").Send()
	}
	c.Cookie(&fiber.Cookie{Name: sessionCookie, Value: token, HTTPOnly: true, SameSite: "Lax", MaxAge: int(auth.SessionTTL.Seconds()), Path: "/"})
	if c.Get("HX-Request") == "true" {
		c.Set("HX-Redirect", "/")
		return c.SendStatus(200)
	}
	if c.Method() == fiber.MethodPost && strings.Contains(c.Get("Content-Type"), "application/x-www-form-urlencoded") {
		return c.Redirect().To("/")
	}
	return responseapi.For(c).Response().Status(fiber.StatusOK).Data(sessionResponse(sess)).Send()
}

// Session returns safe metadata for the current browser or bearer session.
// @Summary Get current session
// @Description Returns safe metadata for the current authenticated browser or bearer session.
// @Tags authentication
// @Security BearerAuth
// @Success 200 {object} LoginResponse
// @Failure 401 {object} responseapi.ErrorResponse
// @Router /admin/session [get]
func (a *Admin) Session(c fiber.Ctx) error {
	return responseapi.For(c).Response().Status(fiber.StatusOK).Data(sessionResponse(SessionFrom(c))).Send()
}

func sessionResponse(sess *entities.Session) loginResponse {
	if sess == nil {
		return loginResponse{Scopes: []string{}}
	}
	return loginResponse{OK: true, Role: sess.Role, PrincipalType: sess.PrincipalType, UserID: sess.UserID, Username: sess.Username, OrganizationID: sess.OrganizationID, MembershipRole: sess.MembershipRole, Scopes: append([]string(nil), sess.Scopes...)}
}

// Logout clears the signed session cookie.
// @Summary Log out
// @Description Clears the current browser session cookie and redirects to the login page.
// @Tags authentication
// @Success 302
// @Router /logout [post]
func (a *Admin) Logout(c fiber.Ctx) error {
	c.Cookie(&fiber.Cookie{Name: sessionCookie, Value: "", Expires: time.Unix(0, 0), MaxAge: -1, HTTPOnly: true, Path: "/"})
	return c.Redirect().To("/login")
}

// Tenants is the deprecated organization list/create compatibility alias.
// @Summary Deprecated organization alias
// @Description Maintains the legacy tenant endpoint while directing clients to the organization API.
// @Tags organizations
// @Deprecated
// @Security BearerAuth
// @Param request body OrganizationCreateRequest false "Required for POST"
// @Success 200 {object} OrganizationListResponse
// @Success 201 {object} entities.Organization
// @Failure 400,401,403,500 {object} responseapi.ErrorResponse
// @Router /admin/tenants [get]
// @Router /admin/tenants [post]
func (a *Admin) Tenants(c fiber.Ctx) error {
	c.Set("Deprecation", "true")
	c.Set("Sunset", "Wed, 26 Aug 2027 00:00:00 GMT")
	c.Set("Link", `</admin/organizations>; rel="successor-version"`)
	if a.IdentitySvc != nil && a.IdentityRepo != nil {
		return a.Organizations(c)
	}
	if c.Method() == fiber.MethodGet {
		v, err := a.TenantSvc.List(c.Context())
		if err != nil {
			return responseapi.For(c).InternalError("failed to load tenants").Send()
		}
		if sess := SessionFrom(c); sess != nil && !sess.IsMaster() {
			v = filterTenants(v, sess.TenantID)
		}
		return responseapi.For(c).Response().Status(fiber.StatusOK).Data(v).Send()
	}
	if sess := SessionFrom(c); sess == nil || !sess.IsMaster() {
		return responseapi.For(c).Forbidden("only the master session can create tenants").Send()
	}
	var b TenantCreateRequest
	if err := c.Bind().Body(&b); err != nil || strings.TrimSpace(b.Name) == "" {
		return responseapi.For(c).BadRequest("name required").Send()
	}
	v, err := a.TenantSvc.Create(c.Context(), strings.TrimSpace(b.Name))
	if err != nil {
		return responseapi.For(c).InternalError("failed to create tenant").Send()
	}
	return responseapi.For(c).Response().Status(fiber.StatusCreated).Data(v).Send()
}

// Credentials lists safe metadata or creates an encrypted credential.
// @Summary List or create credentials
// @Description Lists safe credential metadata or creates an encrypted provider credential for the authorized owner or organization.
// @Tags credentials
// @Security BearerAuth
// @Param organization_id query string false "Organization context"
// @Param view_user_id query string false "Master-only user View As filter"
// @Param request body CredentialCreateRequest false "Required for POST"
// @Success 200 {array} entities.Credential
// @Success 201 {object} entities.Credential
// @Failure 400,401,403,500 {object} responseapi.ErrorResponse
// @Router /admin/credentials [get]
// @Router /admin/credentials [post]
func (a *Admin) Credentials(c fiber.Ctx) error {
	sess := SessionFrom(c)
	ownerUserID, allowed := credentialOwnerForSession(sess)
	if !allowed {
		return responseapi.For(c).Forbidden("provider connections are personal to users").Send()
	}
	if c.Method() == fiber.MethodGet {
		v, err := a.CredsSvc.List(c.Context())
		if err != nil {
			return responseapi.For(c).InternalError("failed to load credentials").Send()
		}
		return responseapi.For(c).Response().Status(fiber.StatusOK).Data(filterCredentialsForSession(v, sess)).Send()
	}
	var b CredentialCreateRequest
	if err := c.Bind().Body(&b); err != nil {
		return responseapi.For(c).BadRequest("invalid body").Send()
	}
	if b.Kind == "" {
		b.Kind = entities.KindAPIKey
	}
	if b.Provider == "" {
		b.Provider = entities.ProviderOpenAICompatible
	}
	v, err := a.CredsSvc.Create(c.Context(), entities.CredentialInput{Name: b.Name, Provider: b.Provider, Kind: b.Kind, BaseURL: b.BaseURL, APIKey: b.APIKey, OAuthAccess: b.OAuthAccess, OAuthRefresh: b.OAuthRefresh, OwnerUserID: ownerUserID})
	if err != nil {
		return responseapi.For(c).BadRequest(err.Error()).Send()
	}
	return responseapi.For(c).Response().Status(fiber.StatusCreated).Data(v).Send()
}

// CredentialByID updates or deletes a credential.
// @Summary Update or delete a credential
// @Description Updates safe credential metadata, rotates its secret when supplied, or deletes the authorized credential.
// @Tags credentials
// @Security BearerAuth
// @Param id path string true "Credential ID"
// @Param request body CredentialUpdateRequest false "Required for PUT"
// @Success 200 {object} entities.Credential
// @Failure 400,401,403,404,500 {object} responseapi.ErrorResponse
// @Router /admin/credentials/{id} [put]
// @Router /admin/credentials/{id} [delete]
func (a *Admin) CredentialByID(c fiber.Ctx) error {
	sess := SessionFrom(c)
	if sess == nil || !a.sessionOwnsCredential(c, sess, c.Params("id")) {
		return responseapi.For(c).NotFound("credential not found").Send()
	}
	if c.Method() == fiber.MethodPut {
		var b CredentialUpdateRequest
		if err := c.Bind().Body(&b); err != nil {
			return responseapi.For(c).BadRequest("invalid body").Send()
		}
		updated, err := a.CredsSvc.Update(c.Context(), c.Params("id"), entities.CredentialUpdate{
			Name: b.Name, BaseURL: b.BaseURL, Status: b.Status, APIKey: b.APIKey,
			OAuthAccess: b.OAuthAccess, OAuthRefresh: b.OAuthRefresh,
		})
		if errors.Is(err, entities.ErrNotFound) {
			return responseapi.For(c).NotFound("credential not found").Send()
		}
		if err != nil {
			return responseapi.For(c).BadRequest(err.Error()).Send()
		}
		return responseapi.For(c).Response().Status(fiber.StatusOK).Data(updated).Send()
	}
	err := a.CredsSvc.Delete(c.Context(), c.Params("id"))
	if errors.Is(err, entities.ErrNotFound) {
		return responseapi.For(c).NotFound("credential not found").Send()
	}
	if err != nil {
		return responseapi.For(c).InternalError("failed to delete credential").Send()
	}
	return responseapi.For(c).Response().Status(fiber.StatusOK).Data(okResponse{OK: true}).Send()
}

// KeysList returns safe key metadata constrained by principal policy.
// @Summary List API keys
// @Description Lists API-key metadata after applying principal ownership, organization context, status, and pagination filters.
// @Tags api-keys
// @Security BearerAuth
// @Param owner_type query string false "user or organization"
// @Param owner_id query string false "Owner user or organization ID"
// @Param organization_id query string false "Context organization ID"
// @Param view_user_id query string false "Master-only user View As filter"
// @Param status query string false "enabled or disabled"
// @Param cursor query string false "Opaque cursor"
// @Param limit query int false "Page size" default(100) maximum(500)
// @Success 200 {object} APIKeyListResponse
// @Failure 401,403,500 {object} responseapi.ErrorResponse
// @Router /admin/api-keys [get]
func (a *Admin) KeysList(c fiber.Ctx) error {
	sess := SessionFrom(c)
	actor, readErr := a.principalForRead(c)
	if readErr != nil {
		return principalReadError(c, readErr)
	}
	viewingAsUser := strings.TrimSpace(c.Query("view_user_id")) != ""
	v, err := a.KeysSvc.List(c.Context())
	if err != nil {
		return responseapi.For(c).InternalError("failed to load API keys").Send()
	}
	ownerType := strings.TrimSpace(c.Query("owner_type"))
	ownerID := strings.TrimSpace(c.Query("owner_id"))
	organizationID := strings.TrimSpace(c.Query("organization_id"))
	status := strings.TrimSpace(c.Query("status"))
	if actor.Type == entities.PrincipalUser && organizationID != "" && actor.OrganizationID == "" {
		if membership, membershipErr := a.IdentityRepo.Membership(c.Context(), organizationID, actor.UserID); membershipErr == nil {
			actor.OrganizationID, actor.MembershipRole = organizationID, membership.Role
		}
	}
	if ownerType != "" && ownerType != entities.OwnerUser && ownerType != entities.OwnerOrganization {
		return responseapi.For(c).BadRequest("owner_type must be user or organization").Send()
	}
	if status != "" && status != "enabled" && status != "disabled" {
		return responseapi.For(c).BadRequest("status must be enabled or disabled").Send()
	}
	filtered := make([]entities.ApiKey, 0, len(v))
	for _, key := range v {
		if (!sess.IsMaster() || viewingAsUser) && policy.ViewKeyMetadata(actor, key) != nil {
			continue
		}
		if ownerType != "" && key.OwnerType != ownerType {
			continue
		}
		keyOwnerID := key.OwnerUserID
		if key.OwnerType == entities.OwnerOrganization {
			keyOwnerID = key.OwnerOrganizationID
		}
		if ownerID != "" && keyOwnerID != ownerID {
			continue
		}
		if organizationID != "" && key.ContextOrganizationID != organizationID {
			continue
		}
		if status == "enabled" && !key.Enabled || status == "disabled" && key.Enabled {
			continue
		}
		filtered = append(filtered, key)
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return filtered[i].ID > filtered[j].ID
		}
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})
	if cursor := strings.TrimSpace(c.Query("cursor")); cursor != "" {
		decoded, decodeErr := base64.RawURLEncoding.DecodeString(cursor)
		if decodeErr != nil {
			return responseapi.For(c).BadRequest("cursor is invalid").Send()
		}
		parts := strings.SplitN(string(decoded), "\x00", 2)
		if len(parts) != 2 {
			return responseapi.For(c).BadRequest("cursor is invalid").Send()
		}
		createdAt, parseErr := time.Parse(time.RFC3339Nano, parts[0])
		if parseErr != nil {
			return responseapi.For(c).BadRequest("cursor is invalid").Send()
		}
		cursorID := parts[1]
		start := 0
		for start < len(filtered) && (filtered[start].CreatedAt.After(createdAt) || filtered[start].CreatedAt.Equal(createdAt) && filtered[start].ID >= cursorID) {
			start++
		}
		filtered = filtered[start:]
	}
	limit, parseErr := strconv.Atoi(c.Query("limit", "100"))
	if parseErr != nil || limit < 1 || limit > 500 {
		return responseapi.For(c).BadRequest("limit must be between 1 and 500").Send()
	}
	next := ""
	if len(filtered) > limit {
		last := filtered[limit-1]
		next = base64.RawURLEncoding.EncodeToString([]byte(last.CreatedAt.UTC().Format(time.RFC3339Nano) + "\x00" + last.ID))
		filtered = filtered[:limit]
	}
	return responseapi.For(c).Response().
		Status(fiber.StatusOK).
		Object("list").
		Data(filtered).
		Next(next).
		Send()
}

// KeyModelOptions lists callable models that the current principal may grant
// to an API key, including the effective per-million-token price.
// @Summary List grantable API-key models
// @Description Lists models that the current principal may assign to an API key, including effective pricing.
// @Tags api-keys
// @Security BearerAuth
// @Param organization_id query string false "Target organization"
// @Success 200 {object} APIKeyModelOptionsResponse
// @Failure 401,403,500 {object} responseapi.ErrorResponse
// @Router /admin/api-keys/models [get]
func (a *Admin) KeyModelOptions(c fiber.Ctx) error {
	sess := SessionFrom(c)
	organizationID := strings.TrimSpace(c.Query("organization_id"))
	if !sess.IsMaster() {
		if sess.PrincipalType != entities.PrincipalUser || sess.UserID == "" ||
			sess.MembershipRole != entities.MembershipAdmin || sess.OrganizationID == "" {
			return responseapi.For(c).Forbidden("organization administration is required").Send()
		}
		if organizationID != "" && organizationID != sess.OrganizationID {
			return responseapi.For(c).Forbidden("organization administration is required").Send()
		}
		organizationID = sess.OrganizationID
	}
	models, err := a.ModelsSvc.List(c.Context())
	if err != nil {
		return responseapi.For(c).InternalError("failed to load models").Send()
	}
	visibleCredentials := map[string]bool{}
	credentials, listErr := a.CredsSvc.List(c.Context())
	if listErr != nil {
		return responseapi.For(c).InternalError("failed to load model connections").Send()
	}
	for _, credential := range credentials {
		ownedBySharer := credential.OwnerUserID == sess.UserID
		if sess.IsMaster() {
			ownedBySharer = credential.OwnerUserID == "" && credential.OwnerTenantID == nil
		}
		if credential.Status == entities.StatusActive && ownedBySharer {
			visibleCredentials[credential.ID] = true
		}
	}
	options := make([]APIKeyModelOption, 0, len(models))
	for _, model := range models {
		if !model.Enabled {
			continue
		}
		callable := false
		for _, route := range model.Routes {
			if route.Enabled && visibleCredentials[route.CredentialID] {
				callable = true
				break
			}
		}
		if !callable {
			continue
		}
		price := entities.Price{}
		priced := false
		if model.Price != nil {
			price, priced = *model.Price, true
		} else if a.Pricing != nil {
			price, priced = a.Pricing.Resolve(model.Name, model.UpstreamModel)
		}
		options = append(options, APIKeyModelOption{ID: model.Name, UpstreamModel: model.UpstreamModel, Price: price, Free: !priced || price == (entities.Price{})})
	}
	sort.Slice(options, func(i, j int) bool { return options[i].ID < options[j].ID })
	return responseapi.For(c).Response().
		Status(fiber.StatusOK).
		Object("list").
		Data(options).
		Send()
}

// KeysCreate creates a principal-owned key and returns plaintext once.
// @Summary Create an API key
// @Description Creates an owned API key and returns its plaintext secret exactly once in the response.
// @Tags api-keys
// @Security BearerAuth
// @Param request body APIKeyCreateRequest true "Key configuration"
// @Success 201 {object} CreatedAPIKeyResponse
// @Failure 400,401,403,404,500 {object} responseapi.ErrorResponse
// @Router /admin/api-keys [post]
func (a *Admin) KeysCreate(c fiber.Ctx) error {
	var b APIKeyCreateRequest
	if err := c.Bind().Body(&b); err != nil {
		return responseapi.For(c).BadRequest("invalid body").Send()
	}
	if len(b.Scopes) == 0 {
		b.Scopes = []string{entities.ScopeChat}
	}
	sess := SessionFrom(c)
	actor := principalFromSession(sess)
	if !sess.IsMaster() {
		if !policy.CanGrant(actor, b.Scopes, nil, nil) {
			return responseapi.For(c).Forbidden("cannot grant scopes or models not held by the current session").Send()
		}
		switch actor.Type {
		case entities.PrincipalUser:
			if b.OwnerType == entities.OwnerOrganization {
				organizationID := strings.TrimSpace(b.OwnerOrganizationID)
				if organizationID == "" {
					organizationID = strings.TrimSpace(b.ContextOrganizationID)
				}
				if actor.MembershipRole != entities.MembershipAdmin || actor.OrganizationID != organizationID {
					return responseapi.For(c).Forbidden("organization administration is required").Send()
				}
				b.OwnerUserID = ""
				b.OwnerOrganizationID = organizationID
				b.ContextOrganizationID = organizationID
			} else {
				b.OwnerType = entities.OwnerUser
				targetUserID := strings.TrimSpace(b.OwnerUserID)
				if targetUserID == "" {
					targetUserID = actor.UserID
				}
				if targetUserID != actor.UserID && (actor.MembershipRole != entities.MembershipAdmin || actor.OrganizationID == "" || actor.OrganizationID != strings.TrimSpace(b.ContextOrganizationID)) {
					return responseapi.For(c).Forbidden("organization administration is required to create a key for another member").Send()
				}
				b.OwnerUserID = targetUserID
				b.OwnerOrganizationID = ""
			}
			if b.OwnerType == entities.OwnerUser && b.ContextOrganizationID != "" {
				if err := a.IdentitySvc.ValidateUserKeyContext(c.Context(), b.OwnerUserID, b.ContextOrganizationID); err != nil {
					return responseapi.For(c).Forbidden("active organization membership is required").Send()
				}
			}
		case entities.PrincipalOrganization:
			if b.OwnerType == entities.OwnerUser && strings.TrimSpace(b.OwnerUserID) != "" {
				b.OwnerOrganizationID = ""
				b.ContextOrganizationID = actor.OrganizationID
				if err := a.IdentitySvc.ValidateUserKeyContext(c.Context(), b.OwnerUserID, actor.OrganizationID); err != nil {
					return responseapi.For(c).Forbidden("selected user must be an active organization member").Send()
				}
			} else {
				b.OwnerType = entities.OwnerOrganization
				b.OwnerUserID = ""
				b.OwnerOrganizationID = actor.OrganizationID
				b.ContextOrganizationID = actor.OrganizationID
			}
		}
	}
	if sess.IsMaster() && b.OwnerType == "" {
		if b.TenantID == "" {
			b.TenantID = "tenant_default"
		}
		b.OwnerType = entities.OwnerOrganization
		b.OwnerOrganizationID = b.TenantID
		b.ContextOrganizationID = b.TenantID
	}
	if b.OwnerType == entities.OwnerUser && strings.TrimSpace(b.ContextOrganizationID) != "" {
		if a.IdentitySvc == nil {
			return responseapi.For(c).InternalError("identity service unavailable").Send()
		}
		if err := a.IdentitySvc.ValidateUserKeyContext(c.Context(), strings.TrimSpace(b.OwnerUserID), strings.TrimSpace(b.ContextOrganizationID)); err != nil {
			return responseapi.For(c).BadRequest("selected user must be an active member of the organization").Send()
		}
	}
	if !sess.IsMaster() && (actor.Type != entities.PrincipalUser || actor.MembershipRole != entities.MembershipAdmin ||
		actor.OrganizationID == "" || actor.OrganizationID != strings.TrimSpace(b.ContextOrganizationID)) {
		return responseapi.For(c).Forbidden("organization administration is required to assign models").Send()
	}
	credentialOwnerUserID := strings.TrimSpace(sess.UserID)
	globalCredentialOwner := sess.IsMaster()
	if credentialOwnerUserID == "" && !globalCredentialOwner {
		return responseapi.For(c).Forbidden("a user provider owner is required to share models").Send()
	}
	if err := a.validateCredentialBackedModels(c.Context(), credentialOwnerUserID, globalCredentialOwner, b.Models); err != nil {
		return responseapi.For(c).Forbidden("only models from your provider connections can be shared").Send()
	}
	in := apikey.CreateInput{TenantID: b.TenantID, Name: b.Name, Models: b.Models, Scopes: b.Scopes, QuotaUSD: b.QuotaUSD, QuotaPeriod: b.QuotaPeriod, OwnerType: b.OwnerType, OwnerUserID: b.OwnerUserID, OwnerOrganizationID: b.OwnerOrganizationID, ContextOrganizationID: b.ContextOrganizationID, CredentialOwnerUserID: credentialOwnerUserID, CredentialOwnerGlobal: globalCredentialOwner}
	v, err := a.KeysSvc.Create(c.Context(), in)
	if err != nil {
		return responseapi.For(c).BadRequest(err.Error()).Send()
	}
	if err = a.appendKeyAudit(c, actor, "key.create", v, map[string]string{"name": v.Name, "owner_type": v.OwnerType}); err != nil {
		return responseapi.For(c).InternalError("API key created but audit write failed").Send()
	}
	return responseapi.For(c).Response().Status(fiber.StatusCreated).Data(keyCreatedResponse(v)).Send()
}

// KeysPatch changes mutable API-key policy fields.
// @Summary Update an API key
// @Description Changes mutable API-key policy fields such as enabled state, model allowlist, scopes, quota, and RPM.
// @Tags api-keys
// @Security BearerAuth
// @Param id path string true "API key ID"
// @Param request body APIKeyPatchRequest true "Mutable key fields"
// @Success 200 {object} OKResponse
// @Failure 400,401,403,404,500 {object} responseapi.ErrorResponse
// @Router /admin/api-keys/{id} [patch]
func (a *Admin) KeysPatch(c fiber.Ctx) error {
	var b APIKeyPatchRequest
	if err := c.Bind().Body(&b); err != nil {
		return responseapi.For(c).BadRequest("invalid body").Send()
	}
	quotaValue := b.QuotaUSD
	period := b.QuotaPeriod
	sess := SessionFrom(c)
	actor := principalFromSession(sess)
	key, getErr := a.KeysSvc.GetByID(c.Context(), c.Params("id"))
	if getErr != nil {
		return responseapi.For(c).NotFound("API key not found").Send()
	}
	if err := policy.ManageKey(actor, *key); err != nil {
		return responseapi.For(c).NotFound("API key not found").Send()
	}
	var err error
	if b.Models != nil {
		providerOwner := key.CredentialOwnerUserID
		globalCredentialOwner := providerOwner == "" && sess.IsMaster()
		if providerOwner == "" && !globalCredentialOwner {
			return responseapi.For(c).Forbidden("only the sharing principal can change assigned models").Send()
		}
		if validateErr := a.validateCredentialBackedModels(c.Context(), providerOwner, globalCredentialOwner, *b.Models); validateErr != nil {
			return responseapi.For(c).Forbidden("only models from the sharing user's provider connections can be assigned").Send()
		}
	}
	if !sess.IsMaster() {
		if b.Scopes != nil && !policy.CanGrant(actor, *b.Scopes, nil, nil) {
			return responseapi.For(c).Forbidden("cannot grant scopes not held by the current session").Send()
		}
	}
	err = a.KeysSvc.PatchQuota(c.Context(), c.Params("id"), b.Enabled, b.Models, b.Scopes, quotaValue, period, b.RPM)
	if err != nil {
		if errors.Is(err, entities.ErrNotFound) {
			return responseapi.For(c).NotFound("API key not found").Send()
		}
		return responseapi.For(c).BadRequest(err.Error()).Send()
	}
	action := "key.update"
	if b.Enabled != nil && !*b.Enabled {
		action = "key.disable"
	}
	if err = a.appendKeyAudit(c, actor, action, key, map[string]string{"changed": "configuration"}); err != nil {
		return responseapi.For(c).InternalError("API key updated but audit write failed").Send()
	}
	return responseapi.For(c).Response().Status(fiber.StatusOK).Data(okResponse{OK: true}).Send()
}

// KeysDelete revokes and deletes an API key.
// @Summary Delete an API key
// @Description Permanently deletes an API key after checking ownership and management permission.
// @Tags api-keys
// @Security BearerAuth
// @Param id path string true "API key ID"
// @Success 200 {object} OKResponse
// @Failure 401,403,404,500 {object} responseapi.ErrorResponse
// @Router /admin/api-keys/{id} [delete]
func (a *Admin) KeysDelete(c fiber.Ctx) error {
	actor := principalFromSession(SessionFrom(c))
	key, getErr := a.KeysSvc.GetByID(c.Context(), c.Params("id"))
	if getErr != nil {
		return responseapi.For(c).NotFound("API key not found").Send()
	}
	if policy.ManageKey(actor, *key) != nil {
		return responseapi.For(c).NotFound("API key not found").Send()
	}
	err := a.KeysSvc.Delete(c.Context(), c.Params("id"))
	if err != nil {
		if errors.Is(err, entities.ErrNotFound) {
			return responseapi.For(c).NotFound("API key not found").Send()
		}
		return responseapi.For(c).InternalError("failed to delete API key").Send()
	}
	if err = a.appendKeyAudit(c, actor, "key.delete", key, nil); err != nil {
		return responseapi.For(c).InternalError("API key deleted but audit write failed").Send()
	}
	return responseapi.For(c).Response().Status(fiber.StatusOK).Data(okResponse{OK: true}).Send()
}

// KeysReveal returns the encrypted-at-rest API key plaintext to an authorized viewer.
// @Summary Reveal an API key
// @Description Reveals an API key to master, its owner, or an administrator of its context organization.
// @Tags api-keys
// @Security BearerAuth
// @Param id path string true "API key ID"
// @Success 200 {object} APIKeyRevealResponse
// @Failure 401,403,404,500 {object} responseapi.ErrorResponse
// @Router /admin/api-keys/{id}/reveal [get]
func (a *Admin) KeysReveal(c fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	actor := principalFromSession(SessionFrom(c))
	key, err := a.KeysSvc.GetByID(c.Context(), c.Params("id"))
	if err != nil || policy.ViewKeyMetadata(actor, *key) != nil {
		return responseapi.For(c).NotFound("API key not found").Send()
	}
	plaintext, err := a.KeysSvc.Reveal(c.Context(), key.ID)
	if errors.Is(err, entities.ErrNotFound) {
		return responseapi.For(c).NotFound("API key plaintext is unavailable; rotate the key to enable reveal").Send()
	}
	if err != nil {
		return responseapi.For(c).InternalError("failed to reveal API key").Send()
	}
	return responseapi.For(c).Response().Status(fiber.StatusOK).Data(APIKeyRevealResponse{Plaintext: plaintext}).Send()
}

// KeysRotate atomically invalidates the old secret and returns a new one once.
// @Summary Rotate an API key
// @Description Replaces an API key secret and returns the new plaintext secret exactly once.
// @Tags api-keys
// @Security BearerAuth
// @Param id path string true "API key ID"
// @Success 200 {object} CreatedAPIKeyResponse
// @Failure 401,403,404,500 {object} responseapi.ErrorResponse
// @Router /admin/api-keys/{id}/rotate [post]
func (a *Admin) KeysRotate(c fiber.Ctx) error {
	actor := principalFromSession(SessionFrom(c))
	key, err := a.KeysSvc.GetByID(c.Context(), c.Params("id"))
	if err != nil || policy.ManageKey(actor, *key) != nil {
		return responseapi.For(c).NotFound("API key not found").Send()
	}
	rotated, err := a.KeysSvc.Rotate(c.Context(), key.ID)
	if err != nil {
		return responseapi.For(c).InternalError("failed to rotate API key").Send()
	}
	if err = a.appendKeyAudit(c, actor, "key.rotate", rotated, map[string]string{"key_prefix": rotated.SecretPrefix}); err != nil {
		return responseapi.For(c).InternalError("API key rotated but audit write failed").Send()
	}
	return responseapi.For(c).Response().Status(fiber.StatusOK).Data(keyCreatedResponse(rotated)).Send()
}

func (a *Admin) appendKeyAudit(c fiber.Ctx, actor entities.Principal, action string, key *entities.ApiKey, metadata map[string]string) error {
	if a.AuditRepo == nil {
		return nil
	}
	actorID, label := actor.UserID, actor.Username
	if actor.Type == entities.PrincipalMaster {
		actorID, label = "master", "master"
	} else if actor.Type == entities.PrincipalOrganization {
		actorID = actor.OrganizationID
		if label == "" {
			label = "org:" + actor.OrganizationName
		}
	}
	organizationID := key.ContextOrganizationID
	return a.AuditRepo.AppendAudit(c.Context(), entities.AuditEvent{ID: entities.NewID("audit"), TS: time.Now().UTC(), ActorType: actor.Type, ActorID: actorID, ActorLabel: label, OrganizationID: organizationID, Action: action, TargetType: "api_key", TargetID: key.ID, SafeMetadata: metadata})
}

func keyCreatedResponse(v *entities.ApiKey) createdAPIKeyResponse {
	return createdAPIKeyResponse{ID: v.ID, TenantID: v.TenantID, Name: v.Name, KeyPrefix: v.SecretPrefix, Models: v.Models, Scopes: v.Scopes, QuotaUSD: v.QuotaUSD, QuotaPeriod: v.QuotaPeriod, RPM: v.RPM, Enabled: v.Enabled, Plaintext: v.Plaintext, OwnerType: v.OwnerType, OwnerUserID: v.OwnerUserID, OwnerOrganizationID: v.OwnerOrganizationID, ContextOrganizationID: v.ContextOrganizationID}
}

// ModelsList returns models visible to the principal.
// @Summary List configured models
// @Description Lists configured model routes visible in the current organization or user context.
// @Tags models
// @Security BearerAuth
// @Param organization_id query string false "Organization context"
// @Param view_user_id query string false "Master-only user View As filter"
// @Success 200 {array} entities.ModelDef
// @Failure 401,403,500 {object} responseapi.ErrorResponse
// @Router /admin/models [get]
func (a *Admin) ModelsList(c fiber.Ctx) error {
	v, err := a.ModelsSvc.List(c.Context())
	if err != nil {
		return responseapi.For(c).InternalError("failed to load models").Send()
	}
	sess := SessionFrom(c)
	if sess == nil || sess.PrincipalType != entities.PrincipalUser || sess.UserID == "" {
		return responseapi.For(c).Response().Status(fiber.StatusOK).Data([]entities.ModelDef{}).Send()
	}
	credentials, err := a.CredsSvc.List(c.Context())
	if err != nil {
		return responseapi.For(c).InternalError("failed to filter model routes").Send()
	}
	allowed := make(map[string]bool)
	for _, credential := range credentials {
		if credential.OwnerUserID == sess.UserID {
			allowed[credential.ID] = true
		}
	}
	filtered := make([]entities.ModelDef, 0, len(v))
	for _, model := range v {
		routes := make([]entities.ModelRoute, 0, len(model.Routes))
		for _, route := range model.Routes {
			if allowed[route.CredentialID] {
				routes = append(routes, route)
			}
		}
		if len(routes) != 0 {
			model.Routes = routes
			filtered = append(filtered, model)
		}
	}
	return responseapi.For(c).Response().Status(fiber.StatusOK).Data(filtered).Send()
}

// ModelUpsert creates or replaces a model route definition.
// @Summary Upsert a model
// @Description Creates or replaces a model route definition. Global model route changes require the master principal.
// @Tags models
// @Security BearerAuth
// @Param name path string true "Public model name"
// @Param request body entities.ModelDef true "Model definition"
// @Success 200 {object} OKResponse
// @Failure 400,401,403,500 {object} responseapi.ErrorResponse
// @Router /admin/models/{name} [put]
func (a *Admin) ModelUpsert(c fiber.Ctx) error {
	if sess := SessionFrom(c); sess == nil || !sess.IsMaster() {
		return responseapi.For(c).Forbidden("only the master session can change global model routes").Send()
	}
	var m entities.ModelDef
	if err := c.Bind().Body(&m); err != nil {
		return responseapi.For(c).BadRequest("invalid body").Send()
	}
	m.Name = decodedPathParam(c, "name")
	if err := a.ModelsSvc.Upsert(c.Context(), m); err != nil {
		return responseapi.For(c).BadRequest(err.Error()).Send()
	}
	return responseapi.For(c).Response().Status(fiber.StatusOK).Data(okResponse{OK: true}).Send()
}

// ModelDelete removes a model route.
// @Summary Delete a model
// @Description Removes a configured model route after checking model-management permission.
// @Tags models
// @Security BearerAuth
// @Param name path string true "Public model name"
// @Success 200 {object} OKResponse
// @Failure 401,403,404,500 {object} responseapi.ErrorResponse
// @Router /admin/models/{name} [delete]
func (a *Admin) ModelDelete(c fiber.Ctx) error {
	if sess := SessionFrom(c); sess == nil || !sess.IsMaster() {
		return responseapi.For(c).Forbidden("only the master session can change global model routes").Send()
	}
	if err := a.ModelsSvc.Delete(c.Context(), decodedPathParam(c, "name")); err != nil {
		return responseapi.For(c).InternalError("failed to delete model").Send()
	}
	return responseapi.For(c).Response().Status(fiber.StatusOK).Data(okResponse{OK: true}).Send()
}

// Price sets or deletes a manual model price.
// @Summary Set or delete a model price
// @Description Creates or removes the manual price for a public model. Manual prices override catalog fallback prices.
// @Tags pricing
// @Security BearerAuth
// @Param model path string true "Public model name"
// @Param request body entities.Price false "Required for PUT"
// @Success 200 {object} OKResponse
// @Failure 400,401,403,404,500 {object} responseapi.ErrorResponse
// @Router /admin/prices/{model} [put]
// @Router /admin/prices/{model} [delete]
func (a *Admin) Price(c fiber.Ctx) error {
	if sess := SessionFrom(c); sess == nil || !sess.IsMaster() {
		return responseapi.For(c).Forbidden("only the master session can change global prices").Send()
	}
	if c.Method() == fiber.MethodDelete {
		if err := a.ModelsSvc.DeletePrice(c.Context(), decodedPathParam(c, "model")); err != nil {
			if errors.Is(err, entities.ErrNotFound) {
				return responseapi.For(c).NotFound("price not found").Send()
			}
			return responseapi.For(c).InternalError("failed to delete price").Send()
		}
		return responseapi.For(c).Response().Status(fiber.StatusOK).Data(okResponse{OK: true}).Send()
	}
	var p entities.Price
	if err := c.Bind().Body(&p); err != nil {
		return responseapi.For(c).BadRequest("invalid body").Send()
	}
	if err := a.ModelsSvc.SetPrice(c.Context(), decodedPathParam(c, "model"), p); err != nil {
		return responseapi.For(c).BadRequest(err.Error()).Send()
	}
	return responseapi.For(c).Response().Status(fiber.StatusOK).Data(okResponse{OK: true}).Send()
}

func decodedPathParam(c fiber.Ctx, name string) string {
	value := c.Params(name)
	if decoded, err := url.PathUnescape(value); err == nil {
		return decoded
	}
	return value
}

// Prices returns manual model prices.
// @Summary List model prices
// @Description Returns manually configured model prices.
// @Tags pricing
// @Security BearerAuth
// @Success 200 {object} PriceListResponse
// @Failure 401,403,500 {object} responseapi.ErrorResponse
// @Router /admin/prices [get]
func (a *Admin) Prices(c fiber.Ctx) error {
	v, err := a.ModelsSvc.Prices(c.Context())
	if err != nil {
		return responseapi.For(c).InternalError("failed to load prices").Send()
	}
	return responseapi.For(c).Response().Status(fiber.StatusOK).Data(PriceListResponse{Prices: v}).Send()
}

// UsageSummary returns a policy-constrained usage aggregate.
// @Summary Get usage summary
// @Description Returns policy-constrained aggregate request, token, cache, and cost metrics.
// @Tags usage
// @Security BearerAuth
// @Param range query string false "24h, 7d, or 30d"
// @Param organization_id query string false "Organization filter"
// @Param user_id query string false "User filter"
// @Param view_user_id query string false "Master-only user View As filter"
// @Success 200 {object} entities.UsageSummary
// @Failure 400,401,403,500 {object} responseapi.ErrorResponse
// @Router /admin/usage/summary [get]
func (a *Admin) UsageSummary(c fiber.Ctx) error {
	since := time.Now().Add(-24 * time.Hour)
	switch c.Query("range") {
	case "7d":
		since = time.Now().Add(-7 * 24 * time.Hour)
	case "30d":
		since = time.Now().Add(-30 * 24 * time.Hour)
	}
	actor, readErr := a.principalForRead(c)
	if readErr != nil {
		return principalReadError(c, readErr)
	}
	requestedOrganization := strings.TrimSpace(c.Query("organization_id"))
	if actor.Type != entities.PrincipalMaster && len(splitCSV(requestedOrganization)) > 1 {
		return responseapi.For(c).BadRequest("multiple organization filters require the master session").Send()
	}
	if actor.Type == entities.PrincipalUser && requestedOrganization != "" && actor.OrganizationID == "" {
		if membership, membershipErr := a.IdentityRepo.Membership(c.Context(), requestedOrganization, actor.UserID); membershipErr == nil {
			actor.OrganizationID, actor.MembershipRole = requestedOrganization, membership.Role
		}
	}
	organizationWide := actor.Type == entities.PrincipalOrganization || actor.MembershipRole == entities.MembershipAdmin
	visibility, policyErr := policy.UsageVisibility(actor, organizationWide)
	if policyErr != nil {
		return responseapi.For(c).Forbidden("usage access is not allowed").Send()
	}
	query := entities.UsageQuery{Visibility: visibility, Since: &since, OrganizationID: requestedOrganization, UserID: c.Query("user_id")}
	v, err := a.UsageSvc.SummaryQuery(c.Context(), query)
	if err != nil {
		return responseapi.For(c).InternalError("failed to load usage summary").Send()
	}
	return responseapi.For(c).Response().Status(fiber.StatusOK).Data(v).Send()
}

// UsageRecent returns cursor-paginated actor snapshot events.
// @Summary List recent usage
// @Description Returns cursor-paginated usage events filtered by time, principal, organization, model, key, or status.
// @Tags usage
// @Security BearerAuth
// @Param cursor query string false "Opaque cursor"
// @Param limit query int false "Page size" default(100) maximum(500)
// @Param since query string false "RFC3339 lower bound"
// @Param until query string false "RFC3339 upper bound"
// @Param organization_id query string false "Organization filter"
// @Param user_id query string false "User filter"
// @Param model query string false "Model filter"
// @Param api_key_id query string false "API-key filter"
// @Param status query int false "HTTP status filter"
// @Param view_user_id query string false "Master-only user View As filter"
// @Success 200 {object} UsageRecentResponse
// @Failure 400,401,403,500 {object} responseapi.ErrorResponse
// @Router /admin/usage/recent [get]
func (a *Admin) UsageRecent(c fiber.Ctx) error {
	actor, readErr := a.principalForRead(c)
	if readErr != nil {
		return principalReadError(c, readErr)
	}
	requestedOrganization := strings.TrimSpace(c.Query("organization_id"))
	if actor.Type != entities.PrincipalMaster && len(splitCSV(requestedOrganization)) > 1 {
		return responseapi.For(c).BadRequest("multiple organization filters require the master session").Send()
	}
	if actor.Type == entities.PrincipalUser && requestedOrganization != "" && actor.OrganizationID == "" {
		if membership, membershipErr := a.IdentityRepo.Membership(c.Context(), requestedOrganization, actor.UserID); membershipErr == nil {
			actor.OrganizationID, actor.MembershipRole = requestedOrganization, membership.Role
		}
	}
	organizationWide := actor.Type == entities.PrincipalOrganization || actor.MembershipRole == entities.MembershipAdmin
	visibility, policyErr := policy.UsageVisibility(actor, organizationWide)
	if policyErr != nil {
		return responseapi.For(c).Forbidden("usage access is not allowed").Send()
	}
	limit, _ := strconv.Atoi(c.Query("limit", "100"))
	query := entities.UsageQuery{Visibility: visibility, Cursor: c.Query("cursor"), Limit: limit, OrganizationID: requestedOrganization, UserID: c.Query("user_id"), Model: c.Query("model"), APIKeyID: c.Query("api_key_id")}
	if value := c.Query("status"); value != "" {
		status, parseErr := strconv.Atoi(value)
		if parseErr != nil {
			return responseapi.For(c).BadRequest("status must be an integer").Send()
		}
		query.StatusCode = &status
	}
	if value := c.Query("since"); value != "" {
		parsed, parseErr := time.Parse(time.RFC3339, value)
		if parseErr != nil {
			return responseapi.For(c).BadRequest("since must be RFC3339").Send()
		}
		query.Since = &parsed
	}
	if value := c.Query("until"); value != "" {
		parsed, parseErr := time.Parse(time.RFC3339, value)
		if parseErr != nil {
			return responseapi.For(c).BadRequest("until must be RFC3339").Send()
		}
		query.Until = &parsed
	}
	page, err := a.UsageSvc.Query(c.Context(), query)
	if err != nil {
		return responseapi.For(c).InternalError("failed to load recent usage").Send()
	}
	return responseapi.For(c).Response().
		Status(fiber.StatusOK).
		Object("list").
		Data(page.Data).
		Next(page.NextCursor).
		Send()
}

// UsageDetail returns one policy-constrained request and its opt-in captured content.
// @Summary Get usage request detail
// @Description Returns one visible usage event, including encrypted and bounded captured request and response content when enabled.
// @Tags usage
// @Security BearerAuth
// @Param id path string true "Usage event ID"
// @Param organization_id query string false "Organization context"
// @Param view_user_id query string false "Master-only user View As filter"
// @Success 200 {object} entities.UsageDetail
// @Failure 400,401,403,404,500 {object} responseapi.ErrorResponse
// @Router /admin/usage/events/{id} [get]
func (a *Admin) UsageDetail(c fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	actor, readErr := a.principalForRead(c)
	if readErr != nil {
		return principalReadError(c, readErr)
	}
	requestedOrganization := strings.TrimSpace(c.Query("organization_id"))
	if actor.Type != entities.PrincipalMaster && len(splitCSV(requestedOrganization)) > 1 {
		return responseapi.For(c).BadRequest("multiple organization filters require the master session").Send()
	}
	if actor.Type == entities.PrincipalUser && requestedOrganization != "" && actor.OrganizationID == "" && a.IdentityRepo != nil {
		if membership, err := a.IdentityRepo.Membership(c.Context(), requestedOrganization, actor.UserID); err == nil {
			actor.OrganizationID, actor.MembershipRole = requestedOrganization, membership.Role
		}
	}
	organizationWide := actor.Type == entities.PrincipalOrganization || actor.MembershipRole == entities.MembershipAdmin
	visibility, err := policy.UsageVisibility(actor, organizationWide)
	if err != nil {
		return responseapi.For(c).Forbidden("usage access is not allowed").Send()
	}
	detail, err := a.UsageSvc.Detail(c.Context(), c.Params("id"), visibility)
	if errors.Is(err, entities.ErrNotFound) {
		return responseapi.For(c).NotFound("usage event not found").Send()
	}
	if err != nil {
		return responseapi.For(c).InternalError("failed to load usage detail").Send()
	}
	return responseapi.For(c).Response().Status(fiber.StatusOK).Data(detail).Send()
}

// UsageActivity returns policy-constrained time buckets for the analysis UI.
// @Summary Get usage activity
// @Description Returns time-bucketed usage activity and its aggregate summary for the analysis dashboard.
// @Tags usage
// @Security BearerAuth
// @Param range query string false "1d, 7d, 30d, 90d, ytd, or all"
// @Param since query string false "RFC3339 lower bound for a custom range"
// @Param until query string false "RFC3339 upper bound for a custom range"
// @Param group_by query string false "hour, day, or week" default(day)
// @Param organization_id query string false "Organization filter"
// @Param user_id query string false "User filter"
// @Param api_key_id query string false "API-key filter"
// @Param view_user_id query string false "Master-only user View As filter"
// @Success 200 {object} UsageActivityResponse
// @Failure 400,401,403,500 {object} responseapi.ErrorResponse
// @Router /admin/usage/activity [get]
func (a *Admin) UsageActivity(c fiber.Ctx) error {
	actor, readErr := a.principalForRead(c)
	if readErr != nil {
		return principalReadError(c, readErr)
	}
	requestedOrganization := strings.TrimSpace(c.Query("organization_id"))
	if actor.Type != entities.PrincipalMaster && len(splitCSV(requestedOrganization)) > 1 {
		return responseapi.For(c).BadRequest("multiple organization filters require the master session").Send()
	}
	if actor.Type == entities.PrincipalUser && requestedOrganization != "" && actor.OrganizationID == "" && a.IdentityRepo != nil {
		if membership, membershipErr := a.IdentityRepo.Membership(c.Context(), requestedOrganization, actor.UserID); membershipErr == nil {
			actor.OrganizationID, actor.MembershipRole = requestedOrganization, membership.Role
		}
	}
	organizationWide := actor.Type == entities.PrincipalOrganization || actor.MembershipRole == entities.MembershipAdmin
	visibility, policyErr := policy.UsageVisibility(actor, organizationWide)
	if policyErr != nil {
		return responseapi.For(c).Forbidden("usage access is not allowed").Send()
	}
	groupBy := strings.ToLower(strings.TrimSpace(c.Query("group_by", "day")))
	if groupBy != "hour" && groupBy != "day" && groupBy != "week" {
		return responseapi.For(c).BadRequest("group_by must be hour, day, or week").Send()
	}
	now := time.Now().UTC()
	query := entities.UsageQuery{Visibility: visibility, OrganizationID: requestedOrganization, UserID: strings.TrimSpace(c.Query("user_id")), APIKeyID: strings.TrimSpace(c.Query("api_key_id"))}
	switch strings.ToLower(strings.TrimSpace(c.Query("range", "7d"))) {
	case "1d":
		since := now.Add(-24 * time.Hour)
		query.Since = &since
	case "7d":
		since := now.Add(-7 * 24 * time.Hour)
		query.Since = &since
	case "30d":
		since := now.Add(-30 * 24 * time.Hour)
		query.Since = &since
	case "90d":
		since := now.Add(-90 * 24 * time.Hour)
		query.Since = &since
	case "ytd":
		since := time.Date(now.Year(), time.January, 1, 0, 0, 0, 0, time.UTC)
		query.Since = &since
	case "all":
	case "custom":
		since, err := time.Parse(time.RFC3339, c.Query("since"))
		if err != nil {
			return responseapi.For(c).BadRequest("since must be RFC3339 for a custom range").Send()
		}
		until, err := time.Parse(time.RFC3339, c.Query("until"))
		if err != nil {
			return responseapi.For(c).BadRequest("until must be RFC3339 for a custom range").Send()
		}
		if !since.Before(until) {
			return responseapi.For(c).BadRequest("since must be before until").Send()
		}
		query.Since, query.Until = &since, &until
	default:
		return responseapi.For(c).BadRequest("range must be 1d, 7d, 30d, 90d, ytd, all, or custom").Send()
	}
	buckets, err := a.UsageSvc.Activity(c.Context(), query, groupBy)
	if err != nil {
		return responseapi.For(c).InternalError("failed to load usage activity").Send()
	}
	summary, err := a.UsageSvc.SummaryQuery(c.Context(), query)
	if err != nil {
		return responseapi.For(c).InternalError("failed to load usage breakdown").Send()
	}
	health, err := a.UsageSvc.Health(c.Context(), query)
	if err != nil {
		return responseapi.For(c).InternalError("failed to load provider health").Send()
	}
	return responseapi.For(c).Response().Status(fiber.StatusOK).Data(UsageActivityResponse{GroupBy: groupBy, Data: buckets, Summary: summary, Health: health}).Send()
}

// CacheStats returns safe prompt-cache counters.
// @Summary Get cache statistics
// @Description Returns deterministic router response-cache counters without exposing cache contents.
// @Tags cache
// @Security BearerAuth
// @Success 200 {object} chat.CacheStats
// @Failure 401,403 {object} responseapi.ErrorResponse
// @Router /admin/cache/stats [get]
func (a *Admin) CacheStats(c fiber.Ctx) error {
	if a.Cache == nil {
		return responseapi.For(c).Response().Status(fiber.StatusOK).Data(chat.CacheStats{}).Send()
	}
	return responseapi.For(c).Response().Status(fiber.StatusOK).Data(a.Cache.Stats()).Send()
}

// CacheFlush purges prompt-cache entries.
// @Summary Flush prompt cache
// @Description Purges deterministic router response-cache entries.
// @Tags cache
// @Security BearerAuth
// @Success 200 {object} OKResponse
// @Failure 401,403 {object} responseapi.ErrorResponse
// @Router /admin/cache/flush [post]
func (a *Admin) CacheFlush(c fiber.Ctx) error {
	if a.Cache != nil {
		a.Cache.Flush()
	}
	return responseapi.For(c).Response().Status(fiber.StatusOK).Data(okResponse{OK: true}).Send()
}

func (a *Admin) sessionOwnsCredential(c fiber.Ctx, session *entities.Session, credentialID string) bool {
	credentials, err := a.CredsSvc.List(c.Context())
	if err != nil {
		return false
	}
	for _, cred := range credentials {
		if cred.ID == credentialID {
			return credentialOwnedBySession(cred, session)
		}
	}
	return false
}

func (a *Admin) validateCredentialBackedModels(ctx context.Context, ownerUserID string, globalOwner bool, names []string) error {
	credentials, err := a.CredsSvc.List(ctx)
	if err != nil {
		return err
	}
	owned := make(map[string]bool)
	for _, credential := range credentials {
		ownedBySharer := credential.OwnerUserID == ownerUserID
		if globalOwner {
			ownedBySharer = credential.OwnerUserID == "" && credential.OwnerTenantID == nil
		}
		if credential.Status == entities.StatusActive && ownedBySharer {
			owned[credential.ID] = true
		}
	}
	models, err := a.ModelsSvc.List(ctx)
	if err != nil {
		return err
	}
	callable := make(map[string]bool)
	for _, model := range models {
		for _, route := range model.Routes {
			if model.Enabled && route.Enabled && owned[route.CredentialID] {
				callable[model.Name] = true
			}
		}
	}
	for _, name := range names {
		if !callable[name] {
			return entities.ErrNotFound
		}
	}
	return nil
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
