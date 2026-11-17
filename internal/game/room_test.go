package game

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"gem/internal/engine"
	"gem/internal/store"
)

func testHub(t *testing.T, turnSeconds int) (*Hub, *store.Store) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alice", "spectator"} {
		if _, err := st.CreateUser(context.Background(), name, "hash"); err != nil {
			t.Fatal(err)
		}
	}
	cfg := DefaultConfig()
	cfg.AIDelay = 10 * time.Millisecond
	cfg.HardBudget = 40 * time.Millisecond
	cfg.HintBudget = 60 * time.Millisecond
	cfg.IdleClose = time.Hour
	h := NewHub(st, cfg)
	t.Cleanup(func() {
		h.Close()
		st.Close()
	})
	return h, st
}

func readEnv(t *testing.T, s *Session, timeout time.Duration) Envelope {
	t.Helper()
	select {
	case e := <-s.Out():
		return e
	case <-time.After(timeout):
		t.Fatal("timed out waiting for a message")
		return Envelope{}
	}
}

func waitEnv(t *testing.T, s *Session, timeout time.Duration, pred func(Envelope) bool) Envelope {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		left := time.Until(deadline)
		if left <= 0 {
			t.Fatal("timed out waiting for a matching message")
		}
		select {
		case e := <-s.Out():
			if pred(e) {
				return e
			}
		case <-time.After(left):
			t.Fatal("timed out waiting for a matching message")
		}
	}
}

func decode[T any](t *testing.T, e Envelope) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(e.Payload, &v); err != nil {
		t.Fatalf("decode %s: %v", e.Type, err)
	}
	return v
}

func waitGone(t *testing.T, h *Hub, id string) {
	t.Helper()
	for i := 0; i < 300; i++ {
		if h.Get(id) == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("room %s was not stopped", id)
}

func startRoom(t *testing.T, h *Hub, owner *Session, room *Room) {
	t.Helper()
	room.Join(owner)
	waitEnv(t, owner, 2*time.Second, func(e Envelope) bool {
		return e.Type == MsgRoomState && decode[RoomState](t, e).YourSeat == 0
	})
	room.Handle(owner, Envelope{Type: MsgStart})
	waitEnv(t, owner, 2*time.Second, func(e Envelope) bool {
		return e.Type == MsgRoomState && decode[RoomState](t, e).Status == "playing"
	})
}

// TestGamePausesWithoutOnlineHumans covers two behaviours: no AI takes over a
// disconnected human seat, and turn scheduling (including the turn timer) stops
// while nobody online can act.
func TestGamePausesWithoutOnlineHumans(t *testing.T) {
	h, _ := testHub(t, 1)
	room, err := h.Create(1, "alice", RoomSettings{AIs: []string{"medium"}, TurnSeconds: 1})
	if err != nil {
		t.Fatal(err)
	}
	owner := NewSession(1, "alice")
	startRoom(t, h, owner, room)
	waitEnv(t, owner, 2*time.Second, func(e Envelope) bool {
		if e.Type != MsgGameView {
			return false
		}
		v := decode[GameViewPayload](t, e).View
		return v.Viewer == 0 && v.Current == 0 && v.Turn == 0
	})
	room.Leave(owner, false)

	spec := NewSession(2, "spec")
	room.Join(spec)
	rs := decode[RoomState](t, waitEnv(t, spec, 2*time.Second, func(e Envelope) bool {
		return e.Type == MsgRoomState
	}))
	if rs.Seats[0].Connected || rs.Seats[0].Kind != SeatHuman {
		t.Fatalf("dropped human seat should stay reserved: %+v", rs.Seats[0])
	}
	if rs.Deadline != 0 {
		t.Fatalf("paused room must not run a turn timer: %+v", rs)
	}
	v := decode[GameViewPayload](t, waitEnv(t, spec, 2*time.Second, func(e Envelope) bool {
		return e.Type == MsgGameView
	})).View
	if v.Current != 0 || v.Turn != 0 {
		t.Fatalf("unexpected position: current=%d turn=%d", v.Current, v.Turn)
	}
	waitEnv(t, spec, 2*time.Second, func(e Envelope) bool { return e.Type == MsgEvents })

	// Longer than the turn timer and AI delay: nothing may happen while paused.
	time.Sleep(1300 * time.Millisecond)
drained:
	for {
		select {
		case e := <-spec.Out():
			if e.Type == MsgEvents || e.Type == MsgGameView {
				t.Fatalf("game advanced while no human was online (%s)", e.Type)
			}
		default:
			break drained
		}
	}

	// The owner comes back: the turn timer restarts and play continues.
	owner2 := NewSession(1, "alice")
	room.Join(owner2)
	waitEnv(t, owner2, 2*time.Second, func(e Envelope) bool {
		return e.Type == MsgRoomState && decode[RoomState](t, e).YourSeat == 0
	})
	waitEnv(t, owner2, 2*time.Second, func(e Envelope) bool {
		return e.Type == MsgGameView && decode[GameViewPayload](t, e).View.Viewer == 0
	})
	waitEnv(t, owner2, 5*time.Second, func(e Envelope) bool {
		return e.Type == MsgGameView && decode[GameViewPayload](t, e).View.Turn > 0
	})
}

// TestRestoreRoomReplaysGame restores a running room from the same store after
// a graceful shutdown and checks the position and cached hint survive.
func TestRestoreRoomReplaysGame(t *testing.T) {
	h, st := testHub(t, 0)
	room, err := h.Create(1, "alice", RoomSettings{AIs: []string{"medium"}, Hints: boolPtr(true)})
	if err != nil {
		t.Fatal(err)
	}
	roomID := room.ID()
	owner := NewSession(1, "alice")
	startRoom(t, h, owner, room)

	view := decode[GameViewPayload](t, waitEnv(t, owner, 2*time.Second, func(e Envelope) bool {
		return e.Type == MsgGameView && decode[GameViewPayload](t, e).View.Current == 0
	})).View
	room.Handle(owner, Envelope{Type: MsgAction, Payload: mustJSON(ActionPayload{Action: view.Legal[0]})})

	reviewSeen, atOurTurn := false, false
	var turn int
	for !reviewSeen || !atOurTurn {
		e := readEnv(t, owner, 10*time.Second)
		switch e.Type {
		case MsgMoveReview:
			reviewSeen = true
		case MsgGameView:
			v := decode[GameViewPayload](t, e).View
			if v.Viewer == 0 && v.Current == 0 && v.Phase == engine.PhaseMain && v.Turn > 0 {
				atOurTurn, turn = true, v.Turn
			}
		case MsgError:
			t.Fatalf("error: %s", e.Payload)
		}
	}
	room.Handle(owner, Envelope{Type: MsgRequestHint})
	hint := decode[HintPayload](t, waitEnv(t, owner, 5*time.Second, func(e Envelope) bool {
		return e.Type == MsgHint
	}))
	if hint.Analysis == nil || len(hint.Analysis.Candidates) == 0 || hint.Turn == 0 {
		t.Fatalf("bad hint: %+v", hint)
	}
	room.Leave(owner, false)
	h.Shutdown()
	waitGone(t, h, roomID)

	rooms, err := st.LoadRooms(context.Background())
	if err != nil || len(rooms) != 1 || rooms[0].Status != "playing" || rooms[0].GameID == 0 {
		t.Fatalf("persisted room: %v %+v", err, rooms)
	}
	rec, err := st.Game(context.Background(), rooms[0].GameID)
	if err != nil || rec.Status != "playing" || len(rec.Actions) == 0 {
		t.Fatalf("persisted game: %v %+v", err, rec)
	}
	if rec.Actions[0].Review == nil {
		t.Fatal("move review was not persisted")
	}
	curves, err := st.CurveByGame(context.Background(), rec.ID)
	if err != nil || len(curves[0]) == 0 {
		t.Fatalf("curve was not persisted: %v %+v", err, curves)
	}

	h2 := NewHub(st, DefaultConfig())
	t.Cleanup(func() { h2.Close() })
	if err := h2.Restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	restored := h2.Get(roomID)
	if restored == nil || restored.Summary().Status != "playing" {
		t.Fatalf("room was not restored: %+v", restored)
	}

	owner2 := NewSession(1, "alice")
	restored.Join(owner2)
	waitEnv(t, owner2, 2*time.Second, func(e Envelope) bool {
		return e.Type == MsgRoomState && decode[RoomState](t, e).Status == "playing"
	})
	rv := decode[GameViewPayload](t, waitEnv(t, owner2, 2*time.Second, func(e Envelope) bool {
		return e.Type == MsgGameView && decode[GameViewPayload](t, e).View.Viewer == 0
	})).View
	if rv.Current != 0 || rv.Turn != turn {
		t.Fatalf("replayed position differs: current=%d turn=%d want turn %d", rv.Current, rv.Turn, turn)
	}
	cached := decode[HintPayload](t, waitEnv(t, owner2, 2*time.Second, func(e Envelope) bool {
		return e.Type == MsgHint
	}))
	if cached.Turn != hint.Turn || cached.Analysis == nil || len(cached.Analysis.Candidates) == 0 {
		t.Fatalf("cached hint was not restored: got %+v want turn %d", cached, hint.Turn)
	}
}

func boolPtr(b bool) *bool { return &b }

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
