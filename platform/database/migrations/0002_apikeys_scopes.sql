ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS scopes JSONB NOT NULL DEFAULT '["chat"]';

UPDATE api_keys SET scopes = '["chat"]' WHERE scopes IS NULL OR scopes = 'null';
