DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint c
        JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = ANY(c.conkey)
        WHERE c.conrelid = 'usage_events'::regclass AND c.contype = 'p' AND a.attname = 'seq'
    ) THEN
        ALTER TABLE usage_events DROP CONSTRAINT usage_events_pkey;
    END IF;
END $$;
ALTER TABLE usage_events ALTER COLUMN seq DROP NOT NULL;
ALTER TABLE schema_migrations ALTER COLUMN applied_at DROP DEFAULT;
DROP INDEX IF EXISTS idx_usage_recent;
CREATE INDEX idx_usage_recent ON usage_events (ts DESC, event_id DESC);
