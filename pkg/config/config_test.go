package config

import "testing"

func requiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("MASTER_KEY", "master")
	t.Setenv("DB_BACKEND", "postgresql")
	t.Setenv("DB_HOST", "database.internal")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_USER", "gorouter")
	t.Setenv("DB_PASSWORD", "secret")
	t.Setenv("DB_NAME", "gorouter")
	t.Setenv("REDIS_HOST", "redis.internal")
	t.Setenv("REDIS_PORT", "6379")
	t.Setenv("REDIS_USER", "gorouter")
	t.Setenv("REDIS_PASSWORD", "redis-secret")
	t.Setenv("CACHE_MEMORY_FALLBACK", "")
	t.Setenv("CACHE_MAX_ENTRY_BYTES", "")
	t.Setenv("CACHE_MAX_TOTAL_BYTES", "")
	t.Setenv("CACHE_SCOPE", "")
	t.Setenv("CACHE_TTL", "")
	t.Setenv("CACHE_ENABLED", "")
	t.Setenv("REQUEST_LIMIT_MB", "")
	t.Setenv("REQUEST_TIMEOUT", "")
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
	if cfg.DatabaseURL != "postgres://gorouter:secret@database.internal:5432/gorouter" {
		t.Fatalf("database URL = %q", cfg.DatabaseURL)
	}
	if cfg.RedisURL != "redis://gorouter:redis-secret@redis.internal:6379/0" {
		t.Fatalf("redis URL = %q", cfg.RedisURL)
	}
	if cfg.EncryptionKey == "" || cfg.SessionSecret == "" || cfg.EncryptionKey == cfg.SessionSecret {
		t.Fatal("master key did not produce distinct internal keys")
	}
}

func TestRedisIsOptionalForDevelopment(t *testing.T) {
	requiredEnv(t)
	t.Setenv("REDIS_HOST", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RedisURL != "" {
		t.Fatalf("Redis URL = %q, want empty", cfg.RedisURL)
	}
}

func TestRejectsClickHouseAsPrimaryStore(t *testing.T) {
	requiredEnv(t)
	t.Setenv("DB_BACKEND", "clickhouse")
	if _, err := Load(); err == nil {
		t.Fatal("unsupported ClickHouse primary store was accepted")
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
