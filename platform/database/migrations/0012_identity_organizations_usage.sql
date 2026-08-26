CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL,
    normalized_username TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL CHECK (status IN ('active', 'disabled')),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS organizations (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    normalized_name TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL CHECK (status IN ('active', 'disabled')),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

INSERT INTO organizations (id, name, normalized_name, status, created_at, updated_at)
SELECT id, name, lower(btrim(name)), 'active', created_at, created_at
FROM tenants
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS organization_memberships (
    organization_id TEXT NOT NULL REFERENCES organizations(id),
    user_id TEXT NOT NULL REFERENCES users(id),
    role TEXT NOT NULL CHECK (role IN ('member', 'admin')),
    created_at TIMESTAMPTZ NOT NULL,
    created_by_actor_type TEXT NOT NULL,
    created_by_actor_id TEXT NOT NULL,
    PRIMARY KEY (organization_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_memberships_user ON organization_memberships (user_id, organization_id);

ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS owner_type TEXT;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS owner_user_id TEXT REFERENCES users(id);
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS owner_organization_id TEXT REFERENCES organizations(id);
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS context_organization_id TEXT REFERENCES organizations(id);
UPDATE api_keys
SET owner_type = 'organization', owner_organization_id = tenant_id, context_organization_id = tenant_id
WHERE owner_type IS NULL;
ALTER TABLE api_keys ALTER COLUMN owner_type SET NOT NULL;
ALTER TABLE api_keys ALTER COLUMN tenant_id DROP NOT NULL;
ALTER TABLE api_keys DROP CONSTRAINT IF EXISTS api_keys_owner_shape;
ALTER TABLE api_keys ADD CONSTRAINT api_keys_owner_shape CHECK (
    (owner_type = 'user' AND owner_user_id IS NOT NULL AND owner_organization_id IS NULL) OR
    (owner_type = 'organization' AND owner_user_id IS NULL AND owner_organization_id IS NOT NULL
        AND context_organization_id = owner_organization_id)
);
CREATE INDEX IF NOT EXISTS idx_api_keys_owner_user ON api_keys (owner_user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_api_keys_owner_org ON api_keys (owner_organization_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_api_keys_context_org ON api_keys (context_organization_id, created_at DESC);

ALTER TABLE credentials ADD COLUMN IF NOT EXISTS owner_organization_id TEXT REFERENCES organizations(id);
UPDATE credentials SET owner_organization_id = owner_tenant_id
WHERE owner_organization_id IS NULL AND owner_tenant_id IS NOT NULL;

ALTER TABLE usage_events ADD COLUMN IF NOT EXISTS actor_type TEXT NOT NULL DEFAULT 'legacy';
ALTER TABLE usage_events ADD COLUMN IF NOT EXISTS user_id TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_events ADD COLUMN IF NOT EXISTS username TEXT NOT NULL DEFAULT 'legacy';
ALTER TABLE usage_events ADD COLUMN IF NOT EXISTS organization_id TEXT NOT NULL DEFAULT '';
UPDATE usage_events SET organization_id = tenant_id WHERE organization_id = '';
ALTER TABLE usage_events ALTER COLUMN actor_type DROP DEFAULT;
ALTER TABLE usage_events ALTER COLUMN user_id DROP DEFAULT;
ALTER TABLE usage_events ALTER COLUMN username DROP DEFAULT;
ALTER TABLE usage_events ALTER COLUMN organization_id DROP DEFAULT;
CREATE INDEX IF NOT EXISTS idx_usage_user_ts ON usage_events (user_id, ts DESC, event_id DESC);
CREATE INDEX IF NOT EXISTS idx_usage_organization_ts ON usage_events (organization_id, ts DESC, event_id DESC);
CREATE INDEX IF NOT EXISTS idx_usage_model_status_ts ON usage_events (model, status_code, ts DESC, event_id DESC);

CREATE TABLE IF NOT EXISTS audit_events (
    id TEXT PRIMARY KEY,
    ts TIMESTAMPTZ NOT NULL,
    actor_type TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    actor_label TEXT NOT NULL,
    organization_id TEXT NOT NULL,
    action TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id TEXT NOT NULL,
    safe_metadata JSONB NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_recent ON audit_events (ts DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_audit_organization ON audit_events (organization_id, ts DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_audit_actor ON audit_events (actor_id, ts DESC, id DESC);
