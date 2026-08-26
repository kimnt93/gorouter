UPDATE api_keys SET quota_period = CASE WHEN quota_usd IS NULL THEN 'none' ELSE 'week' END
WHERE quota_period NOT IN ('none', 'week');
ALTER TABLE api_keys DROP CONSTRAINT IF EXISTS api_keys_quota_period_valid;
ALTER TABLE api_keys ADD CONSTRAINT api_keys_quota_period_valid CHECK (quota_period IN ('none', 'week'));
