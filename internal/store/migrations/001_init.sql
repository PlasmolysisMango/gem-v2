CREATE TABLE users (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    username   TEXT NOT NULL UNIQUE COLLATE NOCASE,
    pw_hash    TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE TABLE games (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    room_id      TEXT NOT NULL,
    seed         INTEGER NOT NULL,
    player_count INTEGER NOT NULL,
    status       TEXT NOT NULL DEFAULT 'playing',
    started_at   INTEGER NOT NULL,
    ended_at     INTEGER,
    winner_seat  INTEGER
);

CREATE TABLE game_players (
    game_id      INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    seat         INTEGER NOT NULL,
    user_id      INTEGER REFERENCES users(id),
    ai_level     TEXT,
    final_points INTEGER,
    winner       INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (game_id, seat)
);
CREATE INDEX game_players_user ON game_players(user_id);

CREATE TABLE game_actions (
    game_id     INTEGER NOT NULL REFERENCES games(id) ON DELETE CASCADE,
    seq         INTEGER NOT NULL,
    seat        INTEGER NOT NULL,
    action_json TEXT NOT NULL,
    ts          INTEGER NOT NULL,
    review_json TEXT,
    PRIMARY KEY (game_id, seq)
);
