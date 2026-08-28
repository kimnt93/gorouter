ALTER TABLE model_routes ADD COLUMN IF NOT EXISTS upstream_model TEXT NOT NULL DEFAULT '';
UPDATE model_routes mr SET upstream_model = m.upstream_model FROM models m WHERE mr.model = m.name AND mr.upstream_model = '';
ALTER TABLE model_routes ALTER COLUMN upstream_model DROP DEFAULT;
ALTER TABLE model_routes DROP CONSTRAINT IF EXISTS model_routes_pkey;
ALTER TABLE model_routes ADD PRIMARY KEY (model, credential_id, upstream_model);
