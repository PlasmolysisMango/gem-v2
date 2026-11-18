// Package game hosts rooms: each room is an actor goroutine that owns its game state,
// talks to connected sessions and schedules AI seats.
package game

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"gem/internal/ai"
	"gem/internal/store"
)

// Config tunes room behaviour.
type Config struct {
	AIDelay      time.Duration // minimum time an AI move takes, for display
	HardBudget   time.Duration // hard AI thinking time per move
	MasterBudget time.Duration // master AI thinking time per move
	HintBudget   time.Duration // analysis time for hints and move reviews
	IdleClose    time.Duration // close rooms with no connected clients after this long (0 = never)
}

// DefaultConfig sets AI budgets, display delay and idle cleanup.
func DefaultConfig() Config {
	return Config{
		AIDelay:      600 * time.Millisecond,
		HardBudget:   300 * time.Millisecond,
		MasterBudget: 1500 * time.Millisecond,
		HintBudget:   1500 * time.Millisecond,
		IdleClose:    168 * time.Hour,
	}
}

// Session is one connected client. Messages are queued without blocking the room.
type Session struct {
	UserID int64
	Name   string
	out    chan Envelope
	once   sync.Once
	done   chan struct{}
}

func NewSession(userID int64, name string) *Session {
	return &Session{UserID: userID, Name: name, out: make(chan Envelope, 256), done: make(chan struct{})}
}

// Out delivers queued messages to the transport.
func (s *Session) Out() <-chan Envelope { return s.out }

// Done is closed when the session is closed.
func (s *Session) Done() <-chan struct{} { return s.done }

// Close marks the session dead; the transport should stop writing.
func (s *Session) Close() { s.once.Do(func() { close(s.done) }) }

// Send queues a message; a client that cannot keep up is disconnected.
func (s *Session) Send(e Envelope) {
	select {
	case <-s.done:
	case s.out <- e:
	default:
		s.Close()
	}
}

func envelope(typ, room string, payload any) Envelope {
	b, _ := json.Marshal(payload)
	return Envelope{Type: typ, RoomID: room, Payload: b}
}

// SendError reports a failure to s.
func (s *Session) SendError(room, reqID, msg string) {
	e := envelope(MsgError, room, ErrorPayload{Message: msg})
	e.ReqID = reqID
	s.Send(e)
}

// Hub owns all rooms.
type Hub struct {
	cfg   Config
	store *store.Store
	mu    sync.Mutex
	rooms map[string]*Room
}

func NewHub(st *store.Store, cfg Config) *Hub {
	return &Hub{cfg: cfg, store: st, rooms: map[string]*Room{}}
}

func (h *Hub) HintBudget() time.Duration { return h.cfg.HintBudget }

var ErrSettings = errors.New("房间设置无效：座位数 2-4，AI 类型为 easy/medium/hard/master/noble")

// Create opens a room owned by the user; seat 0 is reserved for the owner and the
// requested AIs fill the following seats.
func (h *Hub) Create(ownerID int64, ownerName string, set RoomSettings) (*Room, error) {
	if set.Seats == 0 {
		set.Seats = 1 + len(set.AIs)
		if set.Seats < 2 {
			set.Seats = 2
		}
	}
	if set.Seats < 2 || set.Seats > 4 || len(set.AIs) > set.Seats-1 || set.TurnSeconds < 0 {
		return nil, ErrSettings
	}
	seats := make([]seat, set.Seats)
	seats[0] = seat{kind: SeatHuman, userID: ownerID, name: ownerName}
	for i := 1; i < set.Seats; i++ {
		seats[i].kind = SeatOpen
	}
	for i, l := range set.AIs {
		lv, ok := ai.ParseLevel(strings.ToLower(l))
		if !ok {
			return nil, ErrSettings
		}
		seats[i+1] = seat{kind: SeatAI, level: lv}
	}
	hints := len(set.AIs) == set.Seats-1 // practice / AI rooms: hints on by default
	if set.Hints != nil {
		hints = *set.Hints
	}
	name := strings.TrimSpace(set.Name)
	if name == "" {
		name = ownerName + " 的房间"
	}
	if r := []rune(name); len(r) > 30 {
		name = string(r[:30])
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	id := newID()
	for h.rooms[id] != nil {
		id = newID()
	}
	r := newRoom(h, id, name, ownerID, ownerName, seats, hints, set.TurnSeconds)
	if err := r.persistRoom(); err != nil {
		r.cancel()
		return nil, err
	}
	h.rooms[id] = r
	go r.loop()
	return r, nil
}

// Get returns a room or nil.
func (h *Hub) Get(id string) *Room {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.rooms[id]
}

// List returns lobby summaries, open rooms first.
func (h *Hub) List() []RoomSummary {
	h.mu.Lock()
	out := make([]RoomSummary, 0, len(h.rooms))
	for _, r := range h.rooms {
		if s := r.summary.Load(); s != nil {
			out = append(out, *s)
		}
	}
	h.mu.Unlock()
	rank := map[string]int{"waiting": 0, "playing": 1, "finished": 2}
	sort.Slice(out, func(i, j int) bool {
		if rank[out[i].Status] != rank[out[j].Status] {
			return rank[out[i].Status] < rank[out[j].Status]
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (h *Hub) remove(id string) {
	h.mu.Lock()
	delete(h.rooms, id)
	h.mu.Unlock()
}

func (h *Hub) snapshot() []*Room {
	h.mu.Lock()
	defer h.mu.Unlock()
	rooms := make([]*Room, 0, len(h.rooms))
	for _, r := range h.rooms {
		rooms = append(rooms, r)
	}
	return rooms
}

// Close stops all rooms and aborts their games (used for tests and hard shutdown).
func (h *Hub) Close() {
	for _, r := range h.snapshot() {
		r.post(r.close)
		r.wait()
	}
}

// Shutdown stops the room actors without touching persisted rooms or game
// records, so the next server process can restore them.
func (h *Hub) Shutdown() {
	for _, r := range h.snapshot() {
		r.post(r.stop)
		r.wait()
	}
}

func (r *Room) wait() {
	select {
	case <-r.done:
	case <-time.After(2 * time.Second):
	}
}

// SetIdleClose changes the idle cleanup delay at runtime; 0 disables cleanup.
func (h *Hub) SetIdleClose(d time.Duration) {
	h.mu.Lock()
	h.cfg.IdleClose = d
	h.mu.Unlock()
	for _, r := range h.snapshot() {
		r.post(func() { r.setIdleClose(d) })
	}
}

// IdleClose returns the current idle cleanup delay.
func (h *Hub) IdleClose() time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cfg.IdleClose
}

// CloseRoom closes a room immediately (admin action). It reports whether the
// room existed.
func (h *Hub) CloseRoom(id string) bool {
	r := h.Get(id)
	if r == nil {
		return false
	}
	r.post(r.close)
	return true
}

const idAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func newID() string {
	var b [6]byte
	rand.Read(b[:])
	for i := range b {
		b[i] = idAlphabet[int(b[i])%len(idAlphabet)]
	}
	return string(b[:])
}
