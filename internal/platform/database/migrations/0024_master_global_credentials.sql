-- Master-created credentials remain the global operational compatibility
-- path. Personal credentials keep a non-empty user owner; a NULL user owner
-- represents a global credential and is never returned to ordinary users.
ALTER TABLE credentials DROP CONSTRAINT IF EXISTS credentials_personal_owner_shape;
ALTER TABLE credentials ALTER COLUMN owner_user_id DROP NOT NULL;
ALTER TABLE credentials ADD CONSTRAINT credentials_user_or_global_owner_shape
    CHECK (owner_user_id IS NULL OR owner_user_id <> '');
