-- Migration 0018 is already recorded on some deployments where the metadata
-- column is absent. Use a new version so those databases are repaired without
-- rewriting migration history.
ALTER TABLE models ADD COLUMN IF NOT EXISTS metadata JSONB;
