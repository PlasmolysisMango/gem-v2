package cli

import (
	"context"
	"fmt"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"gem/internal/ai"
	"gem/internal/engine"
	"gem/internal/game"
)

// Offline plays a local game: the human is seat 0, AI agents fill the other seats.
// AIs and the hint coach only ever see engine.MaskFor views.
type Offline struct {
	// Delay is the minimum time an AI move is shown as "thinking".
	Delay time.Duration
	// Review grades every main-phase move of the human with the hard AI.
	Review bool

	mu       sync.Mutex
	g        engine.GameState
	levels   []ai.Level
	agents   []ai.Agent
	coachMu  sync.Mutex
	coach    *ai.HardAgent
	hint     *ai.Analysis
	hintTurn int
	reviews  []ai.MoveReview
	send     func(tea.Msg)
	kick     chan struct{}
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// NewOffline creates a game against the given AI levels (1-3 opponents).
// budget is the thinking time of hard AIs and of the hint coach.
func NewOffline(levels []ai.Level, seed uint64, budget time.Duration) (*Offline, error) {
	if len(levels) < 1 || len(levels) > 3 {
		return nil, fmt.Errorf("需要 1-3 个 AI 对手")
	}
	g, err := engine.NewGame(len(levels)+1, seed)
	if err != nil {
		return nil, err
	}
	o := &Offline{Delay: 500 * time.Millisecond, Review: true, g: g, levels: levels,
		coach: ai.NewHard(budget), hintTurn: -1, kick: make(chan struct{}, 1)}
	o.agents = make([]ai.Agent, len(levels)+1)
	for i, l := range levels {
		if l == ai.Hard {
			o.agents[i+1] = ai.NewHard(budget)
		} else {
			o.agents[i+1] = ai.New(l)
		}
	}
	o.ctx, o.cancel = context.WithCancel(context.Background())
	return o, nil
}

func (o *Offline) name(seat int) string {
	if seat == 0 {
		return "你"
	}
	if seat > 0 && seat < len(o.agents) {
		return fmt.Sprintf("AI%d(%s)", seat, o.levels[seat-1].Name())
	}
	return ""
}

func (o *Offline) roomState() game.RoomState {
	rs := game.RoomState{ID: "local", Name: "离线练习", OwnerName: "你", Status: "playing", Hints: true, YourSeat: 0}
	if o.g.Over() {
		rs.Status = "finished"
	}
	for i := range o.agents {
		si := game.SeatInfo{Index: i, Kind: game.SeatHuman, Name: "你", Connected: true}
		if i > 0 {
			si.Kind, si.Level, si.Name = game.SeatAI, string(o.levels[i-1]), fmt.Sprintf("AI%d", i)
		}
		rs.Seats = append(rs.Seats, si)
	}
	return rs
}

// Start implements Backend.
func (o *Offline) Start(send func(tea.Msg)) {
	o.send = send
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		o.mu.Lock()
		rs, v := o.roomState(), o.g.ViewFor(0)
		o.mu.Unlock()
		send(RoomMsg{State: rs})
		send(ViewMsg{View: v})
		send(LogMsg{Lines: []string{"离线对局开始，你是先手"}})
		o.loop()
	}()
}

func (o *Offline) loop() {
	for {
		o.mu.Lock()
		over, cur := o.g.Over(), int(o.g.Current)
		var m engine.GameState
		if !over && cur != 0 {
			m = o.g.MaskFor(cur)
		}
		o.mu.Unlock()
		if over {
			return
		}
		if cur == 0 {
			select {
			case <-o.kick:
				continue
			case <-o.ctx.Done():
				return
			}
		}
		start := time.Now()
		a, _ := o.agents[cur].Choose(o.ctx, &m, cur)
		if d := o.Delay - time.Since(start); d > 0 {
			select {
			case <-time.After(d):
			case <-o.ctx.Done():
			}
		}
		if o.ctx.Err() != nil {
			return
		}
		o.mu.Lock()
		evs, err := o.g.Apply(a)
		o.mu.Unlock()
		if err != nil {
			o.send(ErrMsg{Err: fmt.Sprintf("AI 动作非法：%v", err)})
			return
		}
		o.publish(evs)
	}
}

// publish sends the log lines, the new view and, at the end, the result.
func (o *Offline) publish(evs []engine.Event) {
	o.mu.Lock()
	var lines []string
	for _, e := range evs {
		lines = append(lines, engine.EventText(engine.MaskEvent(e, 0), o.name(e.Seat)))
	}
	v, rs, over := o.g.ViewFor(0), o.roomState(), o.g.Over()
	var res game.GameOverPayload
	if over {
		for i := 0; i < int(o.g.NPlayers); i++ {
			res.Points = append(res.Points, int(o.g.Players[i].Points))
			res.Cards = append(res.Cards, int(o.g.Players[i].NCards))
			if o.g.IsWinner(i) {
				res.Winners = append(res.Winners, i)
			}
		}
	}
	o.mu.Unlock()
	o.send(LogMsg{Lines: lines})
	o.send(ViewMsg{View: v})
	if over {
		o.coachMu.Lock()
		res.Reviews = append([]ai.MoveReview(nil), o.reviews...)
		o.coachMu.Unlock()
		o.send(RoomMsg{State: rs})
		o.send(OverMsg{Over: res})
	}
}

// Act implements Backend.
func (o *Offline) Act(a engine.Action) {
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		o.mu.Lock()
		if o.g.Over() || o.g.Current != 0 {
			o.mu.Unlock()
			o.send(ErrMsg{Err: "还没轮到你"})
			return
		}
		review := o.Review && o.g.Phase == engine.PhaseMain
		before, turn := o.g.MaskFor(0), int(o.g.Turn)
		evs, err := o.g.Apply(a)
		o.mu.Unlock()
		if err != nil {
			o.send(ErrMsg{Err: "非法动作：" + err.Error()})
			return
		}
		o.publish(evs)
		select {
		case o.kick <- struct{}{}:
		default:
		}
		if review {
			o.reviewMove(&before, turn, a)
		}
	}()
}

func (o *Offline) analysis(m *engine.GameState, turn int, explain bool) *ai.Analysis {
	if o.hint != nil && o.hintTurn == turn {
		return o.hint
	}
	an := o.coach.Analyze(o.ctx, m, 0, explain)
	o.hint, o.hintTurn = an, turn
	return an
}

func (o *Offline) reviewMove(before *engine.GameState, turn int, played engine.Action) {
	o.coachMu.Lock()
	defer o.coachMu.Unlock()
	if o.ctx.Err() != nil {
		return
	}
	an := o.analysis(before, turn, false)
	if an == nil || len(an.Candidates) == 0 {
		return
	}
	r := o.coach.Review(before, 0, an, played)
	o.reviews = append(o.reviews, r)
	if o.ctx.Err() == nil {
		o.send(ReviewMsg{Review: r})
	}
}

// Hint implements Backend.
func (o *Offline) Hint() {
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		o.mu.Lock()
		if o.g.Over() || o.g.Current != 0 {
			o.mu.Unlock()
			o.send(ErrMsg{Err: "只能在自己的回合请求提示"})
			return
		}
		m, turn := o.g.MaskFor(0), int(o.g.Turn)
		o.mu.Unlock()
		o.coachMu.Lock()
		an := o.analysis(&m, turn, true)
		o.coachMu.Unlock()
		if o.ctx.Err() == nil {
			o.send(HintMsg{Turn: turn, Analysis: an})
		}
	}()
}

// StartGame implements Backend; offline games start immediately.
func (o *Offline) StartGame() {}

// Close implements Backend and waits for background work to stop.
func (o *Offline) Close() {
	o.cancel()
	o.wg.Wait()
}
