ALTER TABLE credentials
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

ALTER TABLE api_keys
    DROP CONSTRAINT IF EXISTS api_keys_monthly_quota_nonnegative;
ALTER TABLE api_keys
    ADD CONSTRAINT api_keys_monthly_quota_nonnegative
    CHECK (monthly_quota_usd IS NULL OR monthly_quota_usd >= 0) NOT VALID;

ALTER TABLE api_keys
    DROP CONSTRAINT IF EXISTS api_keys_rpm_positive;
ALTER TABLE api_keys
    ADD CONSTRAINT api_keys_rpm_positive
    CHECK (rpm IS NULL OR rpm > 0) NOT VALID;

CREATE INDEX IF NOT EXISTS idx_api_keys_tenant ON api_keys (tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_credentials_owner ON credentials (owner_tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_usage_tenant_ts ON usage_events (tenant_id, ts DESC);
CREATE INDEX IF NOT EXISTS idx_usage_credential_ts ON usage_events (credential_id, ts DESC);
CREATE INDEX IF NOT EXISTS idx_usage_recent ON usage_events (ts DESC, seq DESC);
