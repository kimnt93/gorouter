package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/kimnt93/gorouter/api/handlers"
	"github.com/kimnt93/gorouter/api/routes"
	"github.com/kimnt93/gorouter/pkg/apikey"
	"github.com/kimnt93/gorouter/pkg/auth"
	"github.com/kimnt93/gorouter/pkg/chat"
	"github.com/kimnt93/gorouter/pkg/config"
	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/modelroute"
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

	client := llm.NewHTTPClient()
	openai := &llm.OpenAIAdapter{HTTP: client}
	anthropic := &llm.AnthropicAdapter{HTTP: client, OAuthClientID: cfg.OAuthClientID}
	gw := &handlers.Gateway{
		Keys: keySvc, Creds: credSvc, Models: modelSvc, Usage: usageSvc,
		Cache: cacheSvc, Auth: authSvc, OpenAI: openai, Anthropic: anthropic,
		Selector: &chat.Selector{}, Health: chat.NewHealth(),
	}
	app := routes.New(routes.Dependencies{
		Auth: authSvc, Tenants: tenantSvc, Credentials: credSvc, Keys: keySvc,
		Models: modelSvc, Usage: usageSvc, Cache: cacheSvc, Gateway: gw,
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
