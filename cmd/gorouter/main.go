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
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"

	"github.com/kimnt93/gorouter/internal/api/handlers"
	"github.com/kimnt93/gorouter/internal/api/routes"
	"github.com/kimnt93/gorouter/internal/platform/database"
	"github.com/kimnt93/gorouter/internal/platform/llm"
	"github.com/kimnt93/gorouter/internal/platform/modeldiscovery"
	"github.com/kimnt93/gorouter/internal/platform/observability"
	platformpricing "github.com/kimnt93/gorouter/internal/platform/pricing"
	"github.com/kimnt93/gorouter/internal/platform/promptcache"
	"github.com/kimnt93/gorouter/internal/platform/refreshlock"
	clickhouserepo "github.com/kimnt93/gorouter/internal/repositories/clickhouse"
	localrepo "github.com/kimnt93/gorouter/internal/repositories/local"
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
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	observability.SetupLogger(cfg.ServiceName, cfg.DevelopmentEnvironment, cfg.LogLevel, cfg.LogTimeFormat)
	if cfg.MasterKey == "secret" {
		log.Warn().Msg("using default master key; override MASTER_KEY outside private local use")
	}

	ctx := context.Background()
	shutdownTracer, err := observability.InitTracer(ctx, cfg.ServiceName, cfg.DevelopmentEnvironment, cfg.Telemetry)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to initialize tracing")
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTracer(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("failed to shut down tracing")
		}
	}()
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
	switch cfg.DatabaseBackend {
	case "clickhouse":
		db, connectErr := database.ConnectClickHouse(ctx, cfg.DatabaseConnectionURL)
		if connectErr != nil {
			log.Fatal().Err(connectErr).Msg("failed to connect ClickHouse")
		}
		defer db.Close()
		if err := db.Migrate(ctx); err != nil {
			log.Fatal().Err(err).Msg("failed to migrate ClickHouse")
		}
		store := clickhouserepo.New(db.Conn)
		clickhouseStore = store
		providerQuotaStore = clickhouserepo.NewProviderQuotaRepo(store)
		tenantRepo, credRepo, keyRepo = clickhouserepo.NewTenantRepo(store), clickhouserepo.NewCredentialRepo(store), clickhouserepo.NewApiKeyRepo(store)
		modelRepo, usageRepo = clickhouserepo.NewModelRouteRepo(store), clickhouserepo.NewUsageRepo(store)
		identityRepo, auditRepo = clickhouserepo.NewIdentityRepo(store), clickhouserepo.NewAuditRepo(store)
		hashSecret, generateSecret = clickhouserepo.HashSecret, clickhouserepo.GenerateSecret
	case "local":
		db, connectErr := database.ConnectSQLite(ctx, cfg.DatabaseConnectionURL)
		if connectErr != nil {
			log.Fatal().Err(connectErr).Msg("failed to connect SQLite")
		}
		defer db.Close()
		if err := db.Migrate(ctx); err != nil {
			log.Fatal().Err(err).Msg("failed to migrate SQLite")
		}
		store := localrepo.New(db.DB)
		providerQuotaStore = localrepo.NewProviderQuotaRepo(store)
		tenantRepo, credRepo, keyRepo = localrepo.NewTenantRepo(store), localrepo.NewCredentialRepo(store), localrepo.NewApiKeyRepo(store)
		modelRepo, usageRepo = localrepo.NewModelRouteRepo(store), localrepo.NewUsageRepo(store)
		identityRepo, auditRepo = localrepo.NewIdentityRepo(store), localrepo.NewAuditRepo(store)
		hashSecret, generateSecret = localrepo.HashSecret, localrepo.GenerateSecret
	case "postgresql":
		db, connectErr := database.Connect(ctx, cfg.DatabaseConnectionURL)
		if connectErr != nil {
			log.Fatal().Err(connectErr).Msg("failed to connect PostgreSQL")
		}
		defer db.Close()
		if err := db.Migrate(ctx); err != nil {
			log.Fatal().Err(err).Msg("failed to migrate PostgreSQL")
		}
		store := postgres.New(db.Pool)
		providerQuotaStore = postgres.NewProviderQuotaRepo(store)
		tenantRepo, credRepo, keyRepo = postgres.NewTenantRepo(store), postgres.NewCredentialRepo(store), postgres.NewApiKeyRepo(store)
		modelRepo, usageRepo = postgres.NewModelRouteRepo(store), postgres.NewUsageRepo(store)
		identityRepo, auditRepo = postgres.NewIdentityRepo(store), postgres.NewAuditRepo(store)
		hashSecret, generateSecret = postgres.HashSecret, postgres.GenerateSecret
	default:
		log.Fatal().Str("database_backend", cfg.DatabaseBackend).Msg("unsupported database backend")
	}

	box, err := seal.New(cfg.EncryptionKey)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to initialize credential encryption")
	}
	tenantSvc := tenant.NewService(tenantRepo)
	if err := tenantSvc.EnsureDefault(ctx); err != nil {
		log.Fatal().Err(err).Msg("failed to ensure default tenant")
	}
	credSvc := credential.NewService(credRepo, box)
	keySvc := apikey.NewService(keyRepo, hashSecret, generateSecret)
	keySvc.SetSecretBox(box)
	identitySvc := identity.NewService(identityRepo, auditRepo)
	identitySvc.SetAuthorizationCache(keySvc)
	modelSvc := modelroute.NewService(modelRepo)
	priceResolver := pricing.NewResolver(modelRepo)
	if err := priceResolver.Refresh(ctx); err != nil {
		log.Warn().Err(err).Msg("failed to load cached model prices")
	}
	modelSvc.SetPriceCache(priceResolver)
	pending := usage.NewPending()
	usageSvc := usage.NewServiceWithConcurrency(usageRepo, cfg.UsageWriteQueueSize, cfg.UsageWriteConcurrency, pending)
	if cfg.StoreCompletions {
		usageSvc.EnableConversationCapture(box)
		log.Warn().Msg("request and completion content storage is enabled")
	}
	defer usageSvc.Close()
	authSvc := auth.NewServiceWithIdentity(cfg.MasterKey, cfg.SessionSecret, keySvc, identityRepo)
	cacheSvc, redisClient, err := promptcache.New(cfg.Cache, cfg.RedisURL)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to initialize prompt cache")
	}
	defer cacheSvc.Close()
	if redisClient != nil {
		defer redisClient.Close()
	}
	if redisClient == nil && cfg.RedisURL != "" {
		redisOptions, parseErr := redis.ParseURL(cfg.RedisURL)
		if parseErr != nil {
			log.Fatal().Err(parseErr).Msg("failed to parse Redis URL")
		}
		redisClient = redis.NewClient(redisOptions)
		pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		pingErr := redisClient.Ping(pingCtx).Err()
		cancel()
		if pingErr != nil {
			_ = redisClient.Close()
			log.Fatal().Err(pingErr).Msg("failed to connect Redis")
		}
		defer redisClient.Close()
	}
	if redisClient != nil {
		keySvc.SetTokenCache(redisClient, cfg.APITokenCacheTTL)
		if err := priceResolver.EnableRedisInvalidation(ctx, redisClient); err != nil {
			log.Fatal().Err(err).Msg("failed to enable pricing invalidation")
		}
	}
	var distributedRefreshLock *refreshlock.Redis
	if redisClient != nil {
		distributedRefreshLock, err = refreshlock.NewRedis(redisClient, 10*time.Minute)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to initialize distributed refresh lock")
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
		priceSync.Start(ctx, cfg.Pricing.SyncInterval, func(err error) { log.Warn().Err(err).Msg("failed to sync OpenRouter catalog") })
	}
	if clickhouseStore != nil {
		if redisClient != nil {
			locker, lockErr := clickhouserepo.NewRedisMutationLocker(redisClient, 15*time.Second)
			if lockErr != nil {
				log.Fatal().Err(lockErr).Msg("failed to initialize ClickHouse mutation lock")
			}
			clickhouseStore.SetMutationLocker(locker)
		} else if !cfg.ClickHouseSingleWriter {
			log.Fatal().Msg("ClickHouse administration requires Redis locking unless CLICKHOUSE_SINGLE_WRITER=true is explicitly declared")
		}
	}
	quota.SetWeekStart(cfg.WeekStart)
	var quotaSvc quota.Coordinator
	if cfg.DatabaseBackend == "local" {
		quotaSvc = quota.NewMemory()
	} else if redisClient != nil {
		policy, parseErr := quota.ParsePolicy(cfg.Quota.RedisPolicy)
		if parseErr != nil {
			log.Fatal().Err(parseErr).Msg("failed to parse Redis outage policy")
		}
		quotaSvc, err = quota.NewRedis(redisClient, policy)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to initialize Redis quota coordinator")
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
	antigravityClientID, antigravityClientSecret := oauthpkg.ResolveAntigravityClientCredentials(cfg.AntigravityOAuthClientID, cfg.AntigravityOAuthClientSecret)
	antigravity := &llm.AntigravityAdapter{HTTP: client, Persister: credSvc, ClientID: antigravityClientID, ClientSecret: antigravityClientSecret}
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
		catalogSync := &modelroute.CatalogSync{Credentials: credSvc, Models: modelSvc, Discoverer: func(providerID string) credential.ModelDiscoverer {
			return credential.ResolveModelDiscoverer(providerID, providerProbes, openai, anthropic, codex)
		}, OrganizationName: func(ctx context.Context, id string) (string, error) {
			organization, err := identityRepo.OrganizationByID(ctx, id)
			if err != nil {
				return "", err
			}
			return organization.Name, nil
		}}
		if distributedRefreshLock != nil {
			catalogSync.Locker = distributedRefreshLock
		}
		catalogSync.Start(ctx, cfg.ModelCatalog.SyncInterval, func(err error) { log.Warn().Err(err).Msg("failed to sync provider model catalogs") })
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
		KimiClientID: cfg.KimiOAuthClientID, AntigravityClientID: antigravityClientID,
		AntigravityClientSecret: antigravityClientSecret,
	})
	if redisClient != nil {
		oauthSvc.SetFlowStore(oauthpkg.NewRedisFlowStore(redisClient))
	}
	providerQuotaSvc := providerquota.New(client, credSvc)
	providerQuotaSvc.SetStore(providerQuotaStore)
	providerQuotaSvc.SetCodexOAuth(codexRefresher)
	if redisClient != nil {
		providerQuotaSvc.SetStateCache(providerquota.NewRedisState(redisClient))
	}
	if err := providerQuotaSvc.Restore(context.Background()); err != nil {
		log.Warn().Err(err).Msg("failed to restore provider quota snapshots")
	}
	if err := providerQuotaSvc.SyncAccountRings(context.Background()); err != nil {
		log.Warn().Err(err).Msg("failed to sync provider account rings")
	}
	credSvc.AddCredentialsChanged(func() {
		if err := providerQuotaSvc.SyncAccountRings(context.Background()); err != nil {
			log.Warn().Err(err).Msg("failed to sync provider account rings")
		}
	})
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
		RouteRetries: cfg.RouteRetries,
	}
	app := routes.New(routes.Dependencies{
		Auth: authSvc, Tenants: tenantSvc, Credentials: credSvc, Keys: keySvc,
		Models: modelSvc, Usage: usageSvc, Cache: cacheSvc, Gateway: gw,
		Identity: identitySvc, IdentityRepo: identityRepo, Audit: auditRepo,
		OpenAI: openai, Anthropic: anthropic, Codex: codex, Providers: providerProbes, OAuth: oauthSvc, OAuthAvailable: oauthSvc.OAuthAvailable,
		Pricing: priceResolver, ProviderQuotas: providerQuotaSvc,
		BodyLimit: int(cfg.RequestLimit), ReadTimeout: cfg.RequestTimeout,
		TelemetryEnabled: cfg.Telemetry.Enabled,
	})

	log.Info().Str("listen", cfg.Listen).Bool("otel_enabled", cfg.Telemetry.Enabled).Msg("starting server")
	go func() {
		if err := app.Listen(cfg.Listen); err != nil && !errors.Is(err, syscall.EINTR) {
			log.Fatal().Err(err).Msg("server stopped unexpectedly")
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Info().Msg("shutting down server")
	if err := app.Shutdown(); err != nil {
		log.Error().Err(err).Msg("failed to shut down server")
	}
}
