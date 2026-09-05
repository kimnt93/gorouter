ALTER TABLE users ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'user';
UPDATE users SET role = 'user' WHERE role IS NULL OR role NOT IN ('org_manager', 'user');
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_valid;
ALTER TABLE users ADD CONSTRAINT users_role_valid CHECK (role IN ('org_manager', 'user'));
ALTER TABLE users ALTER COLUMN role DROP DEFAULT;
