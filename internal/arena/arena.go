// Package arena runs AI self-play matches for evaluation and tuning.
package arena

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"gem/internal/ai"
	"gem/internal/engine"
)

// Result includes partial state on failure. Durations are nanoseconds in JSON.
type Result struct {
	Winners []int           `json:"winners"`
	Turns   int             `json:"turns"`
	Points  []int           `json:"points"`
	Think   []time.Duration `json:"thinkNs"` // total think time per seat
	Moves   []int           `json:"moves"`   // decisions, including subphases

	Duration   time.Duration `json:"durationNs"`
	Status     string        `json:"status"` // completed, error, cancelled
	Finish     string        `json:"finish"` // target_score, round_cap, or failure reason
	Error      string        `json:"error,omitempty"`
	Cancelled  bool          `json:"cancelled"`
	RoundCap   bool          `json:"roundCap"` // cap, rather than target score, ended the game
	Rounds     int           `json:"rounds"`
	TieCredit  []float64     `json:"tieCredit"` // per seat; zero on failure
	Nobles     []int         `json:"nobles"`
	NobleIDs   [][]int8      `json:"nobleIDs"`
	CardLevels [][3]int      `json:"cardLevels"` // counts for levels 1, 2, 3

	Seats                   []SeatMetrics `json:"seats,omitempty"`
	RepeatedPublicPositions int           `json:"repeatedPublicPositions"`
	UniquePublicPositions   int           `json:"uniquePublicPositions"`
	Evidence                []TurnRecord  `json:"evidence,omitempty"`
}

// SeatMetrics count completed primary turns once, never once per subphase.
// EarlyLevel1 counts successful L1 purchases made with fewer than 3 owned cards.
type SeatMetrics struct {
	PrimaryTurns              int            `json:"primaryTurns"`
	TakeTurns                 int            `json:"takeTurns"`
	DiscardTurns              int            `json:"discardTurns"`
	NobleChoiceTurns          int            `json:"nobleChoiceTurns"`
	UnchangedTakeDiscardTurns int            `json:"unchangedTakeDiscardTurns"`
	RepeatedPublicPositions   int            `json:"repeatedPublicPositions"`
	EarlyLevel1               EarlyPurchases `json:"earlyLevel1"`
}

type EarlyPurchases struct {
	Count          int    `json:"count"`
	CostTypeCounts [6]int `json:"costTypeCounts"` // index = number of nonzero printed cost colors
	PrintedCost    [5]int `json:"printedCost"`    // summed printed costs, white/blue/green/red/black
	ActualTokenPay [6]int `json:"actualTokenPay"` // summed real payment, including gold
	TokenPayTotal  int    `json:"tokenPayTotal"`
}

// DecisionRecord preserves the chosen action and Apply events (including real payment
// and resolved deck reserves) for post-game replay; it is not supplied to agents.
type DecisionRecord struct {
	Phase   engine.Phase   `json:"phase"`
	Action  engine.Action  `json:"action"`
	Applied bool           `json:"applied"`
	Events  []engine.Event `json:"events,omitempty"`
}

type PurchaseRecord struct {
	Card        int16       `json:"card"`
	OwnedBefore int         `json:"ownedBefore"`
	PrintedCost engine.Cost `json:"printedCost"`
	CostTypes   int         `json:"costTypes"`
	Paid        engine.Gems `json:"paid"`
}

// Fingerprints use spectator-masked public state, including the player to move,
// but excluding turn/round counters, deck order and all face-down identities.
// Repetition samples only the start of a primary turn, not its subphases.
type TurnRecord struct {
	Turn                 int              `json:"turn"` // zero-based completed turns before the action
	Seat                 int              `json:"seat"`
	Before               string           `json:"before"`
	After                string           `json:"after"`
	PreviousTurn         *int             `json:"previousTurn,omitempty"`
	Completed            bool             `json:"completed"`
	UnchangedTakeDiscard bool             `json:"unchangedTakeDiscard"`
	EarlyPurchase        *PurchaseRecord  `json:"earlyPurchase,omitempty"`
	Decisions            []DecisionRecord `json:"decisions"`
}

// GameRecord is delivered once for every started game, including failed/cancelled
// games. Observers run serially in the match collector, not in game workers.
type GameRecord struct {
	G      int      `json:"g"`
	Seed   uint64   `json:"seed"`
	SeatA  int      `json:"seatA"`
	Labels []string `json:"labels"` // one actual agent level per seat
	Result Result   `json:"result"`
}

// Play runs a full game between agents (one per seat), without collecting evidence.
func Play(ctx context.Context, agents []ai.Agent, seed uint64) (Result, error) {
	return play(ctx, agents, seed, false)
}

// PlayRecorded is Play with optional benchmark metrics and replay evidence.
func PlayRecorded(ctx context.Context, agents []ai.Agent, seed uint64) (Result, error) {
	return play(ctx, agents, seed, true)
}

func play(ctx context.Context, agents []ai.Agent, seed uint64, record bool) (res Result, err error) {
	start := time.Now()
	res.Status, res.Finish = "error", "setup_error"
	defer func() {
		res.Duration = time.Since(start)
		if err != nil {
			res.Error = err.Error()
		}
	}()
	s, err := engine.NewGame(len(agents), seed)
	if err != nil {
		return res, err
	}
	n := len(agents)
	res.Winners = []int{}
	res.Think, res.Moves = make([]time.Duration, n), make([]int, n)
	res.TieCredit = make([]float64, n)
	defer func() { snapshot(&res, &s, err == nil && s.Over()) }()
	for seat, agent := range agents {
		if agent == nil {
			return res, fmt.Errorf("nil agent at seat %d", seat)
		}
	}
	var capture *recorder
	if record {
		res.Seats = make([]SeatMetrics, n)
		capture = &recorder{res: &res, seen: make(map[string]int)}
	}
	for steps := 0; !s.Over(); steps++ {
		if err = ctx.Err(); err != nil {
			res.Status, res.Finish, res.Cancelled = "cancelled", "cancelled", true
			return res, err
		}
		if steps > 2000 {
			res.Finish = "step_cap"
			return res, fmt.Errorf("game did not finish (seed %d)", seed)
		}
		seat := int(s.Current)
		before := s
		m := s.MaskFor(seat)
		t0 := time.Now()
		a, _ := agents[seat].Choose(ctx, &m, seat)
		res.Think[seat] += time.Since(t0)
		res.Moves[seat]++
		if capture != nil {
			capture.begin(&before, a)
		}
		// Never apply a choice produced after cancellation, even by an agent
		// which does not itself honor context cancellation.
		if err = ctx.Err(); err != nil {
			res.Status, res.Finish, res.Cancelled = "cancelled", "cancelled", true
			return res, err
		}
		var events []engine.Event
		events, err = s.Apply(a)
		if err != nil {
			res.Finish = "illegal_move"
			err = fmt.Errorf("seat %d (%s) illegal move %s: %w", seat, agents[seat].Level(), engine.Describe(a), err)
			return res, err
		}
		if capture != nil {
			capture.applied(&before, &s, a, events)
		}
	}
	res.Status, res.Finish = "completed", "target_score"
	res.RoundCap = !s.LastRound && s.Rounds() >= engine.MaxRounds
	if res.RoundCap {
		res.Finish = "round_cap"
	}
	return res, nil
}

func snapshot(res *Result, s *engine.GameState, completed bool) {
	res.Turns, res.Rounds = int(s.Turn), s.Rounds()
	for i := 0; i < int(s.NPlayers); i++ {
		p := &s.Players[i]
		res.Points = append(res.Points, int(p.Points))
		ids := p.NobleIDs()
		if ids == nil {
			ids = []int8{}
		}
		res.NobleIDs = append(res.NobleIDs, ids)
		res.Nobles = append(res.Nobles, len(ids))
		var levels [3]int
		for _, id := range p.OwnedCards() {
			levels[engine.Cards[id].Level-1]++
		}
		res.CardLevels = append(res.CardLevels, levels)
		if completed && s.IsWinner(i) {
			res.Winners = append(res.Winners, i)
		}
	}
	for _, seat := range res.Winners {
		res.TieCredit[seat] = 1 / float64(len(res.Winners))
	}
}

// publicFingerprint deliberately includes public owned-card/noble bitsets, which
// GameState's regular JSON omits. The spectator mask also hides the actor's reserves.
func publicFingerprint(s *engine.GameState) string {
	m := s.MaskFor(-1)
	type publicPlayer struct {
		engine.PlayerState
		Owned  [2]uint64
		Nobles uint16
	}
	var players [4]publicPlayer
	for i := 0; i < int(m.NPlayers); i++ {
		players[i] = publicPlayer{m.Players[i], m.Players[i].Owned, m.Players[i].Nobles}
	}
	position := struct {
		NPlayers  int8
		Current   int8
		Phase     engine.Phase
		Bank      engine.Gems
		Board     [3][4]int16
		DeckLen   [3]int8
		Nobles    [5]int8
		NNobles   int8
		Players   [4]publicPlayer
		LastRound bool
		Winners   uint8
	}{m.NPlayers, m.Current, m.Phase, m.Bank, m.Board, m.DeckLen, m.Nobles, m.NNobles, players, m.LastRound, m.Winners}
	data, _ := json.Marshal(position) // fixed-size values only; cannot fail
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

type recorder struct {
	res    *Result
	seen   map[string]int
	before engine.GameState
}

func (r *recorder) begin(before *engine.GameState, a engine.Action) {
	if before.Phase == engine.PhaseMain {
		r.before = *before
		key := publicFingerprint(before)
		t := TurnRecord{Turn: int(before.Turn), Seat: int(before.Current), Before: key, After: key}
		if previous, ok := r.seen[key]; ok {
			t.PreviousTurn = &previous
			r.res.RepeatedPublicPositions++
			r.res.Seats[t.Seat].RepeatedPublicPositions++
		} else {
			r.res.UniquePublicPositions++
		}
		r.seen[key] = t.Turn
		r.res.Evidence = append(r.res.Evidence, t)
	}
	t := &r.res.Evidence[len(r.res.Evidence)-1]
	t.Decisions = append(t.Decisions, DecisionRecord{Phase: before.Phase, Action: a})
}

func (r *recorder) applied(before, after *engine.GameState, a engine.Action, events []engine.Event) {
	t := &r.res.Evidence[len(r.res.Evidence)-1]
	d := &t.Decisions[len(t.Decisions)-1]
	d.Applied, d.Events = true, events
	t.After = publicFingerprint(after)
	metrics := &r.res.Seats[t.Seat]
	if before.Phase == engine.PhaseMain && (a.Type == engine.ActBuy || a.Type == engine.ActBuyReserved) {
		card := engine.Cards[a.Card]
		if card.Level == 1 && before.Players[t.Seat].NCards < 3 {
			purchase := PurchaseRecord{Card: a.Card, OwnedBefore: int(before.Players[t.Seat].NCards), PrintedCost: card.Cost}
			for c, cost := range card.Cost {
				if cost > 0 {
					purchase.CostTypes++
				}
				metrics.EarlyLevel1.PrintedCost[c] += int(cost)
			}
			// Token delta is the actual payment, not a printed or discounted cost.
			for c, tokens := range before.Players[t.Seat].Tokens {
				purchase.Paid[c] = tokens - after.Players[t.Seat].Tokens[c]
				metrics.EarlyLevel1.ActualTokenPay[c] += int(purchase.Paid[c])
			}
			metrics.EarlyLevel1.Count++
			metrics.EarlyLevel1.CostTypeCounts[purchase.CostTypes]++
			metrics.EarlyLevel1.TokenPayTotal += purchase.Paid.Total()
			t.EarlyPurchase = &purchase
		}
	}
	if after.Turn == before.Turn {
		return // discard/noble still pending
	}
	t.Completed = true
	metrics.PrimaryTurns++
	primary := t.Decisions[0].Action.Type
	take := primary == engine.ActTakeDiff || primary == engine.ActTakeSame
	if take {
		metrics.TakeTurns++
	}
	var discarded, nobleChoice bool
	for _, decision := range t.Decisions {
		discarded = discarded || decision.Action.Type == engine.ActDiscard
		nobleChoice = nobleChoice || decision.Action.Type == engine.ActNoble
	}
	if discarded {
		metrics.DiscardTurns++
	}
	if nobleChoice {
		metrics.NobleChoiceTurns++
	}
	if take && discarded {
		comparison := *after
		comparison.Current = r.before.Current                                   // advancing the seat is not a board change
		comparison.Phase, comparison.Winners = r.before.Phase, r.before.Winners // nor is the cap's terminal transition
		t.UnchangedTakeDiscard = publicFingerprint(&comparison) == t.Before
		if t.UnchangedTakeDiscard {
			metrics.UnchangedTakeDiscardTurns++
		}
	}
}

// MatchStats aggregates completed games only; ties split, including round-cap ties.
type MatchStats struct {
	Games          int           `json:"games"`
	RequestedGames int           `json:"requestedGames"`
	StartedGames   int           `json:"startedGames"`
	FailedGames    int           `json:"failedGames"`
	CancelledGames int           `json:"cancelledGames"`
	WinsA          float64       `json:"winsA"`
	AvgTurns       float64       `json:"avgTurns"`
	AvgThinkA      time.Duration `json:"avgThinkANs"`
	AvgThinkB      time.Duration `json:"avgThinkBNs"`
}

func (m MatchStats) WinRateA() float64 {
	if m.Games == 0 {
		return 0
	}
	return m.WinsA / float64(m.Games)
}

// Match plays games with players seats; A rotates through all seats, the rest are B.
// newA/newB build fresh agents per game.
func Match(ctx context.Context, newA, newB func() ai.Agent, players, games int, seed uint64, progress func(i int, st MatchStats)) (MatchStats, error) {
	return MatchParallel(ctx, newA, newB, players, games, seed, 1, progress)
}

// MatchParallel preserves the unrecorded evaluation API.
func MatchParallel(ctx context.Context, newA, newB func() ai.Agent, players, games int, seed uint64, parallel int, progress func(i int, st MatchStats)) (MatchStats, error) {
	return MatchParallelObserved(ctx, newA, newB, players, games, seed, parallel, progress, nil)
}

// MatchParallelObserved optionally captures per-game evidence. Game g always uses
// seat A = g % players and seed = base + g / players. Observer errors stop scheduling
// and are returned; all started games are drained and reported, with no send leaks.
func MatchParallelObserved(ctx context.Context, newA, newB func() ai.Agent, players, games int, seed uint64, parallel int, progress func(i int, st MatchStats), observe func(GameRecord) error) (MatchStats, error) {
	st := MatchStats{RequestedGames: games}
	if players < 2 || players > 4 || games <= 0 || parallel <= 0 {
		return st, fmt.Errorf("players must be 2-4, games and parallel must be positive (got %d, %d, %d)", players, games, parallel)
	}
	if newA == nil || newB == nil {
		return st, fmt.Errorf("agent factories must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return st, err
	}
	parallel = min(parallel, games)
	type done struct {
		record GameRecord
		err    error
	}
	jobs := make(chan int)
	out := make(chan done)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	for w := 0; w < parallel; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for g := range jobs {
				if ctx.Err() != nil {
					return
				}
				seatA := g % players
				agents := make([]ai.Agent, players)
				record := GameRecord{G: g, Seed: seed + uint64(g/players), SeatA: seatA, Labels: make([]string, players)}
				for i := range agents {
					if i == seatA {
						agents[i] = newA()
					} else {
						agents[i] = newB()
					}
					if agents[i] != nil {
						record.Labels[i] = string(agents[i].Level())
					}
				}
				res, err := play(ctx, agents, record.Seed, observe != nil)
				record.Result = res
				out <- done{record, err} // collector always drains, including on cancellation
			}
		}()
	}
	// Include the producer in the wait group so returning also waits for its exit.
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(jobs)
		for g := 0; g < games; g++ {
			select {
			case jobs <- g:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() { wg.Wait(); close(out) }()

	var thinkA, thinkB time.Duration
	var movesA, movesB int
	var turns int
	var firstErr error
	averages := func() {
		if st.Games > 0 {
			st.AvgTurns = float64(turns) / float64(st.Games)
		}
		if movesA > 0 {
			st.AvgThinkA = thinkA / time.Duration(movesA)
		}
		if movesB > 0 {
			st.AvgThinkB = thinkB / time.Duration(movesB)
		}
	}
	for d := range out {
		st.StartedGames++
		if d.err != nil {
			if d.record.Result.Cancelled {
				st.CancelledGames++
			} else {
				st.FailedGames++
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("game %d (seed %d): %w", d.record.G, d.record.Seed, d.err)
				cancel()
			}
		} else {
			res, seatA := d.record.Result, d.record.SeatA
			st.Games++
			turns += res.Turns
			st.WinsA += res.TieCredit[seatA]
			for i := range res.Think {
				if i == seatA {
					thinkA += res.Think[i]
					movesA += res.Moves[i]
				} else {
					thinkB += res.Think[i]
					movesB += res.Moves[i]
				}
			}
			averages()
			if progress != nil {
				progress(st.Games-1, st)
			}
		}
		if observe != nil {
			if err := observe(d.record); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("game %d observer: %w", d.record.G, err)
				cancel()
			}
		}
	}
	averages()
	if firstErr != nil {
		return st, firstErr
	}
	if err := ctx.Err(); err != nil {
		return st, err
	}
	return st, nil
}
