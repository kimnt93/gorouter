package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/kimnt93/gorouter/internal/cache"
)

type Config struct {
	Listen        string
	DatabaseURL   string
	MasterKey     string
	EncryptionKey string
	Cache         cache.Config
	RequestLimit  int64
	OAuthClientID string
}

func Load() (*Config, error) {
	cfg := &Config{
		Listen:        env("LISTEN", ":8090"),
		DatabaseURL:   env("DATABASE_URL", ""),
		MasterKey:     os.Getenv("MASTER_KEY"),
		EncryptionKey: os.Getenv("ENCRYPTION_KEY"),
		RequestLimit:  int64(envInt("REQUEST_LIMIT_MB", 20)) << 20,
		OAuthClientID: env("ANTHROPIC_OAUTH_CLIENT_ID", "9d1c250a-e61b-44d9-88ed-5944d1962f5e"),
		Cache:         cache.DefaultConfig(),
	}
	if cfg.DatabaseURL == "" {
		return nil, errors.New("DATABASE_URL is required (postgres://...)")
	}
	generated := false
	if cfg.MasterKey == "" {
		k, err := randomToken(24)
		if err != nil {
			return nil, err
		}
		cfg.MasterKey = k
		generated = true
		fmt.Printf("WARNING: MASTER_KEY not set; generated ephemeral master key (will change on restart):\n  %s\n", cfg.MasterKey)
	}
	if cfg.EncryptionKey == "" {
		k, err := randomB64()
		if err != nil {
			return nil, err
		}
		cfg.EncryptionKey = k
		fmt.Println("WARNING: ENCRYPTION_KEY not set; generated ephemeral encryption key — stored credentials will be unreadable after restart")
	}
	_ = generated
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
	switch strings.ToLower(os.Getenv("CACHE_SCOPE")) {
	case "tenant":
		cfg.Cache.Scope = cache.ScopeTenant
	case "global":
		cfg.Cache.Scope = cache.ScopeGlobal
	default:
		cfg.Cache.Scope = cache.ScopeKey
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

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func randomB64() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}
