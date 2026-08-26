ALTER TABLE usage_events ADD COLUMN IF NOT EXISTS actor_type LowCardinality(String) DEFAULT 'legacy';
ALTER TABLE usage_events ADD COLUMN IF NOT EXISTS user_id String DEFAULT '';
ALTER TABLE usage_events ADD COLUMN IF NOT EXISTS username String DEFAULT 'legacy';
ALTER TABLE usage_events ADD COLUMN IF NOT EXISTS organization_id String DEFAULT tenant_id;

CREATE TABLE IF NOT EXISTS audit_events (
    id String,
    ts DateTime64(3, 'UTC'),
    actor_type LowCardinality(String),
    actor_id String,
    actor_label String,
    organization_id String,
    action LowCardinality(String),
    target_type LowCardinality(String),
    target_id String,
    safe_metadata String
) ENGINE = MergeTree
PARTITION BY toYYYYMM(ts)
ORDER BY (ts, organization_id, id);

