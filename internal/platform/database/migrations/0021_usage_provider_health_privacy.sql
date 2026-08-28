ALTER TABLE usage_events ADD COLUMN provider TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS usage_events_provider_ts_idx ON usage_events (provider, ts DESC);
CREATE INDEX IF NOT EXISTS usage_events_credential_health_ts_idx ON usage_events (credential_id, ts DESC) WHERE credential_id <> '';
ALTER TABLE usage_events DROP COLUMN IF EXISTS request_body;
ALTER TABLE usage_events DROP COLUMN IF EXISTS response_body;
ALTER TABLE usage_events DROP COLUMN IF EXISTS content_truncated;
