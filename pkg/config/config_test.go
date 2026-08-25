package config

import "testing"

func requiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("MASTER_KEY", "master")
	t.Setenv("ENCRYPTION_KEY", "encryption")
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
		{"database", "DATABASE_URL"},
		{"master", "MASTER_KEY"},
		{"encryption", "ENCRYPTION_KEY"},
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
