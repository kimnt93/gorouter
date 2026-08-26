ALTER TABLE credentials ADD COLUMN IF NOT EXISTS owner_user_id TEXT REFERENCES users(id) ON DELETE CASCADE;
CREATE INDEX IF NOT EXISTS idx_credentials_owner_user ON credentials (owner_user_id, created_at DESC);
