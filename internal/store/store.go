// Package store persists users and game records in SQLite.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

var (
	ErrExists   = errors.New("already exists")
	ErrNotFound = errors.New("not found")
)

// Store wraps the database.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the database at path and applies migrations.
// Use ":memory:" for a private in-memory database.
func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	if path != ":memory:" {
		dsn += "&_pragma=journal_mode(WAL)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite allows one writer; a single connection also keeps :memory: databases shared.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY)`); err != nil {
		return err
	}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		var n int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, name).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		body, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (name) VALUES (?)`, name); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// User is a registered account.
type User struct {
	ID         int64     `json:"id"`
	Username   string    `json:"username"`
	ExternalID string    `json:"externalId,omitempty"`
	PwHash     string    `json:"-"`
	IsAdmin    bool      `json:"isAdmin"`
	CreatedAt  time.Time `json:"createdAt"`
}

func (s *Store) CreateUser(ctx context.Context, username, pwHash string) (User, error) {
	now := time.Now()
	res, err := s.db.ExecContext(ctx, `INSERT INTO users (username, pw_hash, created_at) VALUES (?, ?, ?)`, username, pwHash, now.Unix())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return User{}, ErrExists
		}
		return User{}, err
	}
	id, _ := res.LastInsertId()
	return User{ID: id, Username: username, PwHash: pwHash, CreatedAt: time.Unix(now.Unix(), 0)}, nil
}

func (s *Store) scanUser(row *sql.Row) (User, error) {
	var u User
	var ts int64
	if err := row.Scan(&u.ID, &u.Username, &u.ExternalID, &u.PwHash, &u.IsAdmin, &ts); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, err
	}
	u.CreatedAt = time.Unix(ts, 0)
	return u, nil
}

func (s *Store) UserByName(ctx context.Context, username string) (User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx, `SELECT id, username, COALESCE(external_id,''), pw_hash, is_admin, created_at FROM users WHERE username = ?`, username))
}

func (s *Store) UserByID(ctx context.Context, id int64) (User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx, `SELECT id, username, COALESCE(external_id,''), pw_hash, is_admin, created_at FROM users WHERE id = ?`, id))
}

func (s *Store) UserByExternalID(ctx context.Context, externalID string) (User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx, `SELECT id, username, COALESCE(external_id,''), pw_hash, is_admin, created_at FROM users WHERE external_id = ?`, strings.TrimSpace(externalID)))
}

// EnsureExternalUser maps a portal identity to a local mirror row, creating it
// on first visit. Legacy rows without external_id are never hijacked.
func (s *Store) EnsureExternalUser(ctx context.Context, externalID, username string) (User, error) {
	externalID = strings.TrimSpace(externalID)
	username = strings.TrimSpace(username)
	if externalID == "" {
		return User{}, errors.New("external id required")
	}
	if username == "" {
		username = externalID
	}
	if u, err := s.UserByExternalID(ctx, externalID); err == nil {
		return u, nil
	} else if !errors.Is(err, ErrNotFound) {
		return User{}, err
	}
	for attempt := 0; attempt < 5; attempt++ {
		candidate := username
		if _, err := s.UserByName(ctx, candidate); err == nil {
			candidate = username + "_" + randomSuffix()
		} else if !errors.Is(err, ErrNotFound) {
			return User{}, err
		}
		now := time.Now()
		res, err := s.db.ExecContext(ctx, `INSERT INTO users (username, pw_hash, external_id, created_at) VALUES (?, ?, ?, ?)`,
			candidate, "!"+randomSuffix(), externalID, now.Unix())
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				if u, lookupErr := s.UserByExternalID(ctx, externalID); lookupErr == nil {
					return u, nil
				}
				continue
			}
			return User{}, err
		}
		id, _ := res.LastInsertId()
		return User{ID: id, Username: candidate, ExternalID: externalID, PwHash: "!", CreatedAt: time.Unix(now.Unix(), 0)}, nil
	}
	return User{}, errors.New("创建本地镜像用户失败")
}

func randomSuffix() string {
	buf := make([]byte, 3)
	rand.Read(buf)
	return hex.EncodeToString(buf)
}

// PromoteAdmin grants admin rights to an existing account. It reports whether
// a user was found; an unknown account is not an error at startup.
func (s *Store) PromoteAdmin(ctx context.Context, username string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET is_admin = 1 WHERE username = ?`, username)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// GamePlayer is one seat of a recorded game.
type GamePlayer struct {
	Seat        int    `json:"seat"`
	UserID      *int64 `json:"userId"`
	Username    string `json:"username,omitempty"`
	AILevel     string `json:"aiLevel,omitempty"`
	FinalPoints *int   `json:"finalPoints"`
	Winner      bool   `json:"winner"`
}

// GameAction is one recorded move.
type GameAction struct {
	Seq    int     `json:"seq"`
	Seat   int     `json:"seat"`
	Action string  `json:"action"` // JSON-encoded engine.Action
	TS     int64   `json:"ts"`
	Review *string `json:"review,omitempty"` // JSON-encoded ai.MoveReview
}

// Game is a recorded game.
type Game struct {
	ID          int64        `json:"id"`
	RoomID      string       `json:"roomId"`
	Seed        uint64       `json:"seed,string"`
	PlayerCount int          `json:"playerCount"`
	Status      string       `json:"status"`
	StartedAt   int64        `json:"startedAt"`
	EndedAt     *int64       `json:"endedAt"`
	WinnerSeat  *int         `json:"winnerSeat"`
	Players     []GamePlayer `json:"players"`
	Actions     []GameAction `json:"actions,omitempty"`
}

// CreateGame records the start of a game.
func (s *Store) CreateGame(ctx context.Context, roomID string, seed uint64, players []GamePlayer) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO games (room_id, seed, player_count, status, started_at) VALUES (?, ?, ?, 'playing', ?)`,
		roomID, int64(seed), len(players), time.Now().Unix())
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	for _, p := range players {
		var lvl any
		if p.AILevel != "" {
			lvl = p.AILevel
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO game_players (game_id, seat, user_id, ai_level) VALUES (?, ?, ?, ?)`, id, p.Seat, p.UserID, lvl); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

// AddAction appends a move.
func (s *Store) AddAction(ctx context.Context, gameID int64, seq, seat int, actionJSON string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO game_actions (game_id, seq, seat, action_json, ts) VALUES (?, ?, ?, ?, ?)`,
		gameID, seq, seat, actionJSON, time.Now().UnixMilli())
	return err
}

// SetReview attaches a move review to a recorded move.
func (s *Store) SetReview(ctx context.Context, gameID int64, seq int, reviewJSON string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE game_actions SET review_json = ? WHERE game_id = ? AND seq = ?`, reviewJSON, gameID, seq)
	return err
}

// AbortUnfinished marks every game still "playing" as aborted.
func (s *Store) AbortUnfinished(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE games SET status = 'aborted', ended_at = ? WHERE status = 'playing'`, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// AbortOrphanGames aborts running games that no persisted room owns, i.e. games
// whose room was lost or was never written by an older server version.
func (s *Store) AbortOrphanGames(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE games SET status = 'aborted', ended_at = ?
		WHERE status = 'playing' AND id NOT IN (SELECT game_id FROM rooms WHERE game_id IS NOT NULL)`, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// FinishGame stores the result. status is "finished" or "aborted".
func (s *Store) FinishGame(ctx context.Context, gameID int64, status string, points []int, winners []int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var ws any
	if len(winners) > 0 {
		ws = winners[0]
	}
	if _, err := tx.ExecContext(ctx, `UPDATE games SET status = ?, ended_at = ?, winner_seat = ? WHERE id = ?`, status, time.Now().Unix(), ws, gameID); err != nil {
		return err
	}
	isWinner := map[int]bool{}
	for _, w := range winners {
		isWinner[w] = true
	}
	for seat, p := range points {
		if _, err := tx.ExecContext(ctx, `UPDATE game_players SET final_points = ?, winner = ? WHERE game_id = ? AND seat = ?`, p, isWinner[seat], gameID, seat); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) players(ctx context.Context, gameID int64) ([]GamePlayer, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT gp.seat, gp.user_id, COALESCE(u.username, ''), COALESCE(gp.ai_level, ''), gp.final_points, gp.winner
		FROM game_players gp LEFT JOIN users u ON u.id = gp.user_id WHERE gp.game_id = ? ORDER BY gp.seat`, gameID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GamePlayer
	for rows.Next() {
		var p GamePlayer
		var uid sql.NullInt64
		var fp sql.NullInt64
		if err := rows.Scan(&p.Seat, &uid, &p.Username, &p.AILevel, &fp, &p.Winner); err != nil {
			return nil, err
		}
		if uid.Valid {
			v := uid.Int64
			p.UserID = &v
		}
		if fp.Valid {
			v := int(fp.Int64)
			p.FinalPoints = &v
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func scanGame(sc interface{ Scan(...any) error }) (Game, error) {
	var g Game
	var seed int64
	var ended sql.NullInt64
	var winner sql.NullInt64
	if err := sc.Scan(&g.ID, &g.RoomID, &seed, &g.PlayerCount, &g.Status, &g.StartedAt, &ended, &winner); err != nil {
		return g, err
	}
	g.Seed = uint64(seed)
	if ended.Valid {
		v := ended.Int64
		g.EndedAt = &v
	}
	if winner.Valid {
		v := int(winner.Int64)
		g.WinnerSeat = &v
	}
	return g, nil
}

const gameCols = `g.id, g.room_id, g.seed, g.player_count, g.status, g.started_at, g.ended_at, g.winner_seat`

// Game loads a game with players and actions.
func (s *Store) Game(ctx context.Context, id int64) (Game, error) {
	g, err := scanGame(s.db.QueryRowContext(ctx, `SELECT `+gameCols+` FROM games g WHERE g.id = ?`, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return g, ErrNotFound
		}
		return g, err
	}
	if g.Players, err = s.players(ctx, id); err != nil {
		return g, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT seq, seat, action_json, ts, review_json FROM game_actions WHERE game_id = ? ORDER BY seq`, id)
	if err != nil {
		return g, err
	}
	defer rows.Close()
	g.Actions = []GameAction{}
	for rows.Next() {
		var a GameAction
		var rv sql.NullString
		if err := rows.Scan(&a.Seq, &a.Seat, &a.Action, &a.TS, &rv); err != nil {
			return g, err
		}
		if rv.Valid {
			v := rv.String
			a.Review = &v
		}
		g.Actions = append(g.Actions, a)
	}
	return g, rows.Err()
}

// Stats summarizes a user's record.
type Stats struct {
	User      User    `json:"user"`
	Games     int     `json:"games"`
	Wins      int     `json:"wins"`
	WinRate   float64 `json:"winRate"`
	AvgPoints float64 `json:"avgPoints"`
	Recent    []Game  `json:"recent"`
}

// UserStats computes finished-game statistics and the most recent games.
func (s *Store) UserStats(ctx context.Context, userID int64, recent int) (Stats, error) {
	u, err := s.UserByID(ctx, userID)
	if err != nil {
		return Stats{}, err
	}
	st := Stats{User: u, Recent: []Game{}}
	var avg sql.NullFloat64
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(gp.winner), 0), AVG(gp.final_points)
		FROM game_players gp JOIN games g ON g.id = gp.game_id WHERE gp.user_id = ? AND g.status = 'finished'`, userID).Scan(&st.Games, &st.Wins, &avg)
	if err != nil {
		return st, err
	}
	st.AvgPoints = avg.Float64
	if st.Games > 0 {
		st.WinRate = float64(st.Wins) / float64(st.Games)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+gameCols+` FROM games g JOIN game_players gp ON gp.game_id = g.id
		WHERE gp.user_id = ? ORDER BY g.id DESC LIMIT ?`, userID, recent)
	if err != nil {
		return st, err
	}
	var games []Game
	for rows.Next() {
		g, err := scanGame(rows)
		if err != nil {
			rows.Close()
			return st, err
		}
		games = append(games, g)
	}
	rows.Close()
	for _, g := range games {
		if g.Players, err = s.players(ctx, g.ID); err != nil {
			return st, err
		}
		st.Recent = append(st.Recent, g)
	}
	return st, nil
}
