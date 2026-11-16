package ai

import (
	"math"

	"gem/internal/engine"
)

// Unreachable is returned by turnsToAfford for cards that cannot be bought (e.g. >10 tokens required).
const Unreachable = 99

// needVec returns the tokens still required per color for card c (after bonuses and held tokens, before gold).
func needVec(p *engine.PlayerState, c *engine.Card) (need [engine.NumGemColors]int, total int) {
	for col := 0; col < engine.NumGemColors; col++ {
		n := int(c.Cost[col]) - int(p.Bonuses[col]) - int(p.Tokens[col])
		if n > 0 {
			need[col] = n
			total += n
		}
	}
	return
}

// payTotal is the number of tokens needed to pay for c given bonuses only.
func payTotal(p *engine.PlayerState, c *engine.Card) int {
	t := 0
	for col := 0; col < engine.NumGemColors; col++ {
		if n := int(c.Cost[col]) - int(p.Bonuses[col]); n > 0 {
			t += n
		}
	}
	return t
}

// turnsToAfford estimates how many collecting turns p needs before c can be bought
// (0 = affordable now). It accounts for gold, the 3-different / 2-same take limits,
// bank shortages and the 10-token hand limit.
func turnsToAfford(p *engine.PlayerState, c *engine.Card, bank *engine.Gems) int {
	need, total := needVec(p, c)
	if total == 0 {
		return 0
	}
	gold := int(p.Tokens[engine.Gold])
	if total <= gold {
		return 0
	}
	if payTotal(p, c) > engine.MaxTokens {
		return Unreachable
	}
	for g := gold; g > 0; g-- {
		bi := 0
		for col := 1; col < engine.NumGemColors; col++ {
			if need[col] > need[bi] {
				bi = col
			}
		}
		need[bi]--
	}
	total -= gold
	maxc, maxi := 0, 0
	for col := 0; col < engine.NumGemColors; col++ {
		if need[col] > maxc {
			maxc, maxi = need[col], col
		}
	}
	t := (total + 2) / 3
	if m := (maxc + 1) / 2; m > t {
		t = m
	}
	if maxc >= 2 && bank[maxi] < 4 && maxc > t {
		t = maxc
	}
	for col := 0; col < engine.NumGemColors; col++ {
		if need[col] > int(bank[col]) {
			t += need[col] - int(bank[col])
		}
	}
	if over := p.Tokens.Total() + total - engine.MaxTokens; over > 0 {
		t += (over + 2) / 3
	}
	return t
}

// nobleDist is how many more bonuses p needs for noble n.
func nobleDist(b *engine.Cost, n *engine.Noble) int {
	d := 0
	for col := 0; col < engine.NumGemColors; col++ {
		if x := int(n.Req[col]) - int(b[col]); x > 0 {
			d += x
		}
	}
	return d
}

// progress estimates how far the game is (0 = start, 1 = someone at 15).
func progress(s *engine.GameState) float64 {
	mp := 0
	for i := 0; i < int(s.NPlayers); i++ {
		if p := int(s.Players[i].Points); p > mp {
			mp = p
		}
	}
	pr := float64(mp) / engine.WinPoints
	if r := float64(s.Rounds()) / 26; r > pr {
		pr = r
	}
	return math.Min(pr, 1)
}

// demand measures how much the visible market (cards + nobles) asks for each color, normalized to mean 1.
func demand(s *engine.GameState) [engine.NumGemColors]float64 {
	var d [engine.NumGemColors]float64
	for lv := 0; lv < 3; lv++ {
		for i := 0; i < 4; i++ {
			id := s.Board[lv][i]
			if id < 0 {
				continue
			}
			c := &engine.Cards[id]
			w := 1 + float64(c.Points)*0.5
			for col := 0; col < engine.NumGemColors; col++ {
				d[col] += float64(c.Cost[col]) * w
			}
		}
	}
	for i := 0; i < int(s.NNobles); i++ {
		n := &engine.Nobles[s.Nobles[i]]
		for col := 0; col < engine.NumGemColors; col++ {
			d[col] += float64(n.Req[col]) * 3
		}
	}
	sum := 0.0
	for _, v := range d {
		sum += v
	}
	if sum == 0 {
		return [5]float64{1, 1, 1, 1, 1}
	}
	for col := range d {
		d[col] = d[col] * engine.NumGemColors / sum
	}
	return d
}

// target is a card a player could aim for.
type target struct {
	id    int16
	turns int
	value float64
}

// visibleCards calls fn for every known card the player could buy: board cards and own known reserves.
func visibleCards(s *engine.GameState, seat int, fn func(id int16, reserved bool)) {
	for lv := 0; lv < 3; lv++ {
		for i := 0; i < 4; i++ {
			if id := s.Board[lv][i]; id >= 0 {
				fn(id, false)
			}
		}
	}
	p := &s.Players[seat]
	for i := 0; i < int(p.NRes); i++ {
		if id := p.Reserved[i]; id >= 0 {
			fn(id, true)
		}
	}
}

// topTargets returns the best targets of seat (by discounted worth), sorted by value.
func (e *Evaluator) topTargets(s *engine.GameState, seat int, k int) []target {
	p := &s.Players[seat]
	var out []target
	sc := e.ctx(s)
	visibleCards(s, seat, func(id int16, reserved bool) {
		c := &engine.Cards[id]
		t := turnsToAfford(p, c, &s.Bank)
		if t >= Unreachable {
			return
		}
		worth := e.cardWorth(s, p, c, &sc)
		v := worth * math.Pow(e.W.TargetDecay, float64(t))
		if reserved {
			v *= 1.15
		}
		out = append(out, target{id: id, turns: t, value: v})
	})
	// partial selection sort for top-k
	for i := 0; i < len(out) && i < k; i++ {
		bi := i
		for j := i + 1; j < len(out); j++ {
			if out[j].value > out[bi].value {
				bi = j
			}
		}
		out[i], out[bi] = out[bi], out[i]
	}
	if len(out) > k {
		out = out[:k]
	}
	return out
}
