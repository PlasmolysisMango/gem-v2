package engine

import (
	"fmt"
	"strings"
)

// GemsString renders token counts like "白2 蓝1".
func GemsString(g Gems) string {
	var parts []string
	for c := 0; c < NumColors; c++ {
		if g[c] > 0 {
			parts = append(parts, fmt.Sprintf("%s%d", Color(c).Name(), g[c]))
		}
	}
	if len(parts) == 0 {
		return "无"
	}
	return strings.Join(parts, " ")
}

// CostString renders a cost like "白2 蓝1".
func CostString(c Cost) string {
	var g Gems
	copy(g[:NumGemColors], c[:])
	return GemsString(g)
}

// CardString describes a card, e.g. "L2 蓝卡(2分, 白5 蓝3)".
func CardString(id int16) string {
	if hid, lv := IsHidden(id); hid {
		return fmt.Sprintf("L%d 暗牌", lv)
	}
	if id < 0 || id >= NumCards {
		return "?"
	}
	c := Cards[id]
	if c.Points > 0 {
		return fmt.Sprintf("L%d %s卡(%d分, %s)", c.Level, c.Bonus.Name(), c.Points, CostString(c.Cost))
	}
	return fmt.Sprintf("L%d %s卡(%s)", c.Level, c.Bonus.Name(), CostString(c.Cost))
}

// NobleString describes a noble.
func NobleString(id int8) string {
	if id < 0 || int(id) >= NumNobles {
		return "?"
	}
	return fmt.Sprintf("贵族#%d(%s)", id, CostString(Nobles[id].Req))
}

// Describe renders an action in Chinese.
func Describe(a Action) string {
	switch a.Type {
	case ActTakeDiff:
		var cs []string
		for c := 0; c < NumGemColors; c++ {
			if a.Gems[c] > 0 {
				cs = append(cs, Color(c).Name())
			}
		}
		return "拿取 " + strings.Join(cs, "/")
	case ActTakeSame:
		for c := 0; c < NumGemColors; c++ {
			if a.Gems[c] > 0 {
				return fmt.Sprintf("拿取 2 个%s", Color(c).Name())
			}
		}
	case ActReserve:
		return "预留 " + CardString(a.Card)
	case ActReserveDeck:
		return fmt.Sprintf("盲扣 L%d 牌堆", a.Level)
	case ActBuy:
		return "购买 " + CardString(a.Card)
	case ActBuyReserved:
		return "购买预留的 " + CardString(a.Card)
	case ActDiscard:
		return "归还 " + GemsString(a.Gems)
	case ActNoble:
		return "迎接 " + NobleString(a.Noble)
	case ActPass:
		return "跳过"
	}
	return "?"
}

// EventText renders an event in Chinese; who is the acting player's display name.
func EventText(e Event, who string) string {
	if e.Kind == "game_over" || e.Seat < 0 {
		return "对局结束"
	}
	switch e.Kind {
	case "noble":
		return fmt.Sprintf("%s 迎来 %s（+3 分）", who, NobleString(e.Noble))
	}
	a := e.Action
	if a.Type == ActReserveDeck {
		if e.Card >= 0 {
			return fmt.Sprintf("%s 盲扣 L%d 牌堆，得到 %s", who, a.Level, CardString(e.Card))
		}
		return fmt.Sprintf("%s 盲扣 L%d 牌堆", who, a.Level)
	}
	if a.Type == ActBuy || a.Type == ActBuyReserved {
		txt := fmt.Sprintf("%s %s", who, Describe(a))
		if e.Paid.Total() > 0 {
			txt += "，支付 " + GemsString(e.Paid)
		}
		return txt
	}
	txt := fmt.Sprintf("%s %s", who, Describe(a))
	if e.Gold {
		txt += "，获得 1 金"
	}
	return txt
}
