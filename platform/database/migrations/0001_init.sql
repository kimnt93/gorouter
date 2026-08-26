CREATE TABLE IF NOT EXISTS tenants (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS credentials (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    provider TEXT NOT NULL,
    kind TEXT NOT NULL,
    base_url TEXT NOT NULL DEFAULT '',
    api_key_enc BYTEA,
    oauth_blob_enc BYTEA,
    key_preview TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
    owner_tenant_id TEXT REFERENCES tenants(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS api_keys (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    key_hash TEXT NOT NULL UNIQUE,
    key_prefix TEXT NOT NULL,
    models JSONB NOT NULL DEFAULT '[]',
    monthly_quota_usd DOUBLE PRECISION,
    rpm INTEGER,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS models (
    name TEXT PRIMARY KEY,
    strategy TEXT NOT NULL DEFAULT 'priority',
    upstream_model TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS model_routes (
    model TEXT NOT NULL REFERENCES models(name) ON DELETE CASCADE,
    credential_id TEXT NOT NULL REFERENCES credentials(id) ON DELETE CASCADE,
    priority INTEGER NOT NULL DEFAULT 0,
    weight INTEGER NOT NULL DEFAULT 1,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    PRIMARY KEY (model, credential_id)
);

CREATE TABLE IF NOT EXISTS prices (
    model TEXT PRIMARY KEY,
    input_per_m DOUBLE PRECISION NOT NULL DEFAULT 0,
    output_per_m DOUBLE PRECISION NOT NULL DEFAULT 0,
    cached_input_per_m DOUBLE PRECISION NOT NULL DEFAULT 0,
    cache_write_per_m DOUBLE PRECISION NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS usage_events (
    seq BIGINT PRIMARY KEY,
    event_id TEXT NOT NULL UNIQUE,
    ts TIMESTAMPTZ NOT NULL,
    tenant_id TEXT NOT NULL,
    api_key_id TEXT NOT NULL,
    credential_id TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL,
    upstream_model TEXT NOT NULL DEFAULT '',
    prompt_tokens BIGINT NOT NULL DEFAULT 0,
    completion_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    cache_write_tokens BIGINT NOT NULL DEFAULT 0,
    cost_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
    cache_hit BOOLEAN NOT NULL DEFAULT FALSE,
    status_code INTEGER NOT NULL DEFAULT 200,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    error TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_usage_key_ts ON usage_events (api_key_id, ts);
CREATE INDEX IF NOT EXISTS idx_usage_ts ON usage_events (ts);
CREATE INDEX IF NOT EXISTS idx_usage_model_ts ON usage_events (model, ts);
