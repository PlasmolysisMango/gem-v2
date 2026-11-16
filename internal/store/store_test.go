package store

import (
	"context"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	u, err := st.CreateUser(ctx, "alice", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateUser(ctx, "ALICE", "x"); err != ErrExists {
		t.Fatalf("want ErrExists, got %v", err)
	}
	if got, err := st.UserByName(ctx, "Alice"); err != nil || got.ID != u.ID {
		t.Fatalf("lookup: %v %+v", err, got)
	}
	if _, err := st.UserByID(ctx, 999); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	seed := uint64(1) << 62
	gid, err := st.CreateGame(ctx, "ROOM01", seed, []GamePlayer{{Seat: 0, UserID: &u.ID}, {Seat: 1, AILevel: "hard"}})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := st.AddAction(ctx, gid, i, i%2, `{"type":"take_diff"}`); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetReview(ctx, gid, 0, `{"verdict":"best"}`); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishGame(ctx, gid, "finished", []int{15, 9}, []int{0}); err != nil {
		t.Fatal(err)
	}
	g, err := st.Game(ctx, gid)
	if err != nil {
		t.Fatal(err)
	}
	if g.Seed != seed || len(g.Actions) != 3 || g.Actions[0].Review == nil || !g.Players[0].Winner || g.Players[1].AILevel != "hard" || g.Players[0].Username != "alice" {
		t.Fatalf("game: %+v", g)
	}
	stats, err := st.UserStats(ctx, u.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Games != 1 || stats.Wins != 1 || stats.AvgPoints != 15 || len(stats.Recent) != 1 {
		t.Fatalf("stats: %+v", stats)
	}

	// Games orphaned by a restart are aborted; finished games are untouched.
	gid2, err := st.CreateGame(ctx, "ROOM02", 7, []GamePlayer{{Seat: 0, UserID: &u.ID}, {Seat: 1, AILevel: "easy"}})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := st.AbortUnfinished(ctx); err != nil || n != 1 {
		t.Fatalf("abort: %d %v", n, err)
	}
	if g, _ := st.Game(ctx, gid2); g.Status != "aborted" || g.EndedAt == nil {
		t.Fatalf("orphan: %+v", g)
	}
	if g, _ := st.Game(ctx, gid); g.Status != "finished" {
		t.Fatalf("finished game changed: %+v", g)
	}
}
