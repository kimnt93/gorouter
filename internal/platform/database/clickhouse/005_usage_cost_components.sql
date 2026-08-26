ALTER TABLE usage_events ADD COLUMN IF NOT EXISTS input_cost_usd Float64 DEFAULT 0 AFTER cost_usd;
ALTER TABLE usage_events ADD COLUMN IF NOT EXISTS output_cost_usd Float64 DEFAULT 0 AFTER input_cost_usd;
ALTER TABLE usage_events ADD COLUMN IF NOT EXISTS cache_read_cost_usd Float64 DEFAULT 0 AFTER output_cost_usd;
ALTER TABLE usage_events ADD COLUMN IF NOT EXISTS cache_write_cost_usd Float64 DEFAULT 0 AFTER cache_read_cost_usd;
