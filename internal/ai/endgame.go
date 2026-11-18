package ai

import (
	"context"
	"math"
	"math/rand/v2"
	"time"

	"gem/internal/engine"
)

// endgame scores candidate root moves with a paranoid look-ahead (opponents reply to
// minimize seat's winning chances) over several sampled worlds, until the deadline.
// Values are win probabilities in [0,1].
func (h *HardAgent) endgame(ctx context.Context, m *engine.GameState, seat int, cands []engine.Action, deadline time.Time, rng *rand.Rand) map[engine.Action]float64 {
	if ctx.Err() != nil {
		return nil
	}
	sum := make([]float64, len(cands))
	cnt := make([]float64, len(cands))
	depth := int(m.NPlayers) // all opponents reply, then seat moves again
	for sample := 0; sample < 24; sample++ {
		if ctx.Err() != nil {
			return nil
		}
		// Budget expiry preserves two complete worlds; cancellation never does.
		if sample >= 2 && h.searchExpired(deadline) {
			break
		}
		s := engine.Determinize(m, rng)
		for i, a := range cands {
			if ctx.Err() != nil {
				return nil
			}
			if s.Validate(a) != nil {
				continue
			}
			ns := s
			ns.Step(a)
			h.E.resolvePending(&ns)
			v, complete := h.paranoid(ctx, &ns, seat, depth)
			if !complete {
				return nil
			}
			sum[i] += v
			cnt[i]++
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	out := make(map[engine.Action]float64, len(cands))
	for i, a := range cands {
		if cnt[i] > 0 {
			out[a] = sum[i] / cnt[i]
		}
	}
	return out
}

func terminalValue(s *engine.GameState, seat int) float64 {
	if !s.IsWinner(seat) {
		return 0
	}
	k := 0
	for i := 0; i < int(s.NPlayers); i++ {
		if s.IsWinner(i) {
			k++
		}
	}
	return 1 / float64(k)
}

// Budget expiry must not turn an incomplete branch into a forced-win/loss verdict.
func (h *HardAgent) paranoid(ctx context.Context, s *engine.GameState, seat, depth int) (float64, bool) {
	if ctx.Err() != nil {
		return 0, false
	}
	if s.Over() {
		return terminalValue(s, seat), true
	}
	if depth == 0 {
		v := h.E.WinProb(s, seat)
		return v, ctx.Err() == nil
	}
	acts := s.LegalActions(nil)
	r := h.E.rankQuick(s, acts)
	mine := int(s.Current) == seat
	k := 5
	if mine {
		k = 6
	}
	if len(r) > k {
		r = r[:k]
	}
	best := math.Inf(1)
	if mine {
		best = math.Inf(-1)
	}
	for _, x := range r {
		if ctx.Err() != nil {
			return 0, false
		}
		ns := *s
		ns.Step(x.a)
		h.E.resolvePending(&ns)
		v, complete := h.paranoid(ctx, &ns, seat, depth-1)
		if !complete {
			return 0, false
		}
		if mine {
			best = math.Max(best, v)
		} else {
			best = math.Min(best, v)
		}
	}
	return best, ctx.Err() == nil
}
