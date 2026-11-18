package arena

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gem/internal/ai"
	"gem/internal/engine"
)

func TestMatchParallel(t *testing.T) {
	newEasy := func() ai.Agent { return ai.NewEasy() }
	newMed := func() ai.Agent { return ai.NewMedium() }
	st, err := MatchParallel(context.Background(), newMed, newEasy, 2, 12, 5, 4, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Games != 12 || st.AvgTurns < 10 || st.WinsA < 0 || st.WinsA > 12 {
		t.Fatalf("bad stats %+v", st)
	}
	seen := 0
	st, err = Match(context.Background(), newEasy, newEasy, 3, 3, 9, func(i int, s MatchStats) {
		if s.Games != i+1 {
			t.Errorf("progress %d with %d games", i, s.Games)
		}
		seen++
	})
	if err != nil || st.Games != 3 || seen != 3 {
		t.Fatalf("serial match: %v %+v seen=%d", err, st, seen)
	}
}

// hoarder prioritizes takes. Once at the hand limit it returns exactly what it
// took, creating known unchanged turns and repeated positions in two-player games.
type hoarder struct {
	last engine.Gems
}

func (*hoarder) Level() ai.Level { return ai.Easy }
func (h *hoarder) Choose(_ context.Context, s *engine.GameState, _ int) (engine.Action, *ai.Analysis) {
	if s.Phase == engine.PhaseDiscard && h.last.Total() == s.Cur().Tokens.Total()-engine.MaxTokens {
		return engine.Action{Type: engine.ActDiscard, Gems: h.last}, nil
	}
	a := s.LegalActions(nil)[0]
	if s.Phase == engine.PhaseMain {
		h.last = a.Gems
	}
	return a, nil
}

type chooseAgent struct {
	choose func(context.Context, *engine.GameState, int) engine.Action
}

func (chooseAgent) Level() ai.Level { return ai.Medium }
func (a chooseAgent) Choose(ctx context.Context, s *engine.GameState, seat int) (engine.Action, *ai.Analysis) {
	return a.choose(ctx, s, seat), nil
}

func TestRecordedRoundCapTiesAndRotation(t *testing.T) {
	const players, games = 2, 4
	created := 0
	newH := func() ai.Agent { created++; return &hoarder{} }
	var records []GameRecord
	st, err := MatchParallelObserved(context.Background(), newH, newH, players, games, 17, 1, nil, func(r GameRecord) error {
		records = append(records, r)
		return nil
	})
	if err != nil || st.Games != games || st.StartedGames != games || len(records) != games || created != players*games {
		t.Fatalf("stats=%+v err=%v records=%d fresh agents=%d", st, err, len(records), created)
	}
	if st.WinsA != 2 || st.WinRateA() != 0.5 || st.AvgTurns != 200 {
		t.Fatalf("tie aggregation: %+v", st)
	}
	for g, record := range records {
		r := record.Result
		if record.G != g || record.SeatA != g%players || record.Seed != 17+uint64(g/players) || len(record.Labels) != players {
			t.Fatalf("assignment: %+v", record)
		}
		if r.Status != "completed" || r.Finish != "round_cap" || !r.RoundCap || r.Rounds != engine.MaxRounds || len(r.Winners) != players || r.Duration <= 0 {
			t.Fatalf("cap result: %+v", r)
		}
		if r.RepeatedPublicPositions == 0 || r.UniquePublicPositions+r.RepeatedPublicPositions != r.Turns {
			t.Fatalf("repeat sampling: unique=%d repeat=%d turns=%d", r.UniquePublicPositions, r.RepeatedPublicPositions, r.Turns)
		}
		var raw [players]SeatMetrics
		for _, tr := range r.Evidence {
			m := &raw[tr.Seat]
			m.PrimaryTurns++
			primary := tr.Decisions[0].Action.Type
			if primary == engine.ActTakeDiff || primary == engine.ActTakeSame {
				m.TakeTurns++
			}
			for _, d := range tr.Decisions {
				if d.Action.Type == engine.ActDiscard {
					m.DiscardTurns++
				}
			}
			if tr.UnchangedTakeDiscard {
				m.UnchangedTakeDiscardTurns++
			}
			if tr.PreviousTurn != nil {
				m.RepeatedPublicPositions++
			}
		}
		for seat, metrics := range r.Seats {
			if r.TieCredit[seat] != 0.5 || r.Points[seat] != 0 || r.Nobles[seat] != 0 || r.CardLevels[seat] != [3]int{} || metrics.EarlyLevel1.Count != 0 {
				t.Fatalf("seat %d: result=%+v metrics=%+v", seat, r, metrics)
			}
			if metrics.PrimaryTurns != engine.MaxRounds || metrics.TakeTurns < 90 || metrics.DiscardTurns < 90 || metrics.UnchangedTakeDiscardTurns < 90 || !reflect.DeepEqual(metrics, raw[seat]) {
				t.Fatalf("primary/subphase counts: %+v raw=%+v", metrics, raw[seat])
			}
		}
		if _, err := json.Marshal(record); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMultiSeatTieCreditAndRotation(t *testing.T) {
	for players := 2; players <= 4; players++ {
		// A real engine terminal transition, with all seats tied on score/cards.
		s, _ := engine.NewGame(players, 5)
		s.Turn = int16(players*engine.MaxRounds - 1)
		s.Current = int8(players - 1)
		events, err := s.Apply(s.LegalActions(nil)[0])
		if err != nil || !s.Over() || len(events) == 0 {
			t.Fatalf("terminal transition: %v", err)
		}
		r := Result{TieCredit: make([]float64, players)}
		snapshot(&r, &s, true)
		for seat := 0; seat < players; seat++ {
			if r.TieCredit[seat] != 1/float64(players) {
				t.Fatalf("%d-way tie: %+v", players, r.TieCredit)
			}
		}
		newH := func() ai.Agent { return &hoarder{} }
		seen := make(map[int]bool)
		_, err = MatchParallelObserved(context.Background(), newH, newH, players, players+1, 21, 3, nil, func(r GameRecord) error {
			if seen[r.G] || r.SeatA != r.G%players || r.Seed != 21+uint64(r.G/players) {
				t.Fatalf("bad assignment: g=%d seed=%d seat=%d", r.G, r.Seed, r.SeatA)
			}
			seen[r.G] = true
			return nil
		})
		if err != nil || len(seen) != players+1 {
			t.Fatalf("rotation: %v records=%d", err, len(seen))
		}
	}
}

func TestRecordedReplayAndMetrics(t *testing.T) {
	newBuyer := func() ai.Agent {
		return chooseAgent{choose: func(_ context.Context, m *engine.GameState, seat int) engine.Action {
			for _, deck := range m.Decks {
				for _, id := range deck {
					if id != engine.NoCard {
						t.Fatal("agent received raw deck")
					}
				}
			}
			for p := 0; p < int(m.NPlayers); p++ {
				if p == seat {
					continue
				}
				for i := 0; i < int(m.Players[p].NRes); i++ {
					if m.Players[p].ResDeck[i] && m.Players[p].Reserved[i] >= 0 {
						t.Fatal("agent received opponent hidden card")
					}
				}
			}
			acts := m.LegalActions(nil)
			for _, a := range acts {
				if a.Type == engine.ActBuy || a.Type == engine.ActBuyReserved {
					return a
				}
			}
			return acts[0]
		}}
	}
	const seed = 24
	res, err := PlayRecorded(context.Background(), []ai.Agent{newBuyer(), newBuyer()}, seed)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := engine.NewGame(2, seed)
	counts := make([]SeatMetrics, 2)
	decisions := make([]int, 2)
	seen := make(map[string]int)
	for _, tr := range res.Evidence {
		before := s
		if tr.Before != publicFingerprint(&s) || tr.Turn != int(s.Turn) || tr.Seat != int(s.Current) {
			t.Fatalf("bad before evidence at turn %d", tr.Turn)
		}
		if previous, ok := seen[tr.Before]; ok {
			if tr.PreviousTurn == nil || *tr.PreviousTurn != previous {
				t.Fatal("bad previous turn")
			}
			counts[tr.Seat].RepeatedPublicPositions++
		} else if tr.PreviousTurn != nil {
			t.Fatal("false repetition")
		}
		seen[tr.Before] = tr.Turn
		var discard, noble bool
		for _, d := range tr.Decisions {
			decisions[tr.Seat]++
			if !d.Applied || d.Phase != s.Phase {
				t.Fatal("bad phase/applied evidence")
			}
			events, err := s.Apply(d.Action)
			if err != nil || !reflect.DeepEqual(events, d.Events) {
				t.Fatalf("replay: %v events=%+v recorded=%+v", err, events, d.Events)
			}
			discard = discard || d.Action.Type == engine.ActDiscard
			noble = noble || d.Action.Type == engine.ActNoble
		}
		if !tr.Completed || tr.After != publicFingerprint(&s) {
			t.Fatal("bad after evidence")
		}
		m := &counts[tr.Seat]
		m.PrimaryTurns++
		primary := tr.Decisions[0].Action
		take := primary.Type == engine.ActTakeDiff || primary.Type == engine.ActTakeSame
		if take {
			m.TakeTurns++
		}
		if discard {
			m.DiscardTurns++
		}
		if noble {
			m.NobleChoiceTurns++
		}
		comparison := s
		comparison.Current, comparison.Phase, comparison.Winners = before.Current, before.Phase, before.Winners
		unchanged := take && discard && publicFingerprint(&comparison) == publicFingerprint(&before)
		if tr.UnchangedTakeDiscard != unchanged {
			t.Fatal("incorrect unchanged flag")
		}
		if unchanged {
			m.UnchangedTakeDiscardTurns++
		}
		if tr.EarlyPurchase != nil {
			p := tr.EarlyPurchase
			card := engine.Cards[primary.Card]
			if card.Level != 1 || before.Cur().NCards >= 3 || p.OwnedBefore != int(before.Cur().NCards) || p.PrintedCost != card.Cost {
				t.Fatal("incorrect early purchase")
			}
			m.EarlyLevel1.Count++
			types := 0
			for c, cost := range card.Cost {
				if cost > 0 {
					types++
				}
				m.EarlyLevel1.PrintedCost[c] += int(cost)
			}
			if p.CostTypes != types || p.Paid != tr.Decisions[0].Events[0].Paid {
				t.Fatal("incorrect cost/pay evidence")
			}
			m.EarlyLevel1.CostTypeCounts[types]++
			for c, paid := range p.Paid {
				m.EarlyLevel1.ActualTokenPay[c] += int(paid)
			}
			m.EarlyLevel1.TokenPayTotal += p.Paid.Total()
		} else if (primary.Type == engine.ActBuy || primary.Type == engine.ActBuyReserved) && engine.Cards[primary.Card].Level == 1 && before.Cur().NCards < 3 {
			t.Fatal("missing early purchase")
		}
	}
	if !s.Over() || !reflect.DeepEqual(counts, res.Seats) || !reflect.DeepEqual(decisions, res.Moves) {
		t.Fatalf("metrics mismatch\nwant=%+v\ngot=%+v", counts, res.Seats)
	}
	if res.Finish != "target_score" || res.RoundCap {
		t.Fatalf("finish=%s cap=%v", res.Finish, res.RoundCap)
	}
	var expected Result
	expected.TieCredit = make([]float64, 2)
	snapshot(&expected, &s, true)
	if !reflect.DeepEqual(expected.Points, res.Points) || !reflect.DeepEqual(expected.Nobles, res.Nobles) || !reflect.DeepEqual(expected.CardLevels, res.CardLevels) || !reflect.DeepEqual(expected.Winners, res.Winners) {
		t.Fatal("final state mismatch")
	}
}

func applyRecorded(t *testing.T, r *recorder, s *engine.GameState, a engine.Action) {
	t.Helper()
	before := *s
	r.begin(&before, a)
	events, err := s.Apply(a)
	if err != nil {
		t.Fatal(err)
	}
	r.applied(&before, s, a, events)
}

func TestEarlyPrintedCostsAndActualPaymentBoundary(t *testing.T) {
	for _, actionType := range []engine.ActionType{engine.ActBuy, engine.ActBuyReserved} {
		s, _ := engine.NewGame(2, 5)
		s.Board[0][0] = 0
		p := &s.Players[0]
		p.NCards, p.Owned[0] = 2, (1<<1)|(1<<2)
		p.Bonuses = engine.Cost{1, 1, 0, 0, 0}
		p.Tokens = engine.Gems{0, 0, 1, 1, 0, 2}
		if actionType == engine.ActBuyReserved {
			p.Reserved[0], p.NRes = 0, 1
		}
		res := Result{Seats: make([]SeatMetrics, 2)}
		r := recorder{res: &res, seen: make(map[string]int)}
		a := engine.Action{Type: actionType, Card: 0, HasPay: true, Gems: engine.Gems{0, 0, 0, 0, 0, 2}}
		applyRecorded(t, &r, &s, a)
		early := res.Seats[0].EarlyLevel1
		if early.Count != 1 || early.CostTypeCounts[4] != 1 || early.PrintedCost != [5]int{1, 1, 1, 1, 0} || early.ActualTokenPay != [6]int{0, 0, 0, 0, 0, 2} || early.TokenPayTotal != 2 {
			t.Fatalf("early metrics: %+v", early)
		}
		if res.Evidence[0].EarlyPurchase.OwnedBefore != 2 || s.Players[0].NCards != 3 {
			t.Fatal("boundary not exercised")
		}
		// Three cards already owned: the next L1 buy must not enter early metrics.
		s.Current, s.Phase = 0, engine.PhaseMain
		s.Board[0][0] = 6
		s.Players[0].Tokens[engine.Green] = 3
		applyRecorded(t, &r, &s, engine.Action{Type: engine.ActBuy, Card: 6})
		if res.Seats[0].EarlyLevel1.Count != 1 || res.Evidence[1].EarlyPurchase != nil {
			t.Fatal("counted L1 with 3 cards owned")
		}
	}
}

func TestDiscardAndNobleSubphasesCountOnce(t *testing.T) {
	s, _ := engine.NewGame(2, 9)
	s.Players[0].Tokens = engine.Gems{2, 2, 2, 2, 2, 0}
	s.Players[0].Bonuses = engine.Cost{4, 4, 4, 4, 4}
	res := Result{Seats: make([]SeatMetrics, 2)}
	r := recorder{res: &res, seen: make(map[string]int)}
	gems := engine.Gems{1, 1, 1, 0, 0, 0}
	applyRecorded(t, &r, &s, engine.Action{Type: engine.ActTakeDiff, Gems: gems})
	if s.Phase != engine.PhaseDiscard || res.Seats[0].PrimaryTurns != 0 {
		t.Fatal("premature primary count")
	}
	applyRecorded(t, &r, &s, engine.Action{Type: engine.ActDiscard, Gems: gems})
	if s.Phase != engine.PhaseNoble || res.Seats[0].PrimaryTurns != 0 {
		t.Fatal("premature discard count")
	}
	applyRecorded(t, &r, &s, s.LegalActions(nil)[0])
	m := res.Seats[0]
	if m.PrimaryTurns != 1 || m.TakeTurns != 1 || m.DiscardTurns != 1 || m.NobleChoiceTurns != 1 || m.UnchangedTakeDiscardTurns != 0 || len(res.Evidence) != 1 || len(res.Evidence[0].Decisions) != 3 || s.Players[0].Points != 3 {
		t.Fatalf("phase metrics: %+v", m)
	}
}

func TestPublicFingerprintMaskAndCounters(t *testing.T) {
	s, _ := engine.NewGame(2, 7)
	s.Players[1].NRes, s.Players[1].Reserved[0], s.Players[1].ResDeck[0] = 1, 0, true
	base := publicFingerprint(&s)
	other := s
	other.Turn += 2
	other.Decks[0][0], other.Decks[0][1] = other.Decks[0][1], other.Decks[0][0]
	other.Players[1].Reserved[0] = 1 // same level, different hidden identity
	if publicFingerprint(&other) != base {
		t.Fatal("fingerprint exposed hidden data or turn counter")
	}
	other.Players[0].NRes, other.Players[0].Reserved[0], other.Players[0].ResDeck[0] = 1, 3, true
	ownHidden := publicFingerprint(&other)
	other.Players[0].Reserved[0] = 4
	if publicFingerprint(&other) != ownHidden {
		t.Fatal("spectator fingerprint exposed actor hidden identity")
	}
	other = s
	other.Players[1].ResDeck[0] = false
	faceUp := publicFingerprint(&other)
	other.Players[1].Reserved[0] = 1
	if publicFingerprint(&other) == faceUp {
		t.Fatal("public reserved identity omitted")
	}
	other = s
	other.Players[0].Owned[0] = 1
	if publicFingerprint(&other) == base {
		t.Fatal("owned cards omitted")
	}
	other = s
	other.Players[0].Nobles = 1
	if publicFingerprint(&other) == base {
		t.Fatal("owned nobles omitted")
	}
	other = s
	other.Current = 1
	if publicFingerprint(&other) == base {
		t.Fatal("player to move omitted")
	}
}

func TestPlayCancellationAndIllegalMove(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := PlayRecorded(ctx, []ai.Agent{&hoarder{}, &hoarder{}}, 1)
	if !errors.Is(err, context.Canceled) || !res.Cancelled || res.Status != "cancelled" || res.Turns != 0 || len(res.Winners) != 0 || len(res.Evidence) != 0 {
		t.Fatalf("pre-cancel: %+v %v", res, err)
	}

	ctx, cancel = context.WithCancel(context.Background())
	a := chooseAgent{choose: func(_ context.Context, _ *engine.GameState, _ int) engine.Action {
		cancel()
		return engine.Action{Type: engine.ActDiscard}
	}}
	res, err = PlayRecorded(ctx, []ai.Agent{a, &hoarder{}}, 1)
	if !errors.Is(err, context.Canceled) || res.Moves[0] != 1 || res.Turns != 0 || len(res.Evidence) != 1 || res.Evidence[0].Decisions[0].Applied || res.Evidence[0].Completed {
		t.Fatalf("cancel during choose: %+v %v", res, err)
	}

	invalid := chooseAgent{choose: func(context.Context, *engine.GameState, int) engine.Action {
		return engine.Action{Type: engine.ActDiscard}
	}}
	res, err = PlayRecorded(context.Background(), []ai.Agent{invalid, &hoarder{}}, 1)
	if !errors.Is(err, engine.ErrNotLegal) || res.Status != "error" || res.Finish != "illegal_move" || res.Error == "" || len(res.Winners) != 0 || res.TieCredit[0] != 0 || res.Duration <= 0 {
		t.Fatalf("illegal result: %+v %v", res, err)
	}
	var records []GameRecord
	st, err := MatchParallelObserved(context.Background(), func() ai.Agent { return invalid }, func() ai.Agent { return &hoarder{} }, 2, 8, 1, 1, nil, func(r GameRecord) error { records = append(records, r); return nil })
	if err == nil || st.Games != 0 || st.WinsA != 0 || st.FailedGames < 1 || st.StartedGames != len(records) || st.StartedGames != st.FailedGames+st.CancelledGames {
		t.Fatalf("failed match: %+v %v records=%d", st, err, len(records))
	}
}

func TestCancellationInDiscardPreservesPartialEvidence(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	h := &hoarder{}
	a := chooseAgent{choose: func(ctx context.Context, s *engine.GameState, seat int) engine.Action {
		if s.Phase == engine.PhaseDiscard {
			cancel()
		}
		act, _ := h.Choose(ctx, s, seat)
		return act
	}}
	res, err := PlayRecorded(ctx, []ai.Agent{a, &hoarder{}}, 3)
	last := res.Evidence[len(res.Evidence)-1]
	if !errors.Is(err, context.Canceled) || last.Completed || len(last.Decisions) != 2 || !last.Decisions[0].Applied || last.Decisions[1].Applied || last.Decisions[1].Phase != engine.PhaseDiscard {
		t.Fatalf("partial turn: %+v err=%v", last, err)
	}
	if res.Seats[0].PrimaryTurns+res.Seats[1].PrimaryTurns != res.Turns {
		t.Fatal("unfinished turn counted")
	}
}

func TestCancelledParallelDrainsAllStartedGames(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{})
	var once sync.Once
	var active atomic.Int32
	newWait := func() ai.Agent {
		return chooseAgent{choose: func(ctx context.Context, s *engine.GameState, _ int) engine.Action {
			active.Add(1)
			defer active.Add(-1)
			once.Do(func() { close(ready) })
			<-ctx.Done()
			return s.LegalActions(nil)[0]
		}}
	}
	type answer struct {
		st      MatchStats
		err     error
		records []GameRecord
	}
	done := make(chan answer, 1)
	go func() {
		var records []GameRecord
		st, err := MatchParallelObserved(ctx, newWait, newWait, 3, 100, 1, 4, nil, func(r GameRecord) error { records = append(records, r); return nil })
		done <- answer{st, err, records}
	}()
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("no game started")
	}
	cancel()
	select {
	case result := <-done:
		if !errors.Is(result.err, context.Canceled) || result.st.Games != 0 || result.st.StartedGames != len(result.records) || result.st.CancelledGames != result.st.StartedGames || result.st.StartedGames == 0 || active.Load() != 0 {
			t.Fatalf("drain: %+v err=%v records=%d active=%d", result.st, result.err, len(result.records), active.Load())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation leaked or blocked workers")
	}
}

func TestObserverFailureReportsCompletedGamesAndAverages(t *testing.T) {
	sentinel := errors.New("output failed")
	seen, completed := 0, 0
	st, err := MatchParallelObserved(context.Background(), func() ai.Agent { return &hoarder{} }, func() ai.Agent { return &hoarder{} }, 2, 10, 4, 2, nil, func(r GameRecord) error {
		seen++
		if r.Result.Status == "completed" {
			completed++
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) || st.StartedGames != seen || st.Games != completed || st.Games == 0 || st.AvgTurns != 200 || st.StartedGames != st.Games+st.CancelledGames+st.FailedGames || st.WinsA != float64(completed)/2 {
		t.Fatalf("observer error: %+v %v seen=%d completed=%d", st, err, seen, completed)
	}
}

func TestMalformedArenaInputs(t *testing.T) {
	newH := func() ai.Agent { return &hoarder{} }
	for _, args := range [][3]int{{0, 1, 1}, {1, 1, 1}, {5, 1, 1}, {2, 0, 1}, {2, -1, 1}, {2, 1, 0}, {2, 1, -1}} {
		st, err := MatchParallel(context.Background(), newH, newH, args[0], args[1], 1, args[2], nil)
		if err == nil || st.StartedGames != 0 || st.Games != 0 {
			t.Fatalf("accepted malformed %v: %+v %v", args, st, err)
		}
	}
	if _, err := Match(context.Background(), nil, newH, 2, 1, 1, nil); err == nil {
		t.Fatal("accepted nil factory")
	}
	if _, err := Play(context.Background(), []ai.Agent{nil, &hoarder{}}, 1); err == nil {
		t.Fatal("accepted nil agent")
	}
	if _, err := Play(context.Background(), nil, 1); err == nil {
		t.Fatal("accepted zero players")
	}
	if (MatchStats{}).WinRateA() != 0 {
		t.Fatal("zero-game rate is not finite zero")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	st, err := Match(ctx, newH, newH, 2, 1, 1, nil)
	if !errors.Is(err, context.Canceled) || st.StartedGames != 0 {
		t.Fatalf("pre-cancelled match: %+v %v", st, err)
	}
}
