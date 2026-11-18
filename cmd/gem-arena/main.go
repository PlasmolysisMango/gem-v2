// Command gem-arena runs AI self-play matches and simple weight tuning.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"gem/internal/ai"
	"gem/internal/arena"
)

// benchmarkConfig is repeated on each JSONL line so individual games remain
// independently attributable. AI RNGs remain unseeded, as in ordinary matches;
// board seeds plus recorded actions/events allow replay, not identical AI choices.
type benchmarkConfig struct {
	A                      string `json:"a"`
	B                      string `json:"b"`
	Players                int    `json:"players"`
	Games                  int    `json:"games"`
	BaseSeed               uint64 `json:"baseSeed"`
	Parallel               int    `json:"parallel"`
	HardMS                 int    `json:"hardMs"`
	MasterMS               int    `json:"masterMs"`
	Workers                int    `json:"workers"` // requested master workers; zero leaves constructor default
	EffectiveMasterWorkers int    `json:"effectiveMasterWorkers"`
	HardWorkers            int    `json:"hardWorkers"`
	CPUs                   int    `json:"cpus"`
	GOMAXPROCS             int    `json:"gomaxprocs"`
	GoVersion              string `json:"goVersion"`
	AgentRNG               string `json:"agentRng"`
}

type gameLine struct {
	Type          string          `json:"type"`
	SchemaVersion int             `json:"schemaVersion"`
	Config        benchmarkConfig `json:"config"`
	arena.GameRecord
}

type summaryLine struct {
	Type          string           `json:"type"`
	SchemaVersion int              `json:"schemaVersion"`
	Config        benchmarkConfig  `json:"config"`
	Status        string           `json:"status"`
	Error         string           `json:"error,omitempty"`
	Duration      time.Duration    `json:"durationNs"`
	Stats         arena.MatchStats `json:"stats"`
	WinRateA      float64          `json:"winRateA"`
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("gem-arena", flag.ContinueOnError)
	flags.SetOutput(stderr)
	a := flags.String("a", "master", "agent A level (easy|medium|hard|master|noble)")
	b := flags.String("b", "medium", "agent B level (easy|medium|hard|master|noble)")
	players := flags.Int("players", 2, "players per game (2-4); A takes one seat, B the rest")
	games := flags.Int("games", 100, "number of games")
	hardMs := flags.Int("hard-ms", 300, "hard AI think time per move in ms")
	masterMs := flags.Int("master-ms", 1500, "master AI think time per move in ms")
	seed := flags.Uint64("seed", 1, "base seed")
	tune := flags.Int("tune", 0, "run N rounds of hill-climbing on medium weights instead of a match")
	parallel := flags.Int("parallel", 1, "games to run concurrently")
	workers := flags.Int("workers", 0, "master AI search goroutines per move (0 = unchanged constructor default)")
	jsonl := flags.Bool("jsonl", false, "write one JSON line per started game, then a summary; progress stays on stderr")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	bad := func(message string) int {
		fmt.Fprintln(stderr, "error:", message)
		return 2
	}
	if flags.NArg() != 0 {
		return bad("unexpected positional arguments")
	}
	if *players < 2 || *players > 4 {
		return bad("players must be 2-4")
	}
	if *games <= 0 || *parallel <= 0 {
		return bad("games and parallel must be positive")
	}
	maxBudgetMS := int64((1<<63 - 1) / time.Millisecond)
	if *hardMs <= 0 || *masterMs <= 0 || int64(*hardMs) > maxBudgetMS || int64(*masterMs) > maxBudgetMS {
		return bad("hard-ms and master-ms must be positive and fit a time.Duration")
	}
	if *workers < 0 || *tune < 0 {
		return bad("workers and tune must be nonnegative")
	}
	if *tune > 0 && *jsonl {
		return bad("jsonl is supported for matches, not tuning")
	}
	lvA, okA := ai.ParseLevel(*a)
	lvB, okB := ai.ParseLevel(*b)
	if !okA || !okB {
		return bad(fmt.Sprintf("unknown agent level (A=%q, B=%q)", *a, *b))
	}
	if *tune > 0 {
		if err := runTune(ctx, *tune, *games, *players, *seed, *parallel, stdout); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		return 0
	}
	mk := func(lv ai.Level) func() ai.Agent {
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
	config := benchmarkConfig{
		A: string(lvA), B: string(lvB), Players: *players, Games: *games,
		BaseSeed: *seed, Parallel: *parallel, HardMS: *hardMs, MasterMS: *masterMs,
		Workers: *workers, EffectiveMasterWorkers: min(runtime.NumCPU(), 8), HardWorkers: 1,
		CPUs: runtime.NumCPU(), GOMAXPROCS: runtime.GOMAXPROCS(0), GoVersion: runtime.Version(),
		AgentRNG: "unseeded; board seed and recorded actions/events support replay",
	}
	if *workers > 0 {
		config.EffectiveMasterWorkers = *workers
	}
	encoder := json.NewEncoder(stdout)
	var observe func(arena.GameRecord) error
	if *jsonl {
		observe = func(record arena.GameRecord) error {
			return encoder.Encode(gameLine{Type: "game", SchemaVersion: 1, Config: config, GameRecord: record})
		}
	}
	start := time.Now()
	st, err := arena.MatchParallelObserved(ctx, mk(lvA), mk(lvB), *players, *games, *seed, *parallel, func(i int, st arena.MatchStats) {
		fmt.Fprintf(stderr, "\rgame %d/%d  %s win rate %.1f%%", i+1, *games, *a, 100*st.WinRateA())
	}, observe)
	elapsed := time.Since(start)
	fmt.Fprintln(stderr)
	if *jsonl {
		summary := summaryLine{Type: "summary", SchemaVersion: 1, Config: config, Status: "completed", Duration: elapsed, Stats: st, WinRateA: st.WinRateA()}
		if err != nil {
			summary.Status, summary.Error = "error", err.Error()
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				summary.Status = "cancelled"
			}
		}
		if encodeErr := encoder.Encode(summary); encodeErr != nil {
			fmt.Fprintln(stderr, "error writing summary:", encodeErr)
			return 1
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "error: %v; %d/%d games completed (%d started, %d failed, %d cancelled)\n", err, st.Games, *games, st.StartedGames, st.FailedGames, st.CancelledGames)
		return 1
	}
	if !*jsonl {
		fmt.Fprintf(stdout, "%s vs %s (%d players, %d games, seats rotated)\n", *a, *b, *players, st.Games)
		fmt.Fprintf(stdout, "  %s win rate: %.1f%% (expected by chance %.1f%%)\n", *a, 100*st.WinRateA(), 100/float64(*players))
		fmt.Fprintf(stdout, "  avg turns: %.1f\n", st.AvgTurns)
		fmt.Fprintf(stdout, "  avg think: %s=%v %s=%v\n", *a, st.AvgThinkA.Round(time.Millisecond), *b, st.AvgThinkB.Round(time.Millisecond))
		fmt.Fprintf(stdout, "  elapsed: %v\n", elapsed.Round(time.Second))
	}
	return 0
}

// runTune perturbs medium-agent weights and keeps changes that beat the incumbent.
func runTune(ctx context.Context, rounds, games, players int, seed uint64, parallel int, stdout io.Writer) error {
	rng := rand.New(rand.NewPCG(seed, 99))
	best := ai.DefaultWeights
	ptrs := func(w *ai.Weights) []*float64 {
		return []*float64{&w.BonusEarly, &w.BonusLate, &w.Token, &w.Gold, &w.NobleClose, &w.Target, &w.TargetDecay, &w.ReserveDead, &w.Relative}
	}
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
		st, err := arena.MatchParallel(ctx, newA, newB, players, games, seed+uint64(r)*1000, parallel, nil)
		if err != nil {
			return err
		}
		wr := st.WinRateA()
		accept := wr > 1/float64(players)+0.05
		fmt.Fprintf(stdout, "round %d: field %d -> win rate %.1f%% accept=%v\n", r+1, i, 100*wr, accept)
		if accept {
			best = cand
		}
	}
	fmt.Fprintf(stdout, "suggested weights: %+v\n", best)
	return nil
}
