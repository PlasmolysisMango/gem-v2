package cli

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"gem/internal/ai"
	"gem/internal/engine"
	"gem/internal/game"
)

// Cursor rows of the board.
const (
	rowL3 = iota
	rowL2
	rowL1
	rowBank
	rowReserved
	numRows
)

const maxLog = 300

// posKey identifies a decision point so hints are never shown for a stale position.
type posKey struct {
	turn, current int
	phase         engine.Phase
}

// Model is the Bubble Tea model of the game screen.
type Model struct {
	be      Backend
	room    *game.RoomState
	view    *engine.View
	gameID  int64
	logs    []string
	hint    *ai.Analysis
	hintKey posKey
	hintReq *posKey
	review  *ai.MoveReview
	over    *game.GameOverPayload
	status  string
	err     string
	closed  bool

	row, col int
	sel      engine.Gems // tokens chosen to take or to discard
	showLog  bool
	width    int
	height   int
}

// NewModel creates the UI for a backend.
func NewModel(be Backend) *Model { return &Model{be: be, row: rowL1, col: 1} }

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd { return nil }

func keyOf(v *engine.View) posKey {
	return posKey{turn: v.Turn, current: v.Current, phase: v.Phase}
}

func (m *Model) myTurn() bool {
	return m.view != nil && m.view.Viewer >= 0 && m.view.Current == m.view.Viewer &&
		m.view.Phase != engine.PhaseOver && len(m.view.Legal) > 0
}

func (m *Model) me() *engine.PlayerView {
	if m.view == nil || m.view.Viewer < 0 || m.view.Viewer >= len(m.view.Players) {
		return nil
	}
	return &m.view.Players[m.view.Viewer]
}

// currentHint returns the analysis if it belongs to the position on screen.
func (m *Model) currentHint() *ai.Analysis {
	if m.hint == nil || m.view == nil || m.hintKey != keyOf(m.view) || len(m.hint.Candidates) == 0 {
		return nil
	}
	return m.hint
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case RoomMsg:
		st := msg.State
		m.room = &st
	case ViewMsg:
		old := m.view
		v := msg.View
		m.view, m.gameID = &v, msg.GameID
		if old == nil || keyOf(old) != keyOf(&v) {
			m.sel = engine.Gems{}
			m.err = ""
		}
		m.clampCursor()
	case LogMsg:
		if msg.Replace {
			m.logs = nil
		}
		m.logs = append(m.logs, msg.Lines...)
		if len(m.logs) > maxLog {
			m.logs = m.logs[len(m.logs)-maxLog:]
		}
	case HintMsg:
		if m.view != nil && m.hintReq != nil && *m.hintReq == keyOf(m.view) {
			m.hint, m.hintKey = msg.Analysis, *m.hintReq
			m.status = ""
		}
		m.hintReq = nil
	case ReviewMsg:
		r := msg.Review
		m.review = &r
	case OverMsg:
		o := msg.Over
		m.over = &o
		m.status = "对局结束，按 q 退出"
	case ErrMsg:
		m.err = msg.Err
		m.hintReq = nil
	case ClosedMsg:
		m.closed = true
		m.err = "与服务器的连接已断开"
		if msg.Err != nil {
			m.err += "：" + msg.Err.Error()
		}
	case tea.KeyMsg:
		// Fast typing (or a pasted string) can arrive as one message with several runes.
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 1 && !msg.Paste {
			var cmd tea.Cmd
			for _, r := range msg.Runes {
				if c := m.key(string(r)); c != nil {
					cmd = c
				}
			}
			return m, cmd
		}
		return m, m.key(msg.String())
	}
	return m, nil
}

func (m *Model) rowLen(r int) int {
	switch r {
	case rowL3, rowL2, rowL1, rowBank:
		return 5
	case rowReserved:
		if me := m.me(); me != nil {
			return len(me.Reserved)
		}
	}
	return 0
}

func (m *Model) clampCursor() {
	if m.rowLen(m.row) == 0 {
		m.row = rowL1
	}
	if n := m.rowLen(m.row); m.col >= n {
		m.col = n - 1
	}
	if m.col < 0 {
		m.col = 0
	}
}

func (m *Model) move(dr, dc int) {
	if dr != 0 {
		r := m.row
		for i := 0; i < numRows; i++ {
			r = (r + dr + numRows) % numRows
			if m.rowLen(r) > 0 {
				break
			}
		}
		m.row = r
	}
	if n := m.rowLen(m.row); n > 0 && dc != 0 {
		m.col = (m.col + dc + n) % n
	}
	m.clampCursor()
}

// cardAt returns the card under the cursor (nil for decks, bank and empty slots).
func (m *Model) cardAt(row, col int) *engine.CardView {
	if m.view == nil {
		return nil
	}
	switch {
	case row <= rowL1 && col >= 1:
		return m.view.Board[2-row][col-1]
	case row == rowReserved:
		if me := m.me(); me != nil && col < len(me.Reserved) {
			return me.Reserved[col]
		}
	}
	return nil
}

func (m *Model) find(pred func(a engine.Action) bool) (engine.Action, bool) {
	if m.view == nil {
		return engine.Action{}, false
	}
	for _, a := range m.view.Legal {
		if pred(a) {
			return a, true
		}
	}
	return engine.Action{}, false
}

func (m *Model) submit(a engine.Action) {
	m.be.Act(a)
	m.status = "已提交：" + engine.Describe(a)
	m.err = ""
	m.sel = engine.Gems{}
}

func (m *Model) fail(format string, args ...any) { m.err = fmt.Sprintf(format, args...) }

// toggleTake cycles a color in the take selection: none -> 1 -> 2 (if it is the only color) -> none.
func (m *Model) toggleTake(c int) {
	switch {
	case m.sel[c] == 0:
		m.sel[c] = 1
	case m.sel[c] == 1 && m.sel.Total() == 1:
		m.sel[c] = 2
	default:
		m.sel[c] = 0
	}
}

func (m *Model) key(k string) tea.Cmd {
	switch k {
	case "ctrl+c", "q":
		// The caller closes the backend after the program exits; closing here could
		// deadlock with backend goroutines blocked in Program.Send.
		return tea.Quit
	case "l":
		m.showLog = !m.showLog
		return nil
	case "up":
		m.move(-1, 0)
		return nil
	case "down":
		m.move(1, 0)
		return nil
	case "left":
		m.move(0, -1)
		return nil
	case "right":
		m.move(0, 1)
		return nil
	case "esc", "backspace":
		m.sel = engine.Gems{}
		m.err = ""
		return nil
	}
	if m.view == nil || (m.room != nil && m.room.Status == "waiting") {
		if k == "s" {
			m.be.StartGame()
			m.status = "已请求开始对局"
		}
		return nil
	}
	if m.over != nil || m.view.Phase == engine.PhaseOver {
		return nil
	}
	switch k {
	case "h":
		m.requestHint()
		return nil
	case "y":
		if h := m.currentHint(); h != nil && m.myTurn() {
			m.submit(h.Candidates[0].Action)
		} else {
			m.fail("没有可执行的提示，先按 h 请求提示")
		}
		return nil
	}
	if !m.myTurn() {
		if len(k) == 1 || k == "enter" {
			m.fail("还没轮到你")
		}
		return nil
	}
	switch m.view.Phase {
	case engine.PhaseDiscard:
		m.discardKey(k)
	case engine.PhaseNoble:
		m.nobleKey(k)
	default:
		m.mainKey(k)
	}
	return nil
}

func (m *Model) requestHint() {
	if m.room != nil && !m.room.Hints {
		m.fail("本房间未开启提示")
		return
	}
	if !m.myTurn() {
		m.fail("只能在自己的回合请求提示")
		return
	}
	if m.currentHint() != nil {
		return
	}
	k := keyOf(m.view)
	m.hintReq = &k
	m.status = "困难 AI 思考中…"
	m.be.Hint()
}

func (m *Model) discardKey(k string) {
	me := m.me()
	switch {
	case len(k) == 1 && k[0] >= '1' && k[0] <= '6':
		c := int(k[0] - '1')
		if m.sel[c] < me.Tokens[c] {
			m.sel[c]++
		} else {
			m.sel[c] = 0
		}
	case k == "enter":
		sel := m.sel
		if a, ok := m.find(func(a engine.Action) bool { return a.Type == engine.ActDiscard && a.Gems == sel }); ok {
			m.submit(a)
		} else {
			m.fail("需要归还 %d 枚筹码", me.Tokens.Total()-engine.MaxTokens)
		}
	}
}

func (m *Model) nobleKey(k string) {
	if len(k) != 1 || k[0] < '1' || k[0] > '9' {
		return
	}
	i := int(k[0] - '1')
	var nobles []engine.Action
	for _, a := range m.view.Legal {
		if a.Type == engine.ActNoble {
			nobles = append(nobles, a)
		}
	}
	if i < len(nobles) {
		m.submit(nobles[i])
	}
}

func (m *Model) mainKey(k string) {
	switch {
	case len(k) == 1 && k[0] >= '1' && k[0] <= '5':
		m.toggleTake(int(k[0] - '1'))
	case k == " " || k == "space":
		if m.row == rowBank {
			m.toggleTake(m.col)
		}
	case k == "enter" || k == "t":
		m.take()
	case k == "b":
		m.buy()
	case k == "r":
		m.reserve()
	case k == "p":
		if a, ok := m.find(func(a engine.Action) bool { return a.Type == engine.ActPass }); ok {
			m.submit(a)
		} else {
			m.fail("还有其它可行动作，不能跳过")
		}
	}
}

func (m *Model) take() {
	sel := m.sel
	if sel.Total() == 0 {
		m.fail("先用 1-5 或在银行行按空格选择筹码")
		return
	}
	a, ok := m.find(func(a engine.Action) bool {
		return (a.Type == engine.ActTakeDiff || a.Type == engine.ActTakeSame) && a.Gems == sel
	})
	if !ok {
		if sel.Total() == 2 && len(nonZero(sel)) == 1 {
			m.fail("同色拿 2 枚要求该色银行至少 4 枚")
		} else {
			m.fail("不合法的拿取：%s（不同色需拿 3 枚，银行可用颜色不足时可少拿）", engine.GemsString(sel))
		}
		return
	}
	m.submit(a)
}

func nonZero(g engine.Gems) []int {
	var cs []int
	for c, n := range g {
		if n > 0 {
			cs = append(cs, c)
		}
	}
	return cs
}

func (m *Model) buy() {
	cv := m.cardAt(m.row, m.col)
	if cv == nil || cv.Hidden {
		m.fail("把光标移到明牌或自己的预留卡上再按 b 购买")
		return
	}
	typ := engine.ActBuy
	if m.row == rowReserved {
		typ = engine.ActBuyReserved
	}
	a, ok := m.find(func(a engine.Action) bool { return a.Type == typ && a.Card == cv.ID })
	if !ok {
		m.fail("买不起 %s", engine.CardString(cv.ID))
		return
	}
	m.submit(a)
}

func (m *Model) reserve() {
	if m.row > rowL1 {
		m.fail("把光标移到明牌或牌堆上再按 r 预留")
		return
	}
	var a engine.Action
	var ok bool
	if m.col == 0 {
		lv := int8(3 - m.row)
		a, ok = m.find(func(a engine.Action) bool { return a.Type == engine.ActReserveDeck && a.Level == lv })
	} else if cv := m.cardAt(m.row, m.col); cv != nil {
		a, ok = m.find(func(a engine.Action) bool { return a.Type == engine.ActReserve && a.Card == cv.ID })
	}
	if !ok {
		m.fail("不能预留（最多 3 张，或该位置没有牌）")
		return
	}
	m.submit(a)
}
