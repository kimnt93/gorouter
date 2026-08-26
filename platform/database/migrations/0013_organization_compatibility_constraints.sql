ALTER TABLE api_keys DROP CONSTRAINT IF EXISTS api_keys_tenant_id_fkey;
ALTER TABLE api_keys DROP CONSTRAINT IF EXISTS api_keys_tenant_organization_fkey;
ALTER TABLE api_keys ADD CONSTRAINT api_keys_tenant_organization_fkey
    FOREIGN KEY (tenant_id) REFERENCES organizations(id) ON DELETE CASCADE;

ALTER TABLE credentials DROP CONSTRAINT IF EXISTS credentials_owner_tenant_id_fkey;
ALTER TABLE credentials DROP CONSTRAINT IF EXISTS credentials_owner_organization_compat_fkey;
ALTER TABLE credentials ADD CONSTRAINT credentials_owner_organization_compat_fkey
    FOREIGN KEY (owner_tenant_id) REFERENCES organizations(id) ON DELETE SET NULL;

