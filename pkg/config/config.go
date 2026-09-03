package config

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Environment                  string
	ServiceName                  string
	DevelopmentEnvironment       string
	LogLevel                     string
	LogTimeFormat                string
	Telemetry                    TelemetryConfig
	Listen                       string
	DatabaseBackend              string
	DatabaseConnectionURL        string
	ClickHouseSingleWriter       bool
	RedisURL                     string
	APITokenCacheTTL             time.Duration
	WeekStart                    time.Weekday
	UsageWriteConcurrency        int
	UsageWriteQueueSize          int
	MasterKey                    string
	EncryptionKey                string
	SessionSecret                string
	RequestLimit                 int64
	RequestTimeout               time.Duration
	RouteRetries                 int
	Cache                        CacheConfig
	Quota                        QuotaConfig
	Pricing                      PricingConfig
	ModelCatalog                 ModelCatalogConfig
	OAuthClientID                string
	OAuthTokenURL                string
	CodexOAuthClientID           string
	CodexOAuthTokenURL           string
	GitHubOAuthClientID          string
	GrokOAuthClientID            string
	KimiOAuthClientID            string
	AntigravityOAuthClientID     string
	AntigravityOAuthClientSecret string
}

type TelemetryConfig struct {
	Enabled  bool
	Protocol string
	Endpoint string
}

type CacheConfig struct {
	Enabled       bool
	TTL           time.Duration
	Scope         string
	MaxEntryBytes int
	MaxTotalBytes int64
	AllowMemory   bool
}

type QuotaConfig struct {
	RedisPolicy string
}

type ModelCatalogConfig struct {
	Enabled      bool
	SyncInterval time.Duration
	CacheTTL     time.Duration
}

type PricingConfig struct {
	Enabled      bool
	CatalogURL   string
	SyncInterval time.Duration
	HTTPTimeout  time.Duration
}

func Load() (*Config, error) {
	environment := strings.ToLower(strings.TrimSpace(env("APP_ENV", "development")))
	cfg := &Config{
		Environment:            environment,
		ServiceName:            strings.TrimSpace(env("OTEL_SERVICE_NAME", "gorouter")),
		DevelopmentEnvironment: strings.TrimSpace(env("DEVELOPMENT_ENVIRONMENT", "local")),
		LogLevel:               strings.ToLower(strings.TrimSpace(env("LOG_LEVEL", "info"))),
		LogTimeFormat:          strings.ToLower(strings.TrimSpace(env("LOG_TIME_FORMAT", "rfc3339"))),
		Telemetry: TelemetryConfig{
			Protocol: strings.ToLower(strings.TrimSpace(env("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc"))),
			Endpoint: strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")),
		},
		Listen:                       env("LISTEN", ":8090"),
		MasterKey:                    env("MASTER_KEY", "secret"),
		RequestLimit:                 20 << 20,
		RequestTimeout:               5 * time.Minute,
		RouteRetries:                 2,
		OAuthClientID:                env("ANTHROPIC_OAUTH_CLIENT_ID", "9d1c250a-e61b-44d9-88ed-5944d1962f5e"),
		OAuthTokenURL:                os.Getenv("ANTHROPIC_OAUTH_TOKEN_URL"),
		CodexOAuthClientID:           env("CODEX_OAUTH_CLIENT_ID", "app_EMoamEEZ73f0CkXaXp7hrann"),
		CodexOAuthTokenURL:           os.Getenv("CODEX_OAUTH_TOKEN_URL"),
		GitHubOAuthClientID:          env("GITHUB_OAUTH_CLIENT_ID", "Iv1.b507a08c87ecfe98"),
		GrokOAuthClientID:            env("GROK_OAUTH_CLIENT_ID", "b1a00492-073a-47ea-816f-4c329264a828"),
		KimiOAuthClientID:            env("KIMI_CODING_OAUTH_CLIENT_ID", "17e5f671-d194-4dfb-9706-5516cb48c098"),
		AntigravityOAuthClientID:     env("ANTIGRAVITY_OAUTH_CLIENT_ID", ""),
		AntigravityOAuthClientSecret: env("ANTIGRAVITY_OAUTH_CLIENT_SECRET", ""),
		Cache: CacheConfig{
			Enabled:       true,
			TTL:           24 * time.Hour,
			Scope:         "key",
			MaxEntryBytes: 1 << 20,
			MaxTotalBytes: 256 << 20,
			AllowMemory:   environment == "development",
		},
		Quota:                 QuotaConfig{RedisPolicy: "strict"},
		APITokenCacheTTL:      10 * time.Minute,
		WeekStart:             time.Sunday,
		UsageWriteConcurrency: 4,
		UsageWriteQueueSize:   100000,
		ModelCatalog:          ModelCatalogConfig{Enabled: true, SyncInterval: 15 * time.Minute, CacheTTL: time.Hour},
		Pricing: PricingConfig{
			Enabled:      true,
			CatalogURL:   "https://openrouter.ai/api/frontend/v1/catalog/models",
			SyncInterval: time.Hour,
			HTTPTimeout:  30 * time.Second,
		},
	}
	if value := strings.TrimSpace(os.Getenv("OTEL_ENABLED")); value != "" {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return nil, errors.New("OTEL_ENABLED must be true or false")
		}
		cfg.Telemetry.Enabled = enabled
	}
	if cfg.ServiceName == "" {
		return nil, errors.New("OTEL_SERVICE_NAME must not be empty")
	}
	if cfg.DevelopmentEnvironment == "" {
		return nil, errors.New("DEVELOPMENT_ENVIRONMENT must not be empty")
	}
	if !validLogLevel(cfg.LogLevel) {
		return nil, errors.New("LOG_LEVEL must be trace, debug, info, warn, error, fatal, panic, or disabled")
	}
	if cfg.LogTimeFormat != "rfc3339" && cfg.LogTimeFormat != "rfc3339nano" {
		return nil, errors.New("LOG_TIME_FORMAT must be rfc3339 or rfc3339nano")
	}
	if cfg.Telemetry.Protocol != "grpc" && cfg.Telemetry.Protocol != "http" {
		return nil, errors.New("OTEL_EXPORTER_OTLP_PROTOCOL must be grpc or http")
	}
	if cfg.Telemetry.Enabled && cfg.Telemetry.Endpoint == "" {
		return nil, errors.New("OTEL_EXPORTER_OTLP_ENDPOINT is required when OTEL_ENABLED=true")
	}
	antigravityID := strings.TrimSpace(cfg.AntigravityOAuthClientID)
	antigravitySecret := strings.TrimSpace(cfg.AntigravityOAuthClientSecret)
	if (antigravityID == "") != (antigravitySecret == "") {
		return nil, errors.New("ANTIGRAVITY_OAUTH_CLIENT_ID and ANTIGRAVITY_OAUTH_CLIENT_SECRET must be configured together")
	}
	if antigravityID != "" && (!strings.HasSuffix(antigravityID, ".apps.googleusercontent.com") || len(strings.Split(antigravityID, ".")) < 3) {
		return nil, errors.New("ANTIGRAVITY_OAUTH_CLIENT_ID must be a registered Google OAuth client ID")
	}
	cfg.EncryptionKey = deriveKey(cfg.MasterKey, "credential-encryption")
	cfg.SessionSecret = deriveKey(cfg.MasterKey, "session-signing")

	backend := strings.ToLower(strings.TrimSpace(env("DB_BACKEND", "local")))
	if backend == "postgres" {
		backend = "postgresql"
	}
	cfg.DatabaseBackend = backend
	connection := strings.TrimSpace(os.Getenv("DB_CONNECTION_URL"))
	if connection == "" && backend == "local" {
		connection = "gorouter.db"
	}
	if connection == "" {
		return nil, errors.New("DB_CONNECTION_URL is required for PostgreSQL and ClickHouse")
	}
	switch backend {
	case "postgresql":
		if err := validateConnectionURL(connection, "postgres", "postgresql"); err != nil {
			return nil, fmt.Errorf("invalid PostgreSQL DB_CONNECTION_URL: %w", err)
		}
		cfg.DatabaseConnectionURL = connection
	case "clickhouse":
		if err := validateConnectionURL(connection, "clickhouse", "clickhouses"); err != nil {
			return nil, fmt.Errorf("invalid ClickHouse DB_CONNECTION_URL: %w", err)
		}
		cfg.DatabaseConnectionURL = connection
	case "local":
		path, err := sqlitePath(connection)
		if err != nil {
			return nil, err
		}
		cfg.DatabaseConnectionURL = path
		cfg.Cache.AllowMemory = true
	default:
		return nil, errors.New("DB_BACKEND must be postgresql, clickhouse, or local")
	}
	if value := strings.TrimSpace(os.Getenv("CLICKHOUSE_SINGLE_WRITER")); value != "" {
		parsed, parseErr := strconv.ParseBool(value)
		if parseErr != nil {
			return nil, errors.New("CLICKHOUSE_SINGLE_WRITER must be true or false")
		}
		cfg.ClickHouseSingleWriter = parsed
	}
	if redisHost := os.Getenv("REDIS_HOST"); redisHost != "" && backend != "local" {
		cfg.RedisURL = connectionURL("redis", redisHost, env("REDIS_PORT", "6379"), os.Getenv("REDIS_USER"), os.Getenv("REDIS_PASSWORD"), "0")
	}
	if v := os.Getenv("REQUEST_LIMIT_MB"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return nil, errors.New("REQUEST_LIMIT_MB must be a positive integer")
		}
		cfg.RequestLimit = int64(n) << 20
	}
	if v := os.Getenv("API_TOKEN_CACHE_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return nil, errors.New("API_TOKEN_CACHE_TTL must be a positive duration")
		}
		cfg.APITokenCacheTTL = d
	}
	if v := strings.TrimSpace(os.Getenv("WEEK_START")); v != "" {
		weekday, err := parseWeekday(v)
		if err != nil {
			return nil, err
		}
		cfg.WeekStart = weekday
	}
	if v := os.Getenv("USAGE_WRITE_CONCURRENCY"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return nil, errors.New("USAGE_WRITE_CONCURRENCY must be a positive integer")
		}
		cfg.UsageWriteConcurrency = n
	}
	if v := os.Getenv("USAGE_WRITE_QUEUE_SIZE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return nil, errors.New("USAGE_WRITE_QUEUE_SIZE must be a positive integer")
		}
		cfg.UsageWriteQueueSize = n
	}
	if v := os.Getenv("CACHE_ENABLED"); v != "" {
		cfg.Cache.Enabled = strings.EqualFold(v, "true") || v == "1"
	}
	if v := os.Getenv("CACHE_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid CACHE_TTL: %w", err)
		}
		if d <= 0 {
			return nil, errors.New("CACHE_TTL must be a positive duration")
		}
		cfg.Cache.TTL = d
	}
	if v := os.Getenv("REQUEST_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return nil, errors.New("REQUEST_TIMEOUT must be a positive duration")
		}
		cfg.RequestTimeout = d
	}
	if v := os.Getenv("ROUTE_RETRIES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return nil, errors.New("ROUTE_RETRIES must be a non-negative integer")
		}
		cfg.RouteRetries = n
	}
	if v := os.Getenv("CACHE_MAX_ENTRY_BYTES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return nil, errors.New("CACHE_MAX_ENTRY_BYTES must be a positive integer")
		}
		cfg.Cache.MaxEntryBytes = n
	}
	if v := os.Getenv("CACHE_MAX_TOTAL_BYTES"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			return nil, errors.New("CACHE_MAX_TOTAL_BYTES must be a positive integer")
		}
		cfg.Cache.MaxTotalBytes = n
	}
	if v := os.Getenv("CACHE_MEMORY_FALLBACK"); v != "" {
		cfg.Cache.AllowMemory = strings.EqualFold(v, "true") || v == "1"
	}
	if cfg.Environment != "development" && backend != "local" && cfg.Cache.AllowMemory {
		return nil, errors.New("CACHE_MEMORY_FALLBACK is allowed only in development")
	}
	if cfg.Environment != "development" && backend != "local" && cfg.RedisURL == "" {
		return nil, errors.New("Redis is required outside development for distributed runtime state")
	}
	switch strings.ToLower(os.Getenv("REDIS_OUTAGE_POLICY")) {
	case "", "strict":
		cfg.Quota.RedisPolicy = "strict"
	case "open":
		cfg.Quota.RedisPolicy = "open"
	default:
		return nil, errors.New("REDIS_OUTAGE_POLICY must be strict or open")
	}
	switch strings.ToLower(os.Getenv("CACHE_SCOPE")) {
	case "", "key":
		cfg.Cache.Scope = "key"
	case "tenant":
		cfg.Cache.Scope = "tenant"
	case "global":
		cfg.Cache.Scope = "global"
	default:
		return nil, errors.New("CACHE_SCOPE must be key, tenant, or global")
	}
	if v := os.Getenv("MODEL_CATALOG_SYNC_ENABLED"); v != "" {
		cfg.ModelCatalog.Enabled = strings.EqualFold(v, "true") || v == "1"
	}
	if v := os.Getenv("MODEL_CATALOG_SYNC_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return nil, errors.New("MODEL_CATALOG_SYNC_INTERVAL must be a positive duration")
		}
		cfg.ModelCatalog.SyncInterval = d
	}
	if v := os.Getenv("MODEL_CATALOG_CACHE_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return nil, errors.New("MODEL_CATALOG_CACHE_TTL must be a positive duration")
		}
		cfg.ModelCatalog.CacheTTL = d
	}
	if v := os.Getenv("OPENROUTER_CATALOG_ENABLED"); v != "" {
		cfg.Pricing.Enabled = strings.EqualFold(v, "true") || v == "1"
	}
	if v := strings.TrimSpace(os.Getenv("OPENROUTER_CATALOG_URL")); v != "" {
		u, err := url.Parse(v)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, errors.New("OPENROUTER_CATALOG_URL must be an HTTP(S) URL")
		}
		cfg.Pricing.CatalogURL = v
	}
	if v := os.Getenv("OPENROUTER_SYNC_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return nil, errors.New("OPENROUTER_SYNC_INTERVAL must be a positive duration")
		}
		cfg.Pricing.SyncInterval = d
	}
	if v := os.Getenv("OPENROUTER_HTTP_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return nil, errors.New("OPENROUTER_HTTP_TIMEOUT must be a positive duration")
		}
		cfg.Pricing.HTTPTimeout = d
	}
	if backend == "local" {
		cfg.Cache.AllowMemory = true
		cfg.RedisURL = ""
	}
	return cfg, nil
}

func validLogLevel(value string) bool {
	switch value {
	case "trace", "debug", "info", "warn", "error", "fatal", "panic", "disabled":
		return true
	default:
		return false
	}
}

func connectionURL(scheme, host, port, user, password, name string) string {
	value := &url.URL{Scheme: scheme, Host: net.JoinHostPort(host, port), Path: "/" + name}
	if user != "" || password != "" {
		value.User = url.UserPassword(user, password)
	}
	return value.String()
}

func validateConnectionURL(value string, schemes ...string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return errors.New("must be an absolute database URL")
	}
	for _, scheme := range schemes {
		if strings.EqualFold(parsed.Scheme, scheme) {
			return nil
		}
	}
	return fmt.Errorf("scheme must be %s", strings.Join(schemes, " or "))
}

func sqlitePath(value string) (string, error) {
	if value == ":memory:" {
		return value, nil
	}
	parsed, err := url.Parse(value)
	if err == nil && strings.EqualFold(parsed.Scheme, "file") {
		if parsed.Host != "" && parsed.Host != "localhost" {
			return "", errors.New("SQLite DB_CONNECTION_URL file host must be empty or localhost")
		}
		path, unescapeErr := url.PathUnescape(parsed.Path)
		if unescapeErr != nil || strings.TrimSpace(path) == "" {
			return "", errors.New("SQLite DB_CONNECTION_URL requires a file path")
		}
		return path, nil
	}
	if strings.Contains(value, "://") {
		return "", errors.New("local DB_CONNECTION_URL must be a SQLite file URL or path")
	}
	if strings.TrimSpace(value) == "" {
		return "", errors.New("local DB_CONNECTION_URL requires a SQLite path")
	}
	return value, nil
}

func parseWeekday(v string) (time.Weekday, error) {
	names := []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}
	v = strings.ToLower(strings.TrimSpace(v))
	for i, name := range names {
		if v == name || v == name[:3] || v == strconv.Itoa(i) {
			return time.Weekday(i), nil
		}
	}
	return 0, errors.New("WEEK_START must be sunday..saturday or 0..6")
}

func deriveKey(masterKey, purpose string) string {
	sum := sha256.Sum256([]byte("gorouter:" + purpose + ":" + masterKey))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
