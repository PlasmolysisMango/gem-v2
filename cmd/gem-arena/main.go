// Command gem-arena runs AI self-play matches and simple weight tuning.
package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"time"

	"gem/internal/ai"
	"gem/internal/arena"
)

func main() {
	a := flag.String("a", "master", "agent A level (easy|medium|hard|master)")
	b := flag.String("b", "medium", "agent B level (easy|medium|hard|master)")
	players := flag.Int("players", 2, "players per game (2-4); A takes one seat, B the rest")
	games := flag.Int("games", 100, "number of games")
	hardMs := flag.Int("hard-ms", 300, "hard AI think time per move in ms")
	masterMs := flag.Int("master-ms", 1500, "master AI think time per move in ms")
	seed := flag.Uint64("seed", 1, "base seed")
	tune := flag.Int("tune", 0, "run N rounds of hill-climbing on medium weights instead of a match")
	parallel := flag.Int("parallel", 1, "games to run concurrently")
	workers := flag.Int("workers", 0, "master AI search goroutines per move (0 = NumCPU)")
	flag.Parse()

	if *tune > 0 {
		runTune(*tune, *games, *players, *seed, *parallel)
		return
	}
	mk := func(l string) func() ai.Agent {
		lv, ok := ai.ParseLevel(l)
		if !ok {
			fmt.Fprintf(os.Stderr, "unknown level %q\n", l)
			os.Exit(2)
		}
		return func() ai.Agent {
			if lv == ai.Hard {
				return ai.NewHard(time.Duration(*hardMs) * time.Millisecond)
			}
			if lv == ai.Master {
				h := ai.NewMaster(time.Duration(*masterMs) * time.Millisecond)
				if *workers > 0 {
					h.Workers = *workers
				}
				return h
			}
			return ai.New(lv)
		}
	}
	start := time.Now()
	st, err := arena.MatchParallel(context.Background(), mk(*a), mk(*b), *players, *games, *seed, *parallel, func(i int, st arena.MatchStats) {
		fmt.Fprintf(os.Stderr, "\rgame %d/%d  %s win rate %.1f%%", i+1, *games, *a, 100*st.WinRateA())
	})
	fmt.Fprintln(os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Printf("%s vs %s (%d players, %d games, seats rotated)\n", *a, *b, *players, st.Games)
	fmt.Printf("  %s win rate: %.1f%% (expected by chance %.1f%%)\n", *a, 100*st.WinRateA(), 100/float64(*players))
	fmt.Printf("  avg turns: %.1f\n", st.AvgTurns)
	fmt.Printf("  avg think: %s=%v %s=%v\n", *a, st.AvgThinkA.Round(time.Millisecond), *b, st.AvgThinkB.Round(time.Millisecond))
	fmt.Printf("  elapsed: %v\n", time.Since(start).Round(time.Second))
}

// runTune perturbs medium-agent weights and keeps changes that beat the incumbent.
func runTune(rounds, games, players int, seed uint64, parallel int) {
	rng := rand.New(rand.NewPCG(seed, 99))
	best := ai.DefaultWeights
	fields := []*float64{}
	ptrs := func(w *ai.Weights) []*float64 {
		return []*float64{&w.BonusEarly, &w.BonusLate, &w.Token, &w.Gold, &w.NobleClose, &w.Target, &w.TargetDecay, &w.ReserveDead, &w.Relative}
	}
	_ = fields
	for r := 0; r < rounds; r++ {
		cand := best
		ps := ptrs(&cand)
		i := rng.IntN(len(ps))
		*ps[i] *= 1 + (rng.Float64()*0.4 - 0.2)
		if cand.TargetDecay > 0.95 {
			cand.TargetDecay = 0.95
		}
		if cand.Relative > 1 {
			cand.Relative = 1
		}
		newA := func() ai.Agent { m := ai.NewMedium(); m.E.W = cand; return m }
		newB := func() ai.Agent { m := ai.NewMedium(); m.E.W = best; return m }
		st, err := arena.MatchParallel(context.Background(), newA, newB, players, games, seed+uint64(r)*1000, parallel, nil)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		wr := st.WinRateA()
		accept := wr > 1/float64(players)+0.05
		fmt.Printf("round %d: field %d -> win rate %.1f%% accept=%v\n", r+1, i, 100*wr, accept)
		if accept {
			best = cand
		}
	}
	fmt.Printf("suggested weights: %+v\n", best)
}
