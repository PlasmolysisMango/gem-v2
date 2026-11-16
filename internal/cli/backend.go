// Package cli implements the terminal client: a Bubble Tea board UI driven either by
// the server's WebSocket protocol (online) or by a local engine with AI opponents (offline).
package cli

import (
	tea "github.com/charmbracelet/bubbletea"

	"gem/internal/ai"
	"gem/internal/engine"
	"gem/internal/game"
)

// Backend feeds game state into the UI and carries the player's requests.
// All methods must return promptly; results arrive as tea messages through send.
type Backend interface {
	Start(send func(tea.Msg))
	Act(a engine.Action)
	Hint()
	StartGame()
	Close()
}

// RoomMsg carries the room (seats, settings, status).
type RoomMsg struct{ State game.RoomState }

// ViewMsg carries the seat-filtered game state.
type ViewMsg struct {
	GameID int64
	View   engine.View
}

// LogMsg appends lines to the action log, or replaces it with a resent history.
type LogMsg struct {
	Lines   []string
	Replace bool
}

// HintMsg is the strongest AI's analysis for the player's turn.
type HintMsg struct {
	Turn     int
	Analysis *ai.Analysis
}

// ReviewMsg grades the player's last move.
type ReviewMsg struct{ Review ai.MoveReview }

// OverMsg ends the game.
type OverMsg struct{ Over game.GameOverPayload }

// ErrMsg reports a failed request.
type ErrMsg struct{ Err string }

// ClosedMsg reports a lost connection.
type ClosedMsg struct{ Err error }
