package routes

import (
	"path/filepath"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/recover"

	"github.com/kimnt93/gorouter/api/handlers"
	"github.com/kimnt93/gorouter/api/views"
	"github.com/kimnt93/gorouter/pkg/apikey"
	"github.com/kimnt93/gorouter/pkg/auth"
	"github.com/kimnt93/gorouter/pkg/chat"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/modelroute"
	oauthpkg "github.com/kimnt93/gorouter/pkg/oauth"
	"github.com/kimnt93/gorouter/pkg/tenant"
	"github.com/kimnt93/gorouter/pkg/usage"
)

type Dependencies struct {
	Auth        *auth.Service
	Tenants     *tenant.Service
	Credentials *credential.Service
	Keys        *apikey.Service
	Models      *modelroute.Service
	Usage       *usage.Service
	Cache       chat.PromptCache
	Gateway     *handlers.Gateway
	OpenAI      credential.ConnectivityProber
	Anthropic   credential.ConnectivityProber
	Codex       credential.ConnectivityProber
	OAuth       *oauthpkg.Service
	Pricing     handlers.PriceCatalog
	BodyLimit   int
	ReadTimeout time.Duration
}

type healthResponse struct {
	OK bool `json:"ok"`
}

func New(d Dependencies) *fiber.App {
	app := fiber.New(fiber.Config{BodyLimit: d.BodyLimit, ReadTimeout: d.ReadTimeout, IdleTimeout: 60 * time.Second})
	app.Use(recover.New(), logger.New())
	app.Get("/healthz", func(c fiber.Ctx) error { return c.JSON(healthResponse{OK: true}) })
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
	app.Post("/login", (&handlers.Admin{Auth: d.Auth}).Verify)
	app.Post("/logout", (&handlers.Admin{Auth: d.Auth}).Logout)
	app.Get("/login", handlers.LoginPage)
	ui := &handlers.UI{Cache: d.Cache, Usage: d.Usage, Keys: d.Keys, Tenants: d.Tenants, Credentials: d.Credentials, Models: d.Models}
	app.Get("/", handlers.Require(d.Auth, ""), ui.DashboardPage)
	app.Get("/ui/cache", handlers.Require(d.Auth, entities.ScopeUsageRead), ui.CacheFragment)
	app.Get("/ui/usage", handlers.Require(d.Auth, entities.ScopeUsageRead), ui.UsagePage)
	app.Get("/ui/keys", handlers.Require(d.Auth, entities.ScopeKeysManage), ui.KeysPage)
	app.Post("/ui/keys", handlers.Require(d.Auth, entities.ScopeKeysManage), ui.KeysCreate)
	app.Post("/ui/keys/:id/toggle", handlers.Require(d.Auth, entities.ScopeKeysManage), ui.KeyToggle)
	app.Delete("/ui/keys/:id", handlers.Require(d.Auth, entities.ScopeKeysManage), ui.KeyDelete)
	app.Get("/ui/credentials", handlers.Require(d.Auth, entities.ScopeCredentialsManage), ui.CredentialsPage)
	app.Get("/ui/providers", handlers.Require(d.Auth, entities.ScopeCredentialsManage), ui.ProvidersPage)
	app.Post("/ui/providers/:provider/connect", handlers.Require(d.Auth, entities.ScopeCredentialsManage), ui.ProviderConnect)
	app.Post("/ui/credentials", handlers.Require(d.Auth, entities.ScopeCredentialsManage), ui.CredentialsCreate)
	app.Post("/ui/credentials/:id/toggle", handlers.Require(d.Auth, entities.ScopeCredentialsManage), ui.CredentialToggle)
	app.Delete("/ui/credentials/:id", handlers.Require(d.Auth, entities.ScopeCredentialsManage), ui.CredentialDelete)
	app.Get("/ui/models", handlers.Require(d.Auth, entities.ScopeModelsManage), ui.ModelsPage)
	app.Post("/ui/models", handlers.Require(d.Auth, entities.ScopeModelsManage), ui.ModelsCreate)
	app.Post("/ui/models/:name/price", handlers.Require(d.Auth, entities.ScopeModelsManage), ui.ModelPriceSet)
	app.Delete("/ui/models/:name", handlers.Require(d.Auth, entities.ScopeModelsManage), ui.ModelDelete)
	app.Get("/ui/cache-page", handlers.Require(d.Auth, entities.ScopeCachePurge), ui.CachePage)
	app.Post("/ui/cache/flush", handlers.Require(d.Auth, entities.ScopeCachePurge), ui.CacheFlush)

	app.Post("/v1/chat/completions", handlers.Require(d.Auth, "chat"), d.Gateway.Chat)
	app.Post("/v1/chat/completions/", handlers.Require(d.Auth, "chat"), d.Gateway.Chat)
	app.Get("/v1/models", handlers.Require(d.Auth, "chat"), d.Gateway.ListModels)

	admin := &handlers.Admin{Auth: d.Auth, TenantSvc: d.Tenants, CredsSvc: d.Credentials, KeysSvc: d.Keys, ModelsSvc: d.Models, UsageSvc: d.Usage, Cache: d.Cache, Pricing: d.Pricing}
	mgmt := app.Group("/admin", handlers.Require(d.Auth, ""))
	mgmt.Get("/tenants", handlers.Require(d.Auth, entities.ScopeKeysManage), admin.Tenants)
	mgmt.Post("/tenants", handlers.Require(d.Auth, entities.ScopeKeysManage), admin.Tenants)
	mgmt.Get("/credentials", handlers.Require(d.Auth, "credentials:manage"), admin.Credentials)
	mgmt.Post("/credentials", handlers.Require(d.Auth, "credentials:manage"), admin.Credentials)
	mgmt.Put("/credentials/:id", handlers.Require(d.Auth, "credentials:manage"), admin.CredentialByID)
	mgmt.Delete("/credentials/:id", handlers.Require(d.Auth, "credentials:manage"), admin.CredentialByID)
	connectivity := &handlers.CredentialConnectivity{Credentials: d.Credentials, OpenAI: d.OpenAI, Anthropic: d.Anthropic, Codex: d.Codex, ModelRoutes: d.Models}
	oauthConnector := &handlers.OAuthConnector{Service: d.OAuth}
	mgmt.Get("/providers", handlers.Require(d.Auth, entities.ScopeCredentialsManage), admin.Providers)
	mgmt.Post("/oauth/:provider/start", handlers.Require(d.Auth, entities.ScopeCredentialsManage), oauthConnector.Start)
	mgmt.Post("/oauth/:provider/complete", handlers.Require(d.Auth, entities.ScopeCredentialsManage), oauthConnector.Complete)
	mgmt.Post("/credentials/:id/test", handlers.Require(d.Auth, entities.ScopeCredentialsManage), connectivity.Test)
	mgmt.Get("/credentials/:id/models", handlers.Require(d.Auth, entities.ScopeCredentialsManage), connectivity.Models)
	mgmt.Post("/credentials/:id/models/import", handlers.Require(d.Auth, entities.ScopeModelsManage), connectivity.ImportModels)
	mgmt.Post("/credentials/:id/chat-tests", handlers.Require(d.Auth, entities.ScopeCredentialsManage), connectivity.Chat)
	mgmt.Get("/api-keys", handlers.Require(d.Auth, entities.ScopeKeysManage), admin.KeysList)
	mgmt.Post("/api-keys", handlers.Require(d.Auth, "keys:manage"), admin.KeysCreate)
	mgmt.Patch("/api-keys/:id", handlers.Require(d.Auth, "keys:manage"), admin.KeysPatch)
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
	mgmt.Get("/cache/stats", handlers.Require(d.Auth, entities.ScopeUsageRead), admin.CacheStats)
	mgmt.Post("/cache/flush", handlers.Require(d.Auth, "cache:purge"), admin.CacheFlush)
	return app
}
