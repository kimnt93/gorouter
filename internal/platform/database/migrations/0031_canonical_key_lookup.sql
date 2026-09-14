-- Preserve disabled legacy keys; the existing serialized create invariant remains.
CREATE INDEX IF NOT EXISTS api_keys_canonical_lookup_idx ON api_keys
(owner_user_id, (coalesce(context_organization_id,'')='') DESC, created_at, id)
WHERE owner_type='user';
