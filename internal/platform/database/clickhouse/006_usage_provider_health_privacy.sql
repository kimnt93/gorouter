ALTER TABLE usage_events ADD COLUMN IF NOT EXISTS provider String DEFAULT '' AFTER credential_id;
ALTER TABLE usage_events DROP COLUMN IF EXISTS request_body;
ALTER TABLE usage_events DROP COLUMN IF EXISTS response_body;
ALTER TABLE usage_events DROP COLUMN IF EXISTS content_truncated;
