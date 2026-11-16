package ai

import (
	"fmt"

	"gem/internal/engine"
)

// MoveReview grades a played move against the strongest recommendation.
type MoveReview struct {
	Seat          int           `json:"seat"`
	Played        engine.Action `json:"played"`
	PlayedText    string        `json:"playedText"`
	Best          engine.Action `json:"best"`
	BestText      string        `json:"bestText"`
	PlayedWinRate float64       `json:"playedWinRate"`
	BestWinRate   float64       `json:"bestWinRate"`
	Delta         float64       `json:"delta"`
	Verdict       string        `json:"verdict"` // best, good, mistake, blunder
	VerdictText   string        `json:"verdictText"`
	Comment       string        `json:"comment"`
}

// Review compares played with the analysis' best move for the position m (masked for seat).
func (h *HardAgent) Review(m *engine.GameState, seat int, an *Analysis, played engine.Action) MoveReview {
	best := an.Candidates[0]
	r := MoveReview{Seat: seat, Played: played, PlayedText: engine.Describe(played), Best: best.Action, BestText: best.Text}
	if normalize(played) == normalize(best.Action) {
		r.PlayedWinRate, r.BestWinRate = best.WinRate, best.WinRate
		r.Verdict, r.VerdictText = "best", "最佳着法"
		r.Comment = "与困难 AI 的首选一致"
		return r
	}
	est := h.EstimateActions(m, seat, []engine.Action{best.Action, played}, 96)
	r.BestWinRate, r.PlayedWinRate = est[0], est[1]
	r.Delta = est[0] - est[1]
	switch {
	case r.Delta > 0.15:
		r.Verdict, r.VerdictText = "blunder", "严重失误"
	case r.Delta > 0.05:
		r.Verdict, r.VerdictText = "mistake", "失误"
	default:
		r.Verdict, r.VerdictText = "good", "良好"
	}
	if r.Delta < 0 {
		r.Delta = 0
	}
	r.Comment = fmt.Sprintf("推荐：%s（估计胜率差 %.1f%%）", best.Text, r.Delta*100)
	return r
}

// normalize drops explicit payment details for comparison.
func normalize(a engine.Action) engine.Action {
	if a.Type == engine.ActBuy || a.Type == engine.ActBuyReserved {
		a.Gems = engine.Gems{}
		a.HasPay = false
	}
	return a
}
