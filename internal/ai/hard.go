package ai

import (
	"context"
	"math"
	"math/rand/v2"
	"runtime"
	"sort"
	"sync"
	"time"

	"gem/internal/engine"
)

// HardAgent powers the hard and master search levels with single-observer ISMCTS: every iteration samples
// the hidden cards (deck order, opponents' face-down reserves) consistently with what the
// player can see, descends a shared tree with PUCT using heuristic priors, finishes with a
// short heuristic rollout and evaluates the leaf. Near the end of the game a paranoid
// look-ahead over sampled worlds vetoes moves that lose by force.
type HardAgent struct {
	level  Level
	E      *Evaluator
	Budget time.Duration
	// Concurrent-safe search budget hook; it must not cancel the review context.
	SearchExpired func() bool
	Workers       int
	MaxIter       int // per worker; 0 = time-limited only
	TopK          int
	RolloutDepth  int // extra plies per player after the leaf
	C             float64
	PriorTemp     float64
	EndgameAt     int // max score that triggers endgame search
}

func NewHard(budget time.Duration) *HardAgent {
	h := NewMaster(budget)
	h.level, h.Workers = Hard, 1
	return h
}

func NewMaster(budget time.Duration) *HardAgent {
	w := min(runtime.NumCPU(), 8)
	e := &Evaluator{W: DefaultWeights, searchTargets: true}
	return &HardAgent{level: Master, E: e, Budget: budget, Workers: w, TopK: 14, RolloutDepth: 2, C: 1.4, PriorTemp: 0.6, EndgameAt: 12}
}

func (h *HardAgent) Level() Level { return h.level }

func (h *HardAgent) searchExpired(deadline time.Time) bool {
	if h.SearchExpired != nil {
		return h.SearchExpired()
	}
	return time.Now().After(deadline)
}

type node struct {
	action   engine.Action
	player   int8
	parent   *node
	children []*node
	n, w     float64
	avail    float64
	prior    float64
}

func (nd *node) q() float64 { return nd.w / nd.n }

type worker struct {
	h   *HardAgent
	rng *rand.Rand
	buf []engine.Action
}

// expand creates children for the top-K moves of s, with softmax priors over their 1-ply value.
func (wk *worker) expand(nd *node, s *engine.GameState) {
	e := wk.h.E
	acts := s.LegalActions(wk.buf)
	wk.buf = acts
	var cand []engine.Action
	if len(acts) > 2*wk.h.TopK {
		q := e.rankQuick(s, acts)
		cand = make([]engine.Action, 0, 2*wk.h.TopK)
		for i := 0; i < 2*wk.h.TopK; i++ {
			cand = append(cand, q[i].a)
		}
	} else {
		cand = append([]engine.Action(nil), acts...)
	}
	r := e.rankActions(s, cand)
	if len(r) > wk.h.TopK {
		r = r[:wk.h.TopK]
	}
	mx, sum := r[0].v, 0.0
	pri := make([]float64, len(r))
	for i := range r {
		pri[i] = math.Exp((r[i].v - mx) / wk.h.PriorTemp)
		sum += pri[i]
	}
	for i := range r {
		if nd.find(r[i].a) != nil {
			continue
		}
		nd.children = append(nd.children, &node{action: r[i].a, player: s.Current, parent: nd, prior: pri[i] / sum})
	}
}

func (nd *node) find(a engine.Action) *node {
	for _, c := range nd.children {
		if c.action == a {
			return c
		}
	}
	return nil
}

func isLegal(s *engine.GameState, a engine.Action) bool {
	return s.Validate(a) == nil
}

// iterate runs one ISMCTS iteration on a fresh determinization s.
func (wk *worker) iterate(ctx context.Context, root *node, s *engine.GameState, values *[4]float64) bool {
	h := wk.h
	nd := root
	for !s.Over() {
		if ctx.Err() != nil {
			return false
		}
		if s.Phase != engine.PhaseMain {
			h.E.resolvePending(s)
			continue
		}
		if len(nd.children) == 0 {
			if nd != root && nd.n == 0 {
				break // new leaf: evaluate
			}
			wk.expand(nd, s)
		}
		// Select among children legal in this determinization.
		var best *node
		bestU := math.Inf(-1)
		var sumQ, cntQ float64
		legal := 0
		for _, c := range nd.children {
			if !isLegal(s, c.action) {
				continue
			}
			legal++
			if c.n > 0 {
				sumQ += c.q()
				cntQ++
			}
		}
		if legal == 0 {
			wk.expand(nd, s)
		}
		fpu := 1 / float64(s.NPlayers)
		if cntQ > 0 {
			fpu = sumQ/cntQ - 0.1
		}
		for _, c := range nd.children {
			if !isLegal(s, c.action) {
				continue
			}
			c.avail++
			q := fpu
			if c.n > 0 {
				q = c.q()
			}
			u := q + h.C*c.prior*math.Sqrt(c.avail)/(1+c.n)
			if u > bestU {
				best, bestU = c, u
			}
		}
		if best == nil {
			break
		}
		s.Step(best.action)
		nd = best
	}
	if !wk.rollout(ctx, s, values) {
		return false
	}
	for x := nd; x != nil; x = x.parent {
		x.n++
		if x.player >= 0 {
			x.w += values[x.player]
		}
	}
	return true
}

// Only context cancellation interrupts a rollout, never search-budget expiry.
func (wk *worker) rollout(ctx context.Context, s *engine.GameState, values *[4]float64) bool {
	plies := wk.h.RolloutDepth * int(s.NPlayers)
	for i := 0; i < plies && !s.Over(); i++ {
		if ctx.Err() != nil {
			return false
		}
		s.Step(wk.h.E.policyAction(s, wk.rng, 0.1, wk.buf))
	}
	if ctx.Err() != nil {
		return false
	}
	wk.h.E.WinProbs(s, values)
	return ctx.Err() == nil
}

type rootStat struct {
	a    engine.Action
	n, w float64
	tree *node // the worker tree with the most visits for this move (for PV)
}

// search runs parallel ISMCTS and returns merged root statistics sorted by visits.
func (h *HardAgent) search(ctx context.Context, m *engine.GameState, deadline time.Time, workers int) ([]rootStat, int) {
	roots := make([]*node, workers)
	iters := make([]int, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			wk := &worker{h: h, rng: rand.New(rand.NewPCG(rand.Uint64(), uint64(i)))}
			root := &node{player: -1}
			roots[i] = root
			var vals [4]float64
			for it := 0; ; it++ {
				if ctx.Err() != nil || (h.MaxIter > 0 && it >= h.MaxIter) {
					break
				}
				if it&7 == 0 && it > 0 && h.searchExpired(deadline) {
					break
				}
				s := engine.Determinize(m, wk.rng)
				if !wk.iterate(ctx, root, &s, &vals) {
					break
				}
				iters[i]++
			}
		}(i)
	}
	wg.Wait()
	merged := map[engine.Action]*rootStat{}
	var order []engine.Action
	total := 0
	for i, r := range roots {
		total += iters[i]
		for _, c := range r.children {
			st, ok := merged[c.action]
			if !ok {
				st = &rootStat{a: c.action}
				merged[c.action] = st
				order = append(order, c.action)
			}
			st.n += c.n
			st.w += c.w
			if st.tree == nil || c.n > st.tree.n {
				st.tree = c
			}
		}
	}
	out := make([]rootStat, 0, len(order))
	for _, a := range order {
		out = append(out, *merged[a])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].n > out[j].n })
	return out, total
}

// pv follows the most visited children from nd.
func pv(nd *node, max int) []PVStep {
	var out []PVStep
	for x := nd; x != nil && len(out) < max; {
		out = append(out, PVStep{Seat: int(x.player), Action: x.action, Text: engine.Describe(x.action)})
		var nx *node
		for _, c := range x.children {
			if c.n >= 3 && (nx == nil || c.n > nx.n) {
				nx = c
			}
		}
		x = nx
	}
	return out
}

func maxPoints(s *engine.GameState) int {
	mp := 0
	for i := 0; i < int(s.NPlayers); i++ {
		mp = max(mp, int(s.Players[i].Points))
	}
	return mp
}

// Choose implements Agent.
func (h *HardAgent) Choose(ctx context.Context, m *engine.GameState, seat int) (engine.Action, *Analysis) {
	an := h.Analyze(ctx, m, seat, false)
	if ctx.Err() != nil || len(an.Candidates) == 0 {
		return engine.Action{}, an
	}
	return an.Candidates[0].Action, an
}

// Cancellation returns no candidates after all search goroutines stop.
func (h *HardAgent) Analyze(ctx context.Context, m *engine.GameState, seat int, explain bool) *Analysis {
	start := time.Now()
	an := &Analysis{Seat: seat, Level: h.Level()}
	defer func() {
		an.ElapsedMs = time.Since(start).Milliseconds()
		if ctx.Err() != nil {
			an.Candidates = nil
			an.WinRate = 0
		}
	}()
	if ctx.Err() != nil {
		return an
	}
	rng := newRNG()
	if m.Phase != engine.PhaseMain {
		s := engine.Determinize(m, rng)
		acts := s.LegalActions(nil)
		r := h.E.rankActions(&s, acts)
		for i := 0; i < len(r) && i < 3; i++ {
			if ctx.Err() != nil {
				return an
			}
			ns := s
			ns.Step(r[i].a)
			an.Candidates = append(an.Candidates, Candidate{Action: r[i].a, Text: engine.Describe(r[i].a),
				WinRate: h.E.WinProb(&ns, seat), Reasons: []Reason{}, PV: []PVStep{}})
		}
		an.WinRate = an.Candidates[0].WinRate
		if explain && ctx.Err() == nil {
			h.explain(m, seat, an)
		}
		return an
	}
	deadline := start.Add(h.Budget)
	acts := m.LegalActions(nil)
	if ctx.Err() != nil {
		return an
	}
	if len(acts) == 1 {
		an.Candidates = []Candidate{{Action: acts[0], Text: engine.Describe(acts[0]), WinRate: h.E.WinProb(m, seat), Reasons: []Reason{}, PV: []PVStep{}}}
		an.WinRate = an.Candidates[0].WinRate
		if explain && ctx.Err() == nil {
			h.explain(m, seat, an)
		}
		return an
	}
	// Endgame search runs concurrently with the tree search.
	var eg map[engine.Action]float64
	var egWG sync.WaitGroup
	if m.LastRound || maxPoints(m) >= h.EndgameAt {
		an.Endgame = true
		s := engine.Determinize(m, rng)
		ranked := h.E.rankActions(&s, s.LegalActions(nil))
		if ctx.Err() != nil {
			return an
		}
		if len(ranked) > 12 {
			ranked = ranked[:12]
		}
		cands := make([]engine.Action, len(ranked))
		for i := range ranked {
			cands[i] = ranked[i].a
		}
		egWG.Add(1)
		go func() {
			defer egWG.Done()
			eg = h.endgame(ctx, m, seat, cands, deadline, newRNG())
		}()
	}
	workers := h.Workers
	if an.Endgame && workers > 1 {
		workers--
	}
	stats, iters := h.search(ctx, m, deadline, max(1, workers))
	egWG.Wait()
	an.Iterations = iters
	if ctx.Err() != nil {
		return an
	}

	// Combine tree statistics with the endgame verdicts.
	type cand struct {
		st  rootStat
		wr  float64
		key float64
	}
	var cs []cand
	for _, st := range stats {
		if st.n == 0 {
			continue
		}
		cs = append(cs, cand{st: st, wr: st.w / st.n, key: st.n})
	}
	if eg != nil {
		bestEG := 0.0
		for _, v := range eg {
			bestEG = math.Max(bestEG, v)
		}
		for i := range cs {
			if v, ok := eg[cs[i].st.a]; ok {
				cs[i].wr = 0.5*cs[i].wr + 0.5*v
				if bestEG >= 0.98 && v >= 0.98 {
					cs[i].key += 1e9 // forced win
				} else if v < bestEG-0.25 {
					cs[i].key -= 1e9 // loses by force compared to alternatives
				}
			}
		}
		// A forced win found by the endgame search but not explored by the tree.
		for a, v := range eg {
			if v >= 0.98 && bestEG >= 0.98 {
				found := false
				for _, c := range cs {
					if c.st.a == a {
						found = true
					}
				}
				if !found {
					cs = append(cs, cand{st: rootStat{a: a}, wr: v, key: 2e9})
				}
			}
		}
	}
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].key > cs[j].key })
	for i := 0; i < len(cs) && i < 3; i++ {
		c := Candidate{Action: cs[i].st.a, Text: engine.Describe(cs[i].st.a), WinRate: cs[i].wr, Visits: int(cs[i].st.n), Reasons: []Reason{}}
		if cs[i].st.tree != nil {
			c.PV = pv(cs[i].st.tree, 4)
		} else {
			c.PV = []PVStep{{Seat: seat, Action: c.Action, Text: c.Text}}
		}
		an.Candidates = append(an.Candidates, c)
	}
	if ctx.Err() != nil {
		return an
	}
	if len(an.Candidates) == 0 {
		a := h.E.rankActions(m, acts)[0].a
		an.Candidates = []Candidate{{Action: a, Text: engine.Describe(a), WinRate: h.E.WinProb(m, seat), Reasons: []Reason{}, PV: []PVStep{}}}
	}
	an.WinRate = an.Candidates[0].WinRate
	if explain && ctx.Err() == nil {
		h.explain(m, seat, an)
	}
	return an
}

// Canceled estimates are discarded; search-budget expiry does not apply.
func (h *HardAgent) EstimateActions(ctx context.Context, m *engine.GameState, seat int, acts []engine.Action, samples int) []float64 {
	if ctx.Err() != nil {
		return nil
	}
	out := make([]float64, len(acts))
	seed := rand.Uint64()
	wk := &worker{h: h}
	var vals [4]float64
	for i, a := range acts {
		if ctx.Err() != nil {
			return nil
		}
		wk.rng = rand.New(rand.NewPCG(seed, 7))
		for k := 0; k < samples; k++ {
			if ctx.Err() != nil {
				return nil
			}
			s := engine.Determinize(m, wk.rng)
			if s.Validate(a) != nil {
				continue
			}
			s.Step(a)
			h.E.resolvePending(&s)
			if !wk.rollout(ctx, &s, &vals) {
				return nil
			}
			out[i] += vals[seat]
		}
		if samples > 0 {
			out[i] /= float64(samples)
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	return out
}
