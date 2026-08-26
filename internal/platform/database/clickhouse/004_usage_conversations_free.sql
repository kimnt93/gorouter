ALTER TABLE usage_events ADD COLUMN IF NOT EXISTS request_body String DEFAULT '';
ALTER TABLE usage_events ADD COLUMN IF NOT EXISTS response_body String DEFAULT '';
ALTER TABLE usage_events ADD COLUMN IF NOT EXISTS content_truncated Bool DEFAULT false;
ALTER TABLE usage_events UPDATE cost_usd = 0, priced = true WHERE NOT priced;
