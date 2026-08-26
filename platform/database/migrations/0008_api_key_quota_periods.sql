ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS quota_usd DOUBLE PRECISION;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS quota_period TEXT NOT NULL DEFAULT 'none';

UPDATE api_keys
SET quota_usd = monthly_quota_usd,
    quota_period = CASE WHEN monthly_quota_usd IS NULL THEN 'none' ELSE 'week' END
WHERE quota_usd IS NULL;

ALTER TABLE api_keys DROP CONSTRAINT IF EXISTS api_keys_quota_usd_valid;
ALTER TABLE api_keys ADD CONSTRAINT api_keys_quota_usd_valid
    CHECK (quota_usd IS NULL OR quota_usd >= 0);
ALTER TABLE api_keys DROP CONSTRAINT IF EXISTS api_keys_quota_period_valid;
ALTER TABLE api_keys ADD CONSTRAINT api_keys_quota_period_valid
    CHECK (quota_period IN ('none', 'week'));
ALTER TABLE api_keys DROP CONSTRAINT IF EXISTS api_keys_quota_consistent;
ALTER TABLE api_keys ADD CONSTRAINT api_keys_quota_consistent CHECK (
    (quota_period = 'none' AND quota_usd IS NULL) OR
    (quota_period <> 'none' AND quota_usd IS NOT NULL)
);
