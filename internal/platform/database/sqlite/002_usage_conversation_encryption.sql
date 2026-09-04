ALTER TABLE usage_events ADD COLUMN conversation_enc BLOB;
ALTER TABLE usage_events ADD COLUMN content_truncated INTEGER NOT NULL DEFAULT 0;
