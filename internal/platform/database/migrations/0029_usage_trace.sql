ALTER TABLE usage_events ADD COLUMN IF NOT EXISTS trace_id TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_usage_trace_ts ON usage_events (trace_id, ts DESC, event_id DESC) WHERE trace_id <> '';
CREATE INDEX IF NOT EXISTS idx_usage_parent_run_ts ON usage_events (parent_run_id, ts DESC, event_id DESC) WHERE parent_run_id <> '';
CREATE INDEX IF NOT EXISTS idx_usage_request_ts ON usage_events (logical_request_id, ts DESC, event_id DESC) WHERE logical_request_id <> '';
