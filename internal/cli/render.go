package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"gem/internal/ai"
	"gem/internal/engine"
	"gem/internal/game"
)

var gemColors = [engine.NumColors]lipgloss.Color{"#f5f5f5", "#3b82f6", "#22c55e", "#ef4444", "#a8a29e", "#eab308"}

var (
	stTitle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#facc15"))
	stDim     = lipgloss.NewStyle().Foreground(lipgloss.Color("#8a8a8a"))
	stErr     = lipgloss.NewStyle().Foreground(lipgloss.Color("#f87171")).Bold(true)
	stOK      = lipgloss.NewStyle().Foreground(lipgloss.Color("#4ade80"))
	stHint    = lipgloss.NewStyle().Foreground(lipgloss.Color("#e879f9")).Bold(true)
	stSection = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#4b5563")).Padding(0, 1)
	stCursor  = lipgloss.NewStyle().Reverse(true)
)

func gem(c engine.Color, s string) string {
	return lipgloss.NewStyle().Foreground(gemColors[c]).Bold(true).Render(s)
}

func costLines(cost engine.Cost) []string {
	var parts []string
	for c := 0; c < engine.NumGemColors; c++ {
		if cost[c] > 0 {
			parts = append(parts, gem(engine.Color(c), fmt.Sprintf("%s%d", engine.Color(c).Name(), cost[c])))
		}
	}
	var lines []string
	for i := 0; i < len(parts); i += 2 {
		lines = append(lines, strings.Join(parts[i:min(i+2, len(parts))], " "))
	}
	return lines
}

func gemsLine(g engine.Gems, n int) string {
	var parts []string
	for c := 0; c < n; c++ {
		parts = append(parts, gem(engine.Color(c), fmt.Sprintf("%s%d", engine.Color(c).Name(), g[c])))
	}
	return strings.Join(parts, " ")
}

type boxMark int

const (
	markNone boxMark = iota
	markAfford
	markHint
	markCursor
)

func box(lines []string, mark boxMark) string {
	for len(lines) < 4 {
		lines = append(lines, "")
	}
	st := lipgloss.NewStyle().Width(11).Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#4b5563"))
	switch mark {
	case markAfford:
		st = st.BorderForeground(lipgloss.Color("#22c55e"))
	case markHint:
		st = st.Border(lipgloss.DoubleBorder()).BorderForeground(lipgloss.Color("#e879f9"))
	case markCursor:
		st = st.Border(lipgloss.ThickBorder()).BorderForeground(lipgloss.Color("#facc15"))
	}
	return st.Render(strings.Join(lines[:4], "\n"))
}

func cardLines(cv *engine.CardView) []string {
	if cv == nil {
		return []string{stDim.Render("（空）")}
	}
	if cv.Hidden {
		return []string{fmt.Sprintf("L%d", cv.Level), stDim.Render("暗扣牌")}
	}
	head := gem(cv.Bonus, "◆"+cv.Bonus.Name())
	if cv.Points > 0 {
		head += fmt.Sprintf(" %d分", cv.Points)
	}
	return append([]string{head}, costLines(cv.Cost)...)
}

// hintTarget describes what the top recommendation touches, for highlighting.
func (m *Model) hintTarget() (a engine.Action, ok bool) {
	if h := m.currentHint(); h != nil {
		return h.Candidates[0].Action, true
	}
	return a, false
}

func (m *Model) legalHas(t engine.ActionType, card int16) bool {
	_, ok := m.find(func(a engine.Action) bool { return a.Type == t && a.Card == card })
	return ok
}

func (m *Model) cardBox(row, col int, cv *engine.CardView, buyType engine.ActionType) string {
	lines := cardLines(cv)
	mark := markNone
	ha, hok := m.hintTarget()
	switch {
	case m.row == row && m.col == col:
		mark = markCursor
	case cv != nil && hok && ha.Card == cv.ID && (ha.Type == engine.ActBuy || ha.Type == engine.ActBuyReserved || ha.Type == engine.ActReserve):
		mark = markHint
	case cv != nil && m.legalHas(buyType, cv.ID):
		mark = markAfford
	}
	if cv != nil && !cv.Hidden {
		for len(lines) < 3 {
			lines = append(lines, "")
		}
		tag := ""
		if m.legalHas(buyType, cv.ID) {
			tag = stOK.Render("可买")
		}
		if hok && ha.Card == cv.ID {
			tag = stHint.Render("★" + verb(ha.Type))
		}
		lines = append(lines, tag)
	}
	return box(lines, mark)
}

func verb(t engine.ActionType) string {
	switch t {
	case engine.ActReserve, engine.ActReserveDeck:
		return "预留"
	case engine.ActBuy, engine.ActBuyReserved:
		return "购买"
	}
	return "推荐"
}

func (m *Model) renderBoard() string {
	v := m.view
	ha, hok := m.hintTarget()
	var rows []string
	for row := rowL3; row <= rowL1; row++ {
		lv := 2 - row
		deck := []string{fmt.Sprintf("L%d 牌堆", lv+1), fmt.Sprintf("剩 %d 张", v.DeckLen[lv]), "", stDim.Render("r 盲扣")}
		mark := markNone
		if hok && ha.Type == engine.ActReserveDeck && int(ha.Level) == lv+1 {
			mark, deck[2] = markHint, stHint.Render("★预留")
		}
		if m.row == row && m.col == 0 {
			mark = markCursor
		}
		boxes := []string{box(deck, mark)}
		for i := 0; i < 4; i++ {
			boxes = append(boxes, m.cardBox(row, i+1, v.Board[lv][i], engine.ActBuy))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, boxes...))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (m *Model) renderNobles() string {
	var parts []string
	for _, n := range m.view.Nobles {
		parts = append(parts, fmt.Sprintf("👑%d分 %s", n.Points, strings.Join(costLines(n.Req), " ")))
	}
	if len(parts) == 0 {
		return "贵族：无"
	}
	return "贵族  " + strings.Join(parts, stDim.Render("  │  "))
}

func (m *Model) renderBank() string {
	v := m.view
	ha, hok := m.hintTarget()
	hintTake := hok && (ha.Type == engine.ActTakeDiff || ha.Type == engine.ActTakeSame)
	var parts []string
	for c := 0; c < engine.NumColors; c++ {
		s := gem(engine.Color(c), fmt.Sprintf(" %d.%s %d ", c+1, engine.Color(c).Name(), v.Bank[c]))
		if c == int(engine.Gold) {
			s = gem(engine.Gold, fmt.Sprintf(" 金 %d ", v.Bank[c]))
		}
		if m.row == rowBank && m.col == c {
			s = stCursor.Render(s)
		}
		if m.sel[c] > 0 {
			s += stTitle.Render(fmt.Sprintf("[+%d]", m.sel[c]))
		}
		if hintTake && ha.Gems[c] > 0 {
			s += stHint.Render(fmt.Sprintf("★%d", ha.Gems[c]))
		}
		parts = append(parts, s)
	}
	label := "银行"
	if m.view.Phase == engine.PhaseDiscard && m.myTurn() {
		label = "银行（弃牌中，1-6 选择归还）"
	}
	return label + " " + strings.Join(parts, " ")
}

func (m *Model) renderReserved() string {
	me := m.me()
	if me == nil {
		return ""
	}
	if len(me.Reserved) == 0 {
		return stDim.Render("我的预留：无")
	}
	var boxes []string
	for i, cv := range me.Reserved {
		boxes = append(boxes, m.cardBox(rowReserved, i, cv, engine.ActBuyReserved))
	}
	return "我的预留\n" + lipgloss.JoinHorizontal(lipgloss.Top, boxes...)
}

func (m *Model) seatName(i int) string {
	if m.view != nil && i == m.view.Viewer {
		return "你"
	}
	if m.room != nil && i >= 0 && i < len(m.room.Seats) {
		s := m.room.Seats[i]
		name := s.Name
		if s.Kind == game.SeatAI {
			name = aiName(s)
		}
		if s.Bot {
			name += "[托管]"
		}
		if s.Kind == game.SeatHuman && !s.Connected {
			name += "[离线]"
		}
		return name
	}
	return fmt.Sprintf("玩家%d", i+1)
}

// aiName labels an AI seat with its level unless the server name already does.
func aiName(s game.SeatInfo) string {
	lv := ai.Level(s.Level).Name()
	if strings.Contains(s.Name, lv) {
		return s.Name
	}
	return fmt.Sprintf("%s(AI·%s)", s.Name, lv)
}

func (m *Model) renderPlayers() string {
	v := m.view
	var out []string
	n := len(v.Players)
	start := max(v.Viewer, 0)
	for k := 0; k < n; k++ {
		p := v.Players[(start+k)%n]
		head := "  "
		if p.Seat == v.Current && v.Phase != engine.PhaseOver {
			head = stTitle.Render("▶ ")
		}
		head += fmt.Sprintf("%s  %s  卡%d 贵族%d 预留%d", m.seatName(p.Seat), stTitle.Render(fmt.Sprintf("%d分", p.Points)), p.NCards, len(p.Nobles), len(p.Reserved))
		var bon engine.Gems
		copy(bon[:engine.NumGemColors], p.Bonuses[:])
		out = append(out, head,
			fmt.Sprintf("    筹码 %s  (%d/10)", gemsLine(p.Tokens, engine.NumColors), p.Tokens.Total()),
			"    奖励 "+gemsLine(bon, engine.NumGemColors))
	}
	return strings.Join(out, "\n")
}

func (m *Model) renderHint() string {
	h := m.currentHint()
	if h == nil {
		if m.hintReq != nil {
			return stHint.Render("困难 AI 思考中…")
		}
		if m.room != nil && !m.room.Hints {
			return stDim.Render("本房间未开启提示")
		}
		return stDim.Render("按 h 请求困难 AI 提示")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s 策略：%s  胜率 %.1f%%  (%d 次模拟)\n", stHint.Render("提示"), h.StrategyText, h.WinRate*100, h.Iterations)
	for i, c := range h.Candidates {
		if i >= 3 {
			break
		}
		fmt.Fprintf(&b, "%d. %s  %s\n", i+1, c.Text, stTitle.Render(fmt.Sprintf("%.1f%%", c.WinRate*100)))
		// Keep the panel short: all reasons for the best move, the main one for the others.
		reasons := c.Reasons
		if limit := 1 + 2*boolInt(i == 0); len(reasons) > limit {
			reasons = reasons[:limit]
		}
		for _, r := range reasons {
			fmt.Fprintf(&b, "   · %s\n", r.Text)
		}
		if i == 0 && len(c.PV) > 1 {
			var pv []string
			for _, s := range c.PV[1:] {
				pv = append(pv, m.seatName(s.Seat)+" "+s.Text)
			}
			fmt.Fprintf(&b, "   %s %s\n", stDim.Render("预期："), strings.Join(pv, " → "))
		}
	}
	b.WriteString(stDim.Render("按 y 执行首选"))
	return b.String()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// clip keeps at most n lines (n <= 0: unlimited) so the alt screen never scrolls.
func clip(s string, n int) string {
	if n <= 0 {
		return s
	}
	if lines := strings.Split(s, "\n"); len(lines) > n {
		return strings.Join(lines[:n], "\n")
	}
	return s
}

func verdictStyle(v string) lipgloss.Style {
	switch v {
	case "blunder":
		return stErr
	case "mistake":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#fb923c")).Bold(true)
	}
	return stOK
}

func (m *Model) renderReview() string {
	r := m.review
	if r == nil {
		return ""
	}
	s := fmt.Sprintf("上一手复盘：%s  %s", verdictStyle(r.Verdict).Render(r.VerdictText), r.PlayedText)
	if r.Comment != "" {
		s += "\n  " + r.Comment
	}
	return s
}

func (m *Model) renderLog(n int) string {
	lines := m.logs
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	if len(lines) == 0 {
		return stDim.Render("（暂无记录）")
	}
	return strings.Join(lines, "\n")
}

func (m *Model) help() string {
	if m.over != nil {
		return "q 退出  l 日志"
	}
	if !m.myTurn() {
		return "等待其他玩家…   l 日志  q 退出"
	}
	switch m.view.Phase {
	case engine.PhaseDiscard:
		return "1-6 选择归还的筹码（再按超过持有数清零）  Enter 确认  Esc 清空  h 提示"
	case engine.PhaseNoble:
		return "1-9 选择要迎接的贵族  h 提示"
	}
	return "方向键 移动  1-5/空格 选色  Enter 拿取  b 购买  r 预留/盲扣  Esc 清空  h 提示  y 执行提示  l 日志  q 退出"
}

func (m *Model) renderNoblePick() string {
	if m.view.Phase != engine.PhaseNoble || !m.myTurn() {
		return ""
	}
	var lines []string
	i := 0
	for _, a := range m.view.Legal {
		if a.Type == engine.ActNoble {
			i++
			lines = append(lines, fmt.Sprintf("%d. %s", i, engine.NobleString(a.Noble)))
		}
	}
	return stTitle.Render("可迎接多位贵族，请选择：") + "\n" + strings.Join(lines, "\n")
}

func (m *Model) renderWaiting() string {
	var b strings.Builder
	if m.room == nil {
		b.WriteString("正在连接房间…\n")
	} else {
		r := m.room
		fmt.Fprintf(&b, "%s  #%s  房主 %s\n", stTitle.Render(r.Name), r.ID, r.OwnerName)
		hints := "关闭"
		if r.Hints {
			hints = "开启"
		}
		fmt.Fprintf(&b, "提示：%s  回合计时：%d 秒  观战：%d\n\n", hints, r.TurnSeconds, r.Spectators)
		for _, s := range r.Seats {
			desc := "空位（等待玩家加入）"
			switch s.Kind {
			case game.SeatHuman:
				desc = s.Name
				if !s.Connected {
					desc += " [离线]"
				}
			case game.SeatAI:
				desc = aiName(s)
			case game.SeatClosed:
				desc = stDim.Render("关闭")
			}
			mark := "  "
			if s.Index == r.YourSeat {
				mark = "▶ "
			}
			fmt.Fprintf(&b, "%s座位 %d：%s\n", mark, s.Index+1, desc)
		}
		b.WriteString("\n" + stDim.Render("房主按 s 开始对局（至少 2 人，未坐人的空位开局时移除）  q 离开"))
	}
	return b.String()
}

func (m *Model) renderOver() string {
	o := m.over
	var names []string
	for _, w := range o.Winners {
		names = append(names, m.seatName(w))
	}
	var b strings.Builder
	b.WriteString(stTitle.Render("对局结束！胜者："+strings.Join(names, "、")) + "\n")
	for i, p := range o.Points {
		cards := 0
		if i < len(o.Cards) {
			cards = o.Cards[i]
		}
		fmt.Fprintf(&b, "  %s：%d 分，%d 张发展卡\n", m.seatName(i), p, cards)
	}
	if len(o.Reviews) > 0 {
		count := map[string]int{}
		for _, r := range o.Reviews {
			count[r.Verdict]++
		}
		fmt.Fprintf(&b, "复盘：最佳 %d  良好 %d  失误 %d  严重失误 %d\n", count["best"], count["good"], count["mistake"], count["blunder"])
		for _, r := range o.Reviews {
			if r.Verdict == "mistake" || r.Verdict == "blunder" {
				fmt.Fprintf(&b, "  %s %s → %s\n", verdictStyle(r.Verdict).Render(r.VerdictText), r.PlayedText, r.Comment)
			}
		}
	}
	if m.gameID > 0 {
		fmt.Fprintf(&b, stDim.Render("网页端可在 /games/%d 查看逐步复盘")+"\n", m.gameID)
	}
	return b.String()
}

// View implements tea.Model.
func (m *Model) View() string {
	status := ""
	if m.err != "" {
		status = stErr.Render(m.err)
	} else if m.status != "" {
		status = stDim.Render(m.status)
	}
	if m.view == nil || (m.room != nil && m.room.Status == "waiting") {
		return lipgloss.JoinVertical(lipgloss.Left, stSection.Render(m.renderWaiting()), status)
	}
	if m.showLog {
		n := 40
		if m.height > 4 {
			n = m.height - 4
		}
		return lipgloss.JoinVertical(lipgloss.Left, stTitle.Render("操作日志（l 返回）"), m.renderLog(n))
	}
	v := m.view
	title := fmt.Sprintf("璀璨宝石  第 %d 轮", v.Round)
	if m.room != nil {
		title = fmt.Sprintf("%s #%s  ·  %s", m.room.Name, m.room.ID, title)
	}
	if v.LastRound && v.Phase != engine.PhaseOver {
		title += "  " + stErr.Render("最后一轮！")
	}
	turn := fmt.Sprintf("轮到 %s", m.seatName(v.Current))
	if m.myTurn() {
		turn = stOK.Render("轮到你了")
	}
	left := []string{stTitle.Render(title) + "   " + turn, m.renderNobles(), m.renderBoard(), m.renderBank(), m.renderReserved()}
	if np := m.renderNoblePick(); np != "" {
		left = append(left, np)
	}
	left = append(left, status, stDim.Render(m.help()))

	right := []string{stSection.Render(m.renderPlayers())}
	if m.over != nil {
		right = append(right, stSection.Render(m.renderOver()))
	} else {
		right = append(right, stSection.Width(56).Render(m.renderHint()))
	}
	if r := m.renderReview(); r != "" {
		right = append(right, stSection.Width(56).Render(r))
	}
	right = append(right, stSection.Width(56).Render(stDim.Render("最近动作")+"\n"+m.renderLog(8)))

	l := clip(lipgloss.JoinVertical(lipgloss.Left, left...), m.height)
	r := clip(lipgloss.JoinVertical(lipgloss.Left, right...), m.height)
	if m.width > 0 && m.width < lipgloss.Width(l)+lipgloss.Width(r)+2 {
		return lipgloss.JoinVertical(lipgloss.Left, l, r)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, l, "  ", r)
}
