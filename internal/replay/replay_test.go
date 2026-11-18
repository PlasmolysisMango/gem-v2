package replay

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gem/internal/ai"
	"gem/internal/engine"
	"gem/internal/store"
)

func testReplay(t *testing.T) (*store.Store, int64) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "replay.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	u, err := st.CreateUser(ctx, "viewer", "hash")
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.CreateGame(ctx, "replay", 7, []store.GamePlayer{{Seat: 0, UserID: &u.ID}, {Seat: 1, AILevel: "medium"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	state, _ := engine.NewGame(2, 7)
	a := state.LegalActions(nil)[0]
	b, _ := json.Marshal(a)
	if err := st.AddAction(ctx, id, 0, 0, string(b)); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishGame(ctx, id, "finished", []int{15, 0}, []int{0}); err != nil {
		t.Fatal(err)
	}
	return st, id
}

func waitRunning(t *testing.T, st *store.Store, id int64) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		job, err := st.ReplayJobStatus(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if job.State == "running" {
			return
		}
		select {
		case <-deadline:
			t.Fatal("worker did not start")
		case <-tick.C:
		}
	}
}

func TestForegroundPreemptsWorkerAndDeduplicates(t *testing.T) {
	st, id := testReplay(t)
	path, _ := st.ReplayLockPath()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	background := make(chan error, 1)
	go func() { _, err := workNext(ctx, st, path, 5*time.Second); background <- err }()
	waitRunning(t, st, id)
	start := time.Now()
	f, err := Foreground(ctx, st, id, 0, 25*time.Millisecond)
	if err != nil || f.Analysis == nil || f.Review == nil {
		t.Fatalf("foreground: %+v %v", f, err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("preemption took %s", time.Since(start))
	}
	if err := <-background; !errors.Is(err, context.Canceled) && !errors.Is(err, store.ErrReplayLease) {
		t.Fatalf("worker not preempted: %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cached, err := Foreground(ctx, st, id, 0, time.Second)
			if err != nil || cached.Analysis.ElapsedMs != f.Analysis.ElapsedMs || cached.Review.PlayedWinRate != f.Review.PlayedWinRate {
				t.Errorf("cache mismatch: %+v %v", cached, err)
			}
		}()
	}
	wg.Wait()
	worked, err := workNext(ctx, st, path, 5*time.Second)
	if !worked || err != nil {
		t.Fatalf("cached job: %v %v", worked, err)
	}
	job, _ := st.ReplayJobStatus(ctx, id)
	if job.State != "complete" {
		t.Fatalf("complete job: %+v", job)
	}
}

func TestExecutionLockOutlivesLeaseAndWaitIsCancellable(t *testing.T) {
	st, id := testReplay(t)
	path, _ := st.ReplayLockPath()
	lock, err := tryAcquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if _, err := tryAcquire(path); err == nil {
		t.Fatal("execution overlap")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if _, err := Foreground(ctx, st, id, 0, time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("uncancellable wait: %v", err)
	}
	hints, err := st.ReplayHints(context.Background(), id)
	if err != nil || len(hints) != 0 {
		t.Fatalf("cancel wrote hints: %v %v", hints, err)
	}
}

func TestActivityPausesRunningWorker(t *testing.T) {
	st, id := testReplay(t)
	path, _ := st.ReplayLockPath()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := workNext(ctx, st, path, 5*time.Second); result <- err }()
	waitRunning(t, st, id)
	active, err := st.CreateGame(ctx, "paused-but-playing", 8, []store.GamePlayer{{Seat: 0, AILevel: "easy"}, {Seat: 1, AILevel: "easy"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) && !errors.Is(err, store.ErrReplayLease) {
			t.Fatalf("activity ignored: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not cancel")
	}
	hints, err := st.ReplayHints(ctx, id)
	if err != nil || len(hints) != 0 {
		t.Fatalf("partial result saved: %v %v", hints, err)
	}
	if _, err := Foreground(ctx, st, active, 0, time.Millisecond); !errors.Is(err, ErrPlaying) {
		t.Fatalf("live replay allowed: %v", err)
	}
	if err := st.FinishGame(ctx, active, "aborted", []int{0, 0}, nil); err != nil {
		t.Fatal(err)
	}
	worked, err := workNext(ctx, st, path, 25*time.Millisecond)
	if err != nil || !worked {
		t.Fatalf("resume: %v %v", worked, err)
	}
	job, _ := st.ReplayJobStatus(ctx, id)
	if job.State != "complete" {
		t.Fatalf("not completed: %+v", job)
	}
}

func TestWorkerSingletonAndOfflinePersistence(t *testing.T) {
	st, id := testReplay(t)
	path, _ := st.ReplayLockPath()
	singleton, err := tryAcquire(path + ".worker")
	if err != nil {
		t.Fatal(err)
	}
	if err := RunWorker(context.Background(), st, time.Millisecond); err == nil {
		t.Fatal("second worker admitted")
	}
	singleton.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	done := make(chan error, 1)
	go func() { done <- RunWorker(ctx, st, 25*time.Millisecond) }()
	deadline := time.After(4 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		job, err := st.ReplayJobStatus(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if job.State == "complete" {
			break
		}
		select {
		case <-deadline:
			t.Fatal("offline job not completed")
		case <-tick.C:
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	g, err := st.Game(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	rp, err := Load(context.Background(), st, g)
	if err != nil || rp.Frames[0].Analysis == nil || rp.Frames[0].Analysis.Level != ai.Master || rp.Frames[0].Review == nil {
		t.Fatalf("offline persistence: %+v %v", rp, err)
	}
}
