CREATE DATABASE IF NOT EXISTS gorouter;

CREATE TABLE IF NOT EXISTS gorouter.usage_events
(
    ts DateTime64(3),
    tenant_id String,
    api_key_id String,
    credential_id String,
    model String,
    upstream_model String,
    prompt_tokens Int64,
    completion_tokens Int64,
    cache_read_tokens Int64,
    cache_write_tokens Int64,
    cost_usd Float64,
    cache_hit UInt8,
    status_code UInt16,
    duration_ms Int64,
    error String
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(ts)
ORDER BY (tenant_id, api_key_id, ts, model);
