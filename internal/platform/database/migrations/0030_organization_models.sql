CREATE TABLE IF NOT EXISTS organization_model_records (
 organization_id TEXT NOT NULL,
 kind TEXT NOT NULL,
 id TEXT NOT NULL,
 payload JSONB NOT NULL,
 PRIMARY KEY(organization_id,kind,id)
);
CREATE INDEX IF NOT EXISTS idx_organization_models_kind ON organization_model_records(organization_id,kind);
