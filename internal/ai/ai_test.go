package ai

import (
	"context"
	"math"
	"math/rand/v2"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"gem/internal/engine"
)

func playOut(t *testing.T, agents []Agent, seed uint64) engine.GameState {
	t.Helper()
	s, _ := engine.NewGame(len(agents), seed)
	for steps := 0; !s.Over(); steps++ {
		if steps > 1500 {
			t.Fatalf("game did not finish")
		}
		seat := int(s.Current)
		m := s.MaskFor(seat)
		a, _ := agents[seat].Choose(context.Background(), &m, seat)
		if _, err := s.Apply(a); err != nil {
			t.Fatalf("%s produced illegal move %s: %v", agents[seat].Level(), engine.Describe(a), err)
		}
	}
	return s
}

func fastHard() *HardAgent {
	h := NewMaster(time.Second)
	h.MaxIter = 60
	h.Workers = 2
	return h
}

func TestAgentsPlayLegalGames(t *testing.T) {
	for n := 2; n <= 4; n++ {
		h := NewHard(time.Second)
		h.MaxIter = 60
		agents := []Agent{NewEasy(), NewMedium(), h, fastHard()}[:n]
		s := playOut(t, agents, uint64(n))
		if s.Winners == 0 {
			t.Fatal("no winner")
		}
	}
}

func TestLevels(t *testing.T) {
	for _, tc := range []struct {
		level Level
		name  string
	}{
		{Easy, "简单"}, {Medium, "中等"}, {Hard, "困难"}, {Master, "大师"},
	} {
		got, ok := ParseLevel(string(tc.level))
		if !ok || got != tc.level || got.Name() != tc.name || New(got).Level() != tc.level {
			t.Fatalf("level %q: got=%q ok=%v name=%q agent=%q", tc.level, got, ok, got.Name(), New(got).Level())
		}
	}
}

func TestSearchLevelSettings(t *testing.T) {
	hard := NewHard(300 * time.Millisecond)
	master := NewMaster(1500 * time.Millisecond)
	if hard.Level() != Hard || hard.Workers != 1 || !hard.E.searchTargets {
		t.Fatalf("hard settings: level=%s workers=%d searchTargets=%v", hard.Level(), hard.Workers, hard.E.searchTargets)
	}
	if master.Level() != Master || master.Workers < 1 || !master.E.searchTargets {
		t.Fatalf("master settings: level=%s workers=%d searchTargets=%v", master.Level(), master.Workers, master.E.searchTargets)
	}
}

func TestMediumBeatsEasy(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	wins := 0
	games := 30
	for g := 0; g < games; g++ {
		agents := []Agent{NewMedium(), NewEasy()}
		ms := 0
		if g%2 == 1 {
			agents[0], agents[1] = agents[1], agents[0]
			ms = 1
		}
		s := playOut(t, agents, uint64(100+g))
		if s.IsWinner(ms) {
			wins++
		}
	}
	if wins < games*2/3 {
		t.Fatalf("medium won only %d/%d vs easy", wins, games)
	}
}

func TestTurnsToAfford(t *testing.T) {
	var p engine.PlayerState
	bank := engine.Gems{4, 4, 4, 4, 4, 5}
	c := &engine.Card{Cost: engine.Cost{3, 0, 0, 0, 0}}
	if got := turnsToAfford(&p, c, &bank); got != 2 {
		t.Fatalf("3 white needs 2 turns, got %d", got)
	}
	p.Tokens[engine.Gold] = 1
	if got := turnsToAfford(&p, c, &bank); got != 1 {
		t.Fatalf("with gold needs 1 turn, got %d", got)
	}
	p.Bonuses[engine.White] = 3
	if got := turnsToAfford(&p, c, &bank); got != 0 {
		t.Fatalf("covered by bonuses, got %d", got)
	}
	big := &engine.Card{Cost: engine.Cost{7, 7, 0, 0, 0}}
	var q engine.PlayerState
	if got := turnsToAfford(&q, big, &bank); got != Unreachable {
		t.Fatalf("14 tokens should be unreachable, got %d", got)
	}
}

// threatState: player 2 can buy a 5-point card to reach 15 next turn.
func threatState(t *testing.T) (engine.GameState, int16) {
	s, _ := engine.NewGame(2, 11)
	var target int16 = -1
	for _, id := range engine.LevelCards(3) {
		if engine.Cards[id].Points == 5 {
			target = id
			break
		}
	}
	s.Board[2][0] = target
	c := engine.Cards[target]
	opp := &s.Players[1]
	opp.Points = 10
	for col := 0; col < engine.NumGemColors; col++ {
		opp.Bonuses[col] = c.Cost[col]
	}
	opp.NCards = 12
	// Player 1 is close to winning too, so blocking keeps real chances alive.
	me := &s.Players[0]
	me.Points = 12
	me.NCards = 10
	for col := 0; col < engine.NumGemColors; col++ {
		me.Bonuses[col] = 2
	}
	return s, target
}

func TestReasonsExplainBlock(t *testing.T) {
	s, target := threatState(t)
	m := s.MaskFor(0)
	e := NewEvaluator()
	if e.strategyOf(&m, 0) != StratBlock {
		t.Fatalf("expected block strategy, got %s", e.strategyOf(&m, 0))
	}
	rs := e.Reasons(&m, 0, engine.Action{Type: engine.ActReserve, Card: target})
	found := false
	for _, r := range rs {
		if r.Tag == "BlockOpponent" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected BlockOpponent reason, got %+v", rs)
	}
}

func TestHardBlocksWinningCard(t *testing.T) {
	s, target := threatState(t)
	m := s.MaskFor(0)
	h := NewHard(800 * time.Millisecond)
	an := h.Analyze(context.Background(), &m, 0, true)
	a := an.Candidates[0].Action
	if !(a.Type == engine.ActReserve && a.Card == target) {
		t.Fatalf("hard should reserve the winning card, chose %s (%+v)", engine.Describe(a), an.Candidates)
	}
	if len(an.Candidates[0].Reasons) == 0 || an.StrategyText == "" {
		t.Fatal("missing explanation")
	}
}

func TestReview(t *testing.T) {
	s, target := threatState(t)
	m := s.MaskFor(0)
	h := NewMaster(500 * time.Millisecond)
	an := h.Analyze(context.Background(), &m, 0, false)
	r := h.Review(context.Background(), &m, 0, an, an.Candidates[0].Action)
	if r.Verdict != "best" {
		t.Fatalf("same move should be best, got %s", r.Verdict)
	}
	// Ignoring the threat should be graded as a mistake.
	bad := engine.Action{Type: engine.ActReserveDeck, Level: 1, Card: engine.NoCard}
	r = h.Review(context.Background(), &m, 0, an, bad)
	if r.Verdict == "best" || r.Verdict == "good" {
		t.Fatalf("ignoring a winning threat on %s should be a mistake, got %s (delta %.2f)", engine.CardString(target), r.Verdict, r.Delta)
	}
}

func TestAnalyzePreCanceled(t *testing.T) {
	opening, _ := engine.NewGame(2, 21)
	endgame, _ := threatState(t)
	discard := opening
	discard.Phase = engine.PhaseDiscard
	discard.Players[0].Tokens = engine.Gems{4, 4, 3}
	for name, s := range map[string]engine.GameState{"opening": opening, "endgame": endgame, "discard": discard} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			h := NewMaster(time.Hour)
			h.SearchExpired = func() bool { panic("cancellation must precede budget checks") }
			m := s.MaskFor(0)
			start := time.Now()
			an := h.Analyze(ctx, &m, 0, true)
			if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
				t.Fatalf("pre-canceled analysis took %s", elapsed)
			}
			if an.Iterations != 0 || len(an.Candidates) != 0 {
				t.Fatalf("pre-canceled analysis searched: %+v", an)
			}
			// Choose must not index the empty canceled analysis.
			_, an = h.Choose(ctx, &m, 0)
			if len(an.Candidates) != 0 {
				t.Fatal("canceled Choose returned candidates")
			}
		})
	}
}

func TestReviewPreCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h := NewMaster(time.Hour)
	best := engine.Action{Type: engine.ActReserveDeck, Level: 1, Card: engine.NoCard}
	an := &Analysis{Candidates: []Candidate{{Action: best, WinRate: 0.5}}}
	for _, played := range []engine.Action{best, {Type: engine.ActPass, Card: engine.NoCard}} {
		start := time.Now()
		r := h.Review(ctx, nil, 0, an, played)
		if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
			t.Fatalf("pre-canceled review took %s", elapsed)
		}
		if r.Verdict != "" || r.Delta != 0 {
			t.Fatalf("canceled review was graded: %+v", r)
		}
	}
	if got := h.EstimateActions(ctx, nil, 0, []engine.Action{best}, 96); got != nil {
		t.Fatalf("pre-canceled estimates: %v", got)
	}
}

// Cancel on a known cancellation checkpoint, without timing-dependent races.
// The underlying context still supplies the usual cancellation signal.
type checkpointContext struct {
	context.Context
	checks   atomic.Int64
	cancelAt int64
	cancel   context.CancelFunc
}

func (c *checkpointContext) Err() error {
	if c.checks.Add(1) >= c.cancelAt {
		c.cancel()
	}
	return c.Context.Err()
}

func cancelAtCheckpoint(t *testing.T, at int64) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &checkpointContext{Context: ctx, cancelAt: at, cancel: cancel}
}

func TestCancellationDiscardsIncompleteResults(t *testing.T) {
	s, _ := engine.NewGame(2, 22)
	m := s.MaskFor(0)
	h := NewHard(time.Hour)
	acts := m.LegalActions(nil)[:2]
	an := &Analysis{Candidates: []Candidate{{Action: acts[0], Text: engine.Describe(acts[0])}}}
	t.Run("estimates", func(t *testing.T) {
		ctx := cancelAtCheckpoint(t, 20)
		if got := h.EstimateActions(ctx, &m, 0, acts, 96); got != nil {
			t.Fatalf("partial estimates returned: %v", got)
		}
	})
	t.Run("review", func(t *testing.T) {
		ctx := cancelAtCheckpoint(t, 20)
		if got := h.Review(ctx, &m, 0, an, acts[1]); got.Verdict != "" || got.Delta != 0 {
			t.Fatalf("partial review was graded: %+v", got)
		}
	})
	t.Run("paranoid", func(t *testing.T) {
		ctx := cancelAtCheckpoint(t, 20)
		if v, complete := h.paranoid(ctx, &s, 0, 3); complete || v != 0 {
			t.Fatalf("partial branch returned (%v, %v)", v, complete)
		}
	})
	t.Run("endgame", func(t *testing.T) {
		ctx := cancelAtCheckpoint(t, 100)
		if got := h.endgame(ctx, &m, 0, acts, time.Now().Add(time.Hour), newRNG()); got != nil {
			t.Fatalf("partial endgame verdicts: %v", got)
		}
	})
	t.Run("iteration", func(t *testing.T) {
		ctx := cancelAtCheckpoint(t, 3)
		wk := &worker{h: h, rng: newRNG()}
		root := &node{player: -1}
		state := s
		var values [4]float64
		if wk.iterate(ctx, root, &state, &values) || root.n != 0 {
			t.Fatal("incomplete rollout was backpropagated")
		}
	})
}

func TestAnalyzeCancellationDuringSearch(t *testing.T) {
	s, _ := engine.NewGame(4, 23)
	m := s.MaskFor(0)
	h := NewMaster(time.Hour)
	h.Workers, h.RolloutDepth, h.EndgameAt = 4, 100000, 0
	h.SearchExpired = func() bool { return false }
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	an := h.Analyze(ctx, &m, 0, true)
	if ctx.Err() == nil || len(an.Candidates) != 0 {
		t.Fatalf("canceled analysis returned candidates: %+v", an)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("cancellation did not stop all search workers promptly: %s", elapsed)
	}
}

func TestSearchExpiredHook(t *testing.T) {
	s, _ := engine.NewGame(2, 24)
	m := s.MaskFor(0)
	for _, tc := range []struct {
		name   string
		budget time.Duration
		hook   func() bool
		iters  int
	}{
		{"wall budget", -time.Second, nil, 16},
		{"hook replaces wall budget", -time.Second, func() bool { return false }, 48},
		{"hook expires before wall budget", time.Hour, func() bool { return true }, 16},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewMaster(tc.budget)
			h.Workers, h.MaxIter, h.EndgameAt = 2, 24, 100
			h.SearchExpired = tc.hook
			an := h.Analyze(context.Background(), &m, 0, false)
			if an.Iterations != tc.iters {
				t.Fatalf("got %d iterations, want %d", an.Iterations, tc.iters)
			}
			if len(an.Candidates) == 0 || m.Validate(an.Candidates[0].Action) != nil {
				t.Fatal("search budget expiry did not preserve a legal recommendation")
			}
		})
	}
}

func TestExpiredSearchStillReviews(t *testing.T) {
	s, target := threatState(t)
	m := s.MaskFor(0)
	h := NewHard(time.Nanosecond)
	h.SearchExpired = func() bool { return true }
	an := h.Analyze(context.Background(), &m, 0, false)
	if a := an.Candidates[0].Action; a.Type != engine.ActReserve || a.Card != target {
		t.Fatalf("expired search failed to block winning card: %s", engine.Describe(a))
	}
	// Analyze must have waited for every goroutine before the hook is replaced.
	// Review must not consult the already-expired search budget at all.
	h.SearchExpired = func() bool { panic("review checked the search budget") }
	bad := engine.Action{Type: engine.ActReserveDeck, Level: 1, Card: engine.NoCard}
	r := h.Review(context.Background(), &m, 0, an, bad)
	if r.Verdict != "mistake" && r.Verdict != "blunder" {
		t.Fatalf("expired search prevented normal review: %+v", r)
	}
}

func TestEndgameBudgetPreservesCompleteWorlds(t *testing.T) {
	s, _ := engine.NewGame(2, 25)
	m := s.MaskFor(0)
	h := NewHard(time.Hour)
	acts := []engine.Action{
		{Type: engine.ActReserveDeck, Level: 1, Card: engine.NoCard},
		{Type: engine.ActReserveDeck, Level: 2, Card: engine.NoCard},
	}
	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		expired bool
		hook    func() bool
		samples int
	}{
		{"expired wall minimum", true, nil, 2},
		{"expired hook minimum", false, func() bool { return true }, 2},
		{"hook replaces expired wall", true, func() bool { return false }, 24},
		{"normal wall", false, nil, 24},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h.SearchExpired = tc.hook
			deadline := time.Now().Add(time.Hour)
			if tc.expired {
				deadline = time.Now().Add(-time.Second)
			}
			rng := rand.New(rand.NewPCG(17, 19))
			got := h.endgame(ctx, &m, 0, acts, deadline, rng)
			want := make(map[engine.Action]float64)
			rng = rand.New(rand.NewPCG(17, 19))
			for sample := 0; sample < tc.samples; sample++ {
				world := engine.Determinize(&m, rng)
				for _, a := range acts {
					ns := world
					ns.Step(a)
					h.E.resolvePending(&ns)
					v, complete := h.paranoid(ctx, &ns, 0, int(m.NPlayers))
					if !complete || math.IsNaN(v) || math.IsInf(v, 0) {
						t.Fatalf("invalid complete-branch verdict: %v", v)
					}
					want[a] += v
				}
			}
			for a := range want {
				want[a] /= float64(tc.samples)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("incomplete worlds: got %v, want %v", got, want)
			}
		})
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	h.SearchExpired = func() bool { panic("cancellation must bypass the minimum worlds") }
	if got := h.endgame(canceled, &m, 0, acts, time.Now().Add(time.Hour), newRNG()); got != nil {
		t.Fatalf("pre-canceled endgame returned verdicts: %v", got)
	}
	if v, complete := h.paranoid(canceled, &s, 0, 3); complete || v != 0 {
		t.Fatalf("pre-canceled paranoid returned (%v, %v)", v, complete)
	}
}

func TestEstimateActionsCompleteSamples(t *testing.T) {
	s, _ := engine.NewGame(2, 26)
	m := s.MaskFor(0)
	h := NewHard(time.Nanosecond)
	h.SearchExpired = func() bool { panic("estimates checked the search budget") }
	a := m.LegalActions(nil)[0]
	acts := []engine.Action{a, a}
	got := h.EstimateActions(context.Background(), &m, 0, acts, 96)
	if len(got) != 2 || got[0] != got[1] || got[0] <= 0 || got[0] > 1 || math.IsNaN(got[0]) {
		t.Fatalf("common-random-rollout estimates: %v", got)
	}
	got = h.EstimateActions(context.Background(), &m, 0, acts, 0)
	if len(got) != 2 || got[0] != 0 || got[1] != 0 {
		t.Fatalf("empty samples must not be divided: %v", got)
	}
}
