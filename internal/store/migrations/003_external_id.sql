ALTER TABLE users ADD COLUMN external_id TEXT;
CREATE UNIQUE INDEX users_external_id ON users(external_id) WHERE external_id IS NOT NULL;
