// Package engine implements the rules of the base Splendor game (2-4 players)
// as a pure, allocation-light state machine suitable for both the server and AI search.
package engine

import (
	"encoding/json"
	"fmt"
)

// Color of a gem token or card bonus. Gold is a wildcard token only.
type Color int8

const (
	White Color = iota
	Blue
	Green
	Red
	Black
	Gold
)

// NumGemColors is the number of non-gold colors.
const NumGemColors = 5

// NumColors includes gold.
const NumColors = 6

var colorKeys = [NumColors]string{"white", "blue", "green", "red", "black", "gold"}

// ColorNames are the Chinese display names.
var ColorNames = [NumColors]string{"白", "蓝", "绿", "红", "黑", "金"}

func (c Color) String() string {
	if c < 0 || int(c) >= NumColors {
		return "?"
	}
	return colorKeys[c]
}

// Name returns the Chinese display name.
func (c Color) Name() string {
	if c < 0 || int(c) >= NumColors {
		return "?"
	}
	return ColorNames[c]
}

func (c Color) MarshalJSON() ([]byte, error) { return json.Marshal(c.String()) }

func (c *Color) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		var n int8
		if err2 := json.Unmarshal(b, &n); err2 != nil {
			return err
		}
		*c = Color(n)
		return nil
	}
	for i, k := range colorKeys {
		if k == s {
			*c = Color(i)
			return nil
		}
	}
	return fmt.Errorf("unknown color %q", s)
}

// Gems counts tokens per color: white, blue, green, red, black, gold.
type Gems [NumColors]int8

// Total returns the sum of all tokens.
func (g Gems) Total() int {
	t := 0
	for _, v := range g {
		t += int(v)
	}
	return t
}

// Cost is the price of a card in the five gem colors.
type Cost [NumGemColors]int8

// Card is a development card.
type Card struct {
	ID     int16 `json:"id"`
	Level  int8  `json:"level"`
	Bonus  Color `json:"bonus"`
	Points int8  `json:"points"`
	Cost   Cost  `json:"cost"`
}

// Noble tile, always worth 3 points.
type Noble struct {
	ID     int8 `json:"id"`
	Points int8 `json:"points"`
	Req    Cost `json:"req"`
}

// Card slot sentinels.
const (
	NoCard int16 = -1
	// hiddenBase-level encodes a card reserved face-down whose identity is unknown to the viewer.
	hiddenBase int16 = -10
)

// HiddenCard returns the sentinel for an unknown card of the given level (1..3).
func HiddenCard(level int) int16 { return hiddenBase - int16(level) }

// IsHidden reports whether id is a hidden-card sentinel, and its level.
func IsHidden(id int16) (bool, int) {
	if id <= hiddenBase-1 && id >= hiddenBase-3 {
		return true, int(hiddenBase - id)
	}
	return false, 0
}

// Phase of the current player's turn.
type Phase int8

const (
	PhaseMain Phase = iota
	PhaseDiscard
	PhaseNoble
	PhaseOver
)

var phaseKeys = [...]string{"main", "discard", "noble", "over"}

func (p Phase) String() string { return phaseKeys[p] }

func (p Phase) MarshalJSON() ([]byte, error) { return json.Marshal(p.String()) }

func (p *Phase) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	for i, k := range phaseKeys {
		if k == s {
			*p = Phase(i)
			return nil
		}
	}
	return fmt.Errorf("unknown phase %q", s)
}

// ActionType enumerates player actions.
type ActionType int8

const (
	ActTakeDiff    ActionType = iota // take up to 3 different colors
	ActTakeSame                      // take 2 of the same color
	ActReserve                       // reserve a face-up card
	ActReserveDeck                   // reserve the top card of a deck
	ActBuy                           // buy a face-up card
	ActBuyReserved                   // buy a card from own reserve
	ActDiscard                       // return tokens down to 10
	ActNoble                         // choose a noble when several qualify
	ActPass                          // only legal when nothing else is
)

var actionKeys = [...]string{"take_diff", "take_same", "reserve", "reserve_deck", "buy", "buy_reserved", "discard", "noble", "pass"}

func (t ActionType) String() string { return actionKeys[t] }

func (t ActionType) MarshalJSON() ([]byte, error) { return json.Marshal(t.String()) }

func (t *ActionType) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	for i, k := range actionKeys {
		if k == s {
			*t = ActionType(i)
			return nil
		}
	}
	return fmt.Errorf("unknown action type %q", s)
}

// Action is a comparable value describing a move.
//   - TakeDiff/TakeSame/Discard: Gems holds the token amounts.
//   - Reserve/Buy/BuyReserved: Card is the card id.
//   - ReserveDeck: Level is 1..3.
//   - Noble: Noble is the noble id.
//   - Buy/BuyReserved may carry an explicit payment in Gems when HasPay is set;
//     otherwise the cheapest payment (colored tokens before gold) is used.
type Action struct {
	Type   ActionType `json:"type"`
	Gems   Gems       `json:"gems"`
	Card   int16      `json:"card"`
	Level  int8       `json:"level"`
	Noble  int8       `json:"noble"`
	HasPay bool       `json:"hasPay,omitempty"`
}

// PlayerState is the per-seat state. All fields are fixed-size for cheap copying.
type PlayerState struct {
	Tokens   Gems      `json:"tokens"`
	Bonuses  Cost      `json:"bonuses"`
	Points   int8      `json:"points"`
	Reserved [3]int16  `json:"reserved"`
	NRes     int8      `json:"nReserved"`
	ResDeck  [3]bool   `json:"-"` // reserved from a deck (face-down to others)
	NCards   int8      `json:"nCards"`
	Owned    [2]uint64 `json:"-"` // bitset of owned card ids
	Nobles   uint16    `json:"-"` // bitset of noble ids
}

// OwnedCards lists owned card ids.
func (p *PlayerState) OwnedCards() []int16 {
	var out []int16
	for i := 0; i < NumCards; i++ {
		if p.Owned[i>>6]&(1<<(uint(i)&63)) != 0 {
			out = append(out, int16(i))
		}
	}
	return out
}

// NobleIDs lists acquired noble ids.
func (p *PlayerState) NobleIDs() []int8 {
	var out []int8
	for i := 0; i < NumNobles; i++ {
		if p.Nobles&(1<<uint(i)) != 0 {
			out = append(out, int8(i))
		}
	}
	return out
}

// GameState is the complete game state. It is a plain value: assignment clones it.
type GameState struct {
	NPlayers int8
	Current  int8
	Phase    Phase
	Turn     int16 // number of completed player turns
	Bank     Gems
	Board    [3][4]int16
	Decks    [3][40]int16
	DeckLen  [3]int8
	Nobles   [5]int8
	NNobles  int8
	Players  [4]PlayerState
	Winners  uint8 // bitmask when Phase == PhaseOver
	// LastRound becomes true once some player reached the target score.
	LastRound bool
}

// Clone returns a copy (GameState is a value type; provided for readability).
func (s *GameState) Clone() GameState { return *s }

// WinPoints is the target score that triggers the final round.
const WinPoints = 15

// MaxRounds ends a deadlocked game (everyone hoarding tokens) after this many full rounds;
// the usual score/tiebreak rules decide the winner.
const MaxRounds = 100

// MaxTokens is the hand limit.
const MaxTokens = 10
