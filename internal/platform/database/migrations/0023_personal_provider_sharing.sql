-- Provider credentials are personal-only. Remove legacy global/organization
-- connections and retain the compatibility columns as nullable read fields for
-- rolling application upgrades.
DELETE FROM credentials WHERE owner_user_id IS NULL;
UPDATE credentials SET owner_tenant_id = NULL, owner_organization_id = NULL;
ALTER TABLE credentials ALTER COLUMN owner_user_id SET NOT NULL;
ALTER TABLE credentials DROP CONSTRAINT IF EXISTS credentials_personal_owner_shape;
ALTER TABLE credentials ADD CONSTRAINT credentials_personal_owner_shape CHECK (owner_user_id <> '');

-- The user sharing an API key records whose personal provider routes back the
-- selected models. This is server-derived and immutable with key ownership.
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS credential_owner_user_id TEXT REFERENCES users(id);
UPDATE api_keys SET credential_owner_user_id = owner_user_id
WHERE credential_owner_user_id IS NULL AND owner_type = 'user';

-- API key plaintext remains encrypted at rest and is revealed only through an
-- authorized, no-store endpoint. Existing keys cannot be recovered and return
-- unavailable until their next rotation.
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS key_enc BYTEA;
