package store

import (
	"context"
	"path/filepath"
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

func TestReplayAnalysisSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "replay.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	gid, err := st.CreateGame(ctx, "REPLAY", 7, []GamePlayer{{Seat: 0, AILevel: "easy"}, {Seat: 1, AILevel: "medium"}})
	if err != nil {
		t.Fatal(err)
	}
	for seq := 0; seq < 2; seq++ {
		if err := st.AddAction(ctx, gid, seq, 0, `{}`); err != nil {
			t.Fatal(err)
		}
		if err := st.SaveReplayAnalysis(ctx, gid, 0, seq, seq+1, `{"level":"master"}`, `{"verdict":"best"}`, 0.4, 0.5); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	hints, err := st.ReplayHints(ctx, gid)
	if err != nil || len(hints) != 2 {
		t.Fatalf("all hints: %v %+v", err, hints)
	}
	latest, err := st.HintsByGame(ctx, gid)
	if err != nil || latest[0].Seq != 1 {
		t.Fatalf("latest hint: %v %+v", err, latest)
	}
	g, err := st.Game(ctx, gid)
	if err != nil || g.Actions[0].Review == nil || g.Actions[1].Review == nil {
		t.Fatalf("reviews: %v %+v", err, g)
	}
	curve, err := st.CurveByGame(ctx, gid)
	if err != nil || len(curve[0]) != 2 || curve[0][1].WinRate != 0.4 {
		t.Fatalf("curve: %v %+v", err, curve)
	}
}

func TestRoomsAdminSettingsHintsCurves(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	u, err := st.CreateUser(ctx, "mango", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if u.IsAdmin {
		t.Fatal("new accounts must not be admin")
	}
	if ok, err := st.PromoteAdmin(ctx, "MANGO"); err != nil || !ok {
		t.Fatalf("promote: %v %v", ok, err)
	}
	if u, err = st.UserByName(ctx, "mango"); err != nil || !u.IsAdmin {
		t.Fatalf("admin flag: %v %+v", err, u)
	}
	if ok, err := st.PromoteAdmin(ctx, "nobody"); err != nil || ok {
		t.Fatalf("promote unknown: %v %v", ok, err)
	}

	if _, ok, err := st.GetSetting(ctx, "idle_close_seconds"); err != nil || ok {
		t.Fatalf("missing setting: %v %v", ok, err)
	}
	if err := st.SetSetting(ctx, "idle_close_seconds", "604800"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting(ctx, "idle_close_seconds", "0"); err != nil {
		t.Fatal(err)
	}
	if v, ok, err := st.GetSetting(ctx, "idle_close_seconds"); err != nil || !ok || v != "0" {
		t.Fatalf("setting: %q %v %v", v, ok, err)
	}

	gid, err := st.CreateGame(ctx, "ROOMAA", 42, []GamePlayer{{Seat: 0, UserID: &u.ID}, {Seat: 1, AILevel: "medium"}})
	if err != nil {
		t.Fatal(err)
	}
	rec := RoomRecord{ID: "ROOMAA", Name: "persisted", OwnerID: u.ID, OwnerName: "mango", Status: "waiting",
		Hints: true, TurnSeconds: 30, Seats: []RoomSeat{
			{Kind: "human", UserID: u.ID, Name: "mango"},
			{Kind: "ai", Level: "medium"},
		}}
	if err := st.SaveRoom(ctx, rec); err != nil {
		t.Fatal(err)
	}
	rec.Status, rec.GameID = "playing", gid
	if err := st.SaveRoom(ctx, rec); err != nil {
		t.Fatal(err)
	}
	rooms, err := st.LoadRooms(ctx)
	if err != nil || len(rooms) != 1 {
		t.Fatalf("load rooms: %v %+v", err, rooms)
	}
	got := rooms[0]
	if got.Status != "playing" || got.GameID != gid || got.TurnSeconds != 30 || len(got.Seats) != 2 ||
		got.Seats[0].Name != "mango" || got.Seats[1].Level != "medium" || got.CreatedAt == 0 {
		t.Fatalf("room round trip: %+v", got)
	}

	if err := st.SetHint(ctx, gid, 0, 1, `{"winRate":0.4}`); err != nil {
		t.Fatal(err)
	}
	if err := st.SetHint(ctx, gid, 0, 3, `{"winRate":0.6}`); err != nil {
		t.Fatal(err)
	}
	hints, err := st.HintsByGame(ctx, gid)
	if err != nil || len(hints) != 1 || hints[0].Seq != 3 || hints[0].Analysis != `{"winRate":0.6}` {
		t.Fatalf("hints: %v %+v", err, hints)
	}
	allHints, err := st.ReplayHints(ctx, gid)
	if err != nil || len(allHints) != 2 || allHints[1] != `{"winRate":0.4}` {
		t.Fatalf("historical hints: %v %+v", err, allHints)
	}

	if err := st.AddCurvePoint(ctx, gid, 0, 4, 0.5, 0.7); err != nil {
		t.Fatal(err)
	}
	if err := st.AddCurvePoint(ctx, gid, 0, 4, 0.55, 0.75); err != nil {
		t.Fatal(err)
	}
	if err := st.AddCurvePoint(ctx, gid, 0, 5, 0.6, 0.8); err != nil {
		t.Fatal(err)
	}
	curves, err := st.CurveByGame(ctx, gid)
	if err != nil || len(curves[0]) != 2 || curves[0][0].WinRate != 0.55 || curves[0][1].Best != 0.8 {
		t.Fatalf("curves: %v %+v", err, curves)
	}

	// Orphaned games are aborted; the game owned by a persisted room survives.
	orphan, err := st.CreateGame(ctx, "ROOMBB", 7, []GamePlayer{{Seat: 0, UserID: &u.ID}, {Seat: 1, AILevel: "easy"}})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := st.AbortOrphanGames(ctx); err != nil || n != 1 {
		t.Fatalf("abort orphans: %d %v", n, err)
	}
	if g, _ := st.Game(ctx, orphan); g.Status != "aborted" {
		t.Fatalf("orphan status: %s", g.Status)
	}
	if g, _ := st.Game(ctx, gid); g.Status != "playing" {
		t.Fatalf("owned game status: %s", g.Status)
	}

	if err := st.DeleteRoom(ctx, rec.ID); err != nil {
		t.Fatal(err)
	}
	if rooms, err := st.LoadRooms(ctx); err != nil || len(rooms) != 0 {
		t.Fatalf("rooms after delete: %v %+v", err, rooms)
	}
}
