ALTER TABLE usage_events ADD COLUMN IF NOT EXISTS trace_id String DEFAULT '';
ALTER TABLE usage_events ADD INDEX IF NOT EXISTS idx_usage_trace trace_id TYPE bloom_filter(0.01) GRANULARITY 4;
ALTER TABLE usage_events ADD INDEX IF NOT EXISTS idx_usage_parent_run parent_run_id TYPE bloom_filter(0.01) GRANULARITY 4;
ALTER TABLE usage_events ADD INDEX IF NOT EXISTS idx_usage_request logical_request_id TYPE bloom_filter(0.01) GRANULARITY 4;
