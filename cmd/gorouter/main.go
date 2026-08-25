package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/kimnt93/gorouter/api/handlers"
	"github.com/kimnt93/gorouter/api/routes"
	"github.com/kimnt93/gorouter/pkg/apikey"
	"github.com/kimnt93/gorouter/pkg/auth"
	"github.com/kimnt93/gorouter/pkg/chat"
	"github.com/kimnt93/gorouter/pkg/config"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/modelroute"
	"github.com/kimnt93/gorouter/pkg/quota"
	"github.com/kimnt93/gorouter/pkg/seal"
	"github.com/kimnt93/gorouter/pkg/tenant"
	"github.com/kimnt93/gorouter/pkg/usage"
	"github.com/kimnt93/gorouter/platform/database"
	"github.com/kimnt93/gorouter/platform/llm"
	"github.com/kimnt93/gorouter/platform/promptcache"
	"github.com/kimnt93/gorouter/repositories/postgres"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	db, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		log.Fatal(err)
	}

	box, err := seal.New(cfg.EncryptionKey)
	if err != nil {
		log.Fatal(err)
	}
	repoDB := postgres.New(db.Pool)
	tenantSvc := tenant.NewService(postgres.NewTenantRepo(repoDB))
	if err := tenantSvc.EnsureDefault(ctx); err != nil {
		log.Fatal(err)
	}
	credSvc := credential.NewService(postgres.NewCredentialRepo(repoDB), box)
	keyRepo := postgres.NewApiKeyRepo(repoDB)
	keySvc := apikey.NewService(keyRepo, postgres.HashSecret, postgres.GenerateSecret)
	modelSvc := modelroute.NewService(postgres.NewModelRouteRepo(repoDB))
	pending := usage.NewPending()
	usageSvc := usage.NewService(postgres.NewUsageRepo(repoDB), 2048, pending)
	defer usageSvc.Close()
	authSvc := auth.NewService(cfg.MasterKey, cfg.SessionSecret, keySvc)
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
	gw := &handlers.Gateway{
		Keys: keySvc, Creds: credSvc, Models: modelSvc, Usage: usageSvc,
		Cache: cacheSvc, OpenAI: openai, Anthropic: anthropic,
		Selector: &chat.Selector{}, Health: chat.NewHealth(), Quota: quotaSvc,
	}
	app := routes.New(routes.Dependencies{
		Auth: authSvc, Tenants: tenantSvc, Credentials: credSvc, Keys: keySvc,
		Models: modelSvc, Usage: usageSvc, Cache: cacheSvc, Gateway: gw,
		OpenAI: openai, Anthropic: anthropic, BodyLimit: int(cfg.RequestLimit), ReadTimeout: cfg.RequestTimeout,
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
