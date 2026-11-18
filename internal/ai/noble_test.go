package ai

import (
	"context"
	"math"
	"reflect"
	"testing"

	"gem/internal/engine"
)

func nobleFixture() engine.GameState {
	s := engine.GameState{NPlayers: 2, Bank: engine.Gems{4, 4, 4, 4, 4, 5}}
	for lv := range s.Board {
		for i := range s.Board[lv] {
			s.Board[lv][i] = engine.NoCard
		}
	}
	for i := range s.Nobles {
		s.Nobles[i] = -1
	}
	for i := range s.Players {
		for j := range s.Players[i].Reserved {
			s.Players[i].Reserved[j] = engine.NoCard
		}
	}
	return s
}

func nobleFind(t *testing.T, match func(engine.Card) bool) int16 {
	t.Helper()
	for _, c := range engine.Cards {
		if match(c) {
			return c.ID
		}
	}
	t.Fatal("no matching catalog card")
	return -1
}

func noblePut(s *engine.GameState, ids ...int16) {
	for _, id := range ids {
		lv := engine.Cards[id].Level - 1
		for i := range s.Board[lv] {
			if s.Board[lv][i] < 0 {
				s.Board[lv][i] = id
				break
			}
		}
	}
}

func nobleChoose(t *testing.T, s *engine.GameState) engine.Action {
	t.Helper()
	m := s.MaskFor(int(s.Current))
	before := m
	a, an := NewNoble().Choose(context.Background(), &m, int(m.Current))
	if an != nil {
		t.Fatal("rule strategy must not manufacture analysis/win rates")
	}
	if m != before {
		t.Fatal("Choose mutated input")
	}
	if err := s.Validate(a); err != nil {
		t.Fatalf("illegal choice %+v: %v", a, err)
	}
	return a
}

func nobleScore(s *engine.GameState, a engine.Action) float64 {
	seat := int(s.Current)
	plan := noblePlanFor(s, seat)
	return nobleActionScore(context.Background(), s, seat, plan, plan.utility(s.Players[seat].Tokens), a)
}

func nobleCheap(t *testing.T, colors int) int16 {
	return nobleFind(t, func(c engine.Card) bool {
		n, k := noblePrinted(&c)
		return c.Level == 1 && n == 3 && (colors == 0 || k == colors)
	})
}

func TestNobleRegistryStandalone(t *testing.T) {
	l, ok := ParseLevel("noble")
	if !ok || l != Noble || l.Name() != "基建贵族" || New(l).Level() != Noble {
		t.Fatal("missing standalone strategy registry")
	}
	for _, l := range []Level{Easy, Medium, Hard, Master} {
		if New(l).Level() != l {
			t.Fatalf("original difficulty %s changed", l)
		}
	}
	if _, ok := ParseLevel("Noble"); ok {
		t.Fatal("registry should retain exact parsing")
	}
}

func TestNobleEarlyLowestPrintedCost(t *testing.T) {
	s := nobleFixture()
	three := nobleCheap(t, 1)
	four := nobleFind(t, func(c engine.Card) bool { n, k := noblePrinted(&c); return c.Level == 1 && n == 4 && k > 1 })
	noblePut(&s, four, three)
	// Even a free mixed-four must not redefine the early printed target.
	s.Players[0].Bonuses = engine.Cards[four].Cost
	p := noblePlanFor(&s, 0)
	if p.targets[0].id != three {
		t.Fatalf("expected printed three, got %+v", p.targets)
	}
}

func TestNobleEarlyMixedBeatsMonoTarget(t *testing.T) {
	s := nobleFixture()
	mixed, mono := nobleCheap(t, 2), nobleCheap(t, 1)
	noblePut(&s, mono, mixed)
	if noblePlanFor(&s, 0).targets[0].id != mixed {
		t.Fatal("equal printed cost must prefer mixed")
	}
}

func TestNobleEarlyMixedBeatsMonoBuy(t *testing.T) {
	s := nobleFixture()
	mixed, mono := nobleCheap(t, 2), nobleCheap(t, 1)
	noblePut(&s, mono, mixed)
	s.Players[0].Tokens = engine.Gems{0, 0, 3, 1, 0}
	a := nobleChoose(t, &s)
	if a.Type != engine.ActBuy || a.Card != mixed {
		t.Fatalf("expected mixed-three buy, got %+v", a)
	}
}

func TestNobleEarlyExcludesPrintedFive(t *testing.T) {
	s := nobleFixture()
	five := nobleFind(t, func(c engine.Card) bool { n, _ := noblePrinted(&c); return c.Level == 1 && n == 5 })
	noblePut(&s, five)
	s.Players[0].Bonuses = engine.Cards[five].Cost
	if len(noblePlanFor(&s, 0).targets) != 0 {
		t.Fatal("printed five should not be an early target even when discounted")
	}
	if a := nobleChoose(t, &s); a.Type == engine.ActBuy || a.Type == engine.ActReserve {
		t.Fatalf("should collect/wait rather than pursue five: %+v", a)
	}
}

func TestNobleEarlyExcludesMonoFour(t *testing.T) {
	s := nobleFixture()
	id := nobleFind(t, func(c engine.Card) bool { n, k := noblePrinted(&c); return c.Level == 1 && n == 4 && k == 1 })
	noblePut(&s, id)
	s.Players[0].Tokens = engine.Gems{4, 4, 0, 0, 0}
	if len(noblePlanFor(&s, 0).targets) != 0 {
		t.Fatal("mono-four is not an early target")
	}
	a := nobleChoose(t, &s)
	if a.Type == engine.ActBuy || a.Type == engine.ActReserve {
		t.Fatalf("should not pursue mono-four: %+v", a)
	}
}

func TestNobleEarlyMixedFourFallback(t *testing.T) {
	s := nobleFixture()
	id := nobleFind(t, func(c engine.Card) bool { n, k := noblePrinted(&c); return c.Level == 1 && n == 4 && k > 1 })
	noblePut(&s, id)
	p := noblePlanFor(&s, 0)
	if len(p.targets) != 1 || p.targets[0].id != id {
		t.Fatal("mixed-four fallback missing")
	}
}

func TestNobleEarlyActivelyCollectsThreeCost(t *testing.T) {
	s := nobleFixture()
	id := nobleCheap(t, 2)
	noblePut(&s, id)
	a := nobleChoose(t, &s)
	if a.Type != engine.ActTakeDiff {
		t.Fatalf("expected productive collection, got %+v", a)
	}
	for col, n := range engine.Cards[id].Cost {
		if n > 0 && a.Gems[col] == 0 {
			t.Fatalf("ignored target color %d: %+v", col, a)
		}
	}
}

func TestNobleDiscountedTransitionAtThreeCards(t *testing.T) {
	s := nobleFixture()
	cheap := nobleCheap(t, 2)
	five := nobleFind(t, func(c engine.Card) bool { n, _ := noblePrinted(&c); return c.Level == 1 && n == 5 })
	noblePut(&s, cheap, five)
	s.Players[0].NCards = 3
	s.Players[0].Bonuses = engine.Cards[five].Cost
	if noblePlanFor(&s, 0).targets[0].id != five {
		t.Fatal("after three cards, zero discounted payment should beat printed cost")
	}
}

func TestNoblePrintedRestrictionsExpire(t *testing.T) {
	for _, shape := range []string{"five", "mono-four"} {
		t.Run(shape, func(t *testing.T) {
			s := nobleFixture()
			id := nobleFind(t, func(c engine.Card) bool {
				n, k := noblePrinted(&c)
				return c.Level == 1 && ((shape == "five" && n == 5) || (shape == "mono-four" && n == 4 && k == 1))
			})
			noblePut(&s, id)
			s.Players[0].NCards = 3
			if len(noblePlanFor(&s, 0).targets) != 1 {
				t.Fatal("early restriction leaked into middle game")
			}
		})
	}
}

func TestNobleNoblePreferenceSecondaryToPrintedCost(t *testing.T) {
	s := nobleFixture()
	three := nobleCheap(t, 1)
	four := nobleFind(t, func(c engine.Card) bool { n, k := noblePrinted(&c); return c.Level == 1 && n == 4 && k > 1 })
	noblePut(&s, four, three)
	s.NNobles, s.Nobles[0] = 1, 0
	s.Players[0].Bonuses = engine.Cost{0, 0, 3, 4, 0}
	if noblePlanFor(&s, 0).targets[0].id != three {
		t.Fatal("noble bonus must remain secondary in early target ordering")
	}
}

func TestNobleTargetOrderingFrozenAcrossTake(t *testing.T) {
	s := nobleFixture()
	noblePut(&s, nobleCheap(t, 2), nobleCheap(t, 1), 12)
	before := noblePlanFor(&s, 0)
	s.Players[0].Tokens = engine.Gems{1, 3, 2, 3, 1}
	s.Bank = engine.Gems{0, 1, 2, 1, 3, 0}
	after := noblePlanFor(&s, 0)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("token/bank changes retargeted frozen retention goals")
	}
}

func TestNobleBalancedTargetTokens(t *testing.T) {
	s := nobleFixture()
	id := nobleCheap(t, 2)
	noblePut(&s, id)
	plan := noblePlanFor(&s, 0)
	var balanced, mono engine.Gems
	for col, n := range engine.Cards[id].Cost {
		balanced[col] = n
		if n == 2 {
			mono[col] = 3
		}
	}
	if plan.utility(balanced) <= plan.utility(mono) {
		t.Fatal("complete mixed payment should beat surplus monochrome stack")
	}
}

func TestNobleRetentionKeepsGoldAndUsefulColors(t *testing.T) {
	s := nobleFixture()
	noblePut(&s, nobleCheap(t, 2))
	plan := noblePlanFor(&s, 0)
	have := engine.Gems{3, 3, 2, 2, 1, 2}
	kept, give := plan.retain(context.Background(), have)
	if kept.Total() != 10 || give.Total() != 3 || kept[engine.Gold] != 2 {
		t.Fatalf("unexpected retention kept=%v give=%v", kept, give)
	}
	for col, n := range plan.targets[0].need {
		if n > 0 && kept[col] < min(have[col], n) {
			t.Fatal("discarded useful target payment before surplus")
		}
	}
}

func TestNobleRetentionGloballyOptimal(t *testing.T) {
	s := nobleFixture()
	noblePut(&s, nobleCheap(t, 2), 12, 28)
	s.Phase = engine.PhaseDiscard
	s.Players[0].Tokens = engine.Gems{2, 2, 3, 3, 1, 2}
	plan := noblePlanFor(&s, 0)
	kept, _ := plan.retain(context.Background(), s.Players[0].Tokens)
	best := plan.utility(kept)
	for _, a := range s.LegalActions(nil) {
		have := s.Players[0].Tokens
		for col := range have {
			have[col] -= a.Gems[col]
		}
		if plan.utility(have) > best+1e-9 {
			t.Fatal("retention failed to find actual best discard")
		}
	}
}

func TestNobleTakeDiscardProjectionConsistency(t *testing.T) {
	s := nobleFixture()
	noblePut(&s, nobleCheap(t, 2), 12, 28)
	s.Players[0].Tokens = engine.Gems{3, 3, 0, 1, 3, 0}
	plan := noblePlanFor(&s, 0)
	for _, a := range s.LegalActions(nil) {
		if a.Type != engine.ActTakeDiff && a.Type != engine.ActTakeSame {
			continue
		}
		projected, _ := nobleProject(context.Background(), &s, 0, plan, a)
		actual := s
		if _, err := actual.Apply(a); err != nil {
			t.Fatal(err)
		}
		if actual.Phase != engine.PhaseDiscard {
			t.Fatal("fixture should overflow")
		}
		discard := nobleChoose(t, &actual)
		if _, err := actual.Apply(discard); err != nil {
			t.Fatal(err)
		}
		if actual.Players[0].Tokens != projected.Players[0].Tokens || actual.Bank != projected.Bank {
			t.Fatalf("take/discard mismatch for %+v: actual=%v projection=%v", a, actual.Players[0].Tokens, projected.Players[0].Tokens)
		}
	}
}

func TestNobleNoGainTakeIsNotRewarded(t *testing.T) {
	s := nobleFixture()
	noblePut(&s, nobleCheap(t, 2))
	s.Players[0].Tokens = engine.Gems{0, 0, 5, 5, 0, 0}
	s.Bank = engine.Gems{0, 0, 4, 4, 0, 0}
	a := engine.Action{Type: engine.ActTakeSame, Gems: engine.Gems{0, 0, 2, 0, 0, 0}, Card: engine.NoCard}
	if v := nobleScore(&s, a); v >= 0 {
		t.Fatalf("unchanged take then return was scored as progress: %f", v)
	}
}

func TestNobleZeroValueReversibleSwapPenalty(t *testing.T) {
	s := nobleFixture()
	plan := noblePlanFor(&s, 0) // no useful targets
	s.Players[0].Tokens = engine.Gems{2, 2, 2, 2, 2, 0}
	a := engine.Action{Type: engine.ActTakeDiff, Gems: engine.Gems{1, 1, 1, 0, 0, 0}, Card: engine.NoCard}
	ns, _ := nobleProject(context.Background(), &s, 0, plan, a)
	if math.Abs(plan.utility(ns.Players[0].Tokens)-plan.utility(s.Players[0].Tokens)) > 1e-9 {
		t.Fatal("fixture must be an equal-utility swap")
	}
	if nobleScore(&s, a) >= -3 {
		t.Fatal("equal-value reversible swap lacks explicit non-progress penalty")
	}
}

func TestNobleCashSpillDoesNotForceJunkBuy(t *testing.T) {
	s := nobleFixture()
	id := nobleCheap(t, 2)
	noblePut(&s, id)
	s.Players[0].NCards = 6
	s.Players[0].Bonuses = engine.Cost{0, 0, 3, 3, 0}
	s.Players[0].Tokens = engine.Gems{2, 2, 2, 2, 2, 0}
	a := nobleChoose(t, &s)
	if a.Type == engine.ActBuy {
		t.Fatalf("bought a zero-point surplus bonus only to clear overflow: %+v", a)
	}
}

func TestNobleNextTargetDiscountPlan(t *testing.T) {
	s := nobleFixture()
	// Black bonus reduces the following blue-bonus card's black payment.
	black := nobleCheap(t, 2)
	blue := int16(12)
	noblePut(&s, black, blue)
	s.Players[0].NCards = 3
	s.Players[0].Tokens = engine.Gems{0, 0, 2, 1, 0, 0}
	plan := noblePlanFor(&s, 0)
	ns, _ := nobleProject(context.Background(), &s, 0, plan, engine.Action{Type: engine.ActBuy, Card: black})
	if payTotal(&ns.Players[0], &engine.Cards[blue]) != payTotal(&s.Players[0], &engine.Cards[blue])-1 {
		t.Fatal("projection did not apply discount to next public target")
	}
	if nobleScore(&s, engine.Action{Type: engine.ActBuy, Card: black}) <= 0 {
		t.Fatal("useful cheap infrastructure should have positive purchase utility")
	}
}

func TestNobleCheapFreeNobleInfrastructure(t *testing.T) {
	s := nobleFixture()
	id := nobleFind(t, func(c engine.Card) bool { return c.Level == 1 && c.Bonus == engine.Green && c.Points == 0 })
	noblePut(&s, id)
	s.NNobles, s.Nobles[0] = 1, 0
	s.Players[0].NCards = 8
	s.Players[0].Bonuses = engine.Cost{5, 5, 3, 4, 5}
	a := nobleChoose(t, &s)
	if a.Type != engine.ActBuy || a.Card != id {
		t.Fatalf("expected free noble-completing infrastructure: %+v", a)
	}
}

func TestNobleEfficientLevelTwo(t *testing.T) {
	s := nobleFixture()
	id := nobleFind(t, func(c engine.Card) bool { return c.Level == 2 && c.Points == 2 && c.Bonus == engine.Black })
	noblePut(&s, id)
	s.Players[0].NCards = 5
	s.Players[0].Bonuses = engine.Cards[id].Cost
	a := nobleChoose(t, &s)
	if a.Type != engine.ActBuy || a.Card != id {
		t.Fatalf("efficient L2 should be used occasionally: %+v", a)
	}
}

func nobleFinishFixture(t *testing.T, pts int8) (engine.GameState, int16) {
	s := nobleFixture()
	id := nobleFind(t, func(c engine.Card) bool { return c.Level == 1 && c.Bonus == engine.Green && c.Points == pts })
	noblePut(&s, id)
	s.NNobles, s.Nobles[0] = 1, 0
	s.Players[1].NCards = 8
	s.Players[1].Points = 12 - pts
	s.Players[1].Bonuses = engine.Cost{5, 5, 3, 4, 5}
	return s, id
}

func TestNobleBlocksZeroPointPlusNobleFinish(t *testing.T) {
	s, id := nobleFinishFixture(t, 0)
	if best := noblePublicBest(&s, 1); best.points != 15 {
		t.Fatalf("zero-point+noble finish missed: %+v", best)
	}
	a := nobleChoose(t, &s)
	if a.Type != engine.ActReserve || a.Card != id {
		t.Fatalf("must block public noble finish: %+v", a)
	}
}

func TestNobleBlocksOnePointPlusNobleFinish(t *testing.T) {
	s, id := nobleFinishFixture(t, 1)
	a := nobleChoose(t, &s)
	if a.Type != engine.ActReserve || a.Card != id {
		t.Fatalf("must inspect low-point board buys: %+v", a)
	}
}

func TestNobleBlocksTwoPointPlusNobleFinish(t *testing.T) {
	s := nobleFixture()
	id := nobleFind(t, func(c engine.Card) bool { return c.Level == 2 && c.Bonus == engine.Green && c.Points == 2 })
	noblePut(&s, id)
	s.NNobles, s.Nobles[0] = 1, 0
	s.Players[1].Points = 10
	s.Players[1].Bonuses = engine.Cost{5, 5, 3, 4, 5}
	a := nobleChoose(t, &s)
	if a.Type != engine.ActReserve || a.Card != id {
		t.Fatalf("two points plus noble finishing threat missed: %+v", a)
	}
}

func TestNobleAlternativeFinishThreatRemains(t *testing.T) {
	s, id := nobleFinishFixture(t, 0)
	other := nobleFind(t, func(c engine.Card) bool {
		return c.Level == 1 && c.Bonus == engine.Green && c.Points == 0 && c.ID != id
	})
	noblePut(&s, other)
	plan := noblePlanFor(&s, 0)
	ns, _ := nobleProject(context.Background(), &s, 0, plan, engine.Action{Type: engine.ActReserve, Card: id})
	if noblePublicBest(&ns, 1).points != 15 || nobleDanger(&ns, 0, false) < 40 {
		t.Fatal("blocking one card must not erase alternative legal finish")
	}
}

func TestNoblePaymentBankReturnsEnablePublicPlan(t *testing.T) {
	s := nobleFixture()
	finish := nobleFind(t, func(c engine.Card) bool { return c.Level == 2 && c.Points == 3 && c.Cost[engine.Black] == 6 })
	buy := nobleFind(t, func(c engine.Card) bool { return c.Level == 1 && c.Cost[engine.Black] == 2 && c.Points == 0 })
	noblePut(&s, finish, buy)
	s.Bank = engine.Gems{}
	s.Players[0].NCards = 4
	s.Players[0].Tokens = engine.Gems{3, 3, 0, 0, 2, 0}
	s.Players[1].Points = 12
	s.Players[1].Bonuses[engine.Black] = 4
	s.Players[1].Tokens[engine.Black] = 1
	before := nobleDanger(&s, 0, false)
	ns, pay := nobleProject(context.Background(), &s, 0, noblePlanFor(&s, 0), engine.Action{Type: engine.ActBuy, Card: buy})
	if pay[engine.Black] != 2 || ns.Bank[engine.Black] != 2 || nobleDanger(&ns, 0, false) <= before {
		t.Fatalf("payment returns did not reopen public finishing collect: pay=%v before=%f after=%f", pay, before, nobleDanger(&ns, 0, false))
	}
}

func TestNoblePrudentBlockNotUnreachableHighScore(t *testing.T) {
	s := nobleFixture()
	cheap := nobleCheap(t, 2)
	high := nobleFind(t, func(c engine.Card) bool { return c.Points == 5 })
	noblePut(&s, cheap, high)
	s.Players[0].Tokens = engine.Gems{0, 0, 2, 1, 0, 0}
	a := nobleChoose(t, &s)
	if a.Type != engine.ActBuy || a.Card != cheap {
		t.Fatalf("should not deny an unaffordable speculative five-point card: %+v", a)
	}
}

func TestNobleGoldPaymentAndReservedFinish(t *testing.T) {
	s := nobleFixture()
	id := nobleFind(t, func(c engine.Card) bool { return c.Level == 1 && c.Points == 1 })
	s.Players[0].NRes = 1
	s.Players[0].Reserved[0] = id
	s.Players[0].Tokens[engine.Gold] = 4
	s.Players[0].Points = 14
	s.Players[0].NCards = 8
	a := nobleChoose(t, &s)
	if a.Type != engine.ActBuyReserved || a.Card != id {
		t.Fatalf("known reserve gold finish missed: %+v", a)
	}
	ns, pay := nobleProject(context.Background(), &s, 0, noblePlanFor(&s, 0), a)
	if pay[engine.Gold] != 4 || ns.Players[0].Points != 15 || ns.Players[0].NRes != 0 || ns.Bank[engine.Gold] != 9 {
		t.Fatalf("incorrect public gold/reserved projection: pay=%v player=%+v", pay, ns.Players[0])
	}
}

func TestNobleReserveGoldTowardKnownTarget(t *testing.T) {
	s := nobleFixture()
	id := nobleFind(t, func(c engine.Card) bool {
		n, _ := noblePrinted(&c)
		return c.Level == 2 && c.Points == 2 && c.Cost[engine.Green] == 5 && n == 5
	})
	noblePut(&s, id)
	s.Players[0].Points = 13
	s.Players[0].NCards = 8
	s.Players[0].Tokens[engine.Green] = 4
	s.Bank[engine.Green] = 0
	a := nobleChoose(t, &s)
	if a.Type != engine.ActReserve || a.Card != id {
		t.Fatalf("gold should unlock a known finishing reserve: %+v", a)
	}
}

func TestNobleNoBlindReservePlan(t *testing.T) {
	s := nobleFixture()
	noblePut(&s, nobleCheap(t, 2))
	s.DeckLen = [3]int8{20, 20, 20}
	for i := 0; i < 10; i++ {
		a := nobleChoose(t, &s)
		if a.Type == engine.ActReserveDeck {
			t.Fatal("invented a blind deck reserve plan")
		}
	}
}

func TestNobleOnlyOneNoblePerTurn(t *testing.T) {
	s := nobleFixture()
	s.NNobles = 3
	s.Nobles = [5]int8{3, 0, 2, -1, -1}
	s.Players[0].Bonuses = engine.Cost{4, 4, 4, 4, 4}
	ns, _ := nobleProject(context.Background(), &s, 0, noblePlanFor(&s, 0), engine.Action{Type: engine.ActTakeDiff, Gems: engine.Gems{1, 1, 1, 0, 0, 0}, Card: engine.NoCard})
	if ns.Players[0].Points != 3 || ns.NNobles != 2 {
		t.Fatal("projection incorrectly awarded multiple nobles")
	}
}

func TestNobleEligibleIDDeterministic(t *testing.T) {
	s := nobleFixture()
	s.Phase = engine.PhaseNoble
	s.NNobles = 3
	s.Nobles = [5]int8{3, 0, 2, -1, -1}
	s.Players[0].Bonuses = engine.Cost{4, 4, 4, 4, 4}
	a := nobleChoose(t, &s)
	if a.Type != engine.ActNoble || a.Noble != 0 {
		t.Fatalf("must choose canonical eligible ID: %+v", a)
	}
	s.Nobles[0], s.Nobles[1] = s.Nobles[1], s.Nobles[0]
	if nobleChoose(t, &s) != a {
		t.Fatal("noble ordering affected deterministic choice")
	}
}

func TestNobleRacePrioritizesPointsAtTen(t *testing.T) {
	s := nobleFixture()
	infra := nobleCheap(t, 2)
	point := nobleFind(t, func(c engine.Card) bool { return c.Level == 1 && c.Points == 1 })
	noblePut(&s, infra, point)
	s.Players[0].Points = 10
	s.Players[0].NCards = 2 // race overrides early printed restrictions
	s.Players[0].Bonuses = engine.Cost{4, 4, 4, 4, 4}
	a := nobleChoose(t, &s)
	if a.Type != engine.ActBuy || a.Card != point {
		t.Fatalf("at ten points should prioritize scoring: %+v", a)
	}
}

func TestNobleFinalRoundMaximizesScoreNotFifteen(t *testing.T) {
	s := nobleFixture()
	s.LastRound = true
	s.Players[1].Points = 16
	s.Players[0].Points = 14
	s.Players[0].NCards = 8
	one := nobleFind(t, func(c engine.Card) bool { return c.Level == 1 && c.Points == 1 })
	five := nobleFind(t, func(c engine.Card) bool { return c.Points == 5 })
	noblePut(&s, one, five)
	s.Players[0].Bonuses = engine.Cost{10, 10, 10, 10, 10}
	a := nobleChoose(t, &s)
	if a.Type != engine.ActBuy || a.Card != five {
		t.Fatalf("final round must choose 19, not just 15: %+v", a)
	}
}

func TestNobleFifteenIsNotGuaranteedWin(t *testing.T) {
	s := nobleFixture()
	id := nobleFind(t, func(c engine.Card) bool { return c.Level == 1 && c.Points == 1 })
	noblePut(&s, id)
	s.Players[0].Points = 14
	s.Players[0].NCards = 8
	s.Players[0].Bonuses = engine.Cards[id].Cost
	buy := engine.Action{Type: engine.ActBuy, Card: id}
	winning := nobleScore(&s, buy)
	s.Players[1].Points = 16
	losing := nobleScore(&s, buy)
	if winning-losing < 100 {
		t.Fatalf("15 treated as guaranteed win regardless of standings: win=%f loss=%f", winning, losing)
	}
}

func TestNobleFinalTiePrefersFewerOwnedCards(t *testing.T) {
	s := nobleFixture()
	s.LastRound = true
	s.Players[0].Points = 15
	s.Players[0].NCards = 8
	s.Players[1].Points = 15
	s.Players[1].NCards = 8
	id := nobleCheap(t, 2)
	noblePut(&s, id)
	s.Players[0].Bonuses = engine.Cards[id].Cost
	a := nobleChoose(t, &s)
	if a.Type == engine.ActBuy {
		t.Fatalf("zero-point buy loses score tie through card count: %+v", a)
	}
}

func TestNobleFinalScoreBeforeCardCount(t *testing.T) {
	a := nobleResult{points: 16, cards: 20}
	b := nobleResult{points: 15, cards: 1}
	if !nobleBetter(a, b) || nobleBetter(b, a) {
		t.Fatal("card count incorrectly outranked score")
	}
}

func TestNobleLastSeatIgnoresAlreadyPlayedReply(t *testing.T) {
	s := nobleFixture()
	s.Current = 1
	s.LastRound = true
	s.Players[0].Points = 15
	s.Players[1].Points = 14
	s.Players[1].NCards = 3
	one := nobleFind(t, func(c engine.Card) bool { return c.Level == 1 && c.Points == 1 })
	five := nobleFind(t, func(c engine.Card) bool { return c.Points == 5 })
	noblePut(&s, one, five)
	s.Players[0].Bonuses = engine.Cost{10, 10, 10, 10, 10}
	s.Players[1].Bonuses = engine.Cards[one].Cost
	a := nobleChoose(t, &s)
	if a.Type != engine.ActBuy || a.Card != one {
		t.Fatalf("opponent already played cannot buy five again: %+v", a)
	}
}

func TestNobleMaskedHiddenInformationInvariant(t *testing.T) {
	s := nobleFixture()
	noblePut(&s, nobleCheap(t, 2), 12)
	s.DeckLen = [3]int8{10, 10, 10}
	base := nobleChoose(t, &s)
	for variant := 0; variant < 10; variant++ {
		m := s
		for lv := range m.Decks {
			for i := range m.Decks[lv] {
				m.Decks[lv][i] = int16((i + variant*7) % engine.NumCards)
			}
		}
		m.Players[1].NRes = 3
		m.Players[1].Reserved = [3]int16{int16(variant), engine.HiddenCard(2), int16(70 + variant)}
		m.Players[1].ResDeck = [3]bool{true, true, true}
		m.Players[1].Owned = [2]uint64{uint64(variant), ^uint64(variant)}
		before := m
		a, an := NewNoble().Choose(context.Background(), &m, 0)
		if a != base || an != nil || m != before {
			t.Fatalf("private identity/order affected strategy: variant=%d got=%+v base=%+v", variant, a, base)
		}
	}
}

func TestNobleMarketRefreshOpportunityCost(t *testing.T) {
	s := nobleFixture()
	id := nobleCheap(t, 2)
	noblePut(&s, id)
	for col, cost := range engine.Cards[id].Cost {
		s.Players[0].Tokens[col] = cost
	}
	withRefill := s
	withRefill.DeckLen[0] = 1
	for _, kind := range []engine.ActionType{engine.ActBuy, engine.ActReserve} {
		a := engine.Action{Type: kind, Card: id}
		if delta := nobleScore(&s, a) - nobleScore(&withRefill, a); math.Abs(delta-.4) > 1e-9 {
			t.Fatalf("%v market refresh cost = %v", kind, delta)
		}
	}
	s.LastRound, withRefill.LastRound = true, true
	a := engine.Action{Type: engine.ActBuy, Card: id}
	if nobleScore(&s, a) != nobleScore(&withRefill, a) {
		t.Fatal("future market value must not override final-turn score")
	}
}

func TestNobleNoUnknownReplacementProjection(t *testing.T) {
	s := nobleFixture()
	id := nobleCheap(t, 2)
	noblePut(&s, id)
	s.DeckLen[0] = 1
	s.Decks[0][0] = 7
	s.Players[0].Tokens = engine.Gems{0, 0, 2, 1, 0, 0}
	before := s
	plan := noblePlanFor(&s, 0)
	ns, _ := nobleProject(context.Background(), &s, 0, plan, engine.Action{Type: engine.ActBuy, Card: id})
	if ns.Board[0][0] != engine.NoCard || ns.DeckLen != s.DeckLen || ns.Decks != s.Decks || s != before {
		t.Fatal("projection drew/read a hidden replacement or mutated input")
	}
}

func TestNobleStableBoardOrdering(t *testing.T) {
	s := nobleFixture()
	noblePut(&s, nobleCheap(t, 2), 12, 28)
	a := nobleChoose(t, &s)
	s.Board[0][0], s.Board[0][2] = s.Board[0][2], s.Board[0][0]
	if nobleChoose(t, &s) != a {
		t.Fatal("market slot ordering affected stable ties")
	}
}

func TestNobleCancelledContextStillLegal(t *testing.T) {
	for _, phase := range []engine.Phase{engine.PhaseMain, engine.PhaseDiscard, engine.PhaseNoble} {
		t.Run(phase.String(), func(t *testing.T) {
			s := nobleFixture()
			s.Phase = phase
			noblePut(&s, nobleCheap(t, 2))
			if phase == engine.PhaseDiscard {
				s.Players[0].Tokens = engine.Gems{3, 3, 3, 3, 0, 0}
			}
			if phase == engine.PhaseNoble {
				s.NNobles, s.Nobles[0], s.Nobles[1] = 2, 0, 2
				s.Players[0].Bonuses = engine.Cost{4, 4, 4, 4, 4}
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			a, an := NewNoble().Choose(ctx, &s, 0)
			if err := s.Validate(a); err != nil || an != nil {
				t.Fatalf("cancelled request returned illegal action %+v: %v", a, err)
			}
		})
	}
}

func TestNoblePassOnlyUniqueEngineLegal(t *testing.T) {
	s := nobleFixture()
	s.Bank = engine.Gems{}
	s.Players[0].NRes = 3
	s.Players[0].Reserved = [3]int16{engine.HiddenCard(1), engine.HiddenCard(2), engine.HiddenCard(3)}
	a := nobleChoose(t, &s)
	if a.Type != engine.ActPass || len(s.LegalActions(nil)) != 1 {
		t.Fatalf("expected unique legal pass, got %+v", a)
	}
}

func TestNobleDegenerateMinimumHarmLegal(t *testing.T) {
	s := nobleFixture()
	s.Players[0].Tokens = engine.Gems{2, 2, 2, 2, 2, 0}
	s.Players[0].NRes = 3
	for i := 0; i < 5; i++ {
		a := nobleChoose(t, &s)
		if a.Type == engine.ActPass {
			t.Fatal("illegal skip invented in a no-progress state")
		}
		if _, err := s.Apply(a); err != nil {
			t.Fatal(err)
		}
		if s.Phase == engine.PhaseDiscard {
			if _, err := s.Apply(nobleChoose(t, &s)); err != nil {
				t.Fatal(err)
			}
		}
		s.Current = 0
	}
}

func TestNobleBlindReserveOnlyLegalFallback(t *testing.T) {
	s := nobleFixture()
	s.Bank = engine.Gems{}
	s.DeckLen[0] = 1
	a := nobleChoose(t, &s)
	if a.Type != engine.ActReserveDeck || a.Level != 1 {
		t.Fatalf("must use legal minimum harm when blind reserve is the only move: %+v", a)
	}
}

func TestNobleEarlyBuyPrintedThreeBeforeFreeFour(t *testing.T) {
	s := nobleFixture()
	three := nobleCheap(t, 1)
	four := nobleFind(t, func(c engine.Card) bool {
		n, k := noblePrinted(&c)
		return c.Level == 1 && n == 4 && k > 1
	})
	noblePut(&s, three, four)
	s.Players[0].NCards = 2
	s.Players[0].Bonuses = engine.Cards[four].Cost
	s.Players[0].Tokens = engine.Gems{0, 0, 3, 0, 0, 0}
	a := nobleChoose(t, &s)
	if a.Type != engine.ActBuy || a.Card != three {
		t.Fatalf("early buy ignored lowest printed cost for a discounted four: %+v", a)
	}
}

func TestNobleMixedFourFallbackBuy(t *testing.T) {
	s := nobleFixture()
	id := nobleFind(t, func(c engine.Card) bool {
		n, k := noblePrinted(&c)
		return c.Level == 1 && n == 4 && k > 1
	})
	noblePut(&s, id)
	for col, n := range engine.Cards[id].Cost {
		s.Players[0].Tokens[col] = n
	}
	a := nobleChoose(t, &s)
	if a.Type != engine.ActBuy || a.Card != id {
		t.Fatalf("mixed-four fallback must still be actively bought: %+v", a)
	}
}

func TestNobleGoldDoesNotRewardLosingColoredPayment(t *testing.T) {
	s := nobleFixture()
	noblePut(&s, nobleCheap(t, 2))
	plan := noblePlanFor(&s, 0)
	full := engine.Gems{0, 0, 2, 1, 0, 2}
	lost := full
	lost[engine.Green]--
	if plan.utility(full) <= plan.utility(lost) {
		t.Fatal("gold substitution rewarded losing a useful colored token")
	}
}

func TestNobleFinalNoblePrefersNoExtraCard(t *testing.T) {
	s := nobleFixture()
	s.LastRound = true
	s.NNobles, s.Nobles[0] = 1, 0
	s.Players[0].Points = 12
	s.Players[0].NCards = 8
	s.Players[0].Bonuses = engine.Cost{4, 4, 4, 4, 4}
	s.Players[1].Points = 15
	s.Players[1].NCards = 8
	noblePut(&s, nobleCheap(t, 2))
	a := nobleChoose(t, &s)
	if a.Type == engine.ActBuy {
		t.Fatalf("collecting claims the same noble without losing fewer-cards tie: %+v", a)
	}
}

func TestNobleOwnNobleRemovalClearsOpponentThreat(t *testing.T) {
	s, _ := nobleFinishFixture(t, 0)
	s.Players[0].NCards = 8
	s.Players[0].Bonuses = engine.Cost{0, 0, 4, 4, 0}
	plan := noblePlanFor(&s, 0)
	ns, _ := nobleProject(context.Background(), &s, 0, plan, engine.Action{Type: engine.ActTakeDiff, Gems: engine.Gems{1, 1, 1, 0, 0, 0}, Card: engine.NoCard})
	if noblePublicBest(&ns, 1).points != 12 || ns.NNobles != 0 {
		t.Fatal("opponent threat used an already awarded noble")
	}
}

func TestNobleDeadlockDoesNotWasteReserveSlots(t *testing.T) {
	s := nobleFixture()
	id := nobleCheap(t, 2)
	noblePut(&s, id)
	s.Players[0].NCards = 8
	s.Players[0].Bonuses = engine.Cost{3, 3, 3, 3, 3}
	s.Players[0].Tokens = engine.Gems{2, 2, 2, 2, 2, 0}
	s.Bank[engine.Gold] = 0
	a := nobleChoose(t, &s)
	if a.Type == engine.ActReserve || a.Type == engine.ActBuy || a.Type == engine.ActPass {
		t.Fatalf("no-progress fallback should preserve resources and reserve slots: %+v", a)
	}
}

func TestNobleLegalCompleteGames(t *testing.T) {
	for n := 2; n <= 4; n++ {
		for _, seed := range []uint64{17, 41, 83} {
			t.Run(string(rune('0'+n))+"/"+string(rune('A'+seed%26)), func(t *testing.T) {
				s, err := engine.NewGame(n, seed)
				if err != nil {
					t.Fatal(err)
				}
				for steps := 0; !s.Over(); steps++ {
					if steps > n*engine.MaxRounds*3 {
						t.Fatal("did not finish within unchanged engine round cap")
					}
					a := nobleChoose(t, &s)
					if _, err := s.Apply(a); err != nil {
						t.Fatal(err)
					}
				}
				if s.Winners == 0 || s.Rounds() > engine.MaxRounds {
					t.Fatal("complete legal game has no winner or changed round cap")
				}
				t.Logf("players=%d seed=%d rounds=%d points=%d/%d/%d/%d", n, seed, s.Rounds(), s.Players[0].Points, s.Players[1].Points, s.Players[2].Points, s.Players[3].Points)
			})
		}
	}
}
