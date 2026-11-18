package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestReplayJobsEligibilityHistoryAndRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "jobs.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	u, err := st.CreateUser(ctx, "replay", "hash")
	if err != nil {
		t.Fatal(err)
	}
	create := func(name string, human, hints bool, status string) int64 {
		players := []GamePlayer{{Seat: 0, AILevel: "easy"}, {Seat: 1, AILevel: "medium"}}
		if human {
			players[0] = GamePlayer{Seat: 0, UserID: &u.ID}
		}
		id, err := st.CreateGame(ctx, name, 7, players, hints)
		if err != nil {
			t.Fatal(err)
		}
		if status != "playing" {
			if err := st.FinishGame(ctx, id, status, []int{15, 2}, []int{0}); err != nil {
				t.Fatal(err)
			}
		}
		g, err := st.Game(ctx, id)
		if err != nil || g.HintsEnabled == nil || *g.HintsEnabled != hints {
			t.Fatalf("settings: %+v %v", g, err)
		}
		return id
	}
	queued := create("unhinted", true, false, "finished")
	liveHints := create("hinted", true, true, "finished")
	bots := create("bots", false, false, "finished")
	aborted := create("aborted", true, false, "aborted")
	playing := create("paused", true, false, "playing")
	legacy := create("old", true, true, "finished")
	if _, err := st.db.Exec(`UPDATE games SET hints_enabled = NULL WHERE id = ?`, legacy); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := st.BackfillReplayJobs(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []int64{queued, legacy} {
		var count int
		if err := st.db.QueryRow(`SELECT count(*) FROM replay_jobs WHERE game_id = ?`, id).Scan(&count); err != nil || count != 1 {
			t.Fatalf("unique queue: %d %v", count, err)
		}
	}
	for _, id := range []int64{liveHints, bots, aborted, playing} {
		job, err := st.ReplayJobStatus(ctx, id)
		if err != nil || job.State != "none" {
			t.Fatalf("ineligible: %+v %v", job, err)
		}
	}
	g, err := st.Game(ctx, legacy)
	if err != nil || g.HintsEnabled != nil {
		t.Fatalf("legacy setting: %+v %v", g, err)
	}
	if idle, err := st.ReplayIdle(ctx); err != nil || idle {
		t.Fatalf("playing gate: %v %v", idle, err)
	}
	if _, err := st.BeginReplayExecution(ctx, "background", true); !errors.Is(err, ErrReplayLease) {
		t.Fatalf("playing admitted: %v", err)
	}
	if err := st.FinishGame(ctx, playing, "aborted", []int{0, 0}, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplayDemand(ctx, "viewer", true); err != nil {
		t.Fatal(err)
	}
	if idle, _ := st.ReplayIdle(ctx); idle {
		t.Fatal("foreground demand ignored")
	}
	if err := st.ReplayDemand(ctx, "viewer", false); err != nil {
		t.Fatal(err)
	}
	job, err := st.ClaimReplayJob(ctx, "crashed")
	if err != nil || job.GameID != queued {
		t.Fatalf("claim: %+v %v", job, err)
	}
	if _, err := st.db.Exec(`UPDATE replay_jobs SET lease_until = 0 WHERE game_id = ?`, job.GameID); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	job, err = st.ClaimReplayJob(ctx, "restarted")
	if err != nil || job.GameID != queued || job.Owner != "restarted" {
		t.Fatalf("recover: %+v %v", job, err)
	}
	if err := st.FinishReplayJob(ctx, job, true, nil); err != nil {
		t.Fatal(err)
	}
	status, _ := st.ReplayJobStatus(ctx, queued)
	if status.State != "complete" {
		t.Fatalf("finish: %+v", status)
	}
}

func TestReplayLeaseRejectsStaleAndActiveGameWrites(t *testing.T) {
	ctx := context.Background()
	st, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	u, _ := st.CreateUser(ctx, "lease", "hash")
	id, err := st.CreateGame(ctx, "lease", 7, []GamePlayer{{Seat: 0, UserID: &u.ID}, {Seat: 1, AILevel: "easy"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddAction(ctx, id, 0, 0, `{}`); err != nil {
		t.Fatal(err)
	}
	first, err := st.BeginReplayExecution(ctx, "first", false)
	if err != nil {
		t.Fatal(err)
	}
	save := func(lease ReplayLease) error {
		return st.SaveReplayAnalysis(ctx, id, 0, 0, 0, `{"level":"master"}`, `{}`, .5, .5, lease)
	}
	if err := save(first); !errors.Is(err, ErrReplayLease) {
		t.Fatalf("playing saved: %v", err)
	}
	if err := st.FinishGame(ctx, id, "finished", []int{15, 2}, []int{0}); err != nil {
		t.Fatal(err)
	}
	second, err := st.BeginReplayExecution(ctx, "second", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := save(first); !errors.Is(err, ErrReplayLease) {
		t.Fatalf("stale saved: %v", err)
	}
	if err := st.EndReplayExecution(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := save(second); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`UPDATE replay_execution SET lease_until = ?`, time.Now().Add(-time.Second).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if err := save(second); !errors.Is(err, ErrReplayLease) {
		t.Fatalf("expired saved: %v", err)
	}
	if err := st.RenewReplayExecution(ctx, second); !errors.Is(err, ErrReplayLease) {
		t.Fatalf("expired renewed: %v", err)
	}
}

func TestReplayJobRetryIsBounded(t *testing.T) {
	ctx := context.Background()
	st, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	u, _ := st.CreateUser(ctx, "retry", "hash")
	id, err := st.CreateGame(ctx, "retry", 7, []GamePlayer{{Seat: 0, UserID: &u.ID}, {Seat: 1, AILevel: "easy"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishGame(ctx, id, "finished", []int{15, 2}, []int{0}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		job, err := st.ClaimReplayJob(ctx, "retry")
		if err != nil {
			t.Fatal(err)
		}
		if err := st.FinishReplayJob(ctx, job, false, errors.New("bad record")); err != nil {
			t.Fatal(err)
		}
		status, _ := st.ReplayJobStatus(ctx, id)
		if status.Error == "" {
			t.Fatal("missing error")
		}
		if i < 4 {
			if _, err := st.ClaimReplayJob(ctx, "retry"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("backoff ignored: %v", err)
			}
			st.db.Exec(`UPDATE replay_jobs SET retry_at = 0 WHERE game_id = ?`, id)
		} else if status.State != "failed" {
			t.Fatalf("not bounded: %+v", status)
		}
	}
}
