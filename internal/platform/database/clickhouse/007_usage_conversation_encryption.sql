ALTER TABLE usage_events ADD COLUMN IF NOT EXISTS conversation_enc String DEFAULT '';
ALTER TABLE usage_events ADD COLUMN IF NOT EXISTS content_truncated Bool DEFAULT false;
