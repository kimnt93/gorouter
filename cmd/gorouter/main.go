package main

// @title gorouter API
// @version 1.0
// @description Principal-aware model gateway and administration API.
// @BasePath /
// @schemes http https
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Use "Bearer {master-or-api-key}".

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/kimnt93/gorouter/internal/api/handlers"
	"github.com/kimnt93/gorouter/internal/api/routes"
	"github.com/kimnt93/gorouter/internal/platform/database"
	"github.com/kimnt93/gorouter/internal/platform/llm"
	"github.com/kimnt93/gorouter/internal/platform/modeldiscovery"
	platformpricing "github.com/kimnt93/gorouter/internal/platform/pricing"
	"github.com/kimnt93/gorouter/internal/platform/promptcache"
	"github.com/kimnt93/gorouter/internal/platform/refreshlock"
	clickhouserepo "github.com/kimnt93/gorouter/internal/repositories/clickhouse"
	"github.com/kimnt93/gorouter/internal/repositories/postgres"
	"github.com/kimnt93/gorouter/pkg/apikey"
	"github.com/kimnt93/gorouter/pkg/auth"
	"github.com/kimnt93/gorouter/pkg/chat"
	"github.com/kimnt93/gorouter/pkg/config"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
	"github.com/kimnt93/gorouter/pkg/identity"
	"github.com/kimnt93/gorouter/pkg/modelroute"
	oauthpkg "github.com/kimnt93/gorouter/pkg/oauth"
	"github.com/kimnt93/gorouter/pkg/pricing"
	"github.com/kimnt93/gorouter/pkg/providerquota"
	"github.com/kimnt93/gorouter/pkg/quota"
	"github.com/kimnt93/gorouter/pkg/seal"
	"github.com/kimnt93/gorouter/pkg/tenant"
	"github.com/kimnt93/gorouter/pkg/usage"
)

type modelStore interface {
	modelroute.Repository
	entities.CatalogPriceRepository
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	var tenantRepo tenant.Repository
	var credRepo credential.Repository
	var keyRepo apikey.Repository
	var modelRepo modelStore
	var usageRepo usage.Repository
	var identityRepo identity.Repository
	var auditRepo entities.AuditRepository
	var hashSecret func(string) string
	var generateSecret func() string
	var clickhouseStore *clickhouserepo.Store
	var providerQuotaStore providerquota.Store
	if cfg.DatabaseBackend == "clickhouse" {
		db, connectErr := database.ConnectClickHouse(ctx, cfg.ClickHouseURL)
		if connectErr != nil {
			log.Fatal(connectErr)
		}
		defer db.Close()
		if err := db.Migrate(ctx); err != nil {
			log.Fatal(err)
		}
		store := clickhouserepo.New(db.Conn)
		clickhouseStore = store
		providerQuotaStore = clickhouserepo.NewProviderQuotaRepo(store)
		tenantRepo, credRepo, keyRepo = clickhouserepo.NewTenantRepo(store), clickhouserepo.NewCredentialRepo(store), clickhouserepo.NewApiKeyRepo(store)
		modelRepo, usageRepo = clickhouserepo.NewModelRouteRepo(store), clickhouserepo.NewUsageRepo(store)
		identityRepo, auditRepo = clickhouserepo.NewIdentityRepo(store), clickhouserepo.NewAuditRepo(store)
		hashSecret, generateSecret = clickhouserepo.HashSecret, clickhouserepo.GenerateSecret
	} else {
		db, connectErr := database.Connect(ctx, cfg.DatabaseURL)
		if connectErr != nil {
			log.Fatal(connectErr)
		}
		defer db.Close()
		if err := db.Migrate(ctx); err != nil {
			log.Fatal(err)
		}
		store := postgres.New(db.Pool)
		providerQuotaStore = postgres.NewProviderQuotaRepo(store)
		tenantRepo, credRepo, keyRepo = postgres.NewTenantRepo(store), postgres.NewCredentialRepo(store), postgres.NewApiKeyRepo(store)
		modelRepo, usageRepo = postgres.NewModelRouteRepo(store), postgres.NewUsageRepo(store)
		identityRepo, auditRepo = postgres.NewIdentityRepo(store), postgres.NewAuditRepo(store)
		hashSecret, generateSecret = postgres.HashSecret, postgres.GenerateSecret
	}

	box, err := seal.New(cfg.EncryptionKey)
	if err != nil {
		log.Fatal(err)
	}
	tenantSvc := tenant.NewService(tenantRepo)
	if err := tenantSvc.EnsureDefault(ctx); err != nil {
		log.Fatal(err)
	}
	credSvc := credential.NewService(credRepo, box)
	keySvc := apikey.NewService(keyRepo, hashSecret, generateSecret)
	identitySvc := identity.NewService(identityRepo, auditRepo)
	identitySvc.SetAuthorizationCache(keySvc)
	modelSvc := modelroute.NewService(modelRepo)
	priceResolver := pricing.NewResolver(modelRepo)
	if err := priceResolver.Refresh(ctx); err != nil {
		log.Printf("load cached model prices: %v", err)
	}
	modelSvc.SetPriceCache(priceResolver)
	pending := usage.NewPending()
	usageSvc := usage.NewServiceWithConcurrency(usageRepo, cfg.UsageWriteQueueSize, cfg.UsageWriteConcurrency, pending)
	defer usageSvc.Close()
	authSvc := auth.NewServiceWithIdentity(cfg.MasterKey, cfg.SessionSecret, keySvc, identityRepo)
	cacheSvc, redisClient, err := promptcache.New(cfg.Cache, cfg.RedisURL)
	if err != nil {
		log.Fatal(err)
	}
	defer cacheSvc.Close()
	if redisClient != nil {
		defer redisClient.Close()
	}
	if redisClient == nil && cfg.RedisURL != "" {
		redisOptions, parseErr := redis.ParseURL(cfg.RedisURL)
		if parseErr != nil {
			log.Fatal(parseErr)
		}
		redisClient = redis.NewClient(redisOptions)
		pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		pingErr := redisClient.Ping(pingCtx).Err()
		cancel()
		if pingErr != nil {
			_ = redisClient.Close()
			log.Fatal(pingErr)
		}
		defer redisClient.Close()
	}
	if redisClient != nil {
		keySvc.SetTokenCache(redisClient, cfg.APITokenCacheTTL)
		if err := priceResolver.EnableRedisInvalidation(ctx, redisClient); err != nil {
			log.Fatal(err)
		}
	}
	var distributedRefreshLock *refreshlock.Redis
	if redisClient != nil {
		distributedRefreshLock, err = refreshlock.NewRedis(redisClient, 10*time.Minute)
		if err != nil {
			log.Fatal(err)
		}
	}
	if cfg.Pricing.Enabled {
		priceImporter := &platformpricing.HTTPImporter{
			Client: &http.Client{Timeout: cfg.Pricing.HTTPTimeout},
			URL:    cfg.Pricing.CatalogURL, Source: platformpricing.SourceOpenRouter,
		}
		priceSync := pricing.NewCatalogService(modelRepo, priceImporter, platformpricing.SourceOpenRouter, priceResolver)
		if distributedRefreshLock != nil {
			priceSync.SetRefreshLocker(distributedRefreshLock)
		}
		priceSync.Start(ctx, cfg.Pricing.SyncInterval, func(err error) { log.Printf("sync OpenRouter catalog: %v", err) })
	}
	if clickhouseStore != nil {
		if redisClient != nil {
			locker, lockErr := clickhouserepo.NewRedisMutationLocker(redisClient, 15*time.Second)
			if lockErr != nil {
				log.Fatal(lockErr)
			}
			clickhouseStore.SetMutationLocker(locker)
		} else if !cfg.ClickHouseSingleWriter {
			log.Fatal("ClickHouse administration requires Redis locking unless CLICKHOUSE_SINGLE_WRITER=true is explicitly declared")
		}
	}
	quota.SetWeekStart(cfg.WeekStart)
	var quotaSvc quota.Coordinator
	if redisClient != nil {
		policy, parseErr := quota.ParsePolicy(cfg.Quota.RedisPolicy)
		if parseErr != nil {
			log.Fatal(parseErr)
		}
		quotaSvc, err = quota.NewRedis(redisClient, policy)
		if err != nil {
			log.Fatal(err)
		}
	}

	client := llm.NewHTTPClient()
	openai := &llm.OpenAIAdapter{HTTP: client}
	anthropic := &llm.AnthropicAdapter{HTTP: client, OAuthClientID: cfg.OAuthClientID}
	refresher := &llm.AnthropicOAuthRefresher{HTTP: client, TokenURL: cfg.OAuthTokenURL, ClientID: cfg.OAuthClientID, Persister: credSvc}
	anthropic.Refresh = refresher.Refresh
	claudeCode := &llm.ClaudeCodeAdapter{AnthropicAdapter: anthropic}
	codex := &llm.CodexAdapter{HTTP: client}
	codexRefresher := &llm.CodexOAuthRefresher{HTTP: client, TokenURL: cfg.CodexOAuthTokenURL, ClientID: cfg.CodexOAuthClientID, Persister: credSvc}
	codex.Refresh = codexRefresher.Refresh
	copilot := &llm.CopilotAdapter{HTTP: client}
	grokBuild := &llm.GrokBuildAdapter{HTTP: client, Persister: credSvc, ClientID: cfg.GrokOAuthClientID}
	xaiOAuth := &llm.XAIAdapter{HTTP: client, Persister: credSvc, ClientID: cfg.GrokOAuthClientID}
	cline := &llm.ClineAdapter{HTTP: client, Persister: credSvc}
	clinePass := &llm.ClinePassAdapter{HTTP: client, Persister: credSvc}
	kiloCode := &llm.KiloCodeAdapter{HTTP: client}
	kimiCode := &llm.KimiCodeAdapter{HTTP: client, Persister: credSvc, ClientID: cfg.KimiOAuthClientID}
	cursor := &llm.CursorAdapter{HTTP: client, Persister: credSvc}
	kiro := &llm.KiroAdapter{HTTP: client, Persister: credSvc}
	amazonQ := &llm.AmazonQAdapter{HTTP: client, Persister: credSvc}
	antigravity := &llm.AntigravityAdapter{HTTP: client, Persister: credSvc, ClientID: cfg.AntigravityOAuthClientID, ClientSecret: cfg.AntigravityOAuthClientSecret}
	opencodeGo := &llm.OpenCodeGoAdapter{HTTP: client}
	opencodeZen := &llm.OpenCodeZenAdapter{HTTP: client}
	providerProbes := map[string]credential.ConnectivityProber{
		"claude":         claudeCode,
		"github-copilot": copilot,
		"grok-build":     grokBuild, "xai-oauth": xaiOAuth, "cline": cline, "clinepass": clinePass, "kilo-code": kiloCode,
		"kimi-code": kimiCode, "cursor": cursor, "kiro": kiro, "amazon-q": amazonQ, "antigravity": antigravity,
		"opencode-go":  opencodeGo,
		"opencode-zen": opencodeZen,
	}
	if cfg.ModelCatalog.Enabled {
		if redisClient != nil {
			credSvc.SetModelDiscoveryCache(modeldiscovery.NewRedis(redisClient), cfg.ModelCatalog.CacheTTL)
		}
		catalogSync := &modelroute.CatalogSync{Credentials: credSvc, Models: modelSvc, Locker: distributedRefreshLock, Discoverer: func(providerID string) credential.ModelDiscoverer {
			return credential.ResolveModelDiscoverer(providerID, providerProbes, openai, anthropic, codex)
		}, OrganizationName: func(ctx context.Context, id string) (string, error) {
			organization, err := identityRepo.OrganizationByID(ctx, id)
			if err != nil {
				return "", err
			}
			return organization.Name, nil
		}}
		catalogSync.Start(ctx, cfg.ModelCatalog.SyncInterval, func(err error) { log.Printf("sync provider model catalogs: %v", err) })
		credSvc.SetCredentialsChanged(catalogSync.Trigger)
	}
	providerUpstreams := make(map[string]entities.Upstream, len(providerProbes))
	for id, adapter := range providerProbes {
		if upstream, ok := adapter.(entities.Upstream); ok {
			providerUpstreams[id] = upstream
		}
	}
	oauthSvc := oauthpkg.New(client, credSvc, oauthpkg.Config{
		ClaudeClientID: cfg.OAuthClientID, ClaudeTokenURL: cfg.OAuthTokenURL,
		CodexClientID: cfg.CodexOAuthClientID, CodexTokenURL: cfg.CodexOAuthTokenURL,
		GitHubClientID: cfg.GitHubOAuthClientID, GrokClientID: cfg.GrokOAuthClientID,
		KimiClientID: cfg.KimiOAuthClientID, AntigravityClientID: cfg.AntigravityOAuthClientID,
		AntigravityClientSecret: cfg.AntigravityOAuthClientSecret,
	})
	if redisClient != nil {
		oauthSvc.SetFlowStore(oauthpkg.NewRedisFlowStore(redisClient))
	}
	providerQuotaSvc := providerquota.New(client, credSvc)
	providerQuotaSvc.SetStore(providerQuotaStore)
	if redisClient != nil {
		providerQuotaSvc.SetStateCache(providerquota.NewRedisState(redisClient))
	}
	if err := providerQuotaSvc.Restore(context.Background()); err != nil {
		log.Printf("restore provider quota snapshots: %v", err)
	}
	selector := &chat.Selector{}
	health := chat.NewHealth()
	if redisClient != nil {
		selector.SetRedis(redisClient)
		health.SetRedis(redisClient)
	}
	gw := &handlers.Gateway{
		Keys: keySvc, Creds: credSvc, Models: modelSvc, Usage: usageSvc,
		Cache: cacheSvc, OpenAI: openai, Anthropic: anthropic, Codex: codex, Providers: providerUpstreams,
		Selector: selector, Health: health, Quota: quotaSvc,
		Pricing: priceResolver, ProviderQuotas: providerQuotaSvc,
	}
	app := routes.New(routes.Dependencies{
		Auth: authSvc, Tenants: tenantSvc, Credentials: credSvc, Keys: keySvc,
		Models: modelSvc, Usage: usageSvc, Cache: cacheSvc, Gateway: gw,
		Identity: identitySvc, IdentityRepo: identityRepo, Audit: auditRepo,
		OpenAI: openai, Anthropic: anthropic, Codex: codex, Providers: providerProbes, OAuth: oauthSvc, OAuthAvailable: oauthSvc.OAuthAvailable,
		Pricing: priceResolver, ProviderQuotas: providerQuotaSvc,
		BodyLimit: int(cfg.RequestLimit), ReadTimeout: cfg.RequestTimeout,
	})

	go func() {
		if err := app.Listen(cfg.Listen); err != nil && !errors.Is(err, syscall.EINTR) {
			log.Fatal(err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	_ = app.Shutdown()
}
