package ai

import (
	"context"
	"math"
	"sort"

	"gem/internal/engine"
)

// NobleAgent is a stateless, public-information infrastructure/noble strategy.
// Its scores are rule utilities, not probabilities of winning.
type NobleAgent struct{}

func NewNoble() *NobleAgent      { return &NobleAgent{} }
func (*NobleAgent) Level() Level { return Noble }

func (*NobleAgent) Choose(ctx context.Context, m *engine.GameState, seat int) (engine.Action, *Analysis) {
	// Whitelist public inputs. In particular, never carry deck contents or other
	// players' reserve identities into planning, even if a caller supplies them.
	s := noblePublic(m, seat)
	acts := s.LegalActions(nil)
	if len(acts) == 0 {
		return engine.Action{Card: engine.NoCard}, nil // no action exists after game over
	}
	sort.Slice(acts, func(i, j int) bool { return nobleActionLess(acts[i], acts[j]) })
	if ctx.Err() != nil || len(acts) == 1 {
		return acts[0], nil
	}
	if s.Phase == engine.PhaseNoble {
		return engine.Action{Type: engine.ActNoble, Noble: nobleChoice(&s, &s.Players[seat]), Card: engine.NoCard}, nil
	}
	plan := noblePlanFor(&s, seat)
	if s.Phase == engine.PhaseDiscard {
		_, give := plan.retain(ctx, s.Players[seat].Tokens)
		return engine.Action{Type: engine.ActDiscard, Gems: give, Card: engine.NoCard}, nil
	}
	base := plan.utility(s.Players[seat].Tokens)
	best, bestV := acts[0], math.Inf(-1)
	for _, act := range acts {
		if ctx.Err() != nil {
			break
		}
		v := nobleActionScore(ctx, &s, seat, plan, base, act)
		if v > bestV+1e-9 {
			best, bestV = act, v
		}
	}
	return best, nil
}

func nobleActionLess(a, b engine.Action) bool {
	if a.Type != b.Type {
		return a.Type < b.Type
	}
	if a.Card != b.Card {
		return a.Card < b.Card
	}
	if a.Level != b.Level {
		return a.Level < b.Level
	}
	if a.Noble != b.Noble {
		return a.Noble < b.Noble
	}
	for i := range a.Gems {
		if a.Gems[i] != b.Gems[i] {
			return a.Gems[i] > b.Gems[i]
		}
	}
	return false
}

func noblePublic(m *engine.GameState, seat int) engine.GameState {
	s := engine.GameState{
		NPlayers: m.NPlayers, Current: m.Current, Phase: m.Phase, Turn: m.Turn,
		Bank: m.Bank, Board: m.Board, DeckLen: m.DeckLen,
		Nobles: m.Nobles, NNobles: m.NNobles, LastRound: m.LastRound,
	}
	for i := 0; i < int(s.NPlayers); i++ {
		p := &m.Players[i]
		s.Players[i] = engine.PlayerState{Tokens: p.Tokens, Bonuses: p.Bonuses, Points: p.Points, NCards: p.NCards}
		for j := range s.Players[i].Reserved {
			s.Players[i].Reserved[j] = engine.NoCard
		}
		if i == seat {
			s.Players[i].NRes = p.NRes
			s.Players[i].Reserved = p.Reserved
		}
	}
	return s
}

func noblePrinted(c *engine.Card) (total, colors int) {
	for _, n := range c.Cost {
		total += int(n)
		if n > 0 {
			colors++
		}
	}
	return
}

func nobleEarlyCard(c *engine.Card) bool {
	total, colors := noblePrinted(c)
	return c.Level == 1 && (total <= 3 || (total == 4 && colors > 1))
}

func nobleRace(s *engine.GameState, seat int) bool {
	if s.LastRound || s.Players[seat].Points >= 10 {
		return true
	}
	for i := 0; i < int(s.NPlayers); i++ {
		if i != seat && (s.Players[i].Points >= 12 || noblePublicBest(s, i).points >= engine.WinPoints) {
			return true
		}
	}
	return false
}

// nobleBonusUse rewards progress toward the nearest remaining nobles. Faraway
// requirements remain secondary; surplus bonuses with no public use get zero.
func nobleBonusUse(s *engine.GameState, p *engine.PlayerState, col engine.Color) float64 {
	best, second := 0.0, 0.0
	for i := 0; i < int(s.NNobles); i++ {
		n := &engine.Nobles[s.Nobles[i]]
		if p.Bonuses[col] >= n.Req[col] {
			continue
		}
		u := 3.0 / float64(1+nobleDist(&p.Bonuses, n))
		if u > best {
			best, second = u, best
		} else if u > second {
			second = u
		}
	}
	return best + .3*second
}

type nobleTarget struct {
	id    int16
	value float64
	need  engine.Cost
}

type noblePlan struct {
	bonuses engine.Cost
	targets []nobleTarget
}

// Target ordering deliberately excludes held tokens and the bank. The same
// frozen public goals therefore value a take and its subsequent discard, rather
// than re-targeting to justify keeping whichever tokens were just collected.
func noblePlanFor(s *engine.GameState, seat int) noblePlan {
	p := &s.Players[seat]
	race := nobleRace(s, seat)
	early := p.NCards < 3 && !race
	plan := noblePlan{bonuses: p.Bonuses}
	visibleCards(s, seat, func(id int16, reserved bool) {
		c := &engine.Cards[id]
		pay := payTotal(p, c)
		if pay > engine.MaxTokens || (early && !nobleEarlyCard(c)) {
			return
		}
		total, colors := noblePrinted(c)
		v := 9.0 - 1.6*float64(pay) + 2.4*float64(c.Points) + nobleBonusUse(s, p, c.Bonus)
		if early {
			// A whole printed-cost step dominates mixture and noble preference.
			v = 100 - 20*float64(total) + 2*float64(colors) + .4*nobleBonusUse(s, p, c.Bonus)
		} else if race {
			v = 8*float64(c.Points) - 1.3*float64(pay)
			q := *p
			q.Bonuses[c.Bonus]++
			if nobleChoice(s, &q) >= 0 {
				v += 24
			}
		} else if c.Level == 1 {
			v += 2
		}
		if reserved {
			v += .25
		}
		var need engine.Cost
		for col := range need {
			need[col] = max(0, c.Cost[col]-p.Bonuses[col])
		}
		plan.targets = append(plan.targets, nobleTarget{id: id, value: v, need: need})
	})
	sort.Slice(plan.targets, func(i, j int) bool {
		if math.Abs(plan.targets[i].value-plan.targets[j].value) > 1e-9 {
			return plan.targets[i].value > plan.targets[j].value
		}
		return plan.targets[i].id < plan.targets[j].id
	})
	// Keep a small portfolio: balanced useful tokens support the next card too.
	if len(plan.targets) > 3 {
		plan.targets = plan.targets[:3]
	}
	return plan
}

func (plan noblePlan) utility(have engine.Gems) float64 {
	weights := [...]float64{1, .7, .45}
	v, readiness := 0.0, 0.0
	var useful [engine.NumGemColors]bool
	goldUse := 0
	for i, t := range plan.targets {
		_, missing := needVec(&engine.PlayerState{Bonuses: plan.bonuses, Tokens: have}, &engine.Cards[t.id])
		covered, required := 0, 0
		for col, n := range t.need {
			useful[col] = useful[col] || n > 0
			covered += min(int(n), int(have[col]))
			required += int(n)
		}
		gold := min(missing, int(have[engine.Gold]))
		// Option value must not increase when a useful colored token is lost
		// merely because gold now substitutes for it.
		goldUse = max(goldUse, min(required, int(have[engine.Gold])))
		v += weights[i] * (1.15*float64(covered) + float64(gold))
		// Completing a goal beats piling tokens into another monochrome need.
		r := weights[i] * (2.0 / float64(1+max(0, missing-gold)))
		readiness = max(readiness, r)
	}
	for col, ok := range useful {
		if ok && have[col] > 0 {
			v += .25 // first useful token in each color
		}
	}
	// Gold has option value, but keeping useless gold must not force junk buys.
	v += .35*float64(goldUse) + .12*float64(have[engine.Gold])
	return v + readiness
}

// retain exhaustively finds the best kept hand (normally at most three tokens
// overflow). Ties are canonical, independent of which take led to this hand.
func (plan noblePlan) retain(ctx context.Context, have engine.Gems) (engine.Gems, engine.Gems) {
	excess := have.Total() - engine.MaxTokens
	if excess <= 0 {
		return have, engine.Gems{}
	}
	best, kept, cur := engine.Gems{}, have, engine.Gems{}
	bestV := math.Inf(-1)
	var visit func(int, int)
	visit = func(col, left int) {
		if ctx.Err() != nil && !math.IsInf(bestV, -1) {
			return
		}
		if col == engine.NumColors {
			if left != 0 {
				return
			}
			h := have
			for i := range h {
				h[i] -= cur[i]
			}
			v := plan.utility(h)
			if v > bestV+1e-9 {
				bestV, best, kept = v, cur, h
			}
			return
		}
		for n := 0; n <= min(left, int(have[col])); n++ {
			cur[col] = int8(n)
			visit(col+1, left-n)
		}
		cur[col] = 0
	}
	visit(0, excess)
	return kept, best
}

// All nobles are worth three, and only one visits per turn. Consume the lowest
// eligible ID; the identical resulting bonuses make future own compatibility
// independent of this choice. No speculative multi-noble award is made.
func nobleChoice(s *engine.GameState, p *engine.PlayerState) int8 {
	best := int8(-1)
	for i := 0; i < int(s.NNobles); i++ {
		id := s.Nobles[i]
		if nobleDist(&p.Bonuses, &engine.Nobles[id]) == 0 && (best < 0 || id < best) {
			best = id
		}
	}
	return best
}

func nobleRemoveCard(s *engine.GameState, id int16) {
	for lv := range s.Board {
		for i := range s.Board[lv] {
			if s.Board[lv][i] == id {
				s.Board[lv][i] = engine.NoCard // replacement is unknown, not drawn
			}
		}
	}
}

// nobleProject only changes public resources; never invokes engine simulation.
func nobleProject(ctx context.Context, s *engine.GameState, seat int, plan noblePlan, act engine.Action) (engine.GameState, engine.Gems) {
	ns := *s
	p := &ns.Players[seat]
	var pay engine.Gems
	switch act.Type {
	case engine.ActBuy, engine.ActBuyReserved:
		c := &engine.Cards[act.Card]
		pay, _ = p.Affordable(c)
		for col := range pay {
			p.Tokens[col] -= pay[col]
			ns.Bank[col] += pay[col]
		}
		p.Bonuses[c.Bonus]++
		p.NCards++
		p.Points += c.Points
		if act.Type == engine.ActBuy {
			nobleRemoveCard(&ns, act.Card)
		} else {
			for i := 0; i < int(p.NRes); i++ {
				if p.Reserved[i] == act.Card {
					copy(p.Reserved[i:], p.Reserved[i+1:p.NRes])
					p.NRes--
					p.Reserved[p.NRes] = engine.NoCard
					break
				}
			}
		}
	case engine.ActTakeDiff, engine.ActTakeSame:
		for col := range act.Gems {
			p.Tokens[col] += act.Gems[col]
			ns.Bank[col] -= act.Gems[col]
		}
	case engine.ActReserve:
		nobleRemoveCard(&ns, act.Card)
		p.Reserved[p.NRes] = act.Card
		p.NRes++
		if ns.Bank[engine.Gold] > 0 {
			p.Tokens[engine.Gold]++
			ns.Bank[engine.Gold]--
		}
	case engine.ActReserveDeck:
		// No blind-reserve target is invented; legal minimum-harm fallback only.
		p.NRes++
		if ns.Bank[engine.Gold] > 0 {
			p.Tokens[engine.Gold]++
			ns.Bank[engine.Gold]--
		}
	}
	kept, give := plan.retain(ctx, p.Tokens)
	p.Tokens = kept
	for col := range give {
		ns.Bank[col] += give[col]
	}
	if id := nobleChoice(&ns, p); id >= 0 {
		p.Points += engine.Nobles[id].Points
		for i := 0; i < int(ns.NNobles); i++ {
			if ns.Nobles[i] == id {
				copy(ns.Nobles[i:], ns.Nobles[i+1:ns.NNobles])
				ns.NNobles--
				ns.Nobles[ns.NNobles] = -1
				break
			}
		}
	}
	return ns, pay
}

type nobleResult struct{ points, cards int }

func nobleBetter(a, b nobleResult) bool {
	return a.points > b.points || (a.points == b.points && a.cards < b.cards)
}

// Inspect EVERY affordable board buy, including zero/one/two-point cards that
// complete a noble. Opponent hidden (and reserved) cards are never consulted.
func noblePublicBest(s *engine.GameState, seat int) nobleResult {
	p := &s.Players[seat]
	best := nobleResult{int(p.Points), int(p.NCards)}
	if nobleChoice(s, p) >= 0 {
		best.points += 3
	}
	for lv := range s.Board {
		for _, id := range s.Board[lv] {
			if id < 0 {
				continue
			}
			c := &engine.Cards[id]
			if _, ok := p.Affordable(c); !ok {
				continue
			}
			q := *p
			q.Bonuses[c.Bonus]++
			r := nobleResult{int(p.Points + c.Points), int(p.NCards) + 1}
			if nobleChoice(s, &q) >= 0 {
				r.points += 3
			}
			if nobleBetter(r, best) {
				best = r
			}
		}
	}
	return best
}

// Public danger is recomputed after market removal, payment bank returns and
// best discard. Taking denies bank resources; spending may reopen an opponent's
// one-collect finishing plan. Alternative winning buys are not filtered away.
func nobleDanger(s *engine.GameState, seat int, ending bool) float64 {
	me := nobleResult{int(s.Players[seat].Points), int(s.Players[seat].NCards)}
	worst, sum := 0.0, 0.0
	for opp := 0; opp < int(s.NPlayers); opp++ {
		if opp == seat || (ending && opp < seat) {
			continue
		}
		p := &s.Players[opp]
		best := noblePublicBest(s, opp)
		d := 0.0
		if best.points >= engine.WinPoints && (nobleBetter(best, me) || best == me) {
			d = 45 + 3*float64(max(0, best.points-me.points))
		} else if best.points > int(p.Points) {
			d = 1.2 * float64(best.points-int(p.Points))
		}
		if !ending {
			for lv := range s.Board {
				for _, id := range s.Board[lv] {
					if id < 0 {
						continue
					}
					c := &engine.Cards[id]
					q := *p
					q.Bonuses[c.Bonus]++
					points := int(p.Points + c.Points)
					if nobleChoice(s, &q) >= 0 {
						points += 3
					}
					if points >= engine.WinPoints && turnsToAfford(p, c, &s.Bank) == 1 {
						d = max(d, 8.0)
					}
				}
			}
		}
		worst, sum = max(worst, d), sum+d
	}
	return worst + .1*sum
}

func nobleActionScore(ctx context.Context, s *engine.GameState, seat int, plan noblePlan, base float64, act engine.Action) float64 {
	p := &s.Players[seat]
	ns, pay := nobleProject(ctx, s, seat, plan, act)
	q := &ns.Players[seat]
	race := nobleRace(s, seat)
	points := int(q.Points - p.Points)
	gain := plan.utility(q.Tokens) - base
	v := 0.0
	switch act.Type {
	case engine.ActTakeDiff, engine.ActTakeSame:
		v = 2.4*gain - .35
		if gain <= 1e-9 {
			// Equal-value swaps are not progress. In a deadlock the unchanged
			// hand is still less harmful than permanently losing useful resources.
			v = -3 + 2.4*min(0, gain)
			for col := range q.Tokens {
				v -= .03 * math.Abs(float64(q.Tokens[col]-p.Tokens[col]))
			}
		}
	case engine.ActBuy, engine.ActBuyReserved:
		c := &engine.Cards[act.Card]
		bonusUse := nobleBonusUse(s, p, c.Bonus)
		bonusWeight := 1.8
		if p.NCards < 3 && !race {
			bonusWeight = .25
		}
		v = 3.8 + bonusWeight*bonusUse - .85*float64(pay.Total()) - .45*float64(pay[engine.Gold])
		v += .5 * gain // resource opportunity cost, not an overflow reward
		if p.NCards < 3 && !race {
			if !nobleEarlyCard(c) {
				v -= 30
			} else {
				total, colors := noblePrinted(c)
				least := total
				if len(plan.targets) > 0 {
					least, _ = noblePrinted(&engine.Cards[plan.targets[0].id])
				}
				v += 6 - 10*float64(total-least) + .1*float64(colors)
				if colors > 1 {
					v += 1.3
				}
			}
		} else if !race && c.Level == 1 {
			v += 1.3
		}
		// The newly acquired discount must help an actual next public target.
		next := noblePlanFor(&ns, seat)
		use := 0.0
		for i, t := range next.targets {
			x := &engine.Cards[t.id]
			if x.Cost[c.Bonus] > p.Bonuses[c.Bonus] {
				use += 1 / float64(i+1)
			}
		}
		nextWeight := .8
		if p.NCards < 3 && !race {
			nextWeight = .2
		}
		v += nextWeight * use
		if points == 0 && bonusUse == 0 && use == 0 && p.NCards >= 3 {
			v = -5 - .85*float64(pay.Total()) + .5*gain
		}
	case engine.ActReserve:
		c := &engine.Cards[act.Card]
		t := turnsToAfford(q, c, &ns.Bank)
		v = -3.5 + 2*gain - .7*float64(p.NRes)
		if t == 1 && (!(p.NCards < 3 && !race) || nobleEarlyCard(c)) {
			v += 1.2 + .6*float64(c.Points)
		}
		if p.NCards < 3 && !race && !nobleEarlyCard(c) {
			v -= 12
		}
	case engine.ActReserveDeck:
		v = -20 - float64(p.NRes) // only when all known options are worse/absent
	case engine.ActPass:
		v = -50 // engine only offers this as the unique legal main action
	}
	// A market refresh gives opponents a new option without predicting its identity.
	if (act.Type == engine.ActBuy || act.Type == engine.ActReserve) && s.DeckLen[engine.Cards[act.Card].Level-1] > 0 && !s.LastRound {
		v -= .4
	}
	pointWeight := 3.2
	if race {
		pointWeight = 7
	}
	v += pointWeight * float64(points)
	ending := s.LastRound || q.Points >= engine.WinPoints
	if s.LastRound {
		// No future engine/tokens payoff once our final turn is complete.
		v = 100*float64(points) - float64(q.NCards-p.NCards)
		if points == 0 && q.NCards == p.NCards {
			v += .01 * min(gain, 10)
		}
	}
	v -= nobleDanger(&ns, seat, ending)
	if ending {
		// Reaching 15 merely starts the final round. Compare score then fewer
		// cards against public replies of seats that still get their last turn.
		me := nobleResult{int(q.Points), int(q.NCards)}
		ahead := true
		for opp := 0; opp < int(s.NPlayers); opp++ {
			if opp == seat {
				continue
			}
			r := nobleResult{int(ns.Players[opp].Points), int(ns.Players[opp].NCards)}
			if opp > seat {
				r = noblePublicBest(&ns, opp)
			}
			if nobleBetter(r, me) {
				ahead = false
			}
		}
		if ahead {
			v += 120
		} else if !s.LastRound {
			v -= 35 // do not claim a guaranteed win, or rush a losing trigger
		}
	}
	return v
}
