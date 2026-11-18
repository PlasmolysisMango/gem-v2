package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"gem/internal/ai"
	"gem/internal/auth"
	"gem/internal/engine"
	"gem/internal/game"
	"gem/internal/replay"
	"gem/internal/store"
)

func newTestServer(t *testing.T) (*httptest.Server, *game.Hub, *store.Store) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	cfg := game.DefaultConfig()
	cfg.AIDelay = 0
	cfg.HardBudget = 50 * time.Millisecond
	cfg.MasterBudget = 50 * time.Millisecond
	cfg.HintBudget = 60 * time.Millisecond
	hub := game.NewHub(st, cfg)
	a := auth.New(st, []byte("test-secret"))
	a.Cost = 4
	srv := httptest.NewServer((&Server{Auth: a, Store: st, Hub: hub}).Handler())
	t.Cleanup(func() {
		srv.Close()
		hub.Close()
		st.Close()
	})
	return srv, hub, st
}

func doJSON(t *testing.T, method, url, token string, body, out any) int {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, url, rd)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		body, _ := io.ReadAll(resp.Body)
		t.Logf("server error: %s", body)
		return resp.StatusCode
	}
	if out != nil {
		json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func TestAuthEndpoints(t *testing.T) {
	srv, _, _ := newTestServer(t)
	var ar AuthResponse
	if c := doJSON(t, "POST", srv.URL+"/api/auth/register", "", credentials{"alice", "secret1"}, &ar); c != 201 || ar.Token == "" {
		t.Fatalf("register: %d", c)
	}
	if c := doJSON(t, "POST", srv.URL+"/api/auth/register", "", credentials{"Alice", "secret1"}, nil); c != 409 {
		t.Fatalf("duplicate register (case-insensitive) should be 409, got %d", c)
	}
	if c := doJSON(t, "POST", srv.URL+"/api/auth/register", "", credentials{"ab", "secret1"}, nil); c != 400 {
		t.Fatalf("short username should be 400, got %d", c)
	}
	if c := doJSON(t, "POST", srv.URL+"/api/auth/register", "", credentials{"bobby", "123"}, nil); c != 400 {
		t.Fatalf("short password should be 400, got %d", c)
	}
	if c := doJSON(t, "POST", srv.URL+"/api/auth/login", "", credentials{"alice", "wrong!!"}, nil); c != 401 {
		t.Fatalf("wrong password should be 401, got %d", c)
	}
	var lr AuthResponse
	if c := doJSON(t, "POST", srv.URL+"/api/auth/login", "", credentials{"alice", "secret1"}, &lr); c != 200 {
		t.Fatalf("login: %d", c)
	}
	var me store.User
	if c := doJSON(t, "GET", srv.URL+"/api/me", lr.Token, nil, &me); c != 200 || me.Username != "alice" {
		t.Fatalf("me: %d %+v", c, me)
	}
	if c := doJSON(t, "GET", srv.URL+"/api/me", "bogus", nil, nil); c != 401 {
		t.Fatalf("bad token should be 401, got %d", c)
	}
	if c := doJSON(t, "GET", srv.URL+"/api/rooms", "", nil, nil); c != 401 {
		t.Fatalf("rooms without token should be 401, got %d", c)
	}
}

func TestReplayAnalysisWithoutLiveHints(t *testing.T) {
	srv, _, st := newTestServer(t)
	var login AuthResponse
	doJSON(t, "POST", srv.URL+"/api/auth/register", "", credentials{"reviewer", "secret1"}, &login)
	ctx := context.Background()
	const seed = 33
	gid, err := st.CreateGame(ctx, "REPLAY", seed, []store.GamePlayer{{Seat: 0, UserID: &login.User.ID}, {Seat: 1, AILevel: "medium"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	url := fmt.Sprintf("%s/api/games/%d", srv.URL, gid)
	if code := doJSON(t, "GET", url, login.Token, nil, nil); code != 409 {
		t.Fatalf("ongoing replay: %d", code)
	}
	if code := doJSON(t, "POST", url+"/analysis/0", login.Token, nil, nil); code != 409 {
		t.Fatalf("ongoing analysis: %d", code)
	}
	state, _ := engine.NewGame(2, seed)
	bot := ai.NewMedium()
	for seq := 0; !state.Over() && seq < 500; seq++ {
		seat := int(state.Current)
		masked := state.MaskFor(seat)
		a, _ := bot.Choose(ctx, &masked, seat)
		b, _ := json.Marshal(a)
		if err := st.AddAction(ctx, gid, seq, seat, string(b)); err != nil {
			t.Fatal(err)
		}
		if _, err := state.Apply(a); err != nil {
			t.Fatal(err)
		}
	}
	if !state.Over() {
		t.Fatal("fixture game did not finish")
	}
	if err := st.FinishGame(ctx, gid, "finished", []int{int(state.Players[0].Points), int(state.Players[1].Points)}, nil); err != nil {
		t.Fatal(err)
	}
	var before replay.Replay
	if code := doJSON(t, "GET", url, login.Token, nil, &before); code != 200 {
		t.Fatalf("replay: %d", code)
	}
	mainCount := 0
	for _, frame := range before.Frames {
		if frame.Review != nil || frame.Analysis != nil {
			t.Fatal("unhinted game already has analysis")
		}
		if !frame.Main {
			if code := doJSON(t, "POST", fmt.Sprintf("%s/analysis/%d", url, frame.Seq), login.Token, nil, nil); code != 400 {
				t.Fatalf("subphase analysis: %d", code)
			}
			continue
		}
		mainCount++
		var analyzed replay.Frame
		if code := doJSON(t, "POST", fmt.Sprintf("%s/analysis/%d", url, frame.Seq), login.Token, nil, &analyzed); code != 200 {
			t.Fatalf("analyze seq %d: %d", frame.Seq, code)
		}
		if analyzed.Analysis == nil || analyzed.Analysis.Level != ai.Master || analyzed.Review == nil || analyzed.Analysis.Seat != frame.Seat || analyzed.Turn != frame.Turn {
			t.Fatalf("incomplete analysis: %+v", analyzed)
		}
		if mainCount == 1 {
			var cached replay.Frame
			doJSON(t, "POST", fmt.Sprintf("%s/analysis/%d", url, frame.Seq), login.Token, nil, &cached)
			x, _ := json.Marshal(analyzed)
			y, _ := json.Marshal(cached)
			if string(x) != string(y) {
				t.Fatal("cached analysis was recalculated")
			}
		}
	}
	var after replay.Replay
	doJSON(t, "GET", url, login.Token, nil, &after)
	for _, frame := range after.Frames {
		if frame.Main && (frame.Analysis == nil || frame.Review == nil) {
			t.Fatalf("analysis not persisted for seq %d", frame.Seq)
		}
	}
	curves, err := st.CurveByGame(ctx, gid)
	if err != nil || len(curves) != 2 || len(curves[0])+len(curves[1]) != mainCount {
		t.Fatalf("curves: %v %+v", err, curves)
	}
	if code := doJSON(t, "POST", url+"/analysis/999999", login.Token, nil, nil); code != 400 {
		t.Fatalf("invalid seq: %d", code)
	}
}

// wsClient is a minimal scripted player.
type wsClient struct {
	t    *testing.T
	name string
	c    *websocket.Conn
	room string
	seat int
	// observations
	gameID   int64
	hint     *HintPayload
	reviews  int
	over     *GameOverPayload
	moves    int
	wantHint bool
}

func dial(t *testing.T, ctx context.Context, srv *httptest.Server, token string) *websocket.Conn {
	u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?token=" + token
	c, _, err := websocket.Dial(ctx, u, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.SetReadLimit(1 << 22)
	return c
}

func (w *wsClient) send(ctx context.Context, typ string, payload any) {
	if err := wsjson.Write(ctx, w.c, Marshal(typ, w.room, payload)); err != nil {
		w.t.Errorf("%s write: %v", w.name, err)
	}
}

// pick chooses a reasonable move: the best-scoring purchase, else 3 different tokens, else anything legal.
func pick(legal []engine.Action) engine.Action {
	best, bestScore := legal[0], -1
	for _, a := range legal {
		score := 0
		switch a.Type {
		case engine.ActBuy, engine.ActBuyReserved:
			score = 100 + int(engine.Cards[a.Card].Points)
		case engine.ActTakeDiff:
			score = 10 + int(a.Gems.Total())
		case engine.ActTakeSame:
			score = 9
		case engine.ActReserve:
			score = 5
		}
		if score > bestScore {
			best, bestScore = a, score
		}
	}
	return best
}

func (w *wsClient) run(ctx context.Context, done chan<- error) {
	for {
		var e Envelope
		if err := wsjson.Read(ctx, w.c, &e); err != nil {
			done <- fmt.Errorf("%s read: %w", w.name, err)
			return
		}
		switch e.Type {
		case game.MsgGameView:
			var p GameViewPayload
			json.Unmarshal(e.Payload, &p)
			w.gameID = p.GameID
			v := p.View
			for _, pl := range v.Players {
				if pl.Seat != v.Viewer {
					for _, rc := range pl.Reserved {
						if rc.Hidden && rc.ID != -1 {
							done <- fmt.Errorf("hidden card leaked")
							return
						}
					}
				}
			}
			if v.Current != v.Viewer || len(v.Legal) == 0 {
				continue
			}
			if w.wantHint && w.hint == nil && v.Phase == engine.PhaseMain {
				w.send(ctx, game.MsgRequestHint, nil)
				continue
			}
			w.moves++
			w.send(ctx, game.MsgAction, ActionPayload{Action: pick(v.Legal)})
		case game.MsgHint:
			var p HintPayload
			json.Unmarshal(e.Payload, &p)
			w.hint = &p
			if len(p.Analysis.Candidates) == 0 || len(p.Analysis.Candidates[0].Reasons) == 0 {
				done <- fmt.Errorf("hint without candidates or reasons")
				return
			}
			w.moves++
			w.send(ctx, game.MsgAction, ActionPayload{Action: p.Analysis.Candidates[0].Action})
		case game.MsgMoveReview:
			w.reviews++
		case game.MsgError:
			var p ErrorPayload
			json.Unmarshal(e.Payload, &p)
			done <- fmt.Errorf("%s got error: %s", w.name, p.Message)
			return
		case game.MsgGameOver:
			var p GameOverPayload
			json.Unmarshal(e.Payload, &p)
			w.over = &p
			done <- nil
			return
		}
	}
}

func waitRoom(t *testing.T, ctx context.Context, c *websocket.Conn, ok func(RoomState) bool) RoomState {
	t.Helper()
	for {
		var e Envelope
		if err := wsjson.Read(ctx, c, &e); err != nil {
			t.Fatal(err)
		}
		if e.Type == game.MsgError {
			t.Fatalf("error: %s", e.Payload)
		}
		if e.Type == game.MsgRoomState {
			var rs RoomState
			json.Unmarshal(e.Payload, &rs)
			if ok(rs) {
				return rs
			}
		}
	}
}

// TestFullGameTwoHumansTwoAIs plays a complete 4-player game: 2 humans over WebSocket + 2 AIs.
func TestFullGameTwoHumansTwoAIs(t *testing.T) {
	srv, _, _ := newTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	var alice, bob AuthResponse
	doJSON(t, "POST", srv.URL+"/api/auth/register", "", credentials{"alice", "secret1"}, &alice)
	doJSON(t, "POST", srv.URL+"/api/auth/register", "", credentials{"bob_2", "secret2"}, &bob)

	hints := true
	var room RoomSummary
	if c := doJSON(t, "POST", srv.URL+"/api/rooms", alice.Token, RoomSettings{Name: "测试房", Seats: 4, AIs: []string{"easy", "medium"}, Hints: &hints}, &room); c != 201 {
		t.Fatalf("create room: %d", c)
	}
	var rooms []RoomSummary
	doJSON(t, "GET", srv.URL+"/api/rooms", bob.Token, nil, &rooms)
	if len(rooms) != 1 || rooms[0].Open != 1 || rooms[0].AIs != 2 {
		t.Fatalf("lobby: %+v", rooms)
	}

	a := &wsClient{t: t, name: "alice", c: dial(t, ctx, srv, alice.Token), room: room.ID, wantHint: true}
	b := &wsClient{t: t, name: "bob", c: dial(t, ctx, srv, bob.Token), room: room.ID}
	a.send(ctx, game.MsgJoinRoom, nil)
	waitRoom(t, ctx, a.c, func(rs RoomState) bool { return rs.YourSeat == 0 })
	b.send(ctx, game.MsgJoinRoom, nil)
	rs := waitRoom(t, ctx, b.c, func(rs RoomState) bool { return rs.YourSeat >= 0 })
	if rs.YourSeat != 3 || rs.Seats[1].Kind != game.SeatAI {
		t.Fatalf("unexpected seating: %+v", rs)
	}
	// Non-owner cannot start.
	b.send(ctx, game.MsgStart, nil)
	for {
		var e Envelope
		wsjson.Read(ctx, b.c, &e)
		if e.Type == game.MsgError {
			break
		}
	}
	waitRoom(t, ctx, a.c, func(rs RoomState) bool { return rs.Seats[3].Connected })
	a.send(ctx, game.MsgStart, nil)

	done := make(chan error, 2)
	go a.run(ctx, done)
	go b.run(ctx, done)
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if a.hint == nil {
		t.Fatal("alice never received a hint")
	}
	if a.over.GameID == 0 || len(a.over.Points) != 4 || len(a.over.Winners) == 0 {
		t.Fatalf("bad game over: %+v", a.over)
	}
	t.Logf("game %d finished: points %v winners %v, alice moves %d (reviews %d), bob moves %d", a.over.GameID, a.over.Points, a.over.Winners, a.moves, len(a.over.Reviews), b.moves)

	// The stored record replays to the same result.
	var rec replay.Replay
	if c := doJSON(t, "GET", fmt.Sprintf("%s/api/games/%d", srv.URL, a.over.GameID), bob.Token, nil, &rec); c != 200 {
		t.Fatalf("game record: %d", c)
	}
	if rec.Status != "finished" || len(rec.Players) != 4 || rec.Players[1].AILevel != "easy" || len(rec.Frames) == 0 {
		t.Fatalf("record: %+v", rec.Game)
	}
	for _, frame := range rec.Frames {
		if len(frame.Events) == 0 || frame.Events[0].Kind != "action" || frame.Events[0].Seat != frame.Seat {
			t.Fatalf("回放缺少结构化行动事件：%+v", frame)
		}
		for _, event := range frame.Events {
			if event.Kind == "action" && (event.Action.Type == engine.ActBuy || event.Action.Type == engine.ActBuyReserved) {
				if event.Card != event.Action.Card || event.Card < 0 {
					t.Fatalf("购买事件缺少卡牌信息：%+v", event)
				}
			}
		}
	}
	last := rec.Frames[len(rec.Frames)-1].View
	if last.Phase != engine.PhaseOver {
		t.Fatalf("replay did not reach the end (%d frames)", len(rec.Frames))
	}
	for i, p := range a.over.Points {
		if last.Players[i].Points != p || *rec.Players[i].FinalPoints != p {
			t.Fatalf("replay mismatch seat %d", i)
		}
	}
	var st store.Stats
	doJSON(t, "GET", fmt.Sprintf("%s/api/users/%d/stats", srv.URL, alice.User.ID), bob.Token, nil, &st)
	if st.Games != 1 || len(st.Recent) != 1 {
		t.Fatalf("stats: %+v", st)
	}
}

func TestAdminEndpoints(t *testing.T) {
	srv, hub, st := newTestServer(t)
	var alice AuthResponse
	doJSON(t, "POST", srv.URL+"/api/auth/register", "", credentials{"alice", "secret1"}, &alice)
	if c := doJSON(t, "GET", srv.URL+"/api/admin/settings", alice.Token, nil, nil); c != 403 {
		t.Fatalf("non-admin should get 403, got %d", c)
	}
	if ok, err := st.PromoteAdmin(context.Background(), "alice"); err != nil || !ok {
		t.Fatalf("promote: %v %v", ok, err)
	}
	var cfg AdminSettings
	if c := doJSON(t, "GET", srv.URL+"/api/admin/settings", alice.Token, nil, &cfg); c != 200 ||
		cfg.IdleCloseSeconds != int64(game.DefaultConfig().IdleClose/time.Second) {
		t.Fatalf("settings: %d %+v", c, cfg)
	}
	if c := doJSON(t, "PUT", srv.URL+"/api/admin/settings", alice.Token, AdminSettings{IdleCloseSeconds: 0}, &cfg); c != 200 || cfg.IdleCloseSeconds != 0 {
		t.Fatalf("disable cleanup: %d %+v", c, cfg)
	}
	if hub.IdleClose() != 0 {
		t.Fatalf("hub idle close = %s", hub.IdleClose())
	}
	if c := doJSON(t, "PUT", srv.URL+"/api/admin/settings", alice.Token, AdminSettings{IdleCloseSeconds: -1}, nil); c != 400 {
		t.Fatalf("negative idle close should be 400, got %d", c)
	}

	var room RoomSummary
	doJSON(t, "POST", srv.URL+"/api/rooms", alice.Token, RoomSettings{AIs: []string{"easy"}}, &room)
	if c := doJSON(t, "DELETE", srv.URL+"/api/admin/rooms/"+room.ID, alice.Token, nil, nil); c != 204 {
		t.Fatalf("close room: %d", c)
	}
	for i := 0; i < 200 && hub.Get(room.ID) != nil; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if hub.Get(room.ID) != nil {
		t.Fatal("room was not closed")
	}
	if c := doJSON(t, "DELETE", srv.URL+"/api/admin/rooms/"+room.ID, alice.Token, nil, nil); c != 404 {
		t.Fatalf("closing a missing room should be 404, got %d", c)
	}
}

func TestReconnectKeepsSeat(t *testing.T) {
	srv, hub, _ := newTestServer(t)
	_ = hub
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var alice AuthResponse
	doJSON(t, "POST", srv.URL+"/api/auth/register", "", credentials{"alice", "secret1"}, &alice)
	var room RoomSummary
	doJSON(t, "POST", srv.URL+"/api/rooms", alice.Token, RoomSettings{AIs: []string{"medium"}}, &room)
	c := dial(t, ctx, srv, alice.Token)
	wsjson.Write(ctx, c, Marshal(game.MsgJoinRoom, room.ID, nil))
	waitRoom(t, ctx, c, func(rs RoomState) bool { return rs.YourSeat == 0 })
	wsjson.Write(ctx, c, Marshal(game.MsgStart, room.ID, nil))
	waitRoom(t, ctx, c, func(rs RoomState) bool { return rs.Status == "playing" })
	// Play one move so there is history to restore.
	for moved := false; ; {
		var e Envelope
		if err := wsjson.Read(ctx, c, &e); err != nil {
			t.Fatal(err)
		}
		if e.Type == game.MsgGameView && !moved {
			var p GameViewPayload
			json.Unmarshal(e.Payload, &p)
			if p.View.Current == p.View.Viewer && len(p.View.Legal) > 0 {
				wsjson.Write(ctx, c, Marshal(game.MsgAction, room.ID, ActionPayload{Action: pick(p.View.Legal)}))
				moved = true
			}
		}
		if e.Type == game.MsgEvents && moved {
			break
		}
	}
	c.Close(websocket.StatusNormalClosure, "")

	c2 := dial(t, ctx, srv, alice.Token)
	wsjson.Write(ctx, c2, Marshal(game.MsgJoinRoom, room.ID, nil))
	rs := waitRoom(t, ctx, c2, func(rs RoomState) bool { return true })
	if rs.YourSeat != 0 || rs.Status != "playing" || !rs.Seats[0].Connected {
		t.Fatalf("reconnect should restore seat: %+v", rs)
	}
	gotView := false
	for {
		var e Envelope
		if err := wsjson.Read(ctx, c2, &e); err != nil {
			t.Fatal(err)
		}
		if e.Type == game.MsgGameView {
			gotView = true
		}
		if e.Type == game.MsgEvents {
			// The first events message after rejoining replays the history so "last move" survives a reload.
			var p game.EventsPayload
			json.Unmarshal(e.Payload, &p)
			mine := false
			for _, ev := range p.Events {
				mine = mine || strings.HasPrefix(ev.Text, "alice ")
			}
			if !gotView || !p.Replace || !mine {
				t.Fatalf("expected replayed history after the view, got view=%v %+v", gotView, p)
			}
			return
		}
	}
}
