package ai

import (
	"context"
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
	h := NewHard(time.Second)
	h.MaxIter = 60
	h.Workers = 2
	return h
}

func TestAgentsPlayLegalGames(t *testing.T) {
	for n := 2; n <= 4; n++ {
		agents := []Agent{NewEasy(), NewMedium(), fastHard(), NewMedium()}[:n]
		s := playOut(t, agents, uint64(n))
		if s.Winners == 0 {
			t.Fatal("no winner")
		}
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
	h := NewHard(500 * time.Millisecond)
	an := h.Analyze(context.Background(), &m, 0, false)
	r := h.Review(&m, 0, an, an.Candidates[0].Action)
	if r.Verdict != "best" {
		t.Fatalf("same move should be best, got %s", r.Verdict)
	}
	// Ignoring the threat should be graded as a mistake.
	bad := engine.Action{Type: engine.ActReserveDeck, Level: 1, Card: engine.NoCard}
	r = h.Review(&m, 0, an, bad)
	if r.Verdict == "best" || r.Verdict == "good" {
		t.Fatalf("ignoring a winning threat on %s should be a mistake, got %s (delta %.2f)", engine.CardString(target), r.Verdict, r.Delta)
	}
}
