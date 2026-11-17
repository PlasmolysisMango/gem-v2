package ai

import (
	"context"
	"math"
	"math/rand/v2"
	"time"

	"gem/internal/engine"
)

// Level is an AI difficulty.
type Level string

const (
	Easy   Level = "easy"
	Medium Level = "medium"
	Hard   Level = "hard"
	Master Level = "master"
)

// ParseLevel validates a level string.
func ParseLevel(s string) (Level, bool) {
	switch Level(s) {
	case Easy, Medium, Hard, Master:
		return Level(s), true
	}
	return "", false
}

// LevelName is the Chinese display name.
func (l Level) Name() string {
	switch l {
	case Easy:
		return "简单"
	case Medium:
		return "中等"
	case Hard:
		return "困难"
	case Master:
		return "大师"
	}
	return string(l)
}

// Reason explains one aspect of why a move is good.
type Reason struct {
	Tag  string `json:"tag"`
	Text string `json:"text"`
}

// PVStep is one move of the expected continuation.
type PVStep struct {
	Seat   int           `json:"seat"`
	Action engine.Action `json:"action"`
	Text   string        `json:"text"`
}

// Candidate is an analyzed move.
type Candidate struct {
	Action  engine.Action `json:"action"`
	Text    string        `json:"text"`
	WinRate float64       `json:"winRate"`
	Visits  int           `json:"visits"`
	Reasons []Reason      `json:"reasons"`
	PV      []PVStep      `json:"pv"`
}

// Analysis is the result of thinking about a position.
type Analysis struct {
	Seat         int         `json:"seat"`
	Level        Level       `json:"level"`
	Strategy     string      `json:"strategy"`
	StrategyText string      `json:"strategyText"`
	WinRate      float64     `json:"winRate"`
	Candidates   []Candidate `json:"candidates"`
	Iterations   int         `json:"iterations"`
	ElapsedMs    int64       `json:"elapsedMs"`
	Endgame      bool        `json:"endgame"`
}

// Agent chooses moves from a masked state (only information visible to seat).
type Agent interface {
	Level() Level
	Choose(ctx context.Context, m *engine.GameState, seat int) (engine.Action, *Analysis)
}

// New creates an agent for the level with default settings.
func New(l Level) Agent {
	switch l {
	case Easy:
		return NewEasy()
	case Medium:
		return NewMedium()
	case Hard:
		return NewHard(300 * time.Millisecond)
	default:
		return NewMaster(1500 * time.Millisecond)
	}
}

func newRNG() *rand.Rand {
	return rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
}

// EasyAgent: one-ply greedy on its own strength with moderate randomness; ignores opponents.
type EasyAgent struct {
	E       *Evaluator
	Epsilon float64
	Temp    float64
	rng     *rand.Rand
}

func NewEasy() *EasyAgent {
	return &EasyAgent{E: NewEvaluator(), Epsilon: 0.1, Temp: 0.6, rng: newRNG()}
}

func (a *EasyAgent) Level() Level { return Easy }

func (a *EasyAgent) Choose(_ context.Context, m *engine.GameState, seat int) (engine.Action, *Analysis) {
	s := engine.Determinize(m, a.rng)
	switch s.Phase {
	case engine.PhaseDiscard:
		return a.E.quickDiscard(&s), nil
	case engine.PhaseNoble:
		return quickNoble(&s), nil
	}
	acts := s.LegalActions(nil)
	if a.rng.Float64() < a.Epsilon {
		return acts[a.rng.IntN(len(acts))], nil
	}
	vals := make([]float64, len(acts))
	mx := math.Inf(-1)
	for i, act := range acts {
		ns := s
		ns.Step(act)
		if ns.Current == int8(seat) {
			a.E.resolvePending(&ns)
		}
		var v [4]float64
		a.E.Values(&ns, &v)
		vals[i] = v[seat]
		mx = math.Max(mx, vals[i])
	}
	sum := 0.0
	for i := range vals {
		vals[i] = math.Exp((vals[i] - mx) / a.Temp)
		sum += vals[i]
	}
	r := a.rng.Float64() * sum
	for i, v := range vals {
		r -= v
		if r <= 0 {
			return acts[i], nil
		}
	}
	return acts[len(acts)-1], nil
}

// MediumAgent: expert heuristic evaluation plus a one-round lookahead where every
// opponent answers with its best heuristic reply (covers blocking and racing threats).
type MediumAgent struct {
	E   *Evaluator
	K   int
	rng *rand.Rand
}

func NewMedium() *MediumAgent { return &MediumAgent{E: NewEvaluator(), K: 10, rng: newRNG()} }

func (a *MediumAgent) Level() Level { return Medium }

func (a *MediumAgent) Choose(_ context.Context, m *engine.GameState, seat int) (engine.Action, *Analysis) {
	s := engine.Determinize(m, a.rng)
	if s.Phase != engine.PhaseMain {
		return a.E.bestSubPhase(&s), nil
	}
	return a.E.lookahead(&s, seat, a.K), nil
}

// greedyReply returns the best 1-ply move of the player to move among its top quick candidates.
func (e *Evaluator) greedyReply(s *engine.GameState, k int) engine.Action {
	acts := s.LegalActions(nil)
	q := e.rankQuick(s, acts)
	if len(q) > k {
		q = q[:k]
	}
	cand := make([]engine.Action, len(q))
	for i := range q {
		cand[i] = q[i].a
	}
	return e.rankActions(s, cand)[0].a
}

// lookahead scores the top-k moves by playing one full round of greedy opponent replies.
func (e *Evaluator) lookahead(s *engine.GameState, seat, k int) engine.Action {
	ranked := e.rankActions(s, s.LegalActions(nil))
	if len(ranked) > k {
		ranked = ranked[:k]
	}
	best, bestV := ranked[0].a, math.Inf(-1)
	for _, r := range ranked {
		ns := *s
		ns.Step(r.a)
		e.resolvePending(&ns)
		for !ns.Over() && int(ns.Current) != seat {
			ns.Step(e.greedyReply(&ns, 8))
			e.resolvePending(&ns)
		}
		v := e.Eval(&ns, seat) + 0.1*r.v
		if v > bestV {
			best, bestV = r.a, v
		}
	}
	return best
}
