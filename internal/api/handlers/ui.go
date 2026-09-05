package handlers

import (
	"bytes"
	"errors"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	responseapi "github.com/kimnt93/gorouter/internal/api"
	"github.com/kimnt93/gorouter/internal/api/views"
	"github.com/kimnt93/gorouter/pkg/apikey"
	"github.com/kimnt93/gorouter/pkg/chat"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/identity"
	"github.com/kimnt93/gorouter/pkg/modelroute"
	"github.com/kimnt93/gorouter/pkg/policy"
	providerpkg "github.com/kimnt93/gorouter/pkg/provider"
	"github.com/kimnt93/gorouter/pkg/tenant"
	"github.com/kimnt93/gorouter/pkg/usage"
)

func renderTemplate(c fiber.Ctx, name string, data any) error {
	var b bytes.Buffer
	if err := views.Render(&b, name, data); err != nil {
		return responseapi.For(c).InternalError("template rendering failed").Send()
	}
	c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
	return c.Send(b.Bytes())
}

func LoginPage(c fiber.Ctx) error { return renderTemplate(c, "login.html", nil) }

type UI struct {
	Cache        chat.PromptCache
	Usage        *usage.Service
	Keys         *apikey.Service
	Tenants      *tenant.Service
	Credentials  *credential.Service
	Models       *modelroute.Service
	Identity     *identity.Service
	IdentityRepo identity.Repository
	Audit        entities.AuditRepository
}

type pageData struct {
	Title                  string
	Session                *entities.Session
	CanUsage               bool
	CanAudit               bool
	CanKeys                bool
	CanCredentials         bool
	CanModels              bool
	CanCache               bool
	CanManageGlobal        bool
	Cache                  chat.CacheStats
	Keys                   []entities.ApiKey
	KeyViews               []keyView
	Tenants                []entities.Tenant
	Credentials            []entities.Credential
	Models                 []entities.ModelDef
	Summary                *entities.UsageSummary
	Recent                 []entities.RecentEvent
	CreatedSecret          string
	OAuthProviders         []providerCardData
	APIKeyProviders        []providerCardData
	Users                  []entities.User
	Organizations          []entities.Organization
	Memberships            []entities.Membership
	AuditEvents            []entities.AuditEvent
	Organization           *entities.Organization
	User                   *entities.User
	ActiveOrganizationID   string
	CurrentPath            string
	PrincipalLabel         string
	ActiveOrganizationName string
	AllowPersonalContext   bool
	ContextLocked          bool
	CanManageMembers       bool
	CanCreateOrgKey        bool
	NextCursor             string
	NextURL                string
	Filters                map[string]string
	Error                  string
	UserViews              []userView
	OrganizationViews      []organizationView
	MembershipViews        []membershipView
}
type keyView struct {
	Key       entities.ApiKey
	CanManage bool
}
type userView struct {
	User              entities.User
	OrganizationCount int
	KeyCount          int
}
type organizationView struct {
	Organization entities.Organization
	MemberCount  int
	AdminCount   int
	KeyCount     int
	RequestCount int64
}
type membershipView struct {
	Membership       entities.Membership
	Username         string
	OrganizationName string
}

func (u *UI) UsersPage(c fiber.Ctx) error {
	sess := SessionFrom(c)
	if sess == nil || !sess.IsMaster() {
		return responseapi.For(c).Forbidden("only master may view users").Send()
	}
	data := u.page(c, "Users")
	users, next, err := u.IdentityRepo.ListUsers(c.Context(), entities.PageQuery{Cursor: c.Query("cursor"), Limit: 100, Query: c.Query("q"), Status: c.Query("status")})
	if err != nil {
		return responseapi.For(c).InternalError("failed to load users").Send()
	}
	data.Users = users
	data.Models, _ = u.Models.List(c.Context())
	data.NextCursor = next
	data.NextURL = withCursor(c, next)
	keys, _ := u.Keys.List(c.Context())
	for _, user := range users {
		memberships, _ := u.IdentityRepo.ListMembershipsForUser(c.Context(), user.ID)
		view := userView{User: user, OrganizationCount: len(memberships)}
		for _, key := range keys {
			if key.OwnerUserID == user.ID {
				view.KeyCount++
			}
		}
		data.UserViews = append(data.UserViews, view)
	}
	return renderTemplate(c, "users.html", data)
}
func (u *UI) UserCreate(c fiber.Ctx) error {
	actor := principalFromSession(SessionFrom(c))
	secret := ""
	var err error
	if c.FormValue("generate_initial_key") != "" {
		_, key, createErr := u.Identity.CreateUserWithInitialKey(c.Context(), actor, c.FormValue("username"), u.Keys, apikey.CreateInput{Name: "Initial login key", Models: formValues(c, "models"), Scopes: splitCSV(c.FormValue("scopes"))})
		if createErr != nil {
			return responseapi.For(c).BadRequest(createErr.Error()).Send()
		}
		secret = key.Plaintext
	} else {
		if _, err = u.Identity.CreateUser(c.Context(), actor, c.FormValue("username")); err != nil {
			return responseapi.For(c).BadRequest(err.Error()).Send()
		}
	}
	data := u.page(c, "Users")
	data.CreatedSecret = secret
	data.Users, _, err = u.IdentityRepo.ListUsers(c.Context(), entities.PageQuery{Limit: 100})
	if err != nil {
		return responseapi.For(c).InternalError("failed to refresh users").Send()
	}
	data.Models, _ = u.Models.List(c.Context())
	keys, _ := u.Keys.List(c.Context())
	for _, listedUser := range data.Users {
		memberships, _ := u.IdentityRepo.ListMembershipsForUser(c.Context(), listedUser.ID)
		view := userView{User: listedUser, OrganizationCount: len(memberships)}
		for _, listedKey := range keys {
			if listedKey.OwnerUserID == listedUser.ID {
				view.KeyCount++
			}
		}
		data.UserViews = append(data.UserViews, view)
	}
	return renderTemplate(c, "users.html", data)
}
func (u *UI) UserPage(c fiber.Ctx) error {
	sess := SessionFrom(c)
	if sess == nil || !sess.IsMaster() {
		return responseapi.For(c).Forbidden("only master may view users").Send()
	}
	data := u.page(c, "User")
	user, err := u.IdentityRepo.UserByID(c.Context(), c.Params("id"))
	if err != nil {
		return responseapi.For(c).NotFound("user not found").Send()
	}
	data.User = user
	data.Memberships, err = u.IdentityRepo.ListMembershipsForUser(c.Context(), user.ID)
	if err != nil {
		return responseapi.For(c).InternalError("failed to load memberships").Send()
	}
	for _, membership := range data.Memberships {
		view := membershipView{Membership: membership}
		if organization, organizationErr := u.IdentityRepo.OrganizationByID(c.Context(), membership.OrganizationID); organizationErr == nil {
			view.OrganizationName = organization.Name
		}
		data.MembershipViews = append(data.MembershipViews, view)
	}
	keys, _ := u.Keys.List(c.Context())
	for _, key := range keys {
		if key.OwnerUserID == user.ID {
			data.Keys = append(data.Keys, key)
		}
	}
	since := time.Now().UTC().Add(-30 * 24 * time.Hour)
	query := entities.UsageQuery{Visibility: entities.UsageVisibility{PrincipalType: entities.PrincipalMaster}, UserID: user.ID, Since: &since, Limit: 20}
	data.Summary, _ = u.Usage.SummaryQuery(c.Context(), query)
	if recent, recentErr := u.Usage.Query(c.Context(), query); recentErr == nil {
		data.Recent = recent.Data
	}
	return renderTemplate(c, "user.html", data)
}
func (u *UI) UserStatus(c fiber.Ctx) error {
	if err := u.Identity.SetUserStatus(c.Context(), principalFromSession(SessionFrom(c)), c.Params("id"), c.FormValue("status")); err != nil {
		return responseapi.For(c).BadRequest(err.Error()).Send()
	}
	return u.redirectOrRefresh(c, "/ui/users/"+c.Params("id"), u.UserPage)
}
func (u *UI) OrganizationsPage(c fiber.Ctx) error {
	data := u.page(c, "Organizations")
	actor := principalFromSession(SessionFrom(c))
	organizations, next, err := u.IdentityRepo.ListOrganizations(c.Context(), entities.PageQuery{Cursor: c.Query("cursor"), Limit: 100, Query: c.Query("q"), Status: c.Query("status")})
	if err != nil {
		return responseapi.For(c).InternalError("failed to load organizations").Send()
	}
	if actor.Type != entities.PrincipalMaster {
		allowed := map[string]bool{}
		if actor.Type == entities.PrincipalOrganization {
			allowed[actor.OrganizationID] = true
		} else if actor.OrganizationID != "" {
			allowed[actor.OrganizationID] = true
		} else {
			memberships, _ := u.IdentityRepo.ListMembershipsForUser(c.Context(), actor.UserID)
			for _, m := range memberships {
				allowed[m.OrganizationID] = true
			}
		}
		filtered := organizations[:0]
		for _, organization := range organizations {
			if allowed[organization.ID] {
				filtered = append(filtered, organization)
			}
		}
		organizations = filtered
	}
	data.Organizations = organizations
	data.NextCursor = next
	data.NextURL = withCursor(c, next)
	keys, _ := u.Keys.List(c.Context())
	for _, organization := range organizations {
		memberships, _ := u.IdentityRepo.ListMemberships(c.Context(), organization.ID)
		view := organizationView{Organization: organization, MemberCount: len(memberships)}
		for _, membership := range memberships {
			if membership.Role == entities.MembershipAdmin {
				view.AdminCount++
			}
		}
		for _, key := range keys {
			if key.OwnerOrganizationID == organization.ID {
				view.KeyCount++
			}
		}
		if actor.Type == entities.PrincipalMaster && u.Usage != nil {
			since := time.Now().UTC().Add(-30 * 24 * time.Hour)
			summary, summaryErr := u.Usage.SummaryQuery(c.Context(), entities.UsageQuery{Visibility: entities.UsageVisibility{PrincipalType: entities.PrincipalMaster}, OrganizationID: organization.ID, Since: &since})
			if summaryErr == nil {
				view.RequestCount = summary.Requests
			}
		}
		data.OrganizationViews = append(data.OrganizationViews, view)
	}
	return renderTemplate(c, "organizations.html", data)
}
func (u *UI) OrganizationCreate(c fiber.Ctx) error {
	actor := principalFromSession(SessionFrom(c))
	if _, err := u.Identity.CreateOrganization(c.Context(), actor, c.FormValue("name")); err != nil {
		return responseapi.For(c).BadRequest(err.Error()).Send()
	}
	return u.redirectOrRefresh(c, "/ui/organizations", u.OrganizationsPage)
}
func (u *UI) OrganizationPage(c fiber.Ctx) error {
	data := u.page(c, "Organization")
	actor := principalFromSession(SessionFrom(c))
	organizationID := c.Params("id")
	organization, err := u.IdentityRepo.OrganizationByID(c.Context(), organizationID)
	if err != nil {
		return responseapi.For(c).NotFound("organization not found").Send()
	}
	if actor.Type == entities.PrincipalUser {
		if actor.OrganizationID != "" && actor.OrganizationID != organizationID {
			return responseapi.For(c).NotFound("organization not found").Send()
		}
		membership, membershipErr := u.IdentityRepo.Membership(c.Context(), organizationID, actor.UserID)
		if membershipErr != nil {
			return responseapi.For(c).NotFound("organization not found").Send()
		}
		actor.OrganizationID, actor.MembershipRole = organizationID, membership.Role
	}
	if policy.ViewOrganization(actor, organizationID) != nil {
		return responseapi.For(c).NotFound("organization not found").Send()
	}
	data.Organization = organization
	if message, ok := c.Locals("ui_error").(string); ok {
		data.Error = message
	}
	data.CanManageMembers = policy.ManageMembers(actor, organizationID) == nil
	if data.CanManageMembers {
		data.Memberships, err = u.IdentityRepo.ListMemberships(c.Context(), organizationID)
		if err != nil {
			return responseapi.For(c).InternalError("failed to load members").Send()
		}
		for _, membership := range data.Memberships {
			view := membershipView{Membership: membership}
			if user, userErr := u.IdentityRepo.UserByID(c.Context(), membership.UserID); userErr == nil {
				view.Username = user.Username
			}
			data.MembershipViews = append(data.MembershipViews, view)
		}
	}
	return renderTemplate(c, "organization.html", data)
}
func (u *UI) MemberAdd(c fiber.Ctx) error {
	actor, organizationID := u.organizationActor(c)
	userID := strings.TrimSpace(c.FormValue("user_id"))
	if userID == "" {
		normalized, err := entities.NormalizeUsername(c.FormValue("username"))
		if err != nil {
			return responseapi.For(c).BadRequest(err.Error()).Send()
		}
		user, err := u.IdentityRepo.UserByNormalizedUsername(c.Context(), normalized)
		if err != nil {
			return responseapi.For(c).BadRequest("user does not exist; master must create the user first").Send()
		}
		userID = user.ID
	}
	if _, err := u.Identity.AddMembership(c.Context(), actor, organizationID, userID, c.FormValue("role")); err != nil {
		c.Locals("ui_error", err.Error())
		c.Status(fiber.StatusBadRequest)
		return u.OrganizationPage(c)
	}
	return u.redirectOrRefresh(c, "/ui/organizations/"+organizationID, u.OrganizationPage)
}
func (u *UI) OrganizationUpdate(c fiber.Ctx) error {
	actor := principalFromSession(SessionFrom(c))
	if _, err := u.Identity.UpdateOrganization(c.Context(), actor, c.Params("id"), c.FormValue("name"), c.FormValue("status")); err != nil {
		return responseapi.For(c).BadRequest(err.Error()).Send()
	}
	return u.redirectOrRefresh(c, "/ui/organizations/"+c.Params("id"), u.OrganizationPage)
}
func (u *UI) MemberRole(c fiber.Ctx) error {
	actor, organizationID := u.organizationActor(c)
	if err := u.Identity.ChangeMembershipRole(c.Context(), actor, organizationID, c.Params("user_id"), c.FormValue("role")); err != nil {
		c.Locals("ui_error", err.Error())
		c.Status(fiber.StatusBadRequest)
		return u.OrganizationPage(c)
	}
	return u.redirectOrRefresh(c, "/ui/organizations/"+organizationID, u.OrganizationPage)
}
func (u *UI) MemberRemove(c fiber.Ctx) error {
	actor, organizationID := u.organizationActor(c)
	if err := u.Identity.RemoveMembership(c.Context(), actor, organizationID, c.Params("user_id")); err != nil {
		c.Locals("ui_error", err.Error())
		c.Status(fiber.StatusBadRequest)
		return u.OrganizationPage(c)
	}
	return u.redirectOrRefresh(c, "/ui/organizations/"+organizationID, u.OrganizationPage)
}
func (u *UI) organizationActor(c fiber.Ctx) (entities.Principal, string) {
	actor := principalFromSession(SessionFrom(c))
	organizationID := c.Params("id")
	if actor.Type == entities.PrincipalUser {
		if actor.OrganizationID != "" && actor.OrganizationID != organizationID {
			return actor, organizationID
		}
		if membership, err := u.IdentityRepo.Membership(c.Context(), organizationID, actor.UserID); err == nil {
			actor.OrganizationID, actor.MembershipRole = organizationID, membership.Role
		}
	}
	return actor, organizationID
}
func (u *UI) AuditPage(c fiber.Ctx) error {
	data := u.page(c, "Audit")
	actor, err := u.actorForContext(c, principalFromSession(SessionFrom(c)))
	if err != nil {
		return responseapi.For(c).Forbidden(err.Error()).Send()
	}
	visibility, err := policy.AuditVisibility(actor)
	if err != nil {
		return responseapi.For(c).Forbidden("audit access is not allowed").Send()
	}
	limit := 100
	query := entities.AuditQuery{Visibility: visibility, OrganizationID: data.ActiveOrganizationID, Cursor: c.Query("cursor"), Limit: limit, ActorID: c.Query("actor_id"), Action: c.Query("action"), TargetType: c.Query("target_type"), TargetID: c.Query("target_id")}
	if query.Since, err = optionalRFC3339(c.Query("since")); err != nil {
		return responseapi.For(c).BadRequest("since must be RFC3339").Send()
	}
	if query.Until, err = optionalRFC3339(c.Query("until")); err != nil {
		return responseapi.For(c).BadRequest("until must be RFC3339").Send()
	}
	page, err := u.Audit.QueryAudit(c.Context(), query)
	if err != nil {
		return responseapi.For(c).InternalError("failed to load audit").Send()
	}
	data.AuditEvents = page.Data
	data.NextCursor = page.NextCursor
	data.NextURL = withCursor(c, page.NextCursor)
	return renderTemplate(c, "audit.html", data)
}

func (u *UI) actorForContext(c fiber.Ctx, actor entities.Principal) (entities.Principal, error) {
	requested := strings.TrimSpace(c.Query("organization_id"))
	if requested == "" {
		return actor, nil
	}
	if actor.Type == entities.PrincipalMaster {
		organization, err := u.IdentityRepo.OrganizationByID(c.Context(), requested)
		if err != nil || organization.Status != entities.StatusActive {
			return actor, errors.New("organization context is not accessible")
		}
		actor.OrganizationID = requested
		return actor, nil
	}
	if actor.Type == entities.PrincipalOrganization {
		if actor.OrganizationID != requested {
			return actor, errors.New("organization context is not accessible")
		}
		return actor, nil
	}
	if actor.OrganizationID != "" && actor.OrganizationID != requested {
		return actor, errors.New("organization-scoped key is fixed to its organization")
	}
	membership, err := u.IdentityRepo.Membership(c.Context(), requested, actor.UserID)
	if err != nil {
		return actor, errors.New("organization context is not accessible")
	}
	organization, err := u.IdentityRepo.OrganizationByID(c.Context(), requested)
	if err != nil || organization.Status != entities.StatusActive {
		return actor, errors.New("organization context is not active")
	}
	actor.OrganizationID, actor.MembershipRole = requested, membership.Role
	return actor, nil
}

func optionalRFC3339(value string) (*time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, err
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

type providerCardData struct {
	Definition      providerpkg.Definition
	Credentials     []entities.Credential
	Tenants         []entities.Tenant
	CanManageGlobal bool
}

func (u *UI) page(c fiber.Ctx, title string) pageData {
	sess := SessionFrom(c)
	data := pageData{Title: title, Session: sess, CurrentPath: c.Path(), Filters: map[string]string{
		"q": c.Query("q"), "status": c.Query("status"), "since": c.Query("since"), "until": c.Query("until"),
		"organization_id": c.Query("organization_id"), "user_id": c.Query("user_id"), "model": c.Query("model"),
		"api_key_id": c.Query("api_key_id"), "actor_id": c.Query("actor_id"), "action": c.Query("action"),
		"target_type": c.Query("target_type"), "target_id": c.Query("target_id"),
	}}
	if sess != nil {
		data.PrincipalLabel = sess.Username
		if sess.IsMaster() {
			data.PrincipalLabel = "master"
		}
		data.CanUsage = sess.Has(entities.ScopeUsageRead)
		data.CanAudit = sess.IsMaster() || sess.Has(entities.ScopeUsageRead) && (sess.PrincipalType == entities.PrincipalOrganization || sess.MembershipRole == entities.MembershipAdmin)
		data.CanKeys = sess.Has(entities.ScopeKeysManage)
		data.CanCredentials = sess.Has(entities.ScopeCredentialsManage)
		data.CanModels = sess.Has(entities.ScopeModelsManage)
		data.CanCache = sess.Has(entities.ScopeCachePurge)
		data.CanManageGlobal = sess.IsMaster()
	}
	if u.Cache != nil {
		data.Cache = u.Cache.Stats()
	}
	if u.IdentityRepo != nil && sess != nil {
		organizations, _, err := u.IdentityRepo.ListOrganizations(c.Context(), entities.PageQuery{Limit: 500, Status: entities.StatusActive})
		if err == nil {
			actor := principalFromSession(sess)
			data.AllowPersonalContext = actor.Type == entities.PrincipalUser && actor.OrganizationID == ""
			data.ContextLocked = actor.Type == entities.PrincipalOrganization || actor.Type == entities.PrincipalUser && actor.OrganizationID != ""
			allowed := map[string]bool{}
			if actor.Type == entities.PrincipalMaster {
				for _, organization := range organizations {
					allowed[organization.ID] = true
				}
			} else if actor.Type == entities.PrincipalOrganization {
				allowed[actor.OrganizationID] = true
			} else if actor.OrganizationID != "" {
				allowed[actor.OrganizationID] = true
			} else {
				memberships, _ := u.IdentityRepo.ListMembershipsForUser(c.Context(), actor.UserID)
				for _, membership := range memberships {
					allowed[membership.OrganizationID] = true
				}
			}
			for _, organization := range organizations {
				if allowed[organization.ID] {
					data.Organizations = append(data.Organizations, organization)
					if organization.ID == sess.OrganizationID {
						data.ActiveOrganizationName = organization.Name
						if actor.Type == entities.PrincipalOrganization {
							data.PrincipalLabel = "org:" + organization.Name
						}
					}
				}
			}
			requested := strings.TrimSpace(c.Query("organization_id"))
			if allowed[requested] {
				data.ActiveOrganizationID = requested
				for _, organization := range data.Organizations {
					if organization.ID == requested {
						data.ActiveOrganizationName = organization.Name
						break
					}
				}
				if actor.Type == entities.PrincipalUser && actor.OrganizationID == "" && sess.Has(entities.ScopeUsageRead) {
					if membership, membershipErr := u.IdentityRepo.Membership(c.Context(), requested, actor.UserID); membershipErr == nil && membership.Role == entities.MembershipAdmin {
						data.CanAudit = true
					}
				}
			} else if data.ContextLocked {
				data.ActiveOrganizationID = actor.OrganizationID
			}
		}
	}
	return data
}

func withCursor(c fiber.Ctx, cursor string) string {
	if cursor == "" {
		return ""
	}
	values := make(url.Values)
	for key, raw := range c.Queries() {
		if key != "cursor" && raw != "" {
			values.Set(key, raw)
		}
	}
	values.Set("cursor", cursor)
	return c.Path() + "?" + values.Encode()
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
		return responseapi.For(c).InternalError("failed to load API keys").Send()
	}
	return renderTemplate(c, "keys.html", data)
}

func (u *UI) KeysCreate(c fiber.Ctx) error {
	sess := SessionFrom(c)
	quota, err := optionalFloat(c.FormValue("quota"))
	if err != nil {
		return responseapi.For(c).BadRequest("quota must be a non-negative number").Send()
	}
	quotaPeriod := strings.ToLower(strings.TrimSpace(c.FormValue("quota_period")))
	if quotaPeriod == "" {
		quotaPeriod = entities.QuotaPeriodNone
	}
	rpm, err := optionalInt(c.FormValue("rpm"))
	if err != nil {
		return responseapi.For(c).BadRequest("RPM must be a positive integer").Send()
	}
	models := formValues(c, "models")
	if len(models) == 0 {
		return responseapi.For(c).BadRequest("select at least one model").Send()
	}
	actor := principalFromSession(sess)
	contextActor, contextErr := u.actorForContext(c, actor)
	if c.FormValue("context_organization_id") != "" && c.Query("organization_id") == "" && actor.Type == entities.PrincipalUser && actor.OrganizationID == "" {
		membership, membershipErr := u.IdentityRepo.Membership(c.Context(), c.FormValue("context_organization_id"), actor.UserID)
		if membershipErr == nil {
			contextActor.OrganizationID, contextActor.MembershipRole = c.FormValue("context_organization_id"), membership.Role
		}
	}
	if contextErr != nil {
		return responseapi.For(c).Forbidden(contextErr.Error()).Send()
	}
	in := apikey.CreateInput{Name: c.FormValue("name"), Models: models, Scopes: splitCSV(c.FormValue("scopes")), QuotaUSD: quota, QuotaPeriod: quotaPeriod, RPM: rpm}
	if !sess.IsMaster() && !policy.CanGrant(actor, in.Scopes, in.Models, sess.AllowedModels) {
		return responseapi.For(c).Forbidden("cannot grant scopes or models not held by the current session").Send()
	}
	switch actor.Type {
	case entities.PrincipalMaster:
		in.OwnerType = c.FormValue("owner_type")
		in.OwnerUserID = c.FormValue("owner_user_id")
		in.OwnerOrganizationID = c.FormValue("owner_organization_id")
		in.ContextOrganizationID = c.FormValue("context_organization_id")
		if in.OwnerType == entities.OwnerOrganization {
			in.ContextOrganizationID = in.OwnerOrganizationID
		}
	case entities.PrincipalUser:
		if c.FormValue("key_kind") == "organization" {
			if contextActor.OrganizationID == "" || contextActor.MembershipRole != entities.MembershipAdmin {
				return responseapi.For(c).Forbidden("organization administration is required").Send()
			}
			in.OwnerType = entities.OwnerOrganization
			in.OwnerOrganizationID = contextActor.OrganizationID
			in.ContextOrganizationID = contextActor.OrganizationID
		} else {
			in.OwnerType = entities.OwnerUser
			in.OwnerUserID = actor.UserID
			in.ContextOrganizationID = c.FormValue("context_organization_id")
			if err = u.Identity.ValidateUserKeyContext(c.Context(), actor.UserID, in.ContextOrganizationID); err != nil {
				return responseapi.For(c).Forbidden("active organization membership is required").Send()
			}
		}
	case entities.PrincipalOrganization:
		in.OwnerType = entities.OwnerOrganization
		in.OwnerOrganizationID = actor.OrganizationID
		in.ContextOrganizationID = actor.OrganizationID
	}
	key, err := u.Keys.Create(c.Context(), in)
	if err != nil {
		return responseapi.For(c).BadRequest(err.Error()).Send()
	}
	if err = u.appendUIKeyAudit(c, actor, "key.create", key, map[string]string{"name": key.Name, "owner_type": key.OwnerType}); err != nil {
		return responseapi.For(c).InternalError("API key created but audit write failed").Send()
	}
	data, loadErr := u.loadKeys(c, key.Plaintext)
	if loadErr != nil {
		return responseapi.For(c).InternalError("key created but list refresh failed").Send()
	}
	return renderTemplate(c, "keys.html", data)
}

func (u *UI) KeyToggle(c fiber.Ctx) error {
	enabled, err := strconv.ParseBool(c.FormValue("enabled"))
	if err != nil {
		return responseapi.For(c).BadRequest("enabled must be true or false").Send()
	}
	key, getErr := u.Keys.GetByID(c.Context(), c.Params("id"))
	if getErr != nil || policy.ManageKey(principalFromSession(SessionFrom(c)), *key) != nil {
		return responseapi.For(c).NotFound("API key not found").Send()
	}
	err = u.Keys.Patch(c.Context(), c.Params("id"), &enabled, nil, nil, nil, nil)
	if err != nil {
		return responseapi.For(c).BadRequest(err.Error()).Send()
	}
	action := "key.enable"
	if !enabled {
		action = "key.disable"
	}
	if err = u.appendUIKeyAudit(c, principalFromSession(SessionFrom(c)), action, key, nil); err != nil {
		return responseapi.For(c).InternalError("API key updated but audit write failed").Send()
	}
	return u.redirectOrRefresh(c, "/ui/keys", u.KeysPage)
}

func (u *UI) KeyDelete(c fiber.Ctx) error {
	key, getErr := u.Keys.GetByID(c.Context(), c.Params("id"))
	if getErr != nil || policy.ManageKey(principalFromSession(SessionFrom(c)), *key) != nil {
		return responseapi.For(c).NotFound("API key not found").Send()
	}
	err := u.Keys.Delete(c.Context(), c.Params("id"))
	if err != nil {
		return responseapi.For(c).BadRequest(err.Error()).Send()
	}
	if err = u.appendUIKeyAudit(c, principalFromSession(SessionFrom(c)), "key.delete", key, nil); err != nil {
		return responseapi.For(c).InternalError("API key deleted but audit write failed").Send()
	}
	return u.redirectOrRefresh(c, "/ui/keys", u.KeysPage)
}
func (u *UI) KeyRotate(c fiber.Ctx) error {
	key, err := u.Keys.GetByID(c.Context(), c.Params("id"))
	if err != nil || policy.ManageKey(principalFromSession(SessionFrom(c)), *key) != nil {
		return responseapi.For(c).NotFound("API key not found").Send()
	}
	rotated, err := u.Keys.Rotate(c.Context(), key.ID)
	if err != nil {
		return responseapi.For(c).InternalError("failed to rotate API key").Send()
	}
	if err = u.appendUIKeyAudit(c, principalFromSession(SessionFrom(c)), "key.rotate", rotated, map[string]string{"key_prefix": rotated.SecretPrefix}); err != nil {
		return responseapi.For(c).InternalError("API key rotated but audit write failed").Send()
	}
	data, loadErr := u.loadKeys(c, rotated.Plaintext)
	if loadErr != nil {
		return responseapi.For(c).InternalError("key rotated but list refresh failed").Send()
	}
	return renderTemplate(c, "keys.html", data)
}

func (u *UI) loadKeys(c fiber.Ctx, created string) (pageData, error) {
	data := u.page(c, "API keys")
	sess := SessionFrom(c)
	actor := principalFromSession(sess)
	if selected, selectedErr := u.actorForContext(c, actor); selectedErr == nil {
		actor = selected
	} else if c.Query("organization_id") != "" {
		return data, selectedErr
	}
	data.CanCreateOrgKey = actor.Type == entities.PrincipalMaster || actor.Type == entities.PrincipalOrganization || actor.Type == entities.PrincipalUser && actor.MembershipRole == entities.MembershipAdmin
	var err error
	allKeys, listErr := u.Keys.List(c.Context())
	if listErr != nil {
		return data, listErr
	}
	for _, key := range allKeys {
		if policy.ViewKeyMetadata(actor, key) == nil {
			data.Keys = append(data.Keys, key)
			data.KeyViews = append(data.KeyViews, keyView{Key: key, CanManage: policy.ManageKey(actor, key) == nil})
		}
	}
	if err != nil {
		return data, err
	}
	data.Tenants, err = u.Tenants.List(c.Context())
	if err != nil {
		return data, err
	}
	data.Organizations, _, err = u.IdentityRepo.ListOrganizations(c.Context(), entities.PageQuery{Limit: 500})
	if err != nil {
		return data, err
	}
	if sess.IsMaster() {
		data.Users, _, err = u.IdentityRepo.ListUsers(c.Context(), entities.PageQuery{Limit: 500})
		if err != nil {
			return data, err
		}
	} else if actor.Type == entities.PrincipalUser {
		memberships, _ := u.IdentityRepo.ListMembershipsForUser(c.Context(), actor.UserID)
		allowed := map[string]bool{}
		for _, membership := range memberships {
			allowed[membership.OrganizationID] = true
		}
		filtered := data.Organizations[:0]
		for _, organization := range data.Organizations {
			if allowed[organization.ID] && organization.Status == entities.StatusActive {
				filtered = append(filtered, organization)
			}
		}
		data.Organizations = filtered
	} else {
		filtered := data.Organizations[:0]
		for _, organization := range data.Organizations {
			if organization.ID == actor.OrganizationID {
				filtered = append(filtered, organization)
			}
		}
		data.Organizations = filtered
	}
	data.Models, err = u.Models.List(c.Context())
	if err != nil {
		return data, err
	}
	if sess != nil && !sess.IsMaster() {
		data.Tenants = filterTenants(data.Tenants, sess.TenantID)
		credentials, credentialsErr := u.Credentials.List(c.Context())
		if credentialsErr != nil {
			return data, credentialsErr
		}
		allowedCredentials := map[string]bool{}
		for _, item := range filterCredentialsForSession(credentials, sess) {
			allowedCredentials[item.ID] = true
		}
		models := data.Models[:0]
		for _, model := range data.Models {
			for _, route := range model.Routes {
				if route.Enabled && allowedCredentials[route.CredentialID] {
					models = append(models, model)
					break
				}
			}
		}
		data.Models = models
	}
	enabledModels := data.Models[:0]
	for _, model := range data.Models {
		if model.Enabled {
			enabledModels = append(enabledModels, model)
		}
	}
	data.Models = enabledModels
	data.CreatedSecret = created
	return data, err
}

func (u *UI) appendUIKeyAudit(c fiber.Ctx, actor entities.Principal, action string, key *entities.ApiKey, metadata map[string]string) error {
	if u.Audit == nil {
		return nil
	}
	actorID, label := actor.UserID, actor.Username
	switch actor.Type {
	case entities.PrincipalMaster:
		actorID, label = "master", "master"
	case entities.PrincipalOrganization:
		actorID = actor.OrganizationID
		if organization, err := u.IdentityRepo.OrganizationByID(c.Context(), actor.OrganizationID); err == nil {
			label = "org:" + organization.Name
		}
	}
	return u.Audit.AppendAudit(c.Context(), entities.AuditEvent{ID: entities.NewID("audit"), TS: time.Now().UTC(), ActorType: actor.Type, ActorID: actorID, ActorLabel: label, OrganizationID: key.ContextOrganizationID, Action: action, TargetType: "api_key", TargetID: key.ID, SafeMetadata: metadata})
}

func (u *UI) CredentialsPage(c fiber.Ctx) error {
	data, err := u.loadCredentials(c)
	if err != nil {
		return responseapi.For(c).InternalError("failed to load credentials").Send()
	}
	return renderTemplate(c, "credentials.html", data)
}

func (u *UI) ProvidersPage(c fiber.Ctx) error {
	data, err := u.loadCredentials(c)
	if err != nil {
		return responseapi.For(c).InternalError("failed to load providers").Send()
	}
	data.Title = "Providers"
	byProvider := make(map[string][]entities.Credential)
	for _, item := range data.Credentials {
		byProvider[item.Provider] = append(byProvider[item.Provider], item)
	}
	for _, definition := range providerpkg.Catalog() {
		card := providerCardData{Definition: definition, Credentials: byProvider[definition.ID], Tenants: data.Tenants, CanManageGlobal: data.CanManageGlobal}
		if definition.Auth == providerpkg.AuthOAuth {
			data.OAuthProviders = append(data.OAuthProviders, card)
		} else {
			data.APIKeyProviders = append(data.APIKeyProviders, card)
		}
	}
	return renderTemplate(c, "providers.html", data)
}

func (u *UI) ProviderConnect(c fiber.Ctx) error {
	definition, ok := providerpkg.Lookup(c.Params("provider"))
	if !ok || definition.Auth != providerpkg.AuthAPIKey {
		return responseapi.For(c).BadRequest("unknown API-key provider").Send()
	}
	sess := SessionFrom(c)
	ownerUserID, allowed := credentialOwnerForSession(sess)
	if !allowed {
		return responseapi.For(c).Forbidden("provider connections are personal to users").Send()
	}
	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		name = definition.Name
	}
	_, err := u.Credentials.Create(c.Context(), entities.CredentialInput{
		Name: name, Provider: definition.ID, Kind: entities.KindAPIKey,
		BaseURL: c.FormValue("base_url"), APIKey: c.FormValue("api_key"), OwnerUserID: ownerUserID,
	})
	if err != nil {
		return responseapi.For(c).BadRequest(err.Error()).Send()
	}
	return u.redirectOrRefresh(c, "/ui/providers", u.ProvidersPage)
}

func (u *UI) CredentialsCreate(c fiber.Ctx) error {
	sess := SessionFrom(c)
	ownerUserID, allowed := credentialOwnerForSession(sess)
	if !allowed {
		return responseapi.For(c).Forbidden("provider connections are personal to users").Send()
	}
	_, err := u.Credentials.Create(c.Context(), entities.CredentialInput{Name: c.FormValue("name"), Provider: c.FormValue("provider"), Kind: c.FormValue("kind"), BaseURL: c.FormValue("base_url"), APIKey: c.FormValue("api_key"), OAuthAccess: c.FormValue("oauth_access"), OAuthRefresh: c.FormValue("oauth_refresh"), OwnerUserID: ownerUserID})
	if err != nil {
		return responseapi.For(c).BadRequest(err.Error()).Send()
	}
	return u.redirectOrRefresh(c, "/ui/providers", u.ProvidersPage)
}

func (u *UI) CredentialDelete(c fiber.Ctx) error {
	sess := SessionFrom(c)
	if sess == nil || !u.sessionOwnsCredential(c, sess, c.Params("id")) {
		return responseapi.For(c).NotFound("credential not found").Send()
	}
	if err := u.Credentials.Delete(c.Context(), c.Params("id")); err != nil {
		if errors.Is(err, entities.ErrNotFound) {
			return responseapi.For(c).NotFound("credential not found").Send()
		}
		return responseapi.For(c).InternalError("failed to delete credential").Send()
	}
	return u.redirectOrRefresh(c, "/ui/providers", u.ProvidersPage)
}

func (u *UI) CredentialToggle(c fiber.Ctx) error {
	sess := SessionFrom(c)
	credentials, err := u.Credentials.List(c.Context())
	if err != nil {
		return responseapi.For(c).InternalError("failed to load credential").Send()
	}
	var selected *entities.Credential
	for i := range credentials {
		if credentials[i].ID == c.Params("id") {
			selected = &credentials[i]
			break
		}
	}
	if selected == nil || !credentialOwnedBySession(*selected, sess) {
		return responseapi.For(c).NotFound("credential not found").Send()
	}
	status := "active"
	if selected.Status == "active" {
		status = "disabled"
	}
	if _, err := u.Credentials.Update(c.Context(), selected.ID, entities.CredentialUpdate{
		Name: selected.Name, BaseURL: selected.BaseURL, Status: status,
	}); err != nil {
		return responseapi.For(c).BadRequest(err.Error()).Send()
	}
	return u.redirectOrRefresh(c, "/ui/providers", u.ProvidersPage)
}

func (u *UI) loadCredentials(c fiber.Ctx) (pageData, error) {
	data := u.page(c, "Credentials")
	var err error
	data.Credentials, err = u.Credentials.List(c.Context())
	if err != nil {
		return data, err
	}
	sess := SessionFrom(c)
	data.Credentials = filterCredentialsForSession(data.Credentials, sess)
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
		return responseapi.For(c).InternalError("failed to load models").Send()
	}
	data.Credentials, err = u.Credentials.List(c.Context())
	if err != nil {
		return responseapi.For(c).InternalError("failed to load credentials").Send()
	}
	if sess := SessionFrom(c); sess != nil {
		data.Credentials = filterCredentialsForSession(data.Credentials, sess)
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
		return responseapi.For(c).Forbidden("only the master session can change global model routes").Send()
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
		return responseapi.For(c).BadRequest(err.Error()).Send()
	}
	return u.redirectOrRefresh(c, "/ui/models", u.ModelsPage)
}

func (u *UI) ModelDelete(c fiber.Ctx) error {
	if sess := SessionFrom(c); sess == nil || !sess.IsMaster() {
		return responseapi.For(c).Forbidden("only the master session can change global model routes").Send()
	}
	if err := u.Models.Delete(c.Context(), decodedPathParam(c, "name")); err != nil {
		if errors.Is(err, entities.ErrNotFound) {
			return responseapi.For(c).NotFound("model not found").Send()
		}
		return responseapi.For(c).InternalError("failed to delete model").Send()
	}
	return u.redirectOrRefresh(c, "/ui/models", u.ModelsPage)
}

func (u *UI) ModelPriceSet(c fiber.Ctx) error {
	if sess := SessionFrom(c); sess == nil || !sess.IsMaster() {
		return responseapi.For(c).Forbidden("only the master session can change global prices").Send()
	}
	readPrice := func(name string) (float64, error) {
		value, err := optionalFloat(c.FormValue(name))
		if err != nil {
			return 0, err
		}
		if value == nil {
			return 0, nil
		}
		return *value, nil
	}
	input, err := readPrice("input_per_m")
	if err != nil {
		return responseapi.For(c).BadRequest("input price must be non-negative").Send()
	}
	output, err := readPrice("output_per_m")
	if err != nil {
		return responseapi.For(c).BadRequest("output price must be non-negative").Send()
	}
	cacheRead, err := readPrice("cached_input_per_m")
	if err != nil {
		return responseapi.For(c).BadRequest("cache-read price must be non-negative").Send()
	}
	cacheWrite, err := readPrice("cache_write_per_m")
	if err != nil {
		return responseapi.For(c).BadRequest("cache-write price must be non-negative").Send()
	}
	price := entities.Price{InputPerM: input, OutputPerM: output, CachedInputPerM: cacheRead, CacheWritePerM: cacheWrite}
	if err := u.Models.SetPrice(c.Context(), decodedPathParam(c, "name"), price); err != nil {
		return responseapi.For(c).BadRequest(err.Error()).Send()
	}
	return u.redirectOrRefresh(c, "/ui/models", u.ModelsPage)
}

func (u *UI) UsagePage(c fiber.Ctx) error {
	data := u.page(c, "Usage")
	var err error
	since := time.Now().UTC().Add(-30 * 24 * time.Hour)
	actor, contextErr := u.actorForContext(c, principalFromSession(SessionFrom(c)))
	if contextErr != nil {
		return responseapi.For(c).Forbidden(contextErr.Error()).Send()
	}
	organizationWide := actor.Type == entities.PrincipalOrganization || actor.MembershipRole == entities.MembershipAdmin
	visibility, policyErr := policy.UsageVisibility(actor, organizationWide)
	if policyErr != nil {
		return responseapi.For(c).Forbidden("usage access is not allowed").Send()
	}
	query := entities.UsageQuery{Visibility: visibility, Cursor: c.Query("cursor"), Since: &since, Limit: 100, OrganizationID: data.ActiveOrganizationID, UserID: c.Query("user_id"), Model: c.Query("model"), APIKeyID: c.Query("api_key_id")}
	data.Models, _ = u.Models.List(c.Context())
	if keys, keyErr := u.Keys.List(c.Context()); keyErr == nil {
		for _, key := range keys {
			if policy.ViewKeyMetadata(actor, key) == nil {
				data.Keys = append(data.Keys, key)
			}
		}
	}
	if actor.Type == entities.PrincipalMaster {
		data.Users, _, _ = u.IdentityRepo.ListUsers(c.Context(), entities.PageQuery{Limit: 500})
	} else if actor.OrganizationID != "" && organizationWide {
		memberships, _ := u.IdentityRepo.ListMemberships(c.Context(), actor.OrganizationID)
		for _, membership := range memberships {
			if user, userErr := u.IdentityRepo.UserByID(c.Context(), membership.UserID); userErr == nil {
				data.Users = append(data.Users, *user)
			}
		}
	}
	if value := c.Query("since"); value != "" {
		query.Since, err = optionalRFC3339(value)
		if err != nil {
			return responseapi.For(c).BadRequest("since must be RFC3339").Send()
		}
	}
	if query.Until, err = optionalRFC3339(c.Query("until")); err != nil {
		return responseapi.For(c).BadRequest("until must be RFC3339").Send()
	}
	if value := c.Query("status"); value != "" {
		status, parseErr := strconv.Atoi(value)
		if parseErr != nil {
			return responseapi.For(c).BadRequest("status must be an integer").Send()
		}
		query.StatusCode = &status
	}
	data.Summary, err = u.Usage.SummaryQuery(c.Context(), query)
	if err != nil {
		return responseapi.For(c).InternalError("failed to load usage summary").Send()
	}
	page, queryErr := u.Usage.Query(c.Context(), query)
	if queryErr == nil {
		data.Recent = page.Data
		data.NextCursor = page.NextCursor
		data.NextURL = withCursor(c, page.NextCursor)
	}
	err = queryErr
	if err != nil {
		return responseapi.For(c).InternalError("failed to load recent usage").Send()
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

func (u *UI) sessionOwnsCredential(c fiber.Ctx, session *entities.Session, credentialID string) bool {
	credentials, err := u.Credentials.List(c.Context())
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

func credentialOwnedBySession(credential entities.Credential, session *entities.Session) bool {
	if session == nil || credential.OwnerTenantID != nil {
		return false
	}
	if session.IsMaster() {
		return credential.OwnerUserID == ""
	}
	return session.PrincipalType == entities.PrincipalUser && session.UserID != "" && credential.OwnerUserID == session.UserID
}

func credentialOwnerForSession(session *entities.Session) (string, bool) {
	if session == nil {
		return "", false
	}
	if session.IsMaster() {
		return "", true
	}
	if session.PrincipalType == entities.PrincipalUser && strings.TrimSpace(session.UserID) != "" {
		return session.UserID, true
	}
	return "", false
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

func filterCredentialsForSession(in []entities.Credential, session *entities.Session) []entities.Credential {
	out := make([]entities.Credential, 0, len(in))
	for _, item := range in {
		if credentialOwnedBySession(item, session) {
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

func formValues(c fiber.Ctx, key string) []string {
	raw := c.Request().PostArgs().PeekMulti(key)
	values := make([]string, 0, len(raw))
	for _, item := range raw {
		value := strings.TrimSpace(string(item))
		if value != "" {
			values = append(values, value)
		}
	}
	if len(values) == 1 && strings.Contains(values[0], ",") {
		return splitCSV(values[0])
	}
	return values
}

func optionalFloat(value string) (*float64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	n, err := strconv.ParseFloat(value, 64)
	if err != nil || n < 0 || math.IsNaN(n) || math.IsInf(n, 0) {
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
