package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// SettingIdleClose is the settings key of the idle room cleanup delay in seconds.
const SettingIdleClose = "idle_close_seconds"

// RoomSeat is one persisted seat of a room.
type RoomSeat struct {
	Kind   string `json:"kind"`
	UserID int64  `json:"userId,omitempty"`
	Name   string `json:"name,omitempty"`
	Level  string `json:"level,omitempty"`
}

// RoomRecord is the metadata of a waiting or running room. The board itself is
// rebuilt by replaying the game's seed and actions.
type RoomRecord struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	OwnerID     int64      `json:"ownerId"`
	OwnerName   string     `json:"ownerName"`
	Status      string     `json:"status"`
	Hints       bool       `json:"hints"`
	TurnSeconds int        `json:"turnSeconds"`
	Seats       []RoomSeat `json:"seats"`
	GameID      int64      `json:"gameId"`
	CreatedAt   int64      `json:"createdAt"`
	UpdatedAt   int64      `json:"updatedAt"`
}

// SaveRoom inserts or updates a room record.
func (s *Store) SaveRoom(ctx context.Context, rec RoomRecord) error {
	if rec.Seats == nil {
		rec.Seats = []RoomSeat{}
	}
	seats, err := json.Marshal(rec.Seats)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	if rec.CreatedAt == 0 {
		rec.CreatedAt = now
	}
	var gameID any
	if rec.GameID != 0 {
		gameID = rec.GameID
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO rooms (id, name, owner_id, owner_name, status, hints, turn_seconds, seats_json, game_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name, owner_id = excluded.owner_id, owner_name = excluded.owner_name,
			status = excluded.status, hints = excluded.hints, turn_seconds = excluded.turn_seconds,
			seats_json = excluded.seats_json, game_id = excluded.game_id, updated_at = excluded.updated_at`,
		rec.ID, rec.Name, rec.OwnerID, rec.OwnerName, rec.Status, rec.Hints, rec.TurnSeconds, string(seats), gameID, rec.CreatedAt, now)
	return err
}

// DeleteRoom removes a room record.
func (s *Store) DeleteRoom(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM rooms WHERE id = ?`, id)
	return err
}

// LoadRooms returns the waiting and running rooms.
func (s *Store) LoadRooms(ctx context.Context) ([]RoomRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, owner_id, owner_name, status, hints, turn_seconds, seats_json, game_id, created_at, updated_at
		FROM rooms WHERE status IN ('waiting', 'playing') ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RoomRecord
	for rows.Next() {
		var rec RoomRecord
		var seats string
		var gameID sql.NullInt64
		if err := rows.Scan(&rec.ID, &rec.Name, &rec.OwnerID, &rec.OwnerName, &rec.Status, &rec.Hints,
			&rec.TurnSeconds, &seats, &gameID, &rec.CreatedAt, &rec.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(seats), &rec.Seats); err != nil {
			return nil, err
		}
		if gameID.Valid {
			rec.GameID = gameID.Int64
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// GetSetting reads a settings value.
func (s *Store) GetSetting(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// SetSetting writes a settings value.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// HintRow is the latest cached hint of one seat.
type HintRow struct {
	Seq      int
	Analysis string // JSON-encoded ai.Analysis
}

// SetHint stores the latest hint for a seat, replacing older turns.
func (s *Store) SetHint(ctx context.Context, gameID int64, seat, seq int, analysisJSON string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM game_hints WHERE game_id = ? AND seat = ?`, gameID, seat); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO game_hints (game_id, seat, seq, analysis_json) VALUES (?, ?, ?, ?)`,
		gameID, seat, seq, analysisJSON); err != nil {
		return err
	}
	return tx.Commit()
}

// HintsByGame returns the latest hint of every seat.
func (s *Store) HintsByGame(ctx context.Context, gameID int64) (map[int]HintRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT h.seat, h.seq, h.analysis_json FROM game_hints h
		JOIN (SELECT seat, MAX(seq) AS seq FROM game_hints WHERE game_id = ? GROUP BY seat) m
		ON h.seat = m.seat AND h.seq = m.seq WHERE h.game_id = ?`, gameID, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]HintRow{}
	for rows.Next() {
		var seat int
		var h HintRow
		if err := rows.Scan(&seat, &h.Seq, &h.Analysis); err != nil {
			return nil, err
		}
		out[seat] = h
	}
	return out, rows.Err()
}

// CurvePoint is one win-rate sample of a player after their own move.
type CurvePoint struct {
	Turn    int
	WinRate float64
	Best    float64
}

// AddCurvePoint stores or replaces one point of a player's win-rate curve.
func (s *Store) AddCurvePoint(ctx context.Context, gameID int64, seat, turn int, playedWin, bestWin float64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO game_curves (game_id, seat, turn, played_win, best_win) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(game_id, seat, turn) DO UPDATE SET played_win = excluded.played_win, best_win = excluded.best_win`,
		gameID, seat, turn, playedWin, bestWin)
	return err
}

// CurveByGame returns every player's win-rate curve, ordered by turn.
func (s *Store) CurveByGame(ctx context.Context, gameID int64) (map[int][]CurvePoint, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT seat, turn, played_win, best_win FROM game_curves WHERE game_id = ? ORDER BY seat, turn`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int][]CurvePoint{}
	for rows.Next() {
		var seat int
		var p CurvePoint
		if err := rows.Scan(&seat, &p.Turn, &p.WinRate, &p.Best); err != nil {
			return nil, err
		}
		out[seat] = append(out[seat], p)
	}
	return out, rows.Err()
}
