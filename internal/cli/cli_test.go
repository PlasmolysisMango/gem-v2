package cli

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"gem/internal/ai"
	"gem/internal/api"
	"gem/internal/auth"
	"gem/internal/engine"
	"gem/internal/game"
	"gem/internal/store"
)

type fakeBackend struct {
	acts  []engine.Action
	hints int
	start int
}

func (f *fakeBackend) Start(func(tea.Msg)) {}
func (f *fakeBackend) Act(a engine.Action) { f.acts = append(f.acts, a) }
func (f *fakeBackend) Hint()               { f.hints++ }
func (f *fakeBackend) StartGame()          { f.start++ }
func (f *fakeBackend) Close()              {}
func runes(s string) tea.KeyMsg            { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
func press(m *Model, keys ...tea.KeyMsg) {
	for _, k := range keys {
		m.Update(k)
	}
}
func enter() tea.KeyMsg                   { return tea.KeyMsg{Type: tea.KeyEnter} }
func arrow(t tea.KeyType) tea.KeyMsg      { return tea.KeyMsg{Type: t} }
func last(f *fakeBackend) engine.Action   { return f.acts[len(f.acts)-1] }
func gems(w, b, g, r, k int8) engine.Gems { return engine.Gems{w, b, g, r, k, 0} }

func TestModelKeysBuildLegalActions(t *testing.T) {
	g, err := engine.NewGame(2, 7)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeBackend{}
	m := NewModel(f)
	m.Update(RoomMsg{State: game.RoomState{ID: "T", Name: "测试", Status: "playing", Hints: true, YourSeat: 0,
		Seats: []game.SeatInfo{{Index: 0, Kind: game.SeatHuman, Name: "me"}, {Index: 1, Kind: game.SeatAI, Name: "bot", Level: "hard"}}}})
	m.Update(ViewMsg{View: g.ViewFor(0)})

	press(m, runes("1"), runes("2"), runes("3"), enter())
	if len(f.acts) != 1 || last(f).Type != engine.ActTakeDiff || last(f).Gems != gems(1, 1, 1, 0, 0) {
		t.Fatalf("take three: %+v", f.acts)
	}
	press(m, runes("4"), runes("4"), enter())
	if len(f.acts) != 2 || last(f).Type != engine.ActTakeSame || last(f).Gems != gems(0, 0, 0, 2, 0) {
		t.Fatalf("take two: %+v", f.acts)
	}
	// Fast typing arrives as a single multi-rune key message.
	press(m, runes("345"), enter())
	if len(f.acts) != 3 || last(f).Gems != gems(0, 0, 1, 1, 1) {
		t.Fatalf("multi-rune take: %+v", f.acts)
	}
	f.acts = f.acts[:2]
	// Two different colors are illegal while three colors are available.
	press(m, runes("1"), runes("2"), enter())
	if len(f.acts) != 2 || m.err == "" {
		t.Fatalf("expected rejection, acts=%d err=%q", len(f.acts), m.err)
	}
	press(m, arrow(tea.KeyEsc))
	// Bank row + space selects the color under the cursor.
	press(m, arrow(tea.KeyDown), runes(" "), arrow(tea.KeyRight), runes(" "), arrow(tea.KeyRight), runes(" "), enter())
	if len(f.acts) != 3 || last(f).Gems != gems(0, 1, 1, 1, 0) {
		t.Fatalf("bank cursor take: row=%d col=%d %+v", m.row, m.col, f.acts)
	}
	// Reserve the first L1 card, then the L1 deck.
	m.row, m.col = rowL1, 1
	press(m, runes("r"))
	if last(f).Type != engine.ActReserve || last(f).Card != g.Board[0][0] {
		t.Fatalf("reserve: %+v", last(f))
	}
	press(m, arrow(tea.KeyLeft), runes("r"))
	if last(f).Type != engine.ActReserveDeck || last(f).Level != 1 {
		t.Fatalf("reserve deck: %+v", last(f))
	}
	// Nothing is affordable at the start.
	m.row, m.col = rowL3, 2
	n := len(f.acts)
	press(m, runes("b"))
	if len(f.acts) != n || !strings.Contains(m.err, "买不起") {
		t.Fatalf("buy should fail: %q", m.err)
	}

	// Hints: request, receive for the current position, execute with y.
	v := g.ViewFor(0)
	press(m, runes("h"))
	if f.hints != 1 || m.hintReq == nil {
		t.Fatal("hint not requested")
	}
	m.Update(HintMsg{Analysis: &ai.Analysis{StrategyText: "引擎", WinRate: 0.6,
		Candidates: []ai.Candidate{{Action: v.Legal[0], Text: engine.Describe(v.Legal[0]), WinRate: 0.6,
			Reasons: []ai.Reason{{Tag: "x", Text: "原因示例"}}}}}})
	out := m.View()
	for _, want := range []string{"轮到你了", "原因示例", "60.0%", "L3 牌堆"} {
		if !strings.Contains(out, want) {
			t.Fatalf("view missing %q", want)
		}
	}
	press(m, runes("y"))
	if last(f) != v.Legal[0] {
		t.Fatalf("hint move: %+v", last(f))
	}

	// A stale hint (position changed) is dropped.
	press(m, runes("h"))
	g.Step(v.Legal[0])
	m.Update(ViewMsg{View: g.ViewFor(0)})
	m.Update(HintMsg{Analysis: &ai.Analysis{Candidates: []ai.Candidate{{Action: v.Legal[0]}}}})
	if m.currentHint() != nil {
		t.Fatal("stale hint shown")
	}
	n = len(f.acts)
	press(m, runes("1"), runes("2"), runes("3"), enter())
	if len(f.acts) != n || !strings.Contains(m.err, "还没轮到你") {
		t.Fatalf("acted out of turn: %q", m.err)
	}
}

func TestModelDiscardAndNoble(t *testing.T) {
	f := &fakeBackend{}
	m := NewModel(f)
	v := engine.View{Viewer: 0, NPlayers: 2, Current: 0, Phase: engine.PhaseDiscard,
		Players: []engine.PlayerView{{Seat: 0, Tokens: engine.Gems{3, 3, 3, 2, 0, 0}}, {Seat: 1}},
		Legal:   []engine.Action{{Type: engine.ActDiscard, Gems: engine.Gems{1, 0, 0, 0, 0, 0}}, {Type: engine.ActDiscard, Gems: engine.Gems{0, 0, 0, 1, 0, 0}}}}
	m.Update(ViewMsg{View: v})
	press(m, runes("4"), enter())
	if len(f.acts) != 1 || last(f).Gems[engine.Red] != 1 {
		t.Fatalf("discard: %+v", f.acts)
	}
	v.Phase = engine.PhaseNoble
	v.Legal = []engine.Action{{Type: engine.ActNoble, Noble: 3}, {Type: engine.ActNoble, Noble: 5}}
	m.Update(ViewMsg{View: v})
	if !strings.Contains(m.View(), "请选择") {
		t.Fatal("noble picker not shown")
	}
	press(m, runes("2"))
	if last(f).Type != engine.ActNoble || last(f).Noble != 5 {
		t.Fatalf("noble: %+v", last(f))
	}
}

// drive plays seat legal[0] (with one hint request) until the game ends.
func drive(t *testing.T, be Backend, ch chan tea.Msg, wantHint bool) game.GameOverPayload {
	t.Helper()
	m := NewModel(be)
	acted := map[posKey]bool{}
	hinted, gotHint, started := false, false, false
	timeout := time.After(90 * time.Second)
	for {
		select {
		case msg := <-ch:
			m.Update(msg)
			switch msg := msg.(type) {
			case RoomMsg:
				if msg.State.Status == "waiting" && !started {
					started = true
					be.StartGame()
				}
			case ViewMsg:
				m.View()
				v := msg.View
				if v.Current != v.Viewer || len(v.Legal) == 0 || acted[keyOf(&v)] {
					continue
				}
				if wantHint && !hinted {
					hinted = true
					press(m, runes("h"))
					continue
				}
				acted[keyOf(&v)] = true
				be.Act(v.Legal[0])
			case HintMsg:
				if msg.Analysis == nil || len(msg.Analysis.Candidates) == 0 {
					t.Fatal("empty hint")
				}
				gotHint = true
				if m.currentHint() == nil {
					t.Fatal("hint not attached to the current position")
				}
				k := keyOf(m.view)
				acted[k] = true
				press(m, runes("y"))
			case ErrMsg:
				t.Logf("server error: %s", msg.Err)
			case ClosedMsg:
				t.Fatalf("closed: %v", msg.Err)
			case OverMsg:
				if wantHint && !gotHint {
					t.Fatal("no hint received")
				}
				if !strings.Contains(m.View(), "对局结束") {
					t.Fatal("game over not rendered")
				}
				return msg.Over
			}
		case <-timeout:
			t.Fatal("game did not finish")
		}
	}
}

func TestOfflineGame(t *testing.T) {
	o, err := NewOffline([]ai.Level{ai.Easy, ai.Medium}, 42, 30*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	o.Delay = 0
	ch := make(chan tea.Msg, 4096)
	o.Start(func(msg tea.Msg) { ch <- msg })
	over := drive(t, o, ch, true)
	o.Close()
	if len(over.Points) != 3 || len(over.Winners) == 0 {
		t.Fatalf("bad result %+v", over)
	}
	if len(over.Reviews) == 0 {
		t.Fatal("no move reviews")
	}
}

func TestOfflineNobleGame(t *testing.T) {
	o, err := NewOffline([]ai.Level{ai.Level("noble")}, 1842, 30*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	o.Delay, o.Review = 0, false
	t.Cleanup(o.Close)
	ch := make(chan tea.Msg, 4096)
	o.Start(func(msg tea.Msg) { ch <- msg })
	over := drive(t, o, ch, false)
	if len(over.Points) != 2 || len(over.Winners) == 0 {
		t.Fatalf("bad result %+v", over)
	}
}

func TestOnlineGame(t *testing.T) {
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
	srv := httptest.NewServer((&api.Server{Auth: a, Store: st, Hub: hub}).Handler())
	defer func() { srv.Close(); hub.Close(); st.Close() }()

	t.Setenv("GEM_CONFIG_DIR", t.TempDir())
	t.Setenv("GEM_SERVER", "")
	c := &Client{Server: srv.URL}
	if err := c.Login("cliuser", "secret1", true); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	c = LoadClient()
	if c.Server != srv.URL || c.Token == "" {
		t.Fatalf("config not persisted: %+v", c)
	}
	if u, err := c.Me(); err != nil || u.Username != "cliuser" {
		t.Fatalf("me: %v %+v", err, u)
	}
	if err := (&Client{Server: srv.URL}).Login("cliuser", "wrong-pw", false); err == nil {
		t.Fatal("wrong password accepted")
	}
	room, err := c.CreateRoom(game.RoomSettings{Seats: 2, AIs: []string{"master"}})
	if err != nil {
		t.Fatal(err)
	}
	rooms, err := c.Rooms()
	if err != nil || len(rooms) != 1 || rooms[0].ID != room.ID || !rooms[0].Hints {
		t.Fatalf("rooms: %v %+v", err, rooms)
	}
	be, err := c.Dial(context.Background(), room.ID)
	if err != nil {
		t.Fatal(err)
	}
	ch := make(chan tea.Msg, 4096)
	be.Start(func(msg tea.Msg) { ch <- msg })
	over := drive(t, be, ch, true)
	be.Close()
	if len(over.Points) != 2 || over.GameID == 0 {
		t.Fatalf("bad result %+v", over)
	}
}

// TestViewFitsTerminal guards the multi-column layout: a long hint must not push
// the view past the terminal height, or Bubble Tea drops the top rows (nobles).
func TestViewFitsTerminal(t *testing.T) {
	g, err := engine.NewGame(4, 7)
	if err != nil {
		t.Fatal(err)
	}
	m := NewModel(&fakeBackend{})
	m.Update(RoomMsg{State: game.RoomState{ID: "T", Name: "终端练习", Status: "playing", Hints: true, YourSeat: 0,
		Seats: []game.SeatInfo{
			{Index: 0, Kind: game.SeatHuman, Name: "me"},
			{Index: 1, Kind: game.SeatAI, Name: "bot1", Level: "hard"},
			{Index: 2, Kind: game.SeatAI, Name: "bot2", Level: "hard"},
			{Index: 3, Kind: game.SeatAI, Name: "bot3", Level: "master"},
		}}})
	v := g.ViewFor(0)
	m.Update(ViewMsg{View: v})
	for i := 0; i < 6; i++ {
		m.Update(LogMsg{Lines: []string{"某玩家拿取 蓝绿红"}})
	}
	press(m, runes("h"))
	m.Update(HintMsg{Analysis: &ai.Analysis{StrategyText: "快速上分：优先高等级低费卡", WinRate: 0.6, Iterations: 900,
		Candidates: []ai.Candidate{
			{Action: v.Legal[0], Text: "购买 L2 蓝卡「很长的卡牌名称」", WinRate: 0.6,
				Reasons: []ai.Reason{{Text: "提供 2 点蓝奖励，直接推进贵族条件"}, {Text: "费用比最高"}},
				PV:      []ai.PVStep{{}, {Seat: 1, Text: "拿取 蓝蓝绿"}, {Seat: 0, Text: "购买 L1 白卡"}}},
			{Action: v.Legal[0], Text: "拿取 蓝绿红", WinRate: 0.5, Reasons: []ai.Reason{{Text: "囤积三色筹码"}}},
		}}})

	for _, sz := range [][2]int{{160, 45}, {140, 40}, {120, 40}, {109, 35}, {104, 32}, {100, 30}, {80, 24}} {
		m.width, m.height = sz[0], sz[1]
		out := m.View()
		lines := strings.Split(out, "\n")
		if len(lines) > sz[1] {
			t.Fatalf("%dx%d: view has %d lines", sz[0], sz[1], len(lines))
		}
		for _, line := range lines {
			if w := lipgloss.Width(line); w > sz[0] {
				t.Fatalf("%dx%d: line is %d cells wide", sz[0], sz[1], w)
			}
		}
		if !strings.Contains(out, "贵族") {
			t.Fatalf("%dx%d: nobles row missing", sz[0], sz[1])
		}
		if sz[0] >= 120 && !strings.Contains(out, "最近动作") {
			t.Fatalf("%dx%d: recent actions missing", sz[0], sz[1])
		}
		if sz[0] >= 120 && !strings.Contains(out, "预留 · ") {
			t.Fatalf("%dx%d: reserved panel missing", sz[0], sz[1])
		}
	}
	m.width, m.height = 120, 40
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if out := m.View(); !strings.Contains(out, "预留 · bot1") {
		t.Fatalf("Tab did not switch the reserved panel: %s", out)
	}
}
