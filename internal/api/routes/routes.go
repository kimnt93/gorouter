package routes

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/contrib/v3/swagger"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/recover"

	"github.com/kimnt93/gorouter/internal/api/handlers"
	"github.com/kimnt93/gorouter/internal/api/spa"
	"github.com/kimnt93/gorouter/internal/api/views"
	"github.com/kimnt93/gorouter/internal/docs"
	"github.com/kimnt93/gorouter/pkg/apikey"
	"github.com/kimnt93/gorouter/pkg/auth"
	"github.com/kimnt93/gorouter/pkg/chat"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/identity"
	"github.com/kimnt93/gorouter/pkg/modelroute"
	oauthpkg "github.com/kimnt93/gorouter/pkg/oauth"
	"github.com/kimnt93/gorouter/pkg/providerquota"
	"github.com/kimnt93/gorouter/pkg/tenant"
	"github.com/kimnt93/gorouter/pkg/usage"
)

type Dependencies struct {
	Auth           *auth.Service
	Tenants        *tenant.Service
	Credentials    *credential.Service
	Keys           *apikey.Service
	Models         *modelroute.Service
	Usage          *usage.Service
	Cache          chat.PromptCache
	Gateway        *handlers.Gateway
	OpenAI         credential.ConnectivityProber
	Anthropic      credential.ConnectivityProber
	Codex          credential.ConnectivityProber
	Providers      map[string]credential.ConnectivityProber
	OAuth          *oauthpkg.Service
	OAuthAvailable func(string) bool
	Pricing        handlers.PriceCatalog
	ProviderQuotas *providerquota.Service
	BodyLimit      int
	ReadTimeout    time.Duration
	Identity       *identity.Service
	IdentityRepo   identity.Repository
	Audit          entities.AuditRepository
}

func New(d Dependencies) *fiber.App {
	app := fiber.New(fiber.Config{BodyLimit: d.BodyLimit, ReadTimeout: d.ReadTimeout, IdleTimeout: 60 * time.Second})
	app.Use(recover.New(), logger.New())
	app.Get("/healthz", handlers.Health)
	app.Use(swagger.New(swagger.Config{BasePath: "/", Path: "docs", Title: "gorouter API", FileContent: []byte(docs.SwaggerInfo.ReadDoc())}))
	app.Get("/assets/:name", func(c fiber.Ctx) error {
		name := filepath.Base(c.Params("name"))
		body, err := views.ReadAsset(name)
		if err != nil {
			return c.SendStatus(fiber.StatusNotFound)
		}
		switch filepath.Ext(name) {
		case ".css":
			c.Type("css")
		case ".js":
			c.Type("js")
		}
		return c.Send(body)
	})
	app.Get("/app-assets/*", func(c fiber.Ctx) error {
		name := c.Params("*")
		body, err := spa.Asset(name)
		if err != nil {
			return c.SendStatus(fiber.StatusNotFound)
		}
		switch {
		case strings.HasSuffix(name, ".css"):
			c.Type("css")
		case strings.HasSuffix(name, ".js"):
			c.Type("js")
		}
		c.Set("Cache-Control", "public, max-age=31536000, immutable")
		return c.Send(body)
	})
	app.Post("/login", (&handlers.Admin{Auth: d.Auth}).Verify)
	app.Post("/logout", (&handlers.Admin{Auth: d.Auth}).Logout)
	app.Get("/login", handlers.LoginPage)
	ui := &handlers.UI{Cache: d.Cache, Usage: d.Usage, Keys: d.Keys, Tenants: d.Tenants, Credentials: d.Credentials, Models: d.Models, Identity: d.Identity, IdentityRepo: d.IdentityRepo, Audit: d.Audit}
	spaPage := func(c fiber.Ctx) error {
		body, err := spa.Index()
		if err != nil {
			return c.SendStatus(fiber.StatusInternalServerError)
		}
		c.Type("html")
		c.Set("Cache-Control", "no-cache")
		return c.Send(body)
	}
	app.Get("/", handlers.Require(d.Auth, ""), spaPage)
	app.Get("/dashboard", handlers.Require(d.Auth, entities.ScopeUsageRead), spaPage)
	app.Get("/dashboard/analysis", handlers.Require(d.Auth, entities.ScopeUsageRead), spaPage)
	app.Get("/dashboard/logs", handlers.Require(d.Auth, entities.ScopeUsageRead), spaPage)
	app.Get("/dashboard/cache", handlers.Require(d.Auth, entities.ScopeUsageRead), spaPage)
	app.Get("/dashboard/providers", handlers.Require(d.Auth, entities.ScopeCredentialsManage), spaPage)
	app.Get("/dashboard/credentials", handlers.Require(d.Auth, entities.ScopeCredentialsManage), spaPage)
	app.Get("/dashboard/models", handlers.Require(d.Auth, entities.ScopeModelsManage), spaPage)
	app.Get("/dashboard/users", handlers.Require(d.Auth, ""), spaPage)
	app.Get("/dashboard/organizations", handlers.Require(d.Auth, ""), spaPage)
	app.Get("/dashboard/keys", handlers.Require(d.Auth, entities.ScopeKeysManage), spaPage)
	app.Get("/dashboard/audit", handlers.Require(d.Auth, entities.ScopeUsageRead), spaPage)
	app.Get("/ui/cache", handlers.Require(d.Auth, entities.ScopeUsageRead), ui.CacheFragment)
	app.Get("/ui/usage", handlers.Require(d.Auth, entities.ScopeUsageRead), func(c fiber.Ctx) error { return c.Redirect().To("/dashboard/logs") })
	app.Get("/ui/users", handlers.Require(d.Auth, ""), func(c fiber.Ctx) error { return c.Redirect().To("/dashboard/users") })
	app.Post("/ui/users", handlers.Require(d.Auth, ""), ui.UserCreate)
	app.Get("/ui/users/:id", handlers.Require(d.Auth, ""), func(c fiber.Ctx) error { return c.Redirect().To("/dashboard/users") })
	app.Post("/ui/users/:id/status", handlers.Require(d.Auth, ""), ui.UserStatus)
	app.Get("/ui/organizations", handlers.Require(d.Auth, ""), func(c fiber.Ctx) error { return c.Redirect().To("/dashboard/organizations") })
	app.Post("/ui/organizations", handlers.Require(d.Auth, ""), ui.OrganizationCreate)
	app.Get("/ui/organizations/:id", handlers.Require(d.Auth, ""), func(c fiber.Ctx) error { return c.Redirect().To("/dashboard/organizations") })
	app.Post("/ui/organizations/:id", handlers.Require(d.Auth, ""), ui.OrganizationUpdate)
	app.Post("/ui/organizations/:id/members", handlers.Require(d.Auth, entities.ScopeMembersManage), ui.MemberAdd)
	app.Post("/ui/organizations/:id/members/:user_id/role", handlers.Require(d.Auth, entities.ScopeMembersManage), ui.MemberRole)
	app.Delete("/ui/organizations/:id/members/:user_id", handlers.Require(d.Auth, entities.ScopeMembersManage), ui.MemberRemove)
	app.Get("/ui/audit", handlers.Require(d.Auth, entities.ScopeUsageRead), func(c fiber.Ctx) error { return c.Redirect().To("/dashboard/audit") })
	app.Get("/ui/keys", handlers.Require(d.Auth, entities.ScopeKeysManage), func(c fiber.Ctx) error { return c.Redirect().To("/dashboard/keys") })
	app.Post("/ui/keys", handlers.Require(d.Auth, entities.ScopeKeysManage), ui.KeysCreate)
	app.Post("/ui/keys/:id/toggle", handlers.Require(d.Auth, entities.ScopeKeysManage), ui.KeyToggle)
	app.Post("/ui/keys/:id/rotate", handlers.Require(d.Auth, entities.ScopeKeysManage), ui.KeyRotate)
	app.Delete("/ui/keys/:id", handlers.Require(d.Auth, entities.ScopeKeysManage), ui.KeyDelete)
	app.Get("/ui/credentials", handlers.Require(d.Auth, entities.ScopeCredentialsManage), func(c fiber.Ctx) error { return c.Redirect().To("/dashboard/credentials") })
	app.Get("/ui/providers", handlers.Require(d.Auth, entities.ScopeCredentialsManage), func(c fiber.Ctx) error { return c.Redirect().To("/dashboard/providers") })
	app.Post("/ui/providers/:provider/connect", handlers.Require(d.Auth, entities.ScopeCredentialsManage), ui.ProviderConnect)
	app.Post("/ui/credentials", handlers.Require(d.Auth, entities.ScopeCredentialsManage), ui.CredentialsCreate)
	app.Post("/ui/credentials/:id/toggle", handlers.Require(d.Auth, entities.ScopeCredentialsManage), ui.CredentialToggle)
	app.Delete("/ui/credentials/:id", handlers.Require(d.Auth, entities.ScopeCredentialsManage), ui.CredentialDelete)
	app.Get("/ui/models", handlers.Require(d.Auth, entities.ScopeModelsManage), func(c fiber.Ctx) error { return c.Redirect().To("/dashboard/models") })
	app.Post("/ui/models", handlers.Require(d.Auth, entities.ScopeModelsManage), ui.ModelsCreate)
	app.Post("/ui/models/:name/price", handlers.Require(d.Auth, entities.ScopeModelsManage), ui.ModelPriceSet)
	app.Delete("/ui/models/:name", handlers.Require(d.Auth, entities.ScopeModelsManage), ui.ModelDelete)
	app.Get("/ui/cache-page", handlers.Require(d.Auth, entities.ScopeUsageRead), func(c fiber.Ctx) error { return c.Redirect().To("/dashboard/cache") })
	app.Post("/ui/cache/flush", handlers.Require(d.Auth, entities.ScopeCachePurge), ui.CacheFlush)

	app.Post("/v1/chat/completions", handlers.Require(d.Auth, "chat"), d.Gateway.Chat)
	app.Post("/v1/chat/completions/", handlers.Require(d.Auth, "chat"), d.Gateway.Chat)
	app.Post("/v1/responses", handlers.Require(d.Auth, "chat"), d.Gateway.Responses)
	app.Post("/v1/responses/", handlers.Require(d.Auth, "chat"), d.Gateway.Responses)
	app.Post("/v1/messages", handlers.Require(d.Auth, "chat"), d.Gateway.Messages)
	app.Post("/v1/messages/", handlers.Require(d.Auth, "chat"), d.Gateway.Messages)
	app.Get("/v1/models", handlers.Require(d.Auth, "chat"), d.Gateway.ListModels)

	admin := &handlers.Admin{Auth: d.Auth, TenantSvc: d.Tenants, CredsSvc: d.Credentials, KeysSvc: d.Keys, ModelsSvc: d.Models, UsageSvc: d.Usage, Cache: d.Cache, Pricing: d.Pricing, IdentitySvc: d.Identity, IdentityRepo: d.IdentityRepo, AuditRepo: d.Audit, OAuthAvailable: d.OAuthAvailable}
	mgmt := app.Group("/admin", handlers.Require(d.Auth, ""))
	mgmt.Get("/session", admin.Session)
	mgmt.Get("/tenants", handlers.Require(d.Auth, entities.ScopeKeysManage), admin.Tenants)
	mgmt.Post("/tenants", handlers.Require(d.Auth, entities.ScopeKeysManage), admin.Tenants)
	mgmt.Get("/organizations", admin.Organizations)
	mgmt.Post("/organizations", admin.Organizations)
	mgmt.Get("/organizations/:id", admin.OrganizationByID)
	mgmt.Patch("/organizations/:id", admin.OrganizationByID)
	mgmt.Get("/organizations/:id/members", admin.Members)
	mgmt.Post("/organizations/:id/members", admin.Members)
	mgmt.Patch("/organizations/:id/members/:user_id", admin.MemberByID)
	mgmt.Delete("/organizations/:id/members/:user_id", admin.MemberByID)
	mgmt.Get("/users", admin.Users)
	mgmt.Post("/users", admin.Users)
	mgmt.Get("/users/:id", admin.UserByID)
	mgmt.Patch("/users/:id", admin.UserByID)
	mgmt.Get("/audit/events", handlers.Require(d.Auth, entities.ScopeUsageRead), admin.AuditEvents)
	mgmt.Get("/credentials", handlers.Require(d.Auth, "credentials:manage"), admin.Credentials)
	mgmt.Post("/credentials", handlers.Require(d.Auth, "credentials:manage"), admin.Credentials)
	mgmt.Put("/credentials/:id", handlers.Require(d.Auth, "credentials:manage"), admin.CredentialByID)
	mgmt.Delete("/credentials/:id", handlers.Require(d.Auth, "credentials:manage"), admin.CredentialByID)
	connectivity := &handlers.CredentialConnectivity{Credentials: d.Credentials, OpenAI: d.OpenAI, Anthropic: d.Anthropic, Codex: d.Codex, Providers: d.Providers, ModelRoutes: d.Models, Quotas: d.ProviderQuotas, Identities: d.IdentityRepo}
	oauthConnector := &handlers.OAuthConnector{Service: d.OAuth, Identities: d.IdentityRepo}
	mgmt.Get("/providers", handlers.Require(d.Auth, entities.ScopeCredentialsManage), admin.Providers)
	mgmt.Post("/oauth/:provider/start", handlers.Require(d.Auth, entities.ScopeCredentialsManage), oauthConnector.Start)
	mgmt.Post("/oauth/:provider/complete", handlers.Require(d.Auth, entities.ScopeCredentialsManage), oauthConnector.Complete)
	mgmt.Post("/credentials/:id/test", handlers.Require(d.Auth, entities.ScopeCredentialsManage), connectivity.Test)
	mgmt.Get("/credentials/:id/quota", handlers.Require(d.Auth, entities.ScopeCredentialsManage), connectivity.Quota)
	mgmt.Post("/credentials/:id/quota", handlers.Require(d.Auth, entities.ScopeCredentialsManage), connectivity.Quota)
	mgmt.Get("/credentials/:id/reset-credits", handlers.Require(d.Auth, entities.ScopeCredentialsManage), connectivity.CodexResetCredits)
	mgmt.Post("/credentials/:id/reset-credits", handlers.Require(d.Auth, entities.ScopeCredentialsManage), connectivity.CodexResetCredits)
	mgmt.Get("/credentials/:id/models", handlers.Require(d.Auth, entities.ScopeCredentialsManage), connectivity.Models)
	mgmt.Post("/credentials/:id/models/import", handlers.Require(d.Auth, entities.ScopeModelsManage), connectivity.ImportModels)
	mgmt.Post("/credentials/:id/models/refresh", handlers.Require(d.Auth, entities.ScopeModelsManage), connectivity.RefreshModelMetadata)
	mgmt.Post("/credentials/:id/chat-tests", handlers.Require(d.Auth, entities.ScopeCredentialsManage), connectivity.Chat)
	mgmt.Get("/api-keys", handlers.Require(d.Auth, entities.ScopeKeysManage), admin.KeysList)
	mgmt.Get("/api-keys/models", handlers.Require(d.Auth, entities.ScopeKeysManage), admin.KeyModelOptions)
	mgmt.Post("/api-keys", handlers.Require(d.Auth, "keys:manage"), admin.KeysCreate)
	mgmt.Patch("/api-keys/:id", handlers.Require(d.Auth, "keys:manage"), admin.KeysPatch)
	mgmt.Post("/api-keys/:id/rotate", handlers.Require(d.Auth, "keys:manage"), admin.KeysRotate)
	mgmt.Delete("/api-keys/:id", handlers.Require(d.Auth, "keys:manage"), admin.KeysDelete)
	mgmt.Get("/models", handlers.Require(d.Auth, "models:manage"), admin.ModelsList)
	mgmt.Put("/models/:name", handlers.Require(d.Auth, "models:manage"), admin.ModelUpsert)
	mgmt.Delete("/models/:name", handlers.Require(d.Auth, "models:manage"), admin.ModelDelete)
	mgmt.Get("/prices", handlers.Require(d.Auth, "models:manage"), admin.Prices)
	mgmt.Get("/pricing/catalog", handlers.Require(d.Auth, entities.ScopeModelsManage), admin.PricingCatalog)
	mgmt.Get("/pricing/estimate", handlers.Require(d.Auth, entities.ScopeModelsManage), admin.PricingEstimate)
	mgmt.Put("/prices/:model", handlers.Require(d.Auth, "models:manage"), admin.Price)
	mgmt.Delete("/prices/:model", handlers.Require(d.Auth, "models:manage"), admin.Price)
	mgmt.Get("/usage/summary", handlers.Require(d.Auth, entities.ScopeUsageRead), admin.UsageSummary)
	mgmt.Get("/usage/recent", handlers.Require(d.Auth, entities.ScopeUsageRead), admin.UsageRecent)
	mgmt.Get("/usage/activity", handlers.Require(d.Auth, entities.ScopeUsageRead), admin.UsageActivity)
	mgmt.Get("/cache/stats", handlers.Require(d.Auth, entities.ScopeUsageRead), admin.CacheStats)
	mgmt.Post("/cache/flush", handlers.Require(d.Auth, "cache:purge"), admin.CacheFlush)
	return app
}
