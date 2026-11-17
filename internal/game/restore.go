package game

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gem/internal/ai"
	"gem/internal/engine"
	"gem/internal/store"
)

// Restore rebuilds waiting and running rooms from the database. Call it after
// NewHub and before serving clients; restored rooms have no clients, so turn
// scheduling stays paused until someone joins.
func (h *Hub) Restore(ctx context.Context) error {
	recs, err := h.store.LoadRooms(ctx)
	if err != nil {
		return err
	}
	for _, rec := range recs {
		r, err := h.restoreRoom(ctx, rec)
		if err != nil {
			fmt.Printf("restore room %s: %v\n", rec.ID, err)
			if derr := h.store.DeleteRoom(ctx, rec.ID); derr != nil {
				fmt.Printf("delete unreadable room %s: %v\n", rec.ID, derr)
			}
			continue
		}
		h.mu.Lock()
		h.rooms[r.id] = r
		h.mu.Unlock()
		go r.loop()
	}
	return nil
}

func (h *Hub) restoreRoom(ctx context.Context, rec store.RoomRecord) (*Room, error) {
	if len(rec.Seats) < 2 || len(rec.Seats) > 4 {
		return nil, fmt.Errorf("invalid seat count %d", len(rec.Seats))
	}
	if rec.Status != "waiting" && rec.Status != "playing" {
		return nil, fmt.Errorf("unexpected status %q", rec.Status)
	}
	seats := make([]seat, len(rec.Seats))
	for i, rs := range rec.Seats {
		st := seat{kind: rs.Kind, userID: rs.UserID, name: rs.Name}
		switch rs.Kind {
		case SeatAI:
			lv, ok := ai.ParseLevel(rs.Level)
			if !ok {
				return nil, fmt.Errorf("invalid AI level %q", rs.Level)
			}
			st.level = lv
		case SeatHuman, SeatOpen, SeatClosed:
		default:
			return nil, fmt.Errorf("unexpected seat kind %q", rs.Kind)
		}
		seats[i] = st
	}
	r := newRoom(h, rec.ID, rec.Name, rec.OwnerID, rec.OwnerName, seats, rec.Hints, rec.TurnSeconds)
	r.createdAt = time.Unix(rec.CreatedAt, 0)
	if rec.Status == "waiting" {
		return r, nil
	}
	for _, st := range seats {
		if st.kind != SeatHuman && st.kind != SeatAI {
			return nil, fmt.Errorf("running room has an empty seat")
		}
	}
	if rec.GameID == 0 {
		return nil, fmt.Errorf("running room has no game")
	}
	g, err := h.store.Game(ctx, rec.GameID)
	if err != nil {
		return nil, err
	}
	if g.Status != "playing" {
		return nil, fmt.Errorf("game %d is %s", g.ID, g.Status)
	}
	if g.PlayerCount != len(seats) {
		return nil, fmt.Errorf("game has %d players but room has %d seats", g.PlayerCount, len(seats))
	}
	gstate, err := engine.NewGame(g.PlayerCount, g.Seed)
	if err != nil {
		return nil, err
	}
	for _, ra := range g.Actions {
		var a engine.Action
		if err := json.Unmarshal([]byte(ra.Action), &a); err != nil {
			return nil, fmt.Errorf("action %d: %w", ra.Seq, err)
		}
		if ra.Seat < 0 || ra.Seat >= len(seats) || int(gstate.Current) != ra.Seat {
			return nil, fmt.Errorf("action %d: seat %d does not match the current player", ra.Seq, ra.Seat)
		}
		evs, err := gstate.Apply(a)
		if err != nil {
			return nil, fmt.Errorf("action %d: %w", ra.Seq, err)
		}
		for _, ev := range evs {
			r.history = append(r.history, seqEvent{ev: ev, seq: ra.Seq})
		}
		if ra.Review != nil {
			var rv ai.MoveReview
			if json.Unmarshal([]byte(*ra.Review), &rv) == nil {
				r.reviews[ra.Seat] = append(r.reviews[ra.Seat], rv)
			}
		}
		r.seq = ra.Seq + 1
	}
	if n := len(r.history) - maxHistory; n > 0 {
		r.history = append(r.history[:0:0], r.history[n:]...)
	}
	hints, err := h.store.HintsByGame(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	for seatIdx, row := range hints {
		if seatIdx < 0 || seatIdx >= len(seats) {
			continue
		}
		var an ai.Analysis
		if json.Unmarshal([]byte(row.Analysis), &an) == nil && an.Level == ai.Master {
			r.hintCache[seatIdx] = hintEntry{seq: row.Seq, an: &an}
		}
	}
	curves, err := h.store.CurveByGame(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	for seatIdx, pts := range curves {
		if seatIdx < 0 || seatIdx >= len(seats) {
			continue
		}
		for _, p := range pts {
			r.curve[seatIdx] = append(r.curve[seatIdx], CurvePoint{Turn: p.Turn, WinRate: p.WinRate, Best: p.Best})
		}
	}
	r.agents = make([]ai.Agent, len(seats))
	for i, st := range seats {
		if st.kind != SeatAI {
			continue
		}
		switch st.level {
		case ai.Hard:
			r.agents[i] = ai.NewHard(h.cfg.HardBudget)
		case ai.Master:
			r.agents[i] = ai.NewMaster(h.cfg.MasterBudget)
		default:
			r.agents[i] = ai.New(st.level)
		}
	}
	r.game, r.gameID, r.status = gstate, g.ID, "playing"
	r.updateSummary()
	return r, nil
}
