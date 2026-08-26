ALTER TABLE usage_events ADD COLUMN IF NOT EXISTS request_body TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_events ADD COLUMN IF NOT EXISTS response_body TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_events ADD COLUMN IF NOT EXISTS content_truncated BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE usage_events SET cost_usd = 0, priced = TRUE WHERE NOT priced;
