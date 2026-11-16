package engine

import (
	"encoding/json"
	"math/rand/v2"
	"testing"
)

func TestCatalog(t *testing.T) {
	var perLevel [3]int
	var perColor [3][5]int
	for _, c := range Cards {
		perLevel[c.Level-1]++
		perColor[c.Level-1][c.Bonus]++
	}
	if perLevel != [3]int{40, 30, 20} {
		t.Fatalf("level counts %v", perLevel)
	}
	for lv, want := range []int{8, 6, 4} {
		for c := 0; c < 5; c++ {
			if perColor[lv][c] != want {
				t.Fatalf("level %d color %d: %d", lv+1, c, perColor[lv][c])
			}
		}
	}
	// Each color appears in the costs equally often across a level in the official set.
	for lv := 0; lv < 3; lv++ {
		var sum [5]int
		for _, id := range levelCards[lv] {
			for c := 0; c < 5; c++ {
				sum[c] += int(Cards[id].Cost[c])
			}
		}
		for c := 1; c < 5; c++ {
			if sum[c] != sum[0] {
				t.Fatalf("level %d unbalanced cost totals %v", lv+1, sum)
			}
		}
	}
}

func totalTokens(s *GameState) Gems {
	g := s.Bank
	for p := 0; p < int(s.NPlayers); p++ {
		for c := 0; c < NumColors; c++ {
			g[c] += s.Players[p].Tokens[c]
		}
	}
	return g
}

func totalCards(s *GameState) int {
	n := 0
	for lv := 0; lv < 3; lv++ {
		n += int(s.DeckLen[lv])
		for i := 0; i < 4; i++ {
			if s.Board[lv][i] >= 0 {
				n++
			}
		}
	}
	for p := 0; p < int(s.NPlayers); p++ {
		n += int(s.Players[p].NCards) + int(s.Players[p].NRes)
	}
	return n
}

func TestRandomGamesConserve(t *testing.T) {
	n := 10000
	if testing.Short() {
		n = 1000
	}
	rng := rand.New(rand.NewPCG(1, 2))
	var buf []Action
	for g := 0; g < n; g++ {
		np := 2 + g%3
		s, err := NewGame(np, uint64(g))
		if err != nil {
			t.Fatal(err)
		}
		start := totalTokens(&s)
		for steps := 0; !s.Over(); steps++ {
			if steps > 2000 {
				break // random play may stall; conservation still checked
			}
			buf = s.LegalActions(buf)
			if len(buf) == 0 {
				t.Fatalf("no legal actions in phase %v", s.Phase)
			}
			a := buf[rng.IntN(len(buf))]
			if _, err := s.Apply(a); err != nil {
				t.Fatalf("legal action rejected: %v %+v", err, a)
			}
			if totalTokens(&s) != start {
				t.Fatalf("token conservation broken")
			}
			if totalCards(&s) != NumCards {
				t.Fatalf("card conservation broken: %d", totalCards(&s))
			}
			for c := 0; c < NumColors; c++ {
				if s.Bank[c] < 0 {
					t.Fatalf("negative bank")
				}
			}
			if s.Phase == PhaseMain && s.Players[(int(s.Current)+int(s.NPlayers)-1)%int(s.NPlayers)].Tokens.Total() > MaxTokens {
				t.Fatalf("hand limit exceeded")
			}
		}
	}
}

func TestSetup(t *testing.T) {
	for n, want := range map[int]int8{2: 4, 3: 5, 4: 7} {
		s, _ := NewGame(n, 7)
		if s.Bank[White] != want || s.Bank[Gold] != 5 || int(s.NNobles) != n+1 {
			t.Fatalf("setup %d: %+v", n, s.Bank)
		}
	}
	if _, err := NewGame(5, 1); err == nil {
		t.Fatal("expected error for 5 players")
	}
}

func TestTakeRules(t *testing.T) {
	s, _ := NewGame(2, 1)
	if _, err := s.Apply(Action{Type: ActTakeSame, Gems: Gems{2}}); err != nil {
		t.Fatal(err)
	}
	// Opponent: white bank now 2 so taking 2 white is illegal.
	if _, err := s.Apply(Action{Type: ActTakeSame, Gems: Gems{2}}); err == nil {
		t.Fatal("take 2 with bank < 4 should fail")
	}
	if _, err := s.Apply(Action{Type: ActTakeDiff, Gems: Gems{1, 1}}); err == nil {
		t.Fatal("must take 3 different when available")
	}
	if _, err := s.Apply(Action{Type: ActTakeDiff, Gems: Gems{0, 0, 0, 0, 0, 1}}); err == nil {
		t.Fatal("cannot take gold")
	}
	if _, err := s.Apply(Action{Type: ActTakeDiff, Gems: Gems{1, 1, 1}}); err != nil {
		t.Fatal(err)
	}
	// Fewer than 3 colors available: may take the remaining ones.
	s2, _ := NewGame(2, 1)
	s2.Bank = Gems{0, 0, 0, 2, 3, 5}
	if _, err := s2.Apply(Action{Type: ActTakeDiff, Gems: Gems{0, 0, 0, 1, 1}}); err != nil {
		t.Fatal(err)
	}
}

func TestDiscardAndReserve(t *testing.T) {
	s, _ := NewGame(2, 3)
	s.Players[0].Tokens = Gems{2, 2, 2, 2, 1, 0}
	if _, err := s.Apply(Action{Type: ActTakeDiff, Gems: Gems{1, 1, 1}}); err != nil {
		t.Fatal(err)
	}
	if s.Phase != PhaseDiscard || s.Current != 0 {
		t.Fatalf("expected discard phase, got %v", s.Phase)
	}
	if _, err := s.Apply(Action{Type: ActDiscard, Gems: Gems{1, 1}}); err != nil {
		t.Fatal(err)
	}
	if s.Phase != PhaseMain || s.Current != 1 || s.Players[0].Tokens.Total() != 10 {
		t.Fatalf("bad state after discard")
	}
	// Reserve from deck gives gold and hides card from opponent.
	if _, err := s.Apply(Action{Type: ActReserveDeck, Level: 3}); err != nil {
		t.Fatal(err)
	}
	p := s.Players[1]
	if p.NRes != 1 || p.Tokens[Gold] != 1 || !p.ResDeck[0] {
		t.Fatalf("reserve deck failed: %+v", p)
	}
	v := s.ViewFor(0)
	if !v.Players[1].Reserved[0].Hidden {
		t.Fatal("deck reserve leaked to opponent")
	}
	v = s.ViewFor(1)
	if v.Players[1].Reserved[0].Hidden {
		t.Fatal("owner should see own reserve")
	}
	m := s.MaskFor(0)
	for _, id := range m.Decks[0] {
		if id != NoCard {
			t.Fatal("deck order leaked")
		}
	}
	d := Determinize(&m, rand.New(rand.NewPCG(1, 1)))
	if d.Players[1].Reserved[0] < 0 || Cards[d.Players[1].Reserved[0]].Level != 3 {
		t.Fatal("determinize should fill hidden reserve with a level 3 card")
	}
	if totalCards(&d) != NumCards {
		t.Fatalf("determinized card count %d", totalCards(&d))
	}
}

func TestBuyWithGoldAndNoble(t *testing.T) {
	s, _ := NewGame(2, 5)
	s.Nobles[0] = 3 // white 4, blue 4
	p := &s.Players[0]
	p.Bonuses = Cost{4, 3, 0, 0, 0}
	// Put a blue card on the board and make it affordable with gold.
	var blue int16 = -1
	for _, id := range levelCards[0] {
		if Cards[id].Bonus == Blue && Cards[id].Points == 0 {
			blue = id
			break
		}
	}
	s.Board[0][0] = blue
	c := Cards[blue]
	for col := 0; col < 5; col++ {
		if col != int(White) && col != int(Blue) {
			p.Tokens[col] = 0
		}
	}
	need := 0
	for col := 2; col < 5; col++ {
		need += int(c.Cost[col])
	}
	p.Tokens[Gold] = int8(need)
	s.Bank[Gold] -= int8(need)
	if _, err := s.Apply(Action{Type: ActBuy, Card: blue}); err != nil {
		t.Fatal(err)
	}
	if s.Players[0].Points != 3 || s.Players[0].Nobles&(1<<3) == 0 {
		t.Fatalf("noble not awarded: %+v", s.Players[0])
	}
}

func TestEndGameTiebreak(t *testing.T) {
	s, _ := NewGame(2, 9)
	s.Players[0].Points = 15
	s.Players[0].NCards = 10
	s.Players[1].Points = 14
	s.Players[1].NCards = 8
	s.Step(Action{Type: ActTakeDiff, Gems: Gems{1, 1, 1}})
	if s.Over() || !s.LastRound {
		t.Fatal("game must continue to end of round")
	}
	s.Players[1].Points = 15 // reaches 15 too on this turn
	s.Step(Action{Type: ActTakeDiff, Gems: Gems{1, 1, 1}})
	if !s.Over() || !s.IsWinner(1) || s.IsWinner(0) {
		t.Fatalf("tiebreak by fewer cards failed: winners=%b", s.Winners)
	}
}

func TestActionJSON(t *testing.T) {
	a := Action{Type: ActTakeDiff, Gems: Gems{1, 0, 1, 0, 1}, Card: NoCard}
	b, _ := json.Marshal(a)
	var back Action
	if err := json.Unmarshal(b, &back); err != nil || back != a {
		t.Fatalf("roundtrip %s -> %+v %v", b, back, err)
	}
}
