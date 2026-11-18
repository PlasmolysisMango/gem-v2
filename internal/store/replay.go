package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var ErrReplayLease = errors.New("replay execution lease lost")

const replayLease = 10 * time.Second

type ReplayLease struct {
	Owner      string
	Generation int64
	Background bool
}

type ReplayJob struct {
	GameID     int64  `json:"gameId"`
	State      string `json:"state"`
	Owner      string `json:"-"`
	LeaseUntil int64  `json:"-"`
	Attempts   int    `json:"-"`
	RetryAt    int64  `json:"retryAt,omitempty"`
	Error      string `json:"error,omitempty"`
}

func (s *Store) ReplayLockPath() (string, error) {
	if s.path == ":memory:" {
		return filepath.Join(os.TempDir(), fmt.Sprintf("gem-replay-%p.lock", s)), nil
	}
	path, err := filepath.EvalSymlinks(s.path)
	if err != nil {
		return "", err
	}
	path, err = filepath.Abs(path)
	return path + ".replay.lock", err
}

func (s *Store) BackfillReplayJobs(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO replay_jobs(game_id)
		SELECT id FROM games WHERE status = 'finished' AND (hints_enabled = 0 OR hints_enabled IS NULL)
		AND EXISTS (SELECT 1 FROM game_players WHERE game_id = games.id AND user_id IS NOT NULL)`)
	return err
}

func (s *Store) ReplayDemand(ctx context.Context, owner string, active bool) error {
	if !active {
		_, err := s.db.ExecContext(ctx, `DELETE FROM replay_demands WHERE owner = ?`, owner)
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO replay_demands(owner, lease_until) VALUES (?, ?)
		ON CONFLICT(owner) DO UPDATE SET lease_until = excluded.lease_until`, owner, time.Now().Add(replayLease).UnixMilli())
	return err
}

func (s *Store) ReplayIdle(ctx context.Context) (bool, error) {
	var idle bool
	err := s.db.QueryRowContext(ctx, `SELECT NOT EXISTS(SELECT 1 FROM games WHERE status = 'playing')
		AND NOT EXISTS(SELECT 1 FROM replay_demands WHERE lease_until > ?)`, time.Now().UnixMilli()).Scan(&idle)
	return idle, err
}

// The file lock must be held before replacing an execution generation, even after expiry.
func (s *Store) BeginReplayExecution(ctx context.Context, owner string, background bool) (ReplayLease, error) {
	lease := ReplayLease{Owner: owner, Background: background}
	query := `UPDATE replay_execution SET owner = ?, generation = generation + 1, lease_until = ? WHERE id = 1`
	args := []any{owner, time.Now().Add(replayLease).UnixMilli()}
	if background {
		query += ` AND NOT EXISTS(SELECT 1 FROM games WHERE status = 'playing')
			AND NOT EXISTS(SELECT 1 FROM replay_demands WHERE lease_until > ?)`
		args = append(args, time.Now().UnixMilli())
	}
	err := s.db.QueryRowContext(ctx, query+` RETURNING generation`, args...).Scan(&lease.Generation)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrReplayLease
	}
	return lease, err
}

func (s *Store) RenewReplayExecution(ctx context.Context, lease ReplayLease) error {
	query := `UPDATE replay_execution SET lease_until = ? WHERE id = 1 AND owner = ? AND generation = ? AND lease_until > ?`
	args := []any{time.Now().Add(replayLease).UnixMilli(), lease.Owner, lease.Generation, time.Now().UnixMilli()}
	if lease.Background {
		query += ` AND NOT EXISTS(SELECT 1 FROM games WHERE status = 'playing')
			AND NOT EXISTS(SELECT 1 FROM replay_demands WHERE lease_until > ?)`
		args = append(args, time.Now().UnixMilli())
	}
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return ErrReplayLease
	}
	return err
}

func (s *Store) EndReplayExecution(ctx context.Context, lease ReplayLease) error {
	_, err := s.db.ExecContext(ctx, `UPDATE replay_execution SET owner = '', lease_until = 0 WHERE id = 1 AND owner = ? AND generation = ?`, lease.Owner, lease.Generation)
	return err
}

func (s *Store) ClaimReplayJob(ctx context.Context, owner string) (ReplayJob, error) {
	var job ReplayJob
	now := time.Now().UnixMilli()
	err := s.db.QueryRowContext(ctx, `UPDATE replay_jobs SET state = 'running', owner = ?, lease_until = ?
		WHERE game_id = (SELECT j.game_id FROM replay_jobs j JOIN games g ON g.id = j.game_id
		WHERE g.status = 'finished' AND ((j.state = 'pending' AND j.retry_at <= ?) OR (j.state = 'running' AND j.lease_until <= ?))
		ORDER BY j.game_id LIMIT 1)
		RETURNING game_id, state, owner, lease_until, attempts, retry_at, error`, owner, now+replayLease.Milliseconds(), now, now).
		Scan(&job.GameID, &job.State, &job.Owner, &job.LeaseUntil, &job.Attempts, &job.RetryAt, &job.Error)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return job, err
}

func (s *Store) RenewReplayJob(ctx context.Context, job ReplayJob) error {
	res, err := s.db.ExecContext(ctx, `UPDATE replay_jobs SET lease_until = ? WHERE game_id = ? AND owner = ? AND state = 'running' AND lease_until > ?`,
		time.Now().Add(replayLease).UnixMilli(), job.GameID, job.Owner, time.Now().UnixMilli())
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return ErrReplayLease
	}
	return err
}

func (s *Store) FinishReplayJob(ctx context.Context, job ReplayJob, complete bool, cause error) error {
	state, retry, message, attempts := "pending", int64(0), "", job.Attempts
	if complete {
		state = "complete"
	} else if cause != nil && !errors.Is(cause, context.Canceled) && !errors.Is(cause, ErrReplayLease) {
		attempts++
		message = cause.Error()
		if attempts >= 5 {
			state = "failed"
		} else {
			retry = time.Now().Add(time.Duration(1<<attempts) * time.Minute).UnixMilli()
		}
	}
	_, err := s.db.ExecContext(ctx, `UPDATE replay_jobs SET state = ?, owner = '', lease_until = 0, attempts = ?, retry_at = ?, error = ?
		WHERE game_id = ? AND owner = ? AND state = 'running'`, state, attempts, retry, message, job.GameID, job.Owner)
	return err
}

func (s *Store) ReplayJobStatus(ctx context.Context, gameID int64) (ReplayJob, error) {
	job := ReplayJob{GameID: gameID, State: "none"}
	err := s.db.QueryRowContext(ctx, `SELECT state, owner, lease_until, attempts, retry_at, error FROM replay_jobs WHERE game_id = ?`, gameID).
		Scan(&job.State, &job.Owner, &job.LeaseUntil, &job.Attempts, &job.RetryAt, &job.Error)
	if errors.Is(err, sql.ErrNoRows) {
		return job, nil
	}
	if err != nil {
		return job, err
	}
	if job.State == "running" && job.LeaseUntil <= time.Now().UnixMilli() {
		job.State = "pending"
	}
	if job.State == "pending" || job.State == "running" {
		idle, err := s.ReplayIdle(ctx)
		if err != nil {
			return job, err
		}
		if !idle {
			job.State = "paused"
		}
	}
	return job, nil
}

func checkReplayLease(ctx context.Context, tx *sql.Tx, gameID int64, lease ReplayLease) error {
	var valid bool
	query := `SELECT EXISTS(SELECT 1 FROM replay_execution WHERE id = 1 AND owner = ? AND generation = ? AND lease_until > ?)
		AND EXISTS(SELECT 1 FROM games WHERE id = ? AND status != 'playing')`
	args := []any{lease.Owner, lease.Generation, time.Now().UnixMilli(), gameID}
	if lease.Background {
		query += ` AND NOT EXISTS(SELECT 1 FROM games WHERE status = 'playing') AND NOT EXISTS(SELECT 1 FROM replay_demands WHERE lease_until > ?)
			AND EXISTS(SELECT 1 FROM replay_jobs WHERE game_id = ? AND owner = ? AND state = 'running' AND lease_until > ?)`
		args = append(args, time.Now().UnixMilli(), gameID, lease.Owner, time.Now().UnixMilli())
	}
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return ErrReplayLease
	}
	return nil
}
