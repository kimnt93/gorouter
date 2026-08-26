CREATE TABLE IF NOT EXISTS provider_quota_snapshots (
    credential_id TEXT PRIMARY KEY REFERENCES credentials(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    account TEXT NOT NULL,
    plan TEXT NOT NULL DEFAULT '',
    fetched_at TIMESTAMPTZ,
    available BOOLEAN NOT NULL,
    windows JSONB NOT NULL DEFAULT '[]'::jsonb,
    message TEXT NOT NULL DEFAULT '',
    in_use BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_provider_quota_provider_in_use
    ON provider_quota_snapshots (provider, in_use) WHERE in_use;
