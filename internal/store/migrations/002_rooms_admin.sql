ALTER TABLE users ADD COLUMN is_admin INTEGER NOT NULL DEFAULT 0;
UPDATE users SET is_admin = 1 WHERE username = 'mango';

CREATE TABLE rooms (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    owner_id     INTEGER NOT NULL,
    owner_name   TEXT NOT NULL,
    status       TEXT NOT NULL,
    hints        INTEGER NOT NULL,
    turn_seconds INTEGER NOT NULL,
    seats_json   TEXT NOT NULL,
    game_id      INTEGER REFERENCES games(id),
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);
CREATE INDEX rooms_status ON rooms(status);

CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE game_hints (
    game_id       INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    seat          INTEGER NOT NULL,
    seq           INTEGER NOT NULL,
    analysis_json TEXT NOT NULL,
    PRIMARY KEY (game_id, seat, seq)
);

CREATE TABLE game_curves (
    game_id    INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    seat       INTEGER NOT NULL,
    turn       INTEGER NOT NULL,
    played_win REAL NOT NULL,
    best_win   REAL NOT NULL,
    PRIMARY KEY (game_id, seat, turn)
);
