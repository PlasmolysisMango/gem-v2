package ai

import (
	"fmt"
	"strings"

	"gem/internal/engine"
)

// Strategy labels.
const (
	StratBlock  = "block"
	StratRace   = "race"
	StratNoble  = "noble"
	StratEngine = "engine"
	StratTarget = "target"
)

var strategyText = map[string]string{
	StratBlock:  "预留封锁：对手即将拿下关键高分卡，优先阻断",
	StratRace:   "冲分收官：已进入后期（有人≥10分），只追求最快得分并规划两条买卡路线",
	StratNoble:  "贵族路线：围绕最接近的贵族积累同色奖励",
	StratEngine: "引擎构建：前期优先买低价 L1 卡、覆盖多种颜色，降低后续成本",
	StratTarget: "锁定目标：中期锁定 1-2 张 L2/L3 高分卡，最小化获取回合数",
}

func seatName(seat int) string { return fmt.Sprintf("玩家%d", seat+1) }

// threat describes an opponent that can buy a strong card next turn.
type threat struct {
	seat   int
	card   int16
	points int
	wins   bool
}

// threats lists visible cards opponents could buy on their next turn that give points.
func threats(m *engine.GameState, seat int) []threat {
	var out []threat
	for o := 0; o < int(m.NPlayers); o++ {
		if o == seat {
			continue
		}
		p := &m.Players[o]
		for lv := 0; lv < 3; lv++ {
			for i := 0; i < 4; i++ {
				id := m.Board[lv][i]
				if id < 0 {
					continue
				}
				c := &engine.Cards[id]
				if c.Points < 3 && int(p.Points)+int(c.Points) < engine.WinPoints {
					continue
				}
				if _, ok := p.Affordable(c); ok {
					wins := int(p.Points)+int(c.Points)+nobleGain(m, p, c) >= engine.WinPoints
					out = append(out, threat{seat: o, card: id, points: int(c.Points), wins: wins})
				}
			}
		}
	}
	return out
}

// nobleGain is the noble points p would receive immediately after buying c.
func nobleGain(m *engine.GameState, p *engine.PlayerState, c *engine.Card) int {
	b := p.Bonuses
	b[c.Bonus]++
	for i := 0; i < int(m.NNobles); i++ {
		if nobleDist(&b, &engine.Nobles[m.Nobles[i]]) == 0 {
			return 3
		}
	}
	return 0
}

// strategyOf labels the overall plan for seat.
func (e *Evaluator) strategyOf(m *engine.GameState, seat int) string {
	for _, t := range threats(m, seat) {
		if t.wins {
			return StratBlock
		}
	}
	if maxPoints(m) >= 10 {
		return StratRace
	}
	p := &m.Players[seat]
	for i := 0; i < int(m.NNobles); i++ {
		if nobleDist(&p.Bonuses, &engine.Nobles[m.Nobles[i]]) <= 3 {
			return StratNoble
		}
	}
	if p.NCards < 5 && m.Rounds() < 8 {
		return StratEngine
	}
	return StratTarget
}

func (h *HardAgent) explain(m *engine.GameState, seat int, an *Analysis) {
	an.Strategy = h.E.strategyOf(m, seat)
	an.StrategyText = strategyText[an.Strategy]
	for i := range an.Candidates {
		an.Candidates[i].Reasons = h.E.Reasons(m, seat, an.Candidates[i].Action)
	}
}

// Reasons explains a move for seat using only information visible in the masked state m.
func (e *Evaluator) Reasons(m *engine.GameState, seat int, a engine.Action) []Reason {
	var rs []Reason
	add := func(tag, format string, args ...any) {
		rs = append(rs, Reason{Tag: tag, Text: fmt.Sprintf(format, args...)})
	}
	before := &m.Players[seat]
	after := afterMove(m, a)
	ap := &after.Players[seat]
	opps := threats(m, seat)

	switch a.Type {
	case engine.ActBuy, engine.ActBuyReserved:
		c := &engine.Cards[a.Card]
		if ap.Points >= engine.WinPoints {
			add("WinNow", "购买后达到 %d 分，触发终局", ap.Points)
		}
		if c.Points > 0 {
			add("TempoPoints", "立即获得 %d 分（L%d 卡），符合“每 3 回合约 2 分”的高手节奏", c.Points, c.Level)
		}
		if ap.Nobles != before.Nobles {
			for _, id := range ap.NobleIDs() {
				if before.Nobles&(1<<uint(id)) == 0 {
					add("NobleGet", "凑齐条件，迎接%s +3 分", engine.NobleString(id))
				}
			}
		} else {
			e.nobleProgress(m, before, ap, c.Bonus, add)
		}
		for _, t := range opps {
			if t.card == a.Card {
				add("DenyBuy", "同时抢走了%s下回合可买的这张卡", seatName(t.seat))
				break
			}
		}
		if c.Points == 0 {
			k := 0
			visibleCards(m, seat, func(id int16, _ bool) {
				if id != a.Card && engine.Cards[id].Cost[c.Bonus] > before.Bonuses[c.Bonus] {
					k++
				}
			})
			add("Engine", "低成本扩充%s色引擎（场上 %d 张卡需要%s），后续购买永久折扣", c.Bonus.Name(), k, c.Bonus.Name())
		}
		e.affordNext(m, seat, before, ap, a.Card, add)
		if a.Type == engine.ActBuyReserved {
			add("FreeSlot", "兑现预留卡，同时释放预留位")
		}
	case engine.ActTakeDiff, engine.ActTakeSame:
		e.targetProgress(m, seat, before, ap, add)
		for col := 0; col < engine.NumGemColors; col++ {
			if a.Gems[col] == 0 || after.Bank[col] > 1 {
				continue
			}
			for o := 0; o < int(m.NPlayers); o++ {
				if o == seat {
					continue
				}
				tg := e.topTargets(m, o, 1)
				if len(tg) > 0 {
					need, _ := needVec(&m.Players[o], &engine.Cards[tg[0].id])
					if need[col] > 0 {
						add("ColorChoke", "%s银行仅剩 %d 枚，卡住%s的目标 %s", engine.Color(col).Name(), after.Bank[col], seatName(o), engine.CardString(tg[0].id))
						break
					}
				}
			}
		}
		if before.Tokens.Total()+a.Gems.Total() > engine.MaxTokens {
			add("TokenWaste", "注意：拿取后超过 10 枚需要归还，存在浪费")
		}
	case engine.ActReserve, engine.ActReserveDeck:
		if m.Bank[engine.Gold] > 0 {
			add("GainGold", "获得 1 枚金币（万能色）：高手预留的主要收益往往是金币本身")
		}
		if a.Type == engine.ActReserve {
			blocked := false
			for _, t := range opps {
				if t.card == a.Card {
					blocked = true
					if t.wins {
						add("BlockOpponent", "%s下回合可买 %s 达到 15 分，预留以阻断", seatName(t.seat), engine.CardString(t.card))
					} else {
						add("BlockOpponent", "抢先预留%s下回合可买的 %s（%d 分）", seatName(t.seat), engine.CardString(t.card), t.points)
					}
					break
				}
			}
			c := &engine.Cards[a.Card]
			t := turnsToAfford(ap, c, &after.Bank)
			if t < Unreachable && !blocked {
				add("ReserveTarget", "锁定目标 %s，预计 %d 回合后可购买，对手无法再拿走", engine.CardString(a.Card), t+1)
			}
		} else {
			add("ReserveDeck", "盲扣 L%d 牌堆：不暴露意图，也不会给对手翻出新卡", a.Level)
		}
	case engine.ActDiscard:
		add("Discard", "归还对当前目标最没用的筹码")
	case engine.ActNoble:
		add("NobleGet", "迎接%s +3 分", engine.NobleString(a.Noble))
	}
	// Tempo check: expert pace is about 2 points every 3 turns.
	r := m.Rounds()
	if r >= 6 && float64(before.Points) < float64(r)*2/3-2 && a.Type != engine.ActBuy && a.Type != engine.ActBuyReserved {
		add("Tempo", "当前第 %d 轮仅 %d 分，落后于高手节奏（每 3 回合约 2 分），需尽快转化为分数", r+1, before.Points)
	}
	if len(rs) == 0 {
		add("Overall", "综合模拟评估该着法胜率最高")
	}
	return rs
}

// afterMove applies a on a determinized copy (only used for the mover's own stats and the bank).
func afterMove(m *engine.GameState, a engine.Action) engine.GameState {
	s := engine.Determinize(m, newRNG())
	if s.Validate(a) == nil {
		s.Step(a)
	}
	return s
}

func (e *Evaluator) nobleProgress(m *engine.GameState, before, after *engine.PlayerState, col engine.Color, add func(string, string, ...any)) {
	bestD, best := 99, int8(-1)
	for i := 0; i < int(m.NNobles); i++ {
		n := &engine.Nobles[m.Nobles[i]]
		if n.Req[col] == 0 || before.Bonuses[col] >= n.Req[col] {
			continue
		}
		if d := nobleDist(&after.Bonuses, n); d < bestD {
			bestD, best = d, m.Nobles[i]
		}
	}
	if best < 0 || bestD > 4 {
		return
	}
	n := &engine.Nobles[best]
	var miss []string
	for c := 0; c < engine.NumGemColors; c++ {
		if x := n.Req[c] - after.Bonuses[c]; x > 0 {
			miss = append(miss, fmt.Sprintf("%s%d", engine.Color(c).Name(), x))
		}
	}
	add("NobleProgress", "购买后%s奖励达到 %d，距%s仅差 %s", col.Name(), after.Bonuses[col], engine.NobleString(best), strings.Join(miss, " "))
}

func (e *Evaluator) affordNext(m *engine.GameState, seat int, before, after *engine.PlayerState, skip int16, add func(string, string, ...any)) {
	bestID, bestPts := int16(-1), -1
	visibleCards(m, seat, func(id int16, _ bool) {
		if id == skip {
			return
		}
		c := &engine.Cards[id]
		if _, ok := before.Affordable(c); ok {
			return
		}
		if _, ok := after.Affordable(c); ok && int(c.Points) > bestPts {
			bestID, bestPts = id, int(c.Points)
		}
	})
	if bestID >= 0 {
		add("AffordNext", "下回合可以直接购买 %s", engine.CardString(bestID))
	}
}

func (e *Evaluator) targetProgress(m *engine.GameState, seat int, before, after *engine.PlayerState, add func(string, string, ...any)) {
	tg := e.topTargets(m, seat, 3)
	for _, t := range tg {
		c := &engine.Cards[t.id]
		ta := turnsToAfford(after, c, &m.Bank)
		if ta < t.turns {
			if ta == 0 {
				add("AffordNext", "为 %s 储备，下回合即可购买", engine.CardString(t.id))
			} else {
				add("TargetProgress", "为 %s 储备，购买距离由 %d 回合缩短到 %d 回合", engine.CardString(t.id), t.turns, ta)
			}
			return
		}
	}
	add("Tokens", "补充资源，为后续购买积累筹码")
}
