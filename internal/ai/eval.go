package ai

import (
	"math"

	"gem/internal/engine"
)

// Evaluator scores positions with a weighted feature model.
type Evaluator struct {
	W Weights
}

// NewEvaluator returns an evaluator with default weights.
func NewEvaluator() *Evaluator { return &Evaluator{W: DefaultWeights} }

const terminalWin = 50.0

// stateCtx caches per-state quantities shared by all players.
type stateCtx struct {
	pr     float64
	bonusW float64
	dem    [engine.NumGemColors]float64
}

func (e *Evaluator) ctx(s *engine.GameState) stateCtx {
	pr := progress(s)
	return stateCtx{pr: pr, bonusW: e.W.BonusEarly*(1-pr) + e.W.BonusLate*pr, dem: demand(s)}
}

// playerValue is the absolute strength of seat.
func (e *Evaluator) playerValue(s *engine.GameState, seat int, sc *stateCtx) float64 {
	w := &e.W
	p := &s.Players[seat]
	v := w.Points * float64(p.Points)
	for col := 0; col < engine.NumGemColors; col++ {
		b := float64(p.Bonuses[col])
		eff := math.Min(b, w.BonusCap) + 0.3*math.Max(0, b-w.BonusCap)
		v += eff * sc.bonusW * (0.6 + 0.4*sc.dem[col])
	}
	// Nobles: closest one counts fully, the second one half.
	best, second := 0.0, 0.0
	for i := 0; i < int(s.NNobles); i++ {
		d := float64(nobleDist(&p.Bonuses, &engine.Nobles[s.Nobles[i]]))
		cl := math.Max(0, 1-d/7)
		val := 3 * w.NobleClose * cl * cl
		if val > best {
			best, second = val, best
		} else if val > second {
			second = val
		}
	}
	v += best + 0.5*second
	// Reachable targets.
	t1, t2 := 0.0, 0.0
	visibleCards(s, seat, func(id int16, reserved bool) {
		c := &engine.Cards[id]
		t := turnsToAfford(p, c, &s.Bank)
		if t >= Unreachable {
			if reserved {
				v -= w.ReserveDead * 2
			}
			return
		}
		if reserved && t >= 4 {
			v -= w.ReserveDead * math.Min(float64(t-3), 4)
		}
		worth := e.cardWorth(s, p, c, sc)
		val := worth * math.Pow(w.TargetDecay, float64(t))
		if reserved {
			val *= 1.15
		}
		if val > t1 {
			t1, t2 = val, t1
		} else if val > t2 {
			t2 = val
		}
	})
	v += w.Target * (t1 + 0.5*t2)
	colored := 0
	for col := 0; col < engine.NumGemColors; col++ {
		colored += int(p.Tokens[col])
	}
	// Tokens beyond 7 have sharply diminishing value (hand limit, idle resources).
	tv := float64(min(colored, 7)) + 0.3*float64(max(colored-7, 0))
	v += w.Token*tv + w.Gold*float64(p.Tokens[engine.Gold])
	return v
}

// cardWorth is the intrinsic worth of owning card c for player p.
func (e *Evaluator) cardWorth(s *engine.GameState, p *engine.PlayerState, c *engine.Card, sc *stateCtx) float64 {
	v := float64(c.Points) * e.W.Points
	if float64(p.Bonuses[c.Bonus]) < e.W.BonusCap {
		v += sc.bonusW * (0.6 + 0.4*sc.dem[c.Bonus])
	}
	for i := 0; i < int(s.NNobles); i++ {
		n := &engine.Nobles[s.Nobles[i]]
		if p.Bonuses[c.Bonus] < n.Req[c.Bonus] {
			d := nobleDist(&p.Bonuses, n)
			if d == 1 {
				v += 3
			} else {
				v += 1.5 / float64(d)
			}
		}
	}
	return v
}

// Values fills raw strength values for every seat.
func (e *Evaluator) Values(s *engine.GameState, out *[4]float64) {
	n := int(s.NPlayers)
	if s.Over() {
		for i := 0; i < n; i++ {
			out[i] = float64(s.Players[i].Points) * 0.1
			if s.IsWinner(i) {
				out[i] += terminalWin
			}
		}
		return
	}
	sc := e.ctx(s)
	for i := 0; i < n; i++ {
		out[i] = e.playerValue(s, i, &sc)
	}
}

// relative turns raw values into a score for seat relative to opponents.
func (e *Evaluator) relative(vals *[4]float64, n, seat int) float64 {
	mx, sum := math.Inf(-1), 0.0
	for i := 0; i < n; i++ {
		if i == seat {
			continue
		}
		sum += vals[i]
		if vals[i] > mx {
			mx = vals[i]
		}
	}
	mean := sum / float64(n-1)
	return vals[seat] - (e.W.Relative*mx + (1-e.W.Relative)*mean)
}

// Eval returns the relative evaluation of s from seat's perspective.
func (e *Evaluator) Eval(s *engine.GameState, seat int) float64 {
	var vals [4]float64
	e.Values(s, &vals)
	return e.relative(&vals, int(s.NPlayers), seat)
}

// WinProbs estimates each seat's win probability.
func (e *Evaluator) WinProbs(s *engine.GameState, out *[4]float64) {
	n := int(s.NPlayers)
	if s.Over() {
		k := 0
		for i := 0; i < n; i++ {
			if s.IsWinner(i) {
				k++
			}
		}
		for i := 0; i < n; i++ {
			out[i] = 0
			if s.IsWinner(i) {
				out[i] = 1 / float64(k)
			}
		}
		return
	}
	var vals [4]float64
	e.Values(s, &vals)
	mx := math.Inf(-1)
	for i := 0; i < n; i++ {
		mx = math.Max(mx, vals[i])
	}
	sum := 0.0
	for i := 0; i < n; i++ {
		out[i] = math.Exp((vals[i] - mx) / e.W.Scale)
		sum += out[i]
	}
	for i := 0; i < n; i++ {
		out[i] /= sum
	}
}

// WinProb is the estimated win probability for seat.
func (e *Evaluator) WinProb(s *engine.GameState, seat int) float64 {
	var p [4]float64
	e.WinProbs(s, &p)
	return p[seat]
}
