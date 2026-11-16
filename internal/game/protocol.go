package game

import (
	"encoding/json"

	"gem/internal/ai"
	"gem/internal/engine"
)

// Envelope is the WebSocket message frame shared by server and clients.
type Envelope struct {
	Type    string          `json:"type"`
	RoomID  string          `json:"roomId,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	ReqID   string          `json:"reqId,omitempty"`
}

// Client -> server message types.
const (
	MsgJoinRoom    = "join_room"
	MsgLeaveRoom   = "leave_room"
	MsgSetSeat     = "set_seat"
	MsgStart       = "start"
	MsgAction      = "action"
	MsgRequestHint = "request_hint"
	MsgChat        = "chat"
)

// Server -> client message types.
const (
	MsgRoomState  = "room_state"
	MsgGameView   = "game_view"
	MsgEvents     = "events"
	MsgHint       = "hint"
	MsgMoveReview = "move_review"
	MsgError      = "error"
	MsgGameOver   = "game_over"
	// MsgChat is also broadcast server -> client.
)

// Seat kinds.
const (
	SeatOpen   = "open"   // waiting for a human
	SeatClosed = "closed" // not used in this game
	SeatHuman  = "human"
	SeatAI     = "ai"
)

// SetSeatPayload changes a seat before the game starts (owner only).
// Kind is open, closed or ai (with Level).
type SetSeatPayload struct {
	Seat  int    `json:"seat"`
	Kind  string `json:"kind"`
	Level string `json:"level,omitempty"`
}

// ActionPayload submits a move.
type ActionPayload struct {
	Action engine.Action `json:"action"`
}

// ChatPayload is a chat message.
type ChatPayload struct {
	From string `json:"from,omitempty"`
	Seat int    `json:"seat"`
	Text string `json:"text"`
	TS   int64  `json:"ts,omitempty"`
}

// SeatInfo describes a seat in room_state.
type SeatInfo struct {
	Index     int    `json:"index"`
	Kind      string `json:"kind"`
	UserID    int64  `json:"userId,omitempty"`
	Name      string `json:"name"`
	Level     string `json:"level,omitempty"`
	Connected bool   `json:"connected"`
	// Bot is set when a medium AI plays for a disconnected human.
	Bot bool `json:"bot,omitempty"`
}

// RoomSettings are chosen at creation.
type RoomSettings struct {
	Name        string   `json:"name"`
	Seats       int      `json:"seats"` // 2-4
	AIs         []string `json:"ai"`    // AI levels for seats after the owner
	Hints       *bool    `json:"hints,omitempty"`
	TurnSeconds int      `json:"turnSeconds"` // 0 = no timer
}

// RoomState is broadcast whenever the room changes.
type RoomState struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	OwnerID     int64      `json:"ownerId"`
	OwnerName   string     `json:"ownerName"`
	Status      string     `json:"status"` // waiting, playing, finished
	Seats       []SeatInfo `json:"seats"`
	Hints       bool       `json:"hints"`
	TurnSeconds int        `json:"turnSeconds"`
	GameID      int64      `json:"gameId,omitempty"`
	YourSeat    int        `json:"yourSeat"` // -1 for spectators
	Spectators  int        `json:"spectators"`
	Deadline    int64      `json:"deadline,omitempty"` // unix ms when the current turn times out
}

// RoomSummary is listed in the lobby.
type RoomSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	OwnerName string `json:"ownerName"`
	Status    string `json:"status"`
	Humans    int    `json:"humans"`
	AIs       int    `json:"ais"`
	Open      int    `json:"open"`
	Seats     int    `json:"seats"`
	Hints     bool   `json:"hints"`
}

// GameViewPayload is a seat-filtered game state.
type GameViewPayload struct {
	GameID int64       `json:"gameId"`
	View   engine.View `json:"view"`
}

// LoggedEvent is an engine event with a Chinese description.
type LoggedEvent struct {
	engine.Event
	Seq  int    `json:"seq"`
	Text string `json:"text"`
}

// EventsPayload carries events produced by one move.
type EventsPayload struct {
	Events []LoggedEvent `json:"events"`
}

// GameOverPayload ends a game.
type GameOverPayload struct {
	GameID  int64           `json:"gameId"`
	Winners []int           `json:"winners"`
	Points  []int           `json:"points"`
	Cards   []int           `json:"cards"`
	Reviews []ai.MoveReview `json:"reviews"` // the receiving player's move reviews (practice mode)
	Curve   []CurvePoint    `json:"curve"`
}

// CurvePoint is the estimated win rate of a player after one of their moves.
type CurvePoint struct {
	Turn    int     `json:"turn"`
	WinRate float64 `json:"winRate"`
	Best    float64 `json:"best"`
}

// HintPayload is the strongest AI's analysis for the requesting player.
type HintPayload struct {
	Turn     int          `json:"turn"`
	Analysis *ai.Analysis `json:"analysis"`
}

// ReviewPayload is a graded move.
type ReviewPayload struct {
	Turn   int           `json:"turn"`
	Review ai.MoveReview `json:"review"`
}

// ErrorPayload reports a failed request.
type ErrorPayload struct {
	Message string `json:"message"`
}
