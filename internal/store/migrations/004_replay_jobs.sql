ALTER TABLE games ADD COLUMN hints_enabled INTEGER;

CREATE TABLE replay_jobs (
    game_id INTEGER PRIMARY KEY REFERENCES games(id) ON DELETE CASCADE,
    state TEXT NOT NULL DEFAULT 'pending',
    owner TEXT NOT NULL DEFAULT '',
    lease_until INTEGER NOT NULL DEFAULT 0,
    attempts INTEGER NOT NULL DEFAULT 0,
    retry_at INTEGER NOT NULL DEFAULT 0,
    error TEXT NOT NULL DEFAULT ''
);
CREATE INDEX replay_jobs_pending ON replay_jobs(state, retry_at);

CREATE TABLE replay_demands (
    owner TEXT PRIMARY KEY,
    lease_until INTEGER NOT NULL
);
CREATE TABLE replay_execution (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    owner TEXT NOT NULL DEFAULT '',
    generation INTEGER NOT NULL DEFAULT 0,
    lease_until INTEGER NOT NULL DEFAULT 0
);
INSERT INTO replay_execution(id) VALUES (1);
