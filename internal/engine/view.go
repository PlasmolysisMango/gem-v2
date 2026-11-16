package engine

import "math/rand/v2"

// MaskFor returns a copy of s with information hidden from seat removed:
// deck order and other players' face-down reserved cards. seat < 0 hides all reserves.
func (s *GameState) MaskFor(seat int) GameState {
	m := *s
	for lv := 0; lv < 3; lv++ {
		for i := range m.Decks[lv] {
			m.Decks[lv][i] = NoCard
		}
	}
	for p := 0; p < int(m.NPlayers); p++ {
		if p == seat {
			continue
		}
		ps := &m.Players[p]
		for i := 0; i < int(ps.NRes); i++ {
			if ps.ResDeck[i] && ps.Reserved[i] >= 0 {
				ps.Reserved[i] = HiddenCard(int(Cards[ps.Reserved[i]].Level))
			}
		}
	}
	return m
}

// Determinize fills the hidden information of a masked state with a random
// assignment consistent with everything visible. Fully known states are returned unchanged
// except for deck order being re-sampled.
func Determinize(m *GameState, rng *rand.Rand) GameState {
	s := *m
	var seen [NumCards]bool
	for lv := 0; lv < 3; lv++ {
		for i := 0; i < 4; i++ {
			if id := s.Board[lv][i]; id >= 0 {
				seen[id] = true
			}
		}
	}
	for p := 0; p < int(s.NPlayers); p++ {
		ps := &s.Players[p]
		for w := 0; w < 2; w++ {
			bits := ps.Owned[w]
			for bits != 0 {
				b := trailingZeros(bits)
				seen[w*64+b] = true
				bits &= bits - 1
			}
		}
		for i := 0; i < int(ps.NRes); i++ {
			if id := ps.Reserved[i]; id >= 0 {
				seen[id] = true
			}
		}
	}
	var pool [3][]int16
	for lv := 0; lv < 3; lv++ {
		for _, id := range levelCards[lv] {
			if !seen[id] {
				pool[lv] = append(pool[lv], id)
			}
		}
		pl := pool[lv]
		rng.Shuffle(len(pl), func(i, j int) { pl[i], pl[j] = pl[j], pl[i] })
	}
	for p := 0; p < int(s.NPlayers); p++ {
		ps := &s.Players[p]
		for i := 0; i < int(ps.NRes); i++ {
			if hid, lv := IsHidden(ps.Reserved[i]); hid {
				n := len(pool[lv-1])
				ps.Reserved[i] = pool[lv-1][n-1]
				pool[lv-1] = pool[lv-1][:n-1]
			}
		}
	}
	for lv := 0; lv < 3; lv++ {
		n := int(s.DeckLen[lv])
		if n > len(pool[lv]) {
			n = len(pool[lv])
			s.DeckLen[lv] = int8(n)
		}
		for i := range s.Decks[lv] {
			s.Decks[lv][i] = NoCard
		}
		copy(s.Decks[lv][:n], pool[lv][:n])
	}
	return s
}

func trailingZeros(x uint64) int {
	n := 0
	for x&1 == 0 {
		x >>= 1
		n++
	}
	return n
}

// CardView is a card as shown to a client; hidden cards only expose their level.
type CardView struct {
	ID     int16 `json:"id"`
	Level  int8  `json:"level"`
	Bonus  Color `json:"bonus"`
	Points int8  `json:"points"`
	Cost   Cost  `json:"cost"`
	Hidden bool  `json:"hidden,omitempty"`
}

// CardViewOf converts an id (possibly hidden sentinel) into a CardView.
func CardViewOf(id int16) *CardView {
	if id == NoCard {
		return nil
	}
	if hid, lv := IsHidden(id); hid {
		return &CardView{ID: -1, Level: int8(lv), Hidden: true}
	}
	c := Cards[id]
	return &CardView{ID: c.ID, Level: c.Level, Bonus: c.Bonus, Points: c.Points, Cost: c.Cost}
}

// PlayerView is the client representation of a player.
type PlayerView struct {
	Seat     int         `json:"seat"`
	Tokens   Gems        `json:"tokens"`
	Bonuses  Cost        `json:"bonuses"`
	Points   int         `json:"points"`
	Reserved []*CardView `json:"reserved"`
	Cards    []int16     `json:"cards"`
	NCards   int         `json:"nCards"`
	Nobles   []int8      `json:"nobles"`
}

// View is the client representation of a game state from one seat's perspective.
type View struct {
	Viewer    int             `json:"viewer"`
	NPlayers  int             `json:"nPlayers"`
	Current   int             `json:"current"`
	Phase     Phase           `json:"phase"`
	Turn      int             `json:"turn"`
	Round     int             `json:"round"`
	Bank      Gems            `json:"bank"`
	Board     [3][4]*CardView `json:"board"`
	DeckLen   [3]int          `json:"deckLen"`
	Nobles    []Noble         `json:"nobles"`
	Players   []PlayerView    `json:"players"`
	LastRound bool            `json:"lastRound"`
	Winners   []int           `json:"winners"`
	Legal     []Action        `json:"legal,omitempty"`
}

// ViewFor builds the client view for seat (seat < 0 for spectators).
func (s *GameState) ViewFor(seat int) View {
	m := s.MaskFor(seat)
	v := View{
		Viewer: seat, NPlayers: int(m.NPlayers), Current: int(m.Current), Phase: m.Phase,
		Turn: int(m.Turn), Round: m.Rounds() + 1, Bank: m.Bank, LastRound: m.LastRound,
		Nobles: []Noble{}, Players: []PlayerView{}, Winners: []int{},
	}
	for lv := 0; lv < 3; lv++ {
		v.DeckLen[lv] = int(m.DeckLen[lv])
		for i := 0; i < 4; i++ {
			v.Board[lv][i] = CardViewOf(m.Board[lv][i])
		}
	}
	for i := 0; i < int(m.NNobles); i++ {
		v.Nobles = append(v.Nobles, Nobles[m.Nobles[i]])
	}
	for p := 0; p < int(m.NPlayers); p++ {
		ps := &m.Players[p]
		pv := PlayerView{Seat: p, Tokens: ps.Tokens, Bonuses: ps.Bonuses, Points: int(ps.Points),
			NCards: int(ps.NCards), Reserved: []*CardView{}, Cards: ps.OwnedCards(), Nobles: ps.NobleIDs()}
		if pv.Cards == nil {
			pv.Cards = []int16{}
		}
		if pv.Nobles == nil {
			pv.Nobles = []int8{}
		}
		for i := 0; i < int(ps.NRes); i++ {
			pv.Reserved = append(pv.Reserved, CardViewOf(ps.Reserved[i]))
		}
		v.Players = append(v.Players, pv)
	}
	if m.Phase == PhaseOver {
		for p := 0; p < int(m.NPlayers); p++ {
			if m.IsWinner(p) {
				v.Winners = append(v.Winners, p)
			}
		}
	} else if seat == int(m.Current) {
		v.Legal = m.LegalActions(nil)
	}
	return v
}

// MaskEvent hides the identity of a deck-reserved card from viewers other than the actor.
func MaskEvent(e Event, viewer int) Event {
	if e.Action.Type == ActReserveDeck && e.Seat != viewer && e.Card >= 0 {
		e.Card = HiddenCard(int(e.Action.Level))
	}
	return e
}
