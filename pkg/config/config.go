package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Listen         string
	DatabaseURL    string
	RedisURL       string
	MasterKey      string
	EncryptionKey  string
	SessionSecret  string
	RequestLimit   int64
	RequestTimeout time.Duration
	Cache          CacheConfig
	Quota          QuotaConfig
	OAuthClientID  string
	OAuthTokenURL  string
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

func Load() (*Config, error) {
	cfg := &Config{
		Listen:         env("LISTEN", ":8090"),
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		RedisURL:       os.Getenv("REDIS_URL"),
		MasterKey:      os.Getenv("MASTER_KEY"),
		EncryptionKey:  os.Getenv("ENCRYPTION_KEY"),
		SessionSecret:  os.Getenv("SESSION_SECRET"),
		RequestLimit:   int64(envInt("REQUEST_LIMIT_MB", 20)) << 20,
		RequestTimeout: 5 * time.Minute,
		OAuthClientID:  env("ANTHROPIC_OAUTH_CLIENT_ID", "9d1c250a-e61b-44d9-88ed-5944d1962f5e"),
		OAuthTokenURL:  os.Getenv("ANTHROPIC_OAUTH_TOKEN_URL"),
		Cache: CacheConfig{
			Enabled:       true,
			TTL:           24 * time.Hour,
			Scope:         "key",
			MaxEntryBytes: 1 << 20,
			MaxTotalBytes: 256 << 20,
			AllowMemory:   !strings.EqualFold(env("APP_ENV", "development"), "production"),
		},
		Quota: QuotaConfig{RedisPolicy: "strict"},
	}
	if cfg.DatabaseURL == "" {
		return nil, errors.New("DATABASE_URL is required (postgres://...)")
	}
	if cfg.MasterKey == "" {
		return nil, errors.New("MASTER_KEY is required; set it during setup")
	}
	if cfg.EncryptionKey == "" {
		return nil, errors.New("ENCRYPTION_KEY is required; set it during setup")
	}
	if cfg.SessionSecret == "" {
		cfg.SessionSecret = "nr-session::" + cfg.MasterKey
	}
	if v := os.Getenv("CACHE_ENABLED"); v != "" {
		cfg.Cache.Enabled = strings.EqualFold(v, "true") || v == "1"
	}
	if v := os.Getenv("CACHE_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid CACHE_TTL: %w", err)
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
	if v := os.Getenv("CACHE_MAX_ENTRY_BYTES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return nil, errors.New("CACHE_MAX_ENTRY_BYTES must be a positive integer")
		}
		cfg.Cache.MaxEntryBytes = n
	}
	if v := os.Getenv("CACHE_MEMORY_FALLBACK"); v != "" {
		cfg.Cache.AllowMemory = strings.EqualFold(v, "true") || v == "1"
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
	case "tenant":
		cfg.Cache.Scope = "tenant"
	case "global":
		cfg.Cache.Scope = "global"
	default:
		cfg.Cache.Scope = "key"
	}
	return cfg, nil
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
