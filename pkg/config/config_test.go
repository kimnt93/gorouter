package config

import (
	"testing"
	"time"
)

func requiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("MASTER_KEY", "master")
	t.Setenv("DB_BACKEND", "postgresql")
	t.Setenv("DB_HOST", "database.internal")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_USER", "gorouter")
	t.Setenv("DB_PASSWORD", "secret")
	t.Setenv("DB_NAME", "gorouter")
	t.Setenv("DB_SSLMODE", "")
	t.Setenv("REDIS_HOST", "redis.internal")
	t.Setenv("REDIS_PORT", "6379")
	t.Setenv("REDIS_USER", "gorouter")
	t.Setenv("REDIS_PASSWORD", "redis-secret")
	t.Setenv("CACHE_MEMORY_FALLBACK", "")
	t.Setenv("APP_ENV", "production")
	t.Setenv("CACHE_MAX_ENTRY_BYTES", "")
	t.Setenv("CACHE_MAX_TOTAL_BYTES", "")
	t.Setenv("CACHE_SCOPE", "")
	t.Setenv("CACHE_TTL", "")
	t.Setenv("CACHE_ENABLED", "")
	t.Setenv("REQUEST_LIMIT_MB", "")
	t.Setenv("REQUEST_TIMEOUT", "")
	t.Setenv("API_TOKEN_CACHE_TTL", "")
	t.Setenv("WEEK_START", "")
	t.Setenv("USAGE_WRITE_CONCURRENCY", "")
	t.Setenv("USAGE_WRITE_QUEUE_SIZE", "")
	t.Setenv("MODEL_CATALOG_SYNC_ENABLED", "")
	t.Setenv("MODEL_CATALOG_SYNC_INTERVAL", "")
	t.Setenv("MODEL_CATALOG_CACHE_TTL", "")
	t.Setenv("OPENROUTER_CATALOG_ENABLED", "")
	t.Setenv("OPENROUTER_CATALOG_URL", "")
	t.Setenv("OPENROUTER_SYNC_INTERVAL", "")
	t.Setenv("OPENROUTER_HTTP_TIMEOUT", "")
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_ID", "")
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_SECRET", "")
}

func TestTokenQuotaAndUsageWriterDefaultsAndOverrides(t *testing.T) {
	requiredEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APITokenCacheTTL != 10*time.Minute || cfg.WeekStart != time.Sunday || cfg.UsageWriteConcurrency != 4 || cfg.UsageWriteQueueSize != 100000 {
		t.Fatalf("defaults: ttl=%s week=%s concurrency=%d queue=%d", cfg.APITokenCacheTTL, cfg.WeekStart, cfg.UsageWriteConcurrency, cfg.UsageWriteQueueSize)
	}
	requiredEnv(t)
	t.Setenv("API_TOKEN_CACHE_TTL", "30m")
	t.Setenv("WEEK_START", "monday")
	t.Setenv("USAGE_WRITE_CONCURRENCY", "8")
	t.Setenv("USAGE_WRITE_QUEUE_SIZE", "2048")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APITokenCacheTTL != 30*time.Minute || cfg.WeekStart != time.Monday || cfg.UsageWriteConcurrency != 8 || cfg.UsageWriteQueueSize != 2048 {
		t.Fatalf("overrides: %+v", cfg)
	}
}

func TestPricingCatalogDefaultsAndOverrides(t *testing.T) {
	requiredEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Pricing.Enabled || cfg.Pricing.SyncInterval != time.Hour || cfg.Pricing.HTTPTimeout != 30*time.Second {
		t.Fatalf("pricing defaults = %+v", cfg.Pricing)
	}
	requiredEnv(t)
	t.Setenv("OPENROUTER_CATALOG_ENABLED", "false")
	t.Setenv("OPENROUTER_CATALOG_URL", "https://catalog.example/models")
	t.Setenv("OPENROUTER_SYNC_INTERVAL", "15m")
	t.Setenv("OPENROUTER_HTTP_TIMEOUT", "5s")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Pricing.Enabled || cfg.Pricing.CatalogURL != "https://catalog.example/models" || cfg.Pricing.SyncInterval != 15*time.Minute || cfg.Pricing.HTTPTimeout != 5*time.Second {
		t.Fatalf("pricing overrides = %+v", cfg.Pricing)
	}
}

func TestModelCatalogDefaultsAndOverrides(t *testing.T) {
	requiredEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ModelCatalog.Enabled || cfg.ModelCatalog.SyncInterval != 15*time.Minute || cfg.ModelCatalog.CacheTTL != time.Hour {
		t.Fatalf("model catalog defaults=%+v", cfg.ModelCatalog)
	}
	t.Setenv("MODEL_CATALOG_SYNC_INTERVAL", "2m")
	t.Setenv("MODEL_CATALOG_CACHE_TTL", "3m")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ModelCatalog.SyncInterval != 2*time.Minute || cfg.ModelCatalog.CacheTTL != 3*time.Minute {
		t.Fatalf("model catalog overrides=%+v", cfg.ModelCatalog)
	}
}

func TestRejectsInvalidPricingConfig(t *testing.T) {
	requiredEnv(t)
	t.Setenv("OPENROUTER_SYNC_INTERVAL", "0")
	if _, err := Load(); err == nil {
		t.Fatal("zero catalog interval accepted")
	}
	requiredEnv(t)
	t.Setenv("OPENROUTER_CATALOG_URL", "file:///tmp/models")
	if _, err := Load(); err == nil {
		t.Fatal("non-HTTP catalog URL accepted")
	}
}

func TestRequiredEnvironment(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{"master", "MASTER_KEY"},
		{"database user", "DB_USER"},
		{"database password", "DB_PASSWORD"},
		{"database name", "DB_NAME"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requiredEnv(t)
			t.Setenv(test.key, "")
			if _, err := Load(); err == nil {
				t.Fatalf("missing %s was accepted", test.key)
			}
		})
	}
}

func TestBuildsConnectionURLsAndDerivesSecrets(t *testing.T) {
	requiredEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabaseURL != "postgres://gorouter:secret@database.internal:5432/gorouter?sslmode=disable" {
		t.Fatalf("database URL = %q", cfg.DatabaseURL)
	}
	if cfg.RedisURL != "redis://gorouter:redis-secret@redis.internal:6379/0" {
		t.Fatalf("redis URL = %q", cfg.RedisURL)
	}
	if cfg.EncryptionKey == "" || cfg.SessionSecret == "" || cfg.EncryptionKey == cfg.SessionSecret {
		t.Fatal("master key did not produce distinct internal keys")
	}
}

func TestDatabaseSSLModeOverride(t *testing.T) {
	requiredEnv(t)
	t.Setenv("DB_SSLMODE", "require")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabaseURL != "postgres://gorouter:secret@database.internal:5432/gorouter?sslmode=require" {
		t.Fatalf("database URL = %q", cfg.DatabaseURL)
	}
}

func TestRedisIsOptionalForDevelopment(t *testing.T) {
	requiredEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("REDIS_HOST", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RedisURL != "" {
		t.Fatalf("Redis URL = %q, want empty", cfg.RedisURL)
	}
}

func TestRedisIsRequiredForDistributedDeployment(t *testing.T) {
	requiredEnv(t)
	t.Setenv("REDIS_HOST", "")
	if _, err := Load(); err == nil {
		t.Fatal("production configuration without Redis was accepted")
	}
	requiredEnv(t)
	t.Setenv("CACHE_MEMORY_FALLBACK", "true")
	if _, err := Load(); err == nil {
		t.Fatal("production in-memory cache fallback was accepted")
	}
}

func TestAcceptsClickHouseAsPrimaryStore(t *testing.T) {
	requiredEnv(t)
	t.Setenv("DB_BACKEND", "clickhouse")
	t.Setenv("CLICKHOUSE_USER", "gorouter")
	t.Setenv("CLICKHOUSE_PASSWORD", "secret")
	t.Setenv("CLICKHOUSE_DB", "gorouter")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ClickHouseURL == "" {
		t.Fatal("ClickHouse URL is empty")
	}
}

func TestRejectsInvalidLimitsAndScope(t *testing.T) {
	requiredEnv(t)
	t.Setenv("CACHE_SCOPE", "everyone")
	if _, err := Load(); err == nil {
		t.Fatal("invalid cache scope accepted")
	}
	requiredEnv(t)
	t.Setenv("REQUEST_LIMIT_MB", "0")
	if _, err := Load(); err == nil {
		t.Fatal("invalid request limit accepted")
	}
	requiredEnv(t)
	t.Setenv("CACHE_TTL", "-1s")
	if _, err := Load(); err == nil {
		t.Fatal("negative cache TTL accepted")
	}
}

func TestProductionCacheAndStrictRedisDefaults(t *testing.T) {
	requiredEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("REDIS_OUTAGE_POLICY", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cache.AllowMemory {
		t.Fatal("production must not silently use memory cache")
	}
	if cfg.Quota.RedisPolicy != "strict" {
		t.Fatalf("default outage policy = %q", cfg.Quota.RedisPolicy)
	}
}

func TestExplicitOpenPolicyAndInvalidPolicy(t *testing.T) {
	requiredEnv(t)
	t.Setenv("REDIS_OUTAGE_POLICY", "open")
	cfg, err := Load()
	if err != nil || cfg.Quota.RedisPolicy != "open" {
		t.Fatalf("open policy: cfg=%+v err=%v", cfg, err)
	}
	t.Setenv("REDIS_OUTAGE_POLICY", "maybe")
	if _, err := Load(); err == nil {
		t.Fatal("invalid outage policy accepted")
	}
}

func TestValidatesOptionalAntigravityOAuthConfiguration(t *testing.T) {
	requiredEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AntigravityOAuthClientID != "" || cfg.AntigravityOAuthClientSecret != "" {
		t.Fatal("Antigravity unexpectedly configured")
	}

	requiredEnv(t)
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_ID", "registered.apps.googleusercontent.com")
	if _, err = Load(); err == nil {
		t.Fatal("Antigravity client ID without secret accepted")
	}

	requiredEnv(t)
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_ID", "invalid-client")
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_SECRET", "synthetic-secret")
	if _, err = Load(); err == nil {
		t.Fatal("malformed Antigravity client ID accepted")
	}

	requiredEnv(t)
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_ID", "registered.apps.googleusercontent.com")
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_SECRET", "synthetic-secret")
	cfg, err = Load()
	if err != nil || cfg.AntigravityOAuthClientID == "" {
		t.Fatalf("valid Antigravity config rejected: %v", err)
	}
}
