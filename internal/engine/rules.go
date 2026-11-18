package engine

import (
	"errors"
	"fmt"
	"math/rand/v2"
)

// Setup parameters by player count.
func tokensPerColor(n int) int8 {
	switch n {
	case 2:
		return 4
	case 3:
		return 5
	default:
		return 7
	}
}

// RandomFirstBit marks seeds whose starting seat is derived from the seed.
// Room seeds set it so restarts and replays reproduce the same first player;
// seeds recorded before this bit existed never set it and still start at seat 0.
const RandomFirstBit = uint64(1) << 63

// NewGame creates a shuffled game for n (2..4) players using seed.
func NewGame(n int, seed uint64) (GameState, error) {
	if n < 2 || n > 4 {
		return GameState{}, fmt.Errorf("player count must be 2-4, got %d", n)
	}
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	var s GameState
	s.NPlayers = int8(n)
	if seed&RandomFirstBit != 0 {
		s.Current = int8(seed % uint64(n))
	}
	t := tokensPerColor(n)
	for c := 0; c < NumGemColors; c++ {
		s.Bank[c] = t
	}
	s.Bank[Gold] = 5
	for lv := 0; lv < 3; lv++ {
		ids := append([]int16(nil), levelCards[lv]...)
		rng.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
		for i := range s.Decks[lv] {
			s.Decks[lv][i] = NoCard
		}
		copy(s.Decks[lv][:], ids)
		s.DeckLen[lv] = int8(len(ids))
		for i := 0; i < 4; i++ {
			s.Board[lv][i] = s.draw(lv)
		}
	}
	nobles := rng.Perm(NumNobles)
	s.NNobles = int8(n + 1)
	for i := range s.Nobles {
		s.Nobles[i] = -1
	}
	for i := 0; i < n+1; i++ {
		s.Nobles[i] = int8(nobles[i])
	}
	for p := range s.Players {
		for i := range s.Players[p].Reserved {
			s.Players[p].Reserved[i] = NoCard
		}
	}
	return s, nil
}

// draw takes the top card of a deck level index (0..2), or NoCard.
func (s *GameState) draw(lv int) int16 {
	if s.DeckLen[lv] == 0 {
		return NoCard
	}
	s.DeckLen[lv]--
	id := s.Decks[lv][s.DeckLen[lv]]
	s.Decks[lv][s.DeckLen[lv]] = NoCard
	return id
}

// Over reports whether the game has ended.
func (s *GameState) Over() bool { return s.Phase == PhaseOver }

// Cur returns the current player's state.
func (s *GameState) Cur() *PlayerState { return &s.Players[s.Current] }

// Affordable reports whether p can buy card c, and the default payment.
func (p *PlayerState) Affordable(c *Card) (Gems, bool) {
	var pay Gems
	need := 0
	for col := 0; col < NumGemColors; col++ {
		req := c.Cost[col] - p.Bonuses[col]
		if req <= 0 {
			continue
		}
		if p.Tokens[col] >= req {
			pay[col] = req
		} else {
			pay[col] = p.Tokens[col]
			need += int(req - p.Tokens[col])
		}
	}
	if need > int(p.Tokens[Gold]) {
		return pay, false
	}
	pay[Gold] = int8(need)
	return pay, true
}

// validPayment checks an explicit payment for card c.
func (p *PlayerState) validPayment(c *Card, pay Gems) bool {
	gold := int(pay[Gold])
	if pay[Gold] < 0 || pay[Gold] > p.Tokens[Gold] {
		return false
	}
	for col := 0; col < NumGemColors; col++ {
		if pay[col] < 0 || pay[col] > p.Tokens[col] {
			return false
		}
		req := c.Cost[col] - p.Bonuses[col]
		if req < 0 {
			req = 0
		}
		if pay[col] > req {
			return false
		}
		gold -= int(req - pay[col])
	}
	return gold == 0
}

// HasCard reports whether the player owns card id.
func (p *PlayerState) HasCard(id int16) bool {
	return p.Owned[id>>6]&(1<<(uint(id)&63)) != 0
}

// EligibleNobles returns the board noble indices the current player qualifies for.
func (s *GameState) EligibleNobles(seat int) []int8 {
	var out []int8
	p := &s.Players[seat]
	for i := 0; i < int(s.NNobles); i++ {
		n := &Nobles[s.Nobles[i]]
		ok := true
		for c := 0; c < NumGemColors; c++ {
			if p.Bonuses[c] < n.Req[c] {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, s.Nobles[i])
		}
	}
	return out
}

// LegalActions appends all legal actions to buf and returns it.
func (s *GameState) LegalActions(buf []Action) []Action {
	buf = buf[:0]
	p := s.Cur()
	switch s.Phase {
	case PhaseOver:
		return buf
	case PhaseDiscard:
		excess := p.Tokens.Total() - MaxTokens
		var g Gems
		return discardCombos(buf, &p.Tokens, &g, 0, excess)
	case PhaseNoble:
		for _, id := range s.EligibleNobles(int(s.Current)) {
			buf = append(buf, Action{Type: ActNoble, Noble: id, Card: NoCard})
		}
		return buf
	}
	// Take different colors.
	var avail [NumGemColors]int
	na := 0
	for c := 0; c < NumGemColors; c++ {
		if s.Bank[c] > 0 {
			avail[na] = c
			na++
		}
	}
	switch {
	case na >= 3:
		for i := 0; i < na; i++ {
			for j := i + 1; j < na; j++ {
				for k := j + 1; k < na; k++ {
					var g Gems
					g[avail[i]], g[avail[j]], g[avail[k]] = 1, 1, 1
					buf = append(buf, Action{Type: ActTakeDiff, Gems: g, Card: NoCard})
				}
			}
		}
	case na > 0:
		var g Gems
		for i := 0; i < na; i++ {
			g[avail[i]] = 1
		}
		buf = append(buf, Action{Type: ActTakeDiff, Gems: g, Card: NoCard})
	}
	for c := 0; c < NumGemColors; c++ {
		if s.Bank[c] >= 4 {
			var g Gems
			g[c] = 2
			buf = append(buf, Action{Type: ActTakeSame, Gems: g, Card: NoCard})
		}
	}
	// Reserve.
	if p.NRes < 3 {
		for lv := 0; lv < 3; lv++ {
			for i := 0; i < 4; i++ {
				if id := s.Board[lv][i]; id >= 0 {
					buf = append(buf, Action{Type: ActReserve, Card: id})
				}
			}
			if s.DeckLen[lv] > 0 {
				buf = append(buf, Action{Type: ActReserveDeck, Level: int8(lv + 1), Card: NoCard})
			}
		}
	}
	// Buy.
	for lv := 0; lv < 3; lv++ {
		for i := 0; i < 4; i++ {
			if id := s.Board[lv][i]; id >= 0 {
				if _, ok := p.Affordable(&Cards[id]); ok {
					buf = append(buf, Action{Type: ActBuy, Card: id})
				}
			}
		}
	}
	for i := 0; i < int(p.NRes); i++ {
		id := p.Reserved[i]
		if id >= 0 {
			if _, ok := p.Affordable(&Cards[id]); ok {
				buf = append(buf, Action{Type: ActBuyReserved, Card: id})
			}
		}
	}
	if len(buf) == 0 {
		buf = append(buf, Action{Type: ActPass, Card: NoCard})
	}
	return buf
}

func discardCombos(buf []Action, have *Gems, cur *Gems, from, left int) []Action {
	if left == 0 {
		buf = append(buf, Action{Type: ActDiscard, Gems: *cur, Card: NoCard})
		return buf
	}
	for c := from; c < NumColors; c++ {
		if cur[c] < have[c] {
			cur[c]++
			buf = discardCombos(buf, have, cur, c, left-1)
			cur[c]--
		}
	}
	return buf
}

// Event records something that happened, for logs and UIs.
type Event struct {
	Kind   string `json:"kind"` // action, noble, reveal, game_over
	Seat   int    `json:"seat"`
	Action Action `json:"action"`
	Card   int16  `json:"card"`  // bought/reserved card id (resolved for deck reserves, may be hidden in views)
	Noble  int8   `json:"noble"` // acquired noble id
	Paid   Gems   `json:"paid"`
	Gold   bool   `json:"gold"` // received a gold token
}

var (
	ErrNotLegal = errors.New("illegal action")
)

// Validate checks that a is legal in s.
func (s *GameState) Validate(a Action) error {
	p := s.Cur()
	switch s.Phase {
	case PhaseOver:
		return fmt.Errorf("%w: game over", ErrNotLegal)
	case PhaseDiscard:
		if a.Type != ActDiscard {
			return fmt.Errorf("%w: must discard", ErrNotLegal)
		}
		excess := p.Tokens.Total() - MaxTokens
		if a.Gems.Total() != excess {
			return fmt.Errorf("%w: must return exactly %d tokens", ErrNotLegal, excess)
		}
		for c := 0; c < NumColors; c++ {
			if a.Gems[c] < 0 || a.Gems[c] > p.Tokens[c] {
				return fmt.Errorf("%w: invalid discard", ErrNotLegal)
			}
		}
		return nil
	case PhaseNoble:
		if a.Type != ActNoble {
			return fmt.Errorf("%w: must choose a noble", ErrNotLegal)
		}
		for _, id := range s.EligibleNobles(int(s.Current)) {
			if id == a.Noble {
				return nil
			}
		}
		return fmt.Errorf("%w: noble not available", ErrNotLegal)
	}
	switch a.Type {
	case ActTakeDiff:
		na := 0
		for c := 0; c < NumGemColors; c++ {
			if s.Bank[c] > 0 {
				na++
			}
		}
		want := min(3, na)
		n := 0
		if a.Gems[Gold] != 0 {
			return fmt.Errorf("%w: cannot take gold", ErrNotLegal)
		}
		for c := 0; c < NumGemColors; c++ {
			switch a.Gems[c] {
			case 0:
			case 1:
				if s.Bank[c] < 1 {
					return fmt.Errorf("%w: %s not available", ErrNotLegal, Color(c).Name())
				}
				n++
			default:
				return fmt.Errorf("%w: take one of each color", ErrNotLegal)
			}
		}
		if n != want || n == 0 {
			return fmt.Errorf("%w: must take %d different colors", ErrNotLegal, want)
		}
		return nil
	case ActTakeSame:
		n := 0
		for c := 0; c < NumColors; c++ {
			if a.Gems[c] == 0 {
				continue
			}
			if c == int(Gold) || a.Gems[c] != 2 || s.Bank[c] < 4 {
				return fmt.Errorf("%w: take 2 same requires 4+ in bank", ErrNotLegal)
			}
			n++
		}
		if n != 1 {
			return fmt.Errorf("%w: take 2 of exactly one color", ErrNotLegal)
		}
		return nil
	case ActReserve:
		if p.NRes >= 3 {
			return fmt.Errorf("%w: at most 3 reserved cards", ErrNotLegal)
		}
		if _, _, ok := s.boardPos(a.Card); !ok {
			return fmt.Errorf("%w: card not on board", ErrNotLegal)
		}
		return nil
	case ActReserveDeck:
		if p.NRes >= 3 {
			return fmt.Errorf("%w: at most 3 reserved cards", ErrNotLegal)
		}
		if a.Level < 1 || a.Level > 3 || s.DeckLen[a.Level-1] == 0 {
			return fmt.Errorf("%w: deck empty", ErrNotLegal)
		}
		return nil
	case ActBuy, ActBuyReserved:
		if a.Type == ActBuy {
			if _, _, ok := s.boardPos(a.Card); !ok {
				return fmt.Errorf("%w: card not on board", ErrNotLegal)
			}
		} else if p.reservedIndex(a.Card) < 0 {
			return fmt.Errorf("%w: card not reserved", ErrNotLegal)
		}
		c := &Cards[a.Card]
		if a.HasPay {
			if !p.validPayment(c, a.Gems) {
				return fmt.Errorf("%w: invalid payment", ErrNotLegal)
			}
		} else if _, ok := p.Affordable(c); !ok {
			return fmt.Errorf("%w: cannot afford", ErrNotLegal)
		}
		return nil
	case ActPass:
		var buf [64]Action
		la := s.LegalActions(buf[:0])
		if len(la) == 1 && la[0].Type == ActPass {
			return nil
		}
		return fmt.Errorf("%w: cannot pass", ErrNotLegal)
	}
	return fmt.Errorf("%w: unexpected action in main phase", ErrNotLegal)
}

func (s *GameState) boardPos(id int16) (int, int, bool) {
	if id < 0 || id >= NumCards {
		return 0, 0, false
	}
	lv := int(Cards[id].Level) - 1
	for i := 0; i < 4; i++ {
		if s.Board[lv][i] == id {
			return lv, i, true
		}
	}
	return 0, 0, false
}

func (p *PlayerState) reservedIndex(id int16) int {
	if id < 0 {
		return -1
	}
	for i := 0; i < int(p.NRes); i++ {
		if p.Reserved[i] == id {
			return i
		}
	}
	return -1
}

// Apply validates and applies a, returning the produced events.
func (s *GameState) Apply(a Action) ([]Event, error) {
	if err := s.Validate(a); err != nil {
		return nil, err
	}
	var ev []Event
	s.apply(a, &ev)
	return ev, nil
}

// Step applies an action already known to be legal (no validation, no events). Used by AI search.
func (s *GameState) Step(a Action) { s.apply(a, nil) }

func (s *GameState) apply(a Action, ev *[]Event) {
	seat := int(s.Current)
	p := &s.Players[seat]
	e := Event{Kind: "action", Seat: seat, Action: a, Card: a.Card, Noble: -1}
	switch a.Type {
	case ActTakeDiff, ActTakeSame:
		for c := 0; c < NumGemColors; c++ {
			p.Tokens[c] += a.Gems[c]
			s.Bank[c] -= a.Gems[c]
		}
	case ActReserve, ActReserveDeck:
		var id int16
		if a.Type == ActReserve {
			lv, i, _ := s.boardPos(a.Card)
			id = a.Card
			s.Board[lv][i] = s.draw(lv)
			p.ResDeck[p.NRes] = false
		} else {
			id = s.draw(int(a.Level) - 1)
			p.ResDeck[p.NRes] = true
		}
		p.Reserved[p.NRes] = id
		p.NRes++
		e.Card = id
		if s.Bank[Gold] > 0 {
			s.Bank[Gold]--
			p.Tokens[Gold]++
			e.Gold = true
		}
	case ActBuy, ActBuyReserved:
		c := &Cards[a.Card]
		pay := a.Gems
		if !a.HasPay {
			pay, _ = p.Affordable(c)
		}
		for col := 0; col < NumColors; col++ {
			p.Tokens[col] -= pay[col]
			s.Bank[col] += pay[col]
		}
		e.Paid = pay
		if a.Type == ActBuy {
			lv, i, _ := s.boardPos(a.Card)
			s.Board[lv][i] = s.draw(lv)
		} else {
			i := p.reservedIndex(a.Card)
			for j := i; j < int(p.NRes)-1; j++ {
				p.Reserved[j] = p.Reserved[j+1]
				p.ResDeck[j] = p.ResDeck[j+1]
			}
			p.NRes--
			p.Reserved[p.NRes] = NoCard
			p.ResDeck[p.NRes] = false
		}
		p.Bonuses[c.Bonus]++
		p.Points += c.Points
		p.NCards++
		p.Owned[a.Card>>6] |= 1 << (uint(a.Card) & 63)
	case ActDiscard:
		for col := 0; col < NumColors; col++ {
			p.Tokens[col] -= a.Gems[col]
			s.Bank[col] += a.Gems[col]
		}
	case ActNoble:
		if ev != nil {
			*ev = append(*ev, e)
		}
		s.takeNoble(seat, a.Noble, ev)
		s.endTurn(ev)
		return
	case ActPass:
	}
	if ev != nil {
		*ev = append(*ev, e)
	}
	if p.Tokens.Total() > MaxTokens {
		s.Phase = PhaseDiscard
		return
	}
	// Nobles visit at the end of the turn.
	el := s.EligibleNobles(seat)
	switch len(el) {
	case 0:
	case 1:
		s.takeNoble(seat, el[0], ev)
	default:
		s.Phase = PhaseNoble
		return
	}
	s.endTurn(ev)
}

func (s *GameState) takeNoble(seat int, id int8, ev *[]Event) {
	p := &s.Players[seat]
	for i := 0; i < int(s.NNobles); i++ {
		if s.Nobles[i] == id {
			copy(s.Nobles[i:], s.Nobles[i+1:s.NNobles])
			s.NNobles--
			s.Nobles[s.NNobles] = -1
			break
		}
	}
	p.Nobles |= 1 << uint(id)
	p.Points += Nobles[id].Points
	if ev != nil {
		*ev = append(*ev, Event{Kind: "noble", Seat: seat, Noble: id, Card: NoCard})
	}
}

func (s *GameState) endTurn(ev *[]Event) {
	if s.Players[s.Current].Points >= WinPoints {
		s.LastRound = true
	}
	s.Turn++
	s.Current++
	s.Phase = PhaseMain
	if int(s.Current) >= int(s.NPlayers) {
		s.Current = 0
		if s.LastRound || s.Rounds() >= MaxRounds {
			s.finish(ev)
		}
	}
}

func (s *GameState) finish(ev *[]Event) {
	s.Phase = PhaseOver
	best := -1
	bestCards := 0
	var mask uint8
	for i := 0; i < int(s.NPlayers); i++ {
		p := &s.Players[i]
		pts, nc := int(p.Points), int(p.NCards)
		switch {
		case pts > best || (pts == best && nc < bestCards):
			best, bestCards, mask = pts, nc, 1<<uint(i)
		case pts == best && nc == bestCards:
			mask |= 1 << uint(i)
		}
	}
	s.Winners = mask
	if ev != nil {
		*ev = append(*ev, Event{Kind: "game_over", Seat: -1, Card: NoCard, Noble: -1})
	}
}

// IsWinner reports whether seat is among the winners of a finished game.
func (s *GameState) IsWinner(seat int) bool { return s.Winners&(1<<uint(seat)) != 0 }

// Rounds returns the number of full rounds played so far.
func (s *GameState) Rounds() int { return int(s.Turn) / int(s.NPlayers) }
