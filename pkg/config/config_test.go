package config

import (
	"testing"
	"time"
)

func requiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("MASTER_KEY", "master")
	t.Setenv("DB_BACKEND", "postgresql")
	t.Setenv("DB_CONNECTION_URL", "postgres://gorouter:secret@database.internal:5432/gorouter?sslmode=disable")
	for _, key := range []string{"DATABASE_BACKEND", "DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME", "DB_SSLMODE", "CLICKHOUSE_HOST", "CLICKHOUSE_PORT", "CLICKHOUSE_USER", "CLICKHOUSE_PASSWORD", "CLICKHOUSE_DB", "CLICKHOUSE_TLS", "SQLITE_PATH"} {
		t.Setenv(key, "")
	}
	t.Setenv("REDIS_HOST", "redis.internal")
	t.Setenv("REDIS_PORT", "6379")
	t.Setenv("REDIS_USER", "gorouter")
	t.Setenv("REDIS_PASSWORD", "redis-secret")
	for _, key := range []string{"CACHE_MEMORY_FALLBACK", "CACHE_MAX_ENTRY_BYTES", "CACHE_MAX_TOTAL_BYTES", "CACHE_SCOPE", "CACHE_TTL", "CACHE_ENABLED", "REQUEST_LIMIT_MB", "REQUEST_TIMEOUT", "API_TOKEN_CACHE_TTL", "WEEK_START", "USAGE_WRITE_CONCURRENCY", "USAGE_WRITE_QUEUE_SIZE", "MODEL_CATALOG_SYNC_ENABLED", "MODEL_CATALOG_SYNC_INTERVAL", "MODEL_CATALOG_CACHE_TTL", "OPENROUTER_CATALOG_ENABLED", "OPENROUTER_CATALOG_URL", "OPENROUTER_SYNC_INTERVAL", "OPENROUTER_HTTP_TIMEOUT", "ANTIGRAVITY_OAUTH_CLIENT_ID", "ANTIGRAVITY_OAUTH_CLIENT_SECRET", "OTEL_ENABLED", "OTEL_EXPORTER_OTLP_PROTOCOL", "OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_SERVICE_NAME", "DEVELOPMENT_ENVIRONMENT", "LOG_TIME_FORMAT"} {
		t.Setenv(key, "")
	}
	t.Setenv("APP_ENV", "production")
}

func TestObservabilityDefaultsAndOverrides(t *testing.T) {
	requiredEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServiceName != "gorouter" || cfg.DevelopmentEnvironment != "local" || cfg.LogTimeFormat != "rfc3339" {
		t.Fatalf("logging defaults: service=%q environment=%q time=%q", cfg.ServiceName, cfg.DevelopmentEnvironment, cfg.LogTimeFormat)
	}
	if cfg.Telemetry.Enabled || cfg.Telemetry.Protocol != "grpc" || cfg.Telemetry.Endpoint != "" {
		t.Fatalf("telemetry defaults: %+v", cfg.Telemetry)
	}

	requiredEnv(t)
	t.Setenv("OTEL_ENABLED", "true")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector.internal:4318")
	t.Setenv("OTEL_SERVICE_NAME", "router-edge")
	t.Setenv("DEVELOPMENT_ENVIRONMENT", "staging")
	t.Setenv("LOG_TIME_FORMAT", "rfc3339nano")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Telemetry.Enabled || cfg.Telemetry.Protocol != "http" || cfg.Telemetry.Endpoint != "http://collector.internal:4318" {
		t.Fatalf("telemetry overrides: %+v", cfg.Telemetry)
	}
	if cfg.ServiceName != "router-edge" || cfg.DevelopmentEnvironment != "staging" || cfg.LogTimeFormat != "rfc3339nano" {
		t.Fatalf("logging overrides: service=%q environment=%q time=%q", cfg.ServiceName, cfg.DevelopmentEnvironment, cfg.LogTimeFormat)
	}
}

func TestRejectsInvalidObservabilityConfig(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "enabled", key: "OTEL_ENABLED", value: "sometimes"},
		{name: "protocol", key: "OTEL_EXPORTER_OTLP_PROTOCOL", value: "udp"},
		{name: "time format", key: "LOG_TIME_FORMAT", value: "unix"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requiredEnv(t)
			t.Setenv(tt.key, tt.value)
			if _, err := Load(); err == nil {
				t.Fatalf("%s=%q was accepted", tt.key, tt.value)
			}
		})
	}

	requiredEnv(t)
	t.Setenv("OTEL_ENABLED", "true")
	if _, err := Load(); err == nil {
		t.Fatal("enabled telemetry without an endpoint was accepted")
	}
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
	for _, key := range []string{"MASTER_KEY", "DB_CONNECTION_URL"} {
		t.Run(key, func(t *testing.T) {
			requiredEnv(t)
			t.Setenv(key, "")
			if _, err := Load(); err == nil {
				t.Fatalf("missing %s was accepted", key)
			}
		})
	}
}

func TestUsesDatabaseConnectionURLAndDerivesSecrets(t *testing.T) {
	requiredEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabaseConnectionURL != "postgres://gorouter:secret@database.internal:5432/gorouter?sslmode=disable" {
		t.Fatalf("database URL = %q", cfg.DatabaseConnectionURL)
	}
	if cfg.RedisURL != "redis://gorouter:redis-secret@redis.internal:6379/0" {
		t.Fatalf("redis URL = %q", cfg.RedisURL)
	}
	if cfg.EncryptionKey == "" || cfg.SessionSecret == "" || cfg.EncryptionKey == cfg.SessionSecret {
		t.Fatal("master key did not produce distinct internal keys")
	}
}

func TestRejectsDatabaseConnectionURLForWrongBackend(t *testing.T) {
	requiredEnv(t)
	t.Setenv("DB_CONNECTION_URL", "clickhouse://gorouter:secret@clickhouse:9000/gorouter")
	if _, err := Load(); err == nil {
		t.Fatal("ClickHouse URL was accepted for PostgreSQL backend")
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
	t.Setenv("DB_CONNECTION_URL", "clickhouse://gorouter:secret@clickhouse.internal:9000/gorouter")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabaseConnectionURL != "clickhouse://gorouter:secret@clickhouse.internal:9000/gorouter" {
		t.Fatalf("ClickHouse URL = %q", cfg.DatabaseConnectionURL)
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

func TestAcceptsFullyLocalBackendWithoutExternalServices(t *testing.T) {
	requiredEnv(t)
	t.Setenv("DB_BACKEND", "local")
	t.Setenv("REDIS_HOST", "redis-must-be-ignored")
	t.Setenv("DB_CONNECTION_URL", "file:///tmp/gorouter-local-test.db")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabaseConnectionURL != "/tmp/gorouter-local-test.db" || cfg.RedisURL != "" || !cfg.Cache.AllowMemory {
		t.Fatalf("local configuration = %+v", cfg)
	}
}

func TestLegacyDatabaseVariablesDoNotOverrideCanonicalConfiguration(t *testing.T) {
	requiredEnv(t)
	t.Setenv("DATABASE_BACKEND", "local")
	t.Setenv("SQLITE_PATH", "/tmp/legacy.db")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabaseBackend != "postgresql" || cfg.DatabaseConnectionURL == "" {
		t.Fatalf("legacy variables changed config: %+v", cfg)
	}
}

func TestLocalBackendAcceptsSQLitePathAndMemory(t *testing.T) {
	for _, connection := range []string{"data/gorouter.db", "file:///tmp/gorouter.db", ":memory:"} {
		t.Run(connection, func(t *testing.T) {
			requiredEnv(t)
			t.Setenv("DB_BACKEND", "local")
			t.Setenv("DB_CONNECTION_URL", connection)
			cfg, err := Load()
			if err != nil || cfg.DatabaseConnectionURL == "" {
				t.Fatalf("connection=%q path=%q err=%v", connection, cfg.DatabaseConnectionURL, err)
			}
		})
	}
}
