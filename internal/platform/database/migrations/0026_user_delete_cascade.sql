ALTER TABLE organization_memberships DROP CONSTRAINT IF EXISTS organization_memberships_user_id_fkey;
ALTER TABLE organization_memberships ADD CONSTRAINT organization_memberships_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;
ALTER TABLE api_keys DROP CONSTRAINT IF EXISTS api_keys_owner_user_id_fkey;
ALTER TABLE api_keys ADD CONSTRAINT api_keys_owner_user_id_fkey
    FOREIGN KEY (owner_user_id) REFERENCES users(id) ON DELETE CASCADE;
ALTER TABLE api_keys DROP CONSTRAINT IF EXISTS api_keys_credential_owner_user_id_fkey;
ALTER TABLE api_keys ADD CONSTRAINT api_keys_credential_owner_user_id_fkey
    FOREIGN KEY (credential_owner_user_id) REFERENCES users(id) ON DELETE CASCADE;
