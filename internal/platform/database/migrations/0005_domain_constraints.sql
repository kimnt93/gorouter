ALTER TABLE credentials DROP CONSTRAINT IF EXISTS credentials_provider_valid;
ALTER TABLE credentials ADD CONSTRAINT credentials_provider_valid
    CHECK (provider IN ('openai-compatible', 'anthropic')) NOT VALID;
ALTER TABLE credentials DROP CONSTRAINT IF EXISTS credentials_kind_valid;
ALTER TABLE credentials ADD CONSTRAINT credentials_kind_valid
    CHECK (kind IN ('api_key', 'oauth')) NOT VALID;
ALTER TABLE credentials DROP CONSTRAINT IF EXISTS credentials_status_valid;
ALTER TABLE credentials ADD CONSTRAINT credentials_status_valid
    CHECK (status IN ('active', 'disabled')) NOT VALID;

ALTER TABLE models DROP CONSTRAINT IF EXISTS models_strategy_valid;
ALTER TABLE models ADD CONSTRAINT models_strategy_valid
    CHECK (strategy IN ('priority', 'round_robin')) NOT VALID;

ALTER TABLE model_routes DROP CONSTRAINT IF EXISTS model_routes_weight_positive;
ALTER TABLE model_routes ADD CONSTRAINT model_routes_weight_positive
    CHECK (weight > 0) NOT VALID;

ALTER TABLE prices DROP CONSTRAINT IF EXISTS prices_nonnegative;
ALTER TABLE prices ADD CONSTRAINT prices_nonnegative CHECK (
    input_per_m >= 0 AND output_per_m >= 0 AND
    cached_input_per_m >= 0 AND cache_write_per_m >= 0
) NOT VALID;

ALTER TABLE usage_events DROP CONSTRAINT IF EXISTS usage_tokens_nonnegative;
ALTER TABLE usage_events ADD CONSTRAINT usage_tokens_nonnegative CHECK (
    prompt_tokens >= 0 AND completion_tokens >= 0 AND
    cache_read_tokens >= 0 AND cache_write_tokens >= 0 AND
    cost_usd >= 0 AND duration_ms >= 0
) NOT VALID;
