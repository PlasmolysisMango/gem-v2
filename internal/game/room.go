package game

import (
	"context"
	"encoding/json"
	"fmt"
	mrand "math/rand/v2"
	"strings"
	"sync/atomic"
	"time"

	"gem/internal/ai"
	"gem/internal/engine"
	"gem/internal/store"
)

type seat struct {
	kind      string
	userID    int64
	name      string
	level     ai.Level
	bot       bool // a medium AI plays for a disconnected human
	discTimer *time.Timer
}

func (s *seat) displayName() string {
	switch s.kind {
	case SeatAI:
		return "AI·" + s.level.Name()
	case SeatHuman:
		return s.name
	}
	return ""
}

type hintEntry struct {
	seq int
	an  *ai.Analysis
}

// Room is an actor: all fields below are owned by the loop goroutine.
type Room struct {
	id      string
	hub     *Hub
	cfg     Config
	inbox   chan func()
	ctx     context.Context
	cancel  context.CancelFunc
	summary atomic.Pointer[RoomSummary]

	name        string
	ownerID     int64
	ownerName   string
	status      string
	seats       []seat
	hints       bool
	turnSeconds int
	clients     map[*Session]int // session -> seat index, -1 for spectators

	game      engine.GameState
	gameID    int64
	seq       int // number of applied actions; identifies a decision point
	agents    []ai.Agent
	bot       *ai.MediumAgent
	hard      *ai.HardAgent
	aiPending int // seq for which an AI move is being computed, -1 if none
	hintCache map[int]hintEntry
	hintBusy  map[int]int // seat -> seq of an in-flight hint analysis
	reviews   map[int][]ai.MoveReview
	curve     map[int][]CurvePoint
	history   []seqEvent // recent unmasked events, replayed to joining clients
	turnTimer *time.Timer
	deadline  time.Time
	idleTimer *time.Timer
	closed    bool
}

func newRoom(h *Hub, id, name string, ownerID int64, ownerName string, seats []seat, hints bool, turnSeconds int) *Room {
	ctx, cancel := context.WithCancel(context.Background())
	r := &Room{
		id: id, hub: h, cfg: h.cfg, inbox: make(chan func(), 256), ctx: ctx, cancel: cancel,
		name: name, ownerID: ownerID, ownerName: ownerName, status: "waiting", seats: seats,
		hints: hints, turnSeconds: turnSeconds, clients: map[*Session]int{}, aiPending: -1,
		hintCache: map[int]hintEntry{}, hintBusy: map[int]int{},
		reviews: map[int][]ai.MoveReview{}, curve: map[int][]CurvePoint{},
	}
	r.hard = ai.NewHard(h.cfg.HintBudget)
	r.bot = ai.NewMedium()
	r.updateSummary()
	r.resetIdle()
	return r
}

// ID returns the room id.
func (r *Room) ID() string { return r.id }

type seqEvent struct {
	ev  engine.Event
	seq int
}

// maxHistory bounds the event history kept for joining clients.
const maxHistory = 300

// loggedFor renders events for one viewer, hiding what that viewer must not see.
func (r *Room) loggedFor(evs []seqEvent, viewer int) []LoggedEvent {
	out := make([]LoggedEvent, 0, len(evs))
	for _, e := range evs {
		me := engine.MaskEvent(e.ev, viewer)
		out = append(out, LoggedEvent{Event: me, Seq: e.seq, Text: r.eventText(me)})
	}
	return out
}

// Summary returns the latest lobby summary.
func (r *Room) Summary() RoomSummary { return *r.summary.Load() }

func (r *Room) post(f func()) {
	select {
	case r.inbox <- f:
	case <-r.ctx.Done():
	}
}

func (r *Room) loop() {
	for {
		select {
		case f := <-r.inbox:
			f()
		case <-r.ctx.Done():
			return
		}
	}
}

// Join attaches a session (new player, reconnecting player or spectator).
func (r *Room) Join(s *Session) { r.post(func() { r.join(s) }) }

// Leave detaches a session. explicit is true for leave_room, false for dropped connections.
func (r *Room) Leave(s *Session, explicit bool) { r.post(func() { r.leave(s, explicit) }) }

// Handle processes a client message addressed to this room.
func (r *Room) Handle(s *Session, e Envelope) { r.post(func() { r.handle(s, e) }) }

func (r *Room) join(s *Session) {
	if r.closed {
		s.SendError(r.id, "", "房间已关闭")
		return
	}
	idx := -1
	for i := range r.seats {
		if r.seats[i].kind == SeatHuman && r.seats[i].userID == s.UserID {
			idx = i
			break
		}
	}
	if idx < 0 && r.status == "waiting" {
		for i := range r.seats {
			if r.seats[i].kind == SeatOpen {
				r.seats[i] = seat{kind: SeatHuman, userID: s.UserID, name: s.Name}
				idx = i
				break
			}
		}
	}
	r.clients[s] = idx
	if idx >= 0 {
		st := &r.seats[idx]
		if st.discTimer != nil {
			st.discTimer.Stop()
			st.discTimer = nil
		}
		if st.bot {
			st.bot = false
			if r.aiPending == r.seq {
				r.aiPending = -1 // result of the bot will be discarded; the human moves now
			}
			r.scheduleTurn()
		}
	}
	if r.idleTimer != nil {
		r.idleTimer.Stop()
		r.idleTimer = nil
	}
	r.broadcastRoom()
	if r.status != "waiting" {
		r.sendView(s, idx)
		s.Send(envelope(MsgEvents, r.id, EventsPayload{Events: r.loggedFor(r.history, idx), Replace: true}))
		if hc, ok := r.hintCache[idx]; ok && idx >= 0 && hc.seq == r.seq {
			s.Send(envelope(MsgHint, r.id, HintPayload{Turn: r.seq, Analysis: hc.an}))
		}
		if r.status == "finished" {
			s.Send(envelope(MsgGameOver, r.id, r.gameOver(idx)))
		}
	}
}

func (r *Room) seatConnected(idx int) bool {
	for _, i := range r.clients {
		if i == idx {
			return true
		}
	}
	return false
}

func (r *Room) leave(s *Session, explicit bool) {
	idx, ok := r.clients[s]
	if !ok {
		return
	}
	delete(r.clients, s)
	if idx >= 0 && !r.seatConnected(idx) {
		st := &r.seats[idx]
		switch r.status {
		case "waiting":
			if explicit && st.userID != r.ownerID {
				*st = seat{kind: SeatOpen}
			} else if explicit {
				r.transferOwner(idx)
			}
		case "playing":
			if explicit {
				r.takeOver(idx)
			} else if st.discTimer == nil && !st.bot {
				uid := st.userID
				st.discTimer = time.AfterFunc(r.cfg.ReconnectGrace, func() {
					r.post(func() {
						if r.seats[idx].userID == uid && !r.seatConnected(idx) {
							r.seats[idx].discTimer = nil
							r.takeOver(idx)
						}
					})
				})
			}
		}
	}
	if len(r.clients) == 0 {
		r.resetIdle()
	}
	if !r.closed {
		r.broadcastRoom()
	}
}

// transferOwner hands the room to another seated human, or closes it.
func (r *Room) transferOwner(old int) {
	r.seats[old] = seat{kind: SeatOpen}
	for i := range r.seats {
		if r.seats[i].kind == SeatHuman {
			r.ownerID, r.ownerName = r.seats[i].userID, r.seats[i].name
			return
		}
	}
	r.close()
}

// takeOver lets a medium AI play for a human seat.
func (r *Room) takeOver(idx int) {
	st := &r.seats[idx]
	if st.bot || r.status != "playing" {
		return
	}
	st.bot = true
	r.broadcastRoom()
	r.scheduleTurn()
}

func (r *Room) resetIdle() {
	if r.idleTimer != nil {
		r.idleTimer.Stop()
	}
	r.idleTimer = time.AfterFunc(r.cfg.IdleClose, func() {
		r.post(func() {
			if len(r.clients) == 0 {
				r.close()
			}
		})
	})
}

func (r *Room) close() {
	if r.closed {
		return
	}
	r.closed = true
	if r.status == "playing" && r.gameID != 0 {
		r.hub.store.FinishGame(context.Background(), r.gameID, "aborted", r.points(), nil)
	}
	for i := range r.seats {
		if r.seats[i].discTimer != nil {
			r.seats[i].discTimer.Stop()
		}
	}
	if r.turnTimer != nil {
		r.turnTimer.Stop()
	}
	if r.idleTimer != nil {
		r.idleTimer.Stop()
	}
	r.status = "closed"
	r.broadcastRoom()
	r.hub.remove(r.id)
	r.cancel()
}

func (r *Room) handle(s *Session, e Envelope) {
	idx, ok := r.clients[s]
	if !ok {
		s.SendError(r.id, e.ReqID, "尚未加入该房间")
		return
	}
	fail := func(msg string) { s.SendError(r.id, e.ReqID, msg) }
	switch e.Type {
	case MsgLeaveRoom:
		r.leave(s, true)
	case MsgChat:
		var p ChatPayload
		if json.Unmarshal(e.Payload, &p) != nil || strings.TrimSpace(p.Text) == "" {
			fail("消息格式错误")
			return
		}
		text := []rune(strings.TrimSpace(p.Text))
		if len(text) > 200 {
			text = text[:200]
		}
		r.broadcast(envelope(MsgChat, r.id, ChatPayload{From: s.Name, Seat: idx, Text: string(text), TS: time.Now().UnixMilli()}))
	case MsgSetSeat:
		var p SetSeatPayload
		if json.Unmarshal(e.Payload, &p) != nil {
			fail("消息格式错误")
			return
		}
		if err := r.setSeat(s, p); err != "" {
			fail(err)
			return
		}
		r.broadcastRoom()
	case MsgStart:
		if err := r.start(s); err != "" {
			fail(err)
		}
	case MsgAction:
		var p ActionPayload
		if json.Unmarshal(e.Payload, &p) != nil {
			fail("动作格式错误")
			return
		}
		if r.status != "playing" {
			fail("对局未在进行")
			return
		}
		if idx < 0 || idx != int(r.game.Current) || r.seats[idx].kind != SeatHuman || r.seats[idx].bot {
			fail("还没轮到你")
			return
		}
		if err := r.applyMove(idx, p.Action, true); err != nil {
			fail("非法动作：" + err.Error())
		}
	case MsgRequestHint:
		r.requestHint(s, idx, e.ReqID)
	default:
		fail("未知消息类型 " + e.Type)
	}
}

func (r *Room) setSeat(s *Session, p SetSeatPayload) string {
	if s.UserID != r.ownerID {
		return "只有房主可以修改座位"
	}
	if r.status != "waiting" {
		return "对局已开始"
	}
	if p.Seat < 0 || p.Seat >= len(r.seats) {
		return "座位不存在"
	}
	st := &r.seats[p.Seat]
	if st.kind == SeatHuman && (st.userID == r.ownerID || r.seatConnected(p.Seat)) {
		return "座位上已有玩家"
	}
	switch p.Kind {
	case SeatOpen, SeatClosed:
		*st = seat{kind: p.Kind}
	case SeatAI:
		lv, ok := ai.ParseLevel(p.Level)
		if !ok {
			return "AI 难度无效"
		}
		*st = seat{kind: SeatAI, level: lv}
	default:
		return "座位类型无效"
	}
	return ""
}

func (r *Room) start(s *Session) string {
	if s.UserID != r.ownerID {
		return "只有房主可以开始游戏"
	}
	if r.status != "waiting" {
		return "对局已开始"
	}
	var seats []seat
	for _, st := range r.seats {
		if st.kind == SeatHuman || st.kind == SeatAI {
			seats = append(seats, st)
		}
	}
	if len(seats) < 2 || len(seats) > 4 {
		return "需要 2-4 名玩家（真人或 AI）才能开始"
	}
	// Seat indices are compacted so game seats match room seats.
	remap := map[int]int{}
	j := 0
	for i, st := range r.seats {
		if st.kind == SeatHuman || st.kind == SeatAI {
			remap[i] = j
			j++
		}
	}
	for c, i := range r.clients {
		if i >= 0 {
			r.clients[c] = remap[i]
		}
	}
	r.seats = seats
	seed := mrand.Uint64() >> 1
	g, err := engine.NewGame(len(seats), seed)
	if err != nil {
		return err.Error()
	}
	players := make([]store.GamePlayer, len(seats))
	r.agents = make([]ai.Agent, len(seats))
	for i, st := range seats {
		players[i].Seat = i
		if st.kind == SeatHuman {
			uid := st.userID
			players[i].UserID = &uid
		} else {
			players[i].AILevel = string(st.level)
			if st.level == ai.Hard {
				r.agents[i] = ai.NewHard(r.cfg.HardBudget)
			} else {
				r.agents[i] = ai.New(st.level)
			}
		}
	}
	id, err := r.hub.store.CreateGame(r.ctx, r.id, seed, players)
	if err != nil {
		return "保存对局失败：" + err.Error()
	}
	r.game, r.gameID, r.seq, r.status = g, id, 0, "playing"
	r.hintCache, r.hintBusy = map[int]hintEntry{}, map[int]int{}
	r.reviews, r.curve = map[int][]ai.MoveReview{}, map[int][]CurvePoint{}
	r.history = nil
	r.broadcastRoom()
	r.broadcastViews()
	r.scheduleTurn()
	return ""
}

func (r *Room) aiControlled(idx int) bool {
	st := &r.seats[idx]
	return st.kind == SeatAI || st.bot
}

// scheduleTurn starts AI thinking or the human turn timer for the current decision.
func (r *Room) scheduleTurn() {
	if r.turnTimer != nil {
		r.turnTimer.Stop()
		r.turnTimer = nil
	}
	r.deadline = time.Time{}
	if r.status != "playing" {
		return
	}
	cur := int(r.game.Current)
	if !r.aiControlled(cur) {
		if r.turnSeconds > 0 {
			seq := r.seq
			r.deadline = time.Now().Add(time.Duration(r.turnSeconds) * time.Second)
			r.turnTimer = time.AfterFunc(time.Duration(r.turnSeconds)*time.Second, func() {
				r.post(func() { r.timeout(seq) })
			})
		}
		return
	}
	if r.aiPending == r.seq {
		return
	}
	r.aiPending = r.seq
	seq := r.seq
	m := r.game.MaskFor(cur)
	agent := r.agents[cur]
	if r.seats[cur].bot || agent == nil {
		agent = r.bot
	}
	ctx := r.ctx
	delay := r.cfg.AIDelay
	go func() {
		start := time.Now()
		tctx, cancel := context.WithTimeout(ctx, r.cfg.HardBudget*2+time.Second)
		a, _ := agent.Choose(tctx, &m, cur)
		cancel()
		if d := delay - time.Since(start); d > 0 {
			select {
			case <-time.After(d):
			case <-ctx.Done():
				return
			}
		}
		r.post(func() {
			if r.seq != seq || r.status != "playing" || !r.aiControlled(cur) {
				return
			}
			r.aiPending = -1
			if err := r.applyMove(cur, a, false); err != nil {
				// Should never happen; fall back to the first legal move.
				r.applyMove(cur, r.game.LegalActions(nil)[0], false)
			}
		})
	}()
}

// timeout makes a quick medium-AI move for a human who ran out of time.
func (r *Room) timeout(seq int) {
	if r.seq != seq || r.status != "playing" {
		return
	}
	cur := int(r.game.Current)
	m := r.game.MaskFor(cur)
	a, _ := r.bot.Choose(r.ctx, &m, cur)
	if r.applyMove(cur, a, false) != nil {
		r.applyMove(cur, r.game.LegalActions(nil)[0], false)
	}
}

// applyMove validates and applies a move, records it and notifies everyone.
func (r *Room) applyMove(idx int, a engine.Action, human bool) error {
	pre := r.game.MaskFor(idx)
	evs, err := r.game.Apply(a)
	if err != nil {
		return err
	}
	seq := r.seq
	r.seq++
	b, _ := json.Marshal(a)
	if err := r.hub.store.AddAction(r.ctx, r.gameID, seq, idx, string(b)); err != nil {
		fmt.Println("store action:", err)
	}
	batch := make([]seqEvent, len(evs))
	for i, ev := range evs {
		batch[i] = seqEvent{ev: ev, seq: seq}
	}
	r.history = append(r.history, batch...)
	if n := len(r.history) - maxHistory; n > 0 {
		r.history = append(r.history[:0:0], r.history[n:]...)
	}
	for c, viewer := range r.clients {
		c.Send(envelope(MsgEvents, r.id, EventsPayload{Events: r.loggedFor(batch, viewer)}))
	}
	if human && r.hints && pre.Phase == engine.PhaseMain {
		r.review(idx, pre, a, seq)
	}
	if r.game.Over() {
		r.finish()
		return nil
	}
	r.broadcastViews()
	r.broadcastRoom()
	r.scheduleTurn()
	return nil
}

func (r *Room) eventText(e engine.Event) string {
	who := ""
	if e.Seat >= 0 && e.Seat < len(r.seats) {
		who = r.seats[e.Seat].displayName()
	}
	return engine.EventText(e, who)
}

func (r *Room) points() []int {
	pts := make([]int, r.game.NPlayers)
	for i := range pts {
		pts[i] = int(r.game.Players[i].Points)
	}
	return pts
}

func (r *Room) finish() {
	r.status = "finished"
	if r.turnTimer != nil {
		r.turnTimer.Stop()
	}
	var winners []int
	for i := 0; i < int(r.game.NPlayers); i++ {
		if r.game.IsWinner(i) {
			winners = append(winners, i)
		}
	}
	if err := r.hub.store.FinishGame(context.Background(), r.gameID, "finished", r.points(), winners); err != nil {
		fmt.Println("store finish:", err)
	}
	r.broadcastViews()
	r.broadcastRoom()
	for c, idx := range r.clients {
		c.Send(envelope(MsgGameOver, r.id, r.gameOver(idx)))
	}
}

func (r *Room) gameOver(idx int) GameOverPayload {
	p := GameOverPayload{GameID: r.gameID, Points: r.points(), Winners: []int{}, Reviews: []ai.MoveReview{}, Curve: []CurvePoint{}}
	for i := 0; i < int(r.game.NPlayers); i++ {
		p.Cards = append(p.Cards, int(r.game.Players[i].NCards))
		if r.game.IsWinner(i) {
			p.Winners = append(p.Winners, i)
		}
	}
	if idx >= 0 {
		p.Reviews = append(p.Reviews, r.reviews[idx]...)
		p.Curve = append(p.Curve, r.curve[idx]...)
	}
	return p
}

func (r *Room) requestHint(s *Session, idx int, reqID string) {
	switch {
	case !r.hints:
		s.SendError(r.id, reqID, "本房间未开启提示")
		return
	case r.status != "playing":
		s.SendError(r.id, reqID, "对局未在进行")
		return
	case idx < 0 || idx != int(r.game.Current) || r.aiControlled(idx):
		s.SendError(r.id, reqID, "只能在自己的回合请求提示")
		return
	}
	if hc, ok := r.hintCache[idx]; ok && hc.seq == r.seq {
		s.Send(envelope(MsgHint, r.id, HintPayload{Turn: r.seq, Analysis: hc.an}))
		return
	}
	if q, ok := r.hintBusy[idx]; ok && q == r.seq {
		return // already computing; the result is sent to all of the seat's sessions
	}
	r.hintBusy[idx] = r.seq
	seq := r.seq
	m := r.game.MaskFor(idx)
	go func() {
		an := r.hard.Analyze(r.ctx, &m, idx, true)
		r.post(func() {
			delete(r.hintBusy, idx)
			r.hintCache[idx] = hintEntry{seq: seq, an: an}
			if r.seq != seq {
				return
			}
			r.sendSeat(idx, envelope(MsgHint, r.id, HintPayload{Turn: seq, Analysis: an}))
		})
	}()
}

// review grades a human move in the background (practice mode).
func (r *Room) review(idx int, pre engine.GameState, a engine.Action, seq int) {
	var an *ai.Analysis
	if hc, ok := r.hintCache[idx]; ok && hc.seq == seq {
		an = hc.an
	}
	turn := int(pre.Turn)
	go func() {
		if an == nil {
			an = r.hard.Analyze(r.ctx, &pre, idx, false)
		}
		if r.ctx.Err() != nil || len(an.Candidates) == 0 {
			return
		}
		rv := r.hard.Review(&pre, idx, an, a)
		r.post(func() {
			r.reviews[idx] = append(r.reviews[idx], rv)
			r.curve[idx] = append(r.curve[idx], CurvePoint{Turn: turn, WinRate: rv.PlayedWinRate, Best: rv.BestWinRate})
			b, _ := json.Marshal(rv)
			r.hub.store.SetReview(context.Background(), r.gameID, seq, string(b))
			r.sendSeat(idx, envelope(MsgMoveReview, r.id, ReviewPayload{Turn: turn, Review: rv}))
		})
	}()
}

func (r *Room) sendSeat(idx int, e Envelope) {
	for c, i := range r.clients {
		if i == idx {
			c.Send(e)
		}
	}
}

func (r *Room) broadcast(e Envelope) {
	for c := range r.clients {
		c.Send(e)
	}
}

func (r *Room) sendView(c *Session, idx int) {
	c.Send(envelope(MsgGameView, r.id, GameViewPayload{GameID: r.gameID, View: r.game.ViewFor(idx)}))
}

func (r *Room) broadcastViews() {
	for c, idx := range r.clients {
		r.sendView(c, idx)
	}
}

func (r *Room) state(yourSeat int) RoomState {
	st := RoomState{ID: r.id, Name: r.name, OwnerID: r.ownerID, OwnerName: r.ownerName, Status: r.status,
		Hints: r.hints, TurnSeconds: r.turnSeconds, GameID: r.gameID, YourSeat: yourSeat}
	for i := range r.seats {
		s := &r.seats[i]
		st.Seats = append(st.Seats, SeatInfo{Index: i, Kind: s.kind, UserID: s.userID, Name: s.displayName(),
			Level: string(s.level), Connected: s.kind == SeatAI || r.seatConnected(i), Bot: s.bot})
	}
	for _, i := range r.clients {
		if i < 0 {
			st.Spectators++
		}
	}
	if !r.deadline.IsZero() {
		st.Deadline = r.deadline.UnixMilli()
	}
	return st
}

func (r *Room) broadcastRoom() {
	for c, idx := range r.clients {
		c.Send(envelope(MsgRoomState, r.id, r.state(idx)))
	}
	r.updateSummary()
}

func (r *Room) updateSummary() {
	s := RoomSummary{ID: r.id, Name: r.name, OwnerName: r.ownerName, Status: r.status, Hints: r.hints}
	for _, st := range r.seats {
		switch st.kind {
		case SeatHuman:
			s.Humans++
		case SeatAI:
			s.AIs++
		case SeatOpen:
			s.Open++
		}
		if st.kind != SeatClosed {
			s.Seats++
		}
	}
	r.summary.Store(&s)
}
