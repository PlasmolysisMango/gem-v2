package replay

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gem/internal/ai"
	"gem/internal/engine"
	"gem/internal/store"
)

var (
	ErrPlaying = errors.New("对局仍在进行，结束后才能复盘")
	ErrNotMain = errors.New("该着法不是可分析的主行动")
)

type Frame struct {
	Seq      int            `json:"seq"`
	Seat     int            `json:"seat"`
	Text     string         `json:"text"`
	View     engine.View    `json:"view"`
	Events   []engine.Event `json:"events,omitempty"`
	Review   *ai.MoveReview `json:"review,omitempty"`
	Analysis *ai.Analysis   `json:"analysis,omitempty"`
	Turn     int            `json:"turn"`
	Main     bool           `json:"main"`
}

type Replay struct {
	store.Game
	Initial    engine.View     `json:"initial"`
	Frames     []Frame         `json:"frames"`
	Background store.ReplayJob `json:"background"`
}

func Build(g store.Game) (Replay, error) {
	rp := Replay{Game: g, Frames: []Frame{}}
	s, err := engine.NewGame(g.PlayerCount, g.Seed)
	if err != nil {
		return rp, err
	}
	rp.Initial = s.ViewFor(-1)
	for _, ra := range g.Actions {
		var a engine.Action
		if err := json.Unmarshal([]byte(ra.Action), &a); err != nil {
			return rp, err
		}
		turn, main := int(s.Turn), s.Phase == engine.PhaseMain
		events, err := s.Apply(a)
		if err != nil {
			return rp, err
		}
		f := Frame{Seq: ra.Seq, Seat: ra.Seat, Text: engine.Describe(a), View: s.ViewFor(-1), Events: events, Turn: turn, Main: main}
		if ra.Review != nil {
			var review ai.MoveReview
			if json.Unmarshal([]byte(*ra.Review), &review) == nil {
				f.Review = &review
			}
		}
		rp.Frames = append(rp.Frames, f)
	}
	rp.Actions = nil
	return rp, nil
}

func Load(ctx context.Context, st *store.Store, g store.Game) (Replay, error) {
	rp, err := Build(g)
	if err != nil {
		return rp, err
	}
	hints, err := st.ReplayHints(ctx, g.ID)
	if err != nil {
		return rp, err
	}
	for i := range rp.Frames {
		f := &rp.Frames[i]
		var an ai.Analysis
		if json.Unmarshal([]byte(hints[f.Seq]), &an) == nil && an.Level == ai.Master && an.Seat == f.Seat && len(an.Candidates) > 0 {
			f.Analysis = &an
		}
	}
	rp.Background, err = st.ReplayJobStatus(ctx, g.ID)
	return rp, err
}

func analyze(ctx context.Context, st *store.Store, gameID int64, seq int, coach *ai.HardAgent, lease store.ReplayLease) (Frame, error) {
	g, err := st.Game(ctx, gameID)
	if err != nil {
		return Frame{}, err
	}
	if g.Status == "playing" {
		return Frame{}, ErrPlaying
	}
	rp, err := Load(ctx, st, g)
	if err != nil {
		return Frame{}, err
	}
	var frame *Frame
	for i := range rp.Frames {
		if rp.Frames[i].Seq == seq && rp.Frames[i].Main {
			frame = &rp.Frames[i]
			break
		}
	}
	if frame == nil {
		return Frame{}, ErrNotMain
	}
	if frame.Analysis != nil && frame.Review != nil {
		return *frame, nil
	}
	state, err := engine.NewGame(g.PlayerCount, g.Seed)
	if err != nil {
		return Frame{}, err
	}
	for _, ra := range g.Actions {
		if err := ctx.Err(); err != nil {
			return Frame{}, err
		}
		var action engine.Action
		if err := json.Unmarshal([]byte(ra.Action), &action); err != nil {
			return Frame{}, err
		}
		if ra.Seq != seq {
			if _, err := state.Apply(action); err != nil {
				return Frame{}, err
			}
			continue
		}
		masked := state.MaskFor(ra.Seat)
		an := frame.Analysis
		if an == nil {
			an = coach.Analyze(ctx, &masked, ra.Seat, true)
		}
		if err := ctx.Err(); err != nil {
			return Frame{}, err
		}
		if an == nil || len(an.Candidates) == 0 {
			return Frame{}, errors.New("AI 分析失败，请重试")
		}
		review := coach.Review(ctx, &masked, ra.Seat, an, action)
		if err := ctx.Err(); err != nil {
			return Frame{}, err
		}
		ab, err := json.Marshal(an)
		if err != nil {
			return Frame{}, err
		}
		rb, err := json.Marshal(review)
		if err != nil {
			return Frame{}, err
		}
		if err := st.SaveReplayAnalysis(ctx, gameID, ra.Seat, seq, frame.Turn, string(ab), string(rb), review.PlayedWinRate, review.BestWinRate, lease); err != nil {
			return Frame{}, err
		}
		frame.Analysis, frame.Review = an, &review
		return *frame, nil
	}
	return Frame{}, ErrNotMain
}

func Foreground(ctx context.Context, st *store.Store, gameID int64, seq int, budget time.Duration) (Frame, error) {
	owner := newOwner()
	if err := st.ReplayDemand(ctx, owner, true); err != nil {
		return Frame{}, err
	}
	ctx, cancel := context.WithCancel(ctx)
	demandDone := heartbeat(ctx, cancel, func(ctx context.Context) error { return st.ReplayDemand(ctx, owner, true) })
	defer func() {
		cancel()
		<-demandDone
		cleanup, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		st.ReplayDemand(cleanup, owner, false)
	}()
	path, err := st.ReplayLockPath()
	if err != nil {
		return Frame{}, err
	}
	lock, err := acquire(ctx, path)
	if err != nil {
		return Frame{}, err
	}
	defer lock.Close()
	lease, err := st.BeginReplayExecution(ctx, owner, false)
	if err != nil {
		return Frame{}, err
	}
	defer endExecution(st, lease)
	execCtx, stop := context.WithCancel(ctx)
	done := heartbeat(execCtx, stop, func(ctx context.Context) error { return st.RenewReplayExecution(ctx, lease) })
	defer func() { stop(); <-done }()
	return analyze(execCtx, st, gameID, seq, ai.NewMaster(budget), lease)
}
