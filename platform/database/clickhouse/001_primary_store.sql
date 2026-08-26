CREATE TABLE IF NOT EXISTS config_records (
    entity LowCardinality(String), key String, payload String,
    version DateTime64(6, 'UTC'), deleted UInt8
) ENGINE = ReplacingMergeTree(version) ORDER BY (entity, key);

CREATE TABLE IF NOT EXISTS usage_events (
    ts DateTime64(3, 'UTC'), tenant_id String, api_key_id String,
    credential_id String, model String, upstream_model String,
    prompt_tokens Int64, completion_tokens Int64, cache_read_tokens Int64,
    cache_write_tokens Int64, cost_usd Float64, priced Bool, cache_hit Bool,
    status_code Int32, duration_ms Int64, error String
) ENGINE = MergeTree PARTITION BY toYYYYMM(ts) ORDER BY (ts, tenant_id, api_key_id);
