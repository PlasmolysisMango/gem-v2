// Package arena runs AI self-play matches for evaluation and tuning.
package arena

import (
	"context"
	"fmt"
	"sync"
	"time"

	"gem/internal/ai"
	"gem/internal/engine"
)

// Result of one game.
type Result struct {
	Winners []int
	Turns   int
	Points  []int
	Think   []time.Duration // total think time per seat
	Moves   []int           // decisions per seat
}

// Play runs a full game between agents (one per seat).
func Play(ctx context.Context, agents []ai.Agent, seed uint64) (Result, error) {
	s, err := engine.NewGame(len(agents), seed)
	if err != nil {
		return Result{}, err
	}
	res := Result{Think: make([]time.Duration, len(agents)), Moves: make([]int, len(agents))}
	for steps := 0; !s.Over(); steps++ {
		if steps > 2000 {
			return res, fmt.Errorf("game did not finish (seed %d)", seed)
		}
		seat := int(s.Current)
		m := s.MaskFor(seat)
		t0 := time.Now()
		a, _ := agents[seat].Choose(ctx, &m, seat)
		res.Think[seat] += time.Since(t0)
		res.Moves[seat]++
		if _, err := s.Apply(a); err != nil {
			return res, fmt.Errorf("seat %d (%s) illegal move %s: %w", seat, agents[seat].Level(), engine.Describe(a), err)
		}
	}
	res.Turns = int(s.Turn)
	for i := 0; i < int(s.NPlayers); i++ {
		res.Points = append(res.Points, int(s.Players[i].Points))
		if s.IsWinner(i) {
			res.Winners = append(res.Winners, i)
		}
	}
	return res, nil
}

// MatchStats aggregates a match of agent A against copies of agent B.
type MatchStats struct {
	Games     int
	WinsA     float64 // ties split
	AvgTurns  float64
	AvgThinkA time.Duration
	AvgThinkB time.Duration
}

func (m MatchStats) WinRateA() float64 { return m.WinsA / float64(m.Games) }

// Match plays games with players seats; A rotates through all seats, the rest are B.
// newA/newB build fresh agents per game.
func Match(ctx context.Context, newA, newB func() ai.Agent, players, games int, seed uint64, progress func(i int, st MatchStats)) (MatchStats, error) {
	return MatchParallel(ctx, newA, newB, players, games, seed, 1, progress)
}

// MatchParallel is Match with up to parallel games running concurrently.
// Game g always uses the same seat assignment and seed, so results do not
// depend on parallelism (apart from time-budgeted search noise).
func MatchParallel(ctx context.Context, newA, newB func() ai.Agent, players, games int, seed uint64, parallel int, progress func(i int, st MatchStats)) (MatchStats, error) {
	parallel = max(1, min(parallel, games))
	type done struct {
		g   int
		res Result
		err error
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
				seatA := g % players
				agents := make([]ai.Agent, players)
				for i := range agents {
					if i == seatA {
						agents[i] = newA()
					} else {
						agents[i] = newB()
					}
				}
				res, err := Play(ctx, agents, seed+uint64(g/players))
				out <- done{g, res, err}
			}
		}()
	}
	go func() {
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

	var st MatchStats
	var thinkA, thinkB time.Duration
	var movesA, movesB int
	var firstErr error
	for d := range out {
		if firstErr != nil {
			continue
		}
		if d.err != nil {
			firstErr = d.err
			cancel()
			continue
		}
		res, seatA := d.res, d.g%players
		st.Games++
		st.AvgTurns += float64(res.Turns)
		for _, w := range res.Winners {
			if w == seatA {
				st.WinsA += 1 / float64(len(res.Winners))
			}
		}
		for i := range res.Think {
			if i == seatA {
				thinkA += res.Think[i]
				movesA += res.Moves[i]
			} else {
				thinkB += res.Think[i]
				movesB += res.Moves[i]
			}
		}
		if progress != nil {
			progress(st.Games-1, st)
		}
	}
	if firstErr != nil {
		return st, firstErr
	}
	if st.Games == 0 {
		return st, ctx.Err()
	}
	st.AvgTurns /= float64(st.Games)
	if movesA > 0 {
		st.AvgThinkA = thinkA / time.Duration(movesA)
	}
	if movesB > 0 {
		st.AvgThinkB = thinkB / time.Duration(movesB)
	}
	return st, nil
}
