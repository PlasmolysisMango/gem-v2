package ai

import (
	"math/rand/v2"
	"sort"

	"gem/internal/engine"
)

// quickCtx caches what the current player is aiming for, used to score actions without applying them.
type quickCtx struct {
	seat    int
	sc      stateCtx
	targets []target
	need    [3][engine.NumGemColors]int
	weight  [3]float64
}

func (e *Evaluator) newQuick(s *engine.GameState) *quickCtx {
	q := &quickCtx{seat: int(s.Current), sc: e.ctx(s)}
	q.targets = e.topTargets(s, q.seat, 3)
	p := &s.Players[q.seat]
	ws := [3]float64{1, 0.6, 0.3}
	for i, t := range q.targets {
		q.need[i], _ = needVec(p, &engine.Cards[t.id])
		q.weight[i] = ws[i]
	}
	return q
}

// colorUse is how useful amt tokens of col are toward the current targets.
func (q *quickCtx) colorUse(col, amt int) float64 {
	u := 0.0
	for i := range q.targets {
		n := q.need[i][col]
		if n > amt {
			n = amt
		}
		u += q.weight[i] * float64(n)
	}
	return u
}

// quickScore estimates the value of a for the current player without applying it.
func (e *Evaluator) quickScore(s *engine.GameState, q *quickCtx, a engine.Action) float64 {
	w := &e.W
	p := &s.Players[q.seat]
	switch a.Type {
	case engine.ActBuy, engine.ActBuyReserved:
		c := &engine.Cards[a.Card]
		pay, _ := p.Affordable(c)
		v := e.cardWorth(s, p, c, &q.sc)
		spent := 0.0
		for col := 0; col < engine.NumGemColors; col++ {
			spent += float64(pay[col]) * w.Token * 0.6
		}
		spent += float64(pay[engine.Gold]) * w.Gold * 0.6
		v -= spent
		if a.Type == engine.ActBuyReserved {
			v += 0.3
		}
		if int(p.Points)+int(c.Points) >= engine.WinPoints {
			v += 10
		}
		return v
	case engine.ActTakeDiff, engine.ActTakeSame:
		v, amt := 0.0, 0
		for col := 0; col < engine.NumGemColors; col++ {
			if n := int(a.Gems[col]); n > 0 {
				amt += n
				v += w.Token * (0.4*float64(n) + q.colorUse(col, n))
			}
		}
		if ex := p.Tokens.Total() + amt - engine.MaxTokens; ex > 0 {
			v -= w.Token * 1.5 * float64(ex)
		}
		return v
	case engine.ActReserve:
		c := &engine.Cards[a.Card]
		v := 0.0
		if s.Bank[engine.Gold] > 0 {
			v += w.Gold + 0.1
		}
		t := turnsToAfford(p, c, &s.Bank)
		if t < Unreachable {
			worth := e.cardWorth(s, p, c, &q.sc)
			v += 0.35 * worth * pow(w.TargetDecay, t)
		}
		if p.NRes == 2 {
			v -= 0.3
		}
		if p.Tokens.Total() >= engine.MaxTokens && s.Bank[engine.Gold] > 0 {
			v -= w.Gold
		}
		return v - 0.2
	case engine.ActReserveDeck:
		v := -0.3 + 0.1*float64(a.Level)
		if s.Bank[engine.Gold] > 0 {
			v += w.Gold
		}
		if p.NRes == 2 {
			v -= 0.3
		}
		return v
	case engine.ActPass:
		return -5
	}
	return 0
}

func pow(b float64, n int) float64 {
	r := 1.0
	for ; n > 0; n-- {
		r *= b
	}
	return r
}

// quickDiscard returns the cheapest tokens to give back: least useful colors first, gold last.
func (e *Evaluator) quickDiscard(s *engine.GameState) engine.Action {
	q := e.newQuick(s)
	p := &s.Players[q.seat]
	excess := p.Tokens.Total() - engine.MaxTokens
	have := p.Tokens
	var g engine.Gems
	for ; excess > 0; excess-- {
		best, bestU := -1, 1e18
		for col := 0; col < engine.NumGemColors; col++ {
			if have[col] == 0 {
				continue
			}
			// marginal usefulness of the last token of this color
			held := int(have[col])
			u := q.colorUse(col, held) - q.colorUse(col, held-1)
			u += 0.01 * float64(-held) // prefer giving back colors we hold many of
			if u < bestU {
				best, bestU = col, u
			}
		}
		if best < 0 {
			best = int(engine.Gold)
		}
		have[best]--
		g[best]++
	}
	return engine.Action{Type: engine.ActDiscard, Gems: g, Card: engine.NoCard}
}

// quickNoble picks the first eligible noble (all nobles are worth 3).
func quickNoble(s *engine.GameState) engine.Action {
	el := s.EligibleNobles(int(s.Current))
	return engine.Action{Type: engine.ActNoble, Noble: el[0], Card: engine.NoCard}
}

// resolvePending finishes discard/noble sub-phases of the current player cheaply.
func (e *Evaluator) resolvePending(s *engine.GameState) {
	for !s.Over() {
		switch s.Phase {
		case engine.PhaseDiscard:
			s.Step(e.quickDiscard(s))
		case engine.PhaseNoble:
			s.Step(quickNoble(s))
		default:
			return
		}
	}
}

// policyAction picks an action for the current player with an epsilon-greedy quick policy.
func (e *Evaluator) policyAction(s *engine.GameState, rng *rand.Rand, eps float64, buf []engine.Action) engine.Action {
	switch s.Phase {
	case engine.PhaseDiscard:
		return e.quickDiscard(s)
	case engine.PhaseNoble:
		return quickNoble(s)
	}
	acts := s.LegalActions(buf)
	if eps > 0 && rng.Float64() < eps {
		return acts[rng.IntN(len(acts))]
	}
	q := e.newQuick(s)
	best, bestV := 0, -1e18
	for i, a := range acts {
		v := e.quickScore(s, q, a) + rng.Float64()*0.05
		if v > bestV {
			best, bestV = i, v
		}
	}
	return acts[best]
}

// scored pairs an action with a value.
type scored struct {
	a engine.Action
	v float64
}

// rankActions orders actions by 1-ply evaluation for seat (the player to move).
func (e *Evaluator) rankActions(s *engine.GameState, acts []engine.Action) []scored {
	seat := int(s.Current)
	out := make([]scored, 0, len(acts))
	for _, a := range acts {
		ns := *s
		ns.Step(a)
		if ns.Current == int8(seat) {
			e.resolvePending(&ns)
		}
		out = append(out, scored{a, e.Eval(&ns, seat)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].v > out[j].v })
	return out
}

// rankQuick orders actions by quick score (cheap, no apply).
func (e *Evaluator) rankQuick(s *engine.GameState, acts []engine.Action) []scored {
	q := e.newQuick(s)
	out := make([]scored, 0, len(acts))
	for _, a := range acts {
		out = append(out, scored{a, e.quickScore(s, q, a)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].v > out[j].v })
	return out
}

// bestSubPhase chooses the best discard/noble action by full evaluation.
func (e *Evaluator) bestSubPhase(s *engine.GameState) engine.Action {
	acts := s.LegalActions(nil)
	r := e.rankActions(s, acts)
	return r[0].a
}
