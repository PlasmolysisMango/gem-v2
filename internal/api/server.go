// Package api exposes the REST and WebSocket interfaces.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"gem/internal/ai"
	"gem/internal/auth"
	"gem/internal/engine"
	"gem/internal/game"
	"gem/internal/store"
)

// Server wires handlers together.
type Server struct {
	Auth     *auth.Service
	Store    *store.Store
	Hub      *game.Hub
	Static   fs.FS // optional built web client (index.html at root)
	replayMu sync.Mutex
}

// Handler returns the HTTP handler with all routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/register", s.register)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /api/cards", s.cards)
	protected := func(h http.HandlerFunc) http.Handler { return s.Auth.Middleware(h) }
	mux.Handle("GET /api/me", protected(s.me))
	mux.Handle("GET /api/rooms", protected(s.listRooms))
	mux.Handle("POST /api/rooms", protected(s.createRoom))
	mux.Handle("GET /api/games/{id}", protected(s.gameRecord))
	mux.Handle("POST /api/games/{id}/analysis/{seq}", protected(s.replayAnalysis))
	mux.Handle("GET /api/users/{id}/stats", protected(s.stats))
	mux.Handle("GET /api/admin/settings", s.adminOnly(s.adminGetSettings))
	mux.Handle("PUT /api/admin/settings", s.adminOnly(s.adminPutSettings))
	mux.Handle("DELETE /api/admin/rooms/{id}", s.adminOnly(s.adminCloseRoom))
	mux.Handle("GET /ws", protected(s.ws))
	if s.Static != nil {
		mux.Handle("/", spa(s.Static))
	}
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// AuthResponse is returned by register and login.
type AuthResponse struct {
	Token string     `json:"token"`
	User  store.User `json:"user"`
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	if s.Auth.Entry != nil {
		writeErr(w, 410, "请通过统一门户注册")
		return
	}
	var c credentials
	if json.NewDecoder(r.Body).Decode(&c) != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	u, tok, err := s.Auth.Register(r.Context(), c.Username, c.Password)
	switch {
	case errors.Is(err, auth.ErrTaken):
		writeErr(w, 409, err.Error())
	case errors.Is(err, auth.ErrBadUsername), errors.Is(err, auth.ErrBadPassword):
		writeErr(w, 400, err.Error())
	case err != nil:
		writeErr(w, 500, "注册失败")
	default:
		writeJSON(w, 201, AuthResponse{Token: tok, User: u})
	}
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if s.Auth.Entry != nil {
		writeErr(w, 410, "请通过统一门户登录")
		return
	}
	var c credentials
	if json.NewDecoder(r.Body).Decode(&c) != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	u, tok, err := s.Auth.Login(r.Context(), c.Username, c.Password)
	switch {
	case errors.Is(err, auth.ErrInvalid):
		writeErr(w, 401, err.Error())
	case err != nil:
		writeErr(w, 500, "登录失败")
	default:
		writeJSON(w, 200, AuthResponse{Token: tok, User: u})
	}
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.From(r.Context())
	u, err := s.Store.UserByID(r.Context(), id.UserID)
	if err != nil {
		writeErr(w, 401, auth.ErrToken.Error())
		return
	}
	if s.Auth.Entry != nil {
		gw, err := s.Auth.Entry.Identity(r)
		if err != nil {
			writeErr(w, 401, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{
			"id": u.ID, "username": u.Username, "externalId": u.ExternalID,
			"isAdmin": id.IsAdmin(), "createdAt": u.CreatedAt,
			"entry": map[string]string{
				"uid": gw.UID, "username": gw.Username, "displayName": gw.DisplayName,
				"role": gw.Role, "grants": gw.Grants,
			},
		})
		return
	}
	writeJSON(w, 200, u)
}

// cards returns the static card and noble tables (used by clients for replays).
func (s *Server) cards(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"cards": engine.Cards, "nobles": engine.Nobles})
}

func (s *Server) listRooms(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.Hub.List())
}

func (s *Server) createRoom(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.From(r.Context())
	var set game.RoomSettings
	if json.NewDecoder(r.Body).Decode(&set) != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	room, err := s.Hub.Create(id.UserID, id.Username, set)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, room.Summary())
}

func (s *Server) gameRecord(w http.ResponseWriter, r *http.Request) {
	gid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, 400, "对局 ID 无效")
		return
	}
	g, err := s.Store.Game(r.Context(), gid)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "对局不存在")
		return
	}
	if err != nil {
		writeErr(w, 500, "读取对局失败")
		return
	}
	if g.Status == "playing" {
		// Full records reveal deck order and hidden reserves; only allow after the game.
		writeErr(w, 409, "对局仍在进行，结束后才能复盘")
		return
	}
	rp, err := s.loadReplay(r.Context(), g)
	if err != nil {
		writeErr(w, 500, "读取复盘分析失败")
		return
	}
	writeJSON(w, 200, rp)
}

func (s *Server) loadReplay(ctx context.Context, g store.Game) (Replay, error) {
	rp := buildReplay(g)
	hints, err := s.Store.ReplayHints(ctx, g.ID)
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
	return rp, nil
}

func (s *Server) replayAnalysis(w http.ResponseWriter, r *http.Request) {
	gid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	seq, seqErr := strconv.Atoi(r.PathValue("seq"))
	if err != nil || seqErr != nil || seq < 0 {
		writeErr(w, 400, "对局或着法 ID 无效")
		return
	}
	s.replayMu.Lock()
	defer s.replayMu.Unlock()
	if r.Context().Err() != nil {
		return
	}
	g, err := s.Store.Game(r.Context(), gid)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "对局不存在")
		return
	}
	if err != nil {
		writeErr(w, 500, "读取对局失败")
		return
	}
	if g.Status == "playing" {
		writeErr(w, 409, "对局仍在进行，结束后才能复盘")
		return
	}
	rp, err := s.loadReplay(r.Context(), g)
	if err != nil {
		writeErr(w, 500, "读取复盘分析失败")
		return
	}
	var frame *Frame
	for i := range rp.Frames {
		if rp.Frames[i].Seq == seq && rp.Frames[i].Main {
			frame = &rp.Frames[i]
			break
		}
	}
	if frame == nil {
		writeErr(w, 400, "该着法不是可分析的主行动")
		return
	}
	if frame.Analysis != nil && frame.Review != nil {
		writeJSON(w, 200, frame)
		return
	}
	state, err := engine.NewGame(g.PlayerCount, g.Seed)
	if err != nil {
		writeErr(w, 500, "重建对局失败")
		return
	}
	for _, ra := range g.Actions {
		var action engine.Action
		if json.Unmarshal([]byte(ra.Action), &action) != nil {
			writeErr(w, 500, "重建着法失败")
			return
		}
		if ra.Seq != seq {
			if _, err := state.Apply(action); err != nil {
				writeErr(w, 500, "重建局面失败")
				return
			}
			continue
		}
		// Never let the coach see future deck order or opponents' hidden reserves.
		masked := state.MaskFor(ra.Seat)
		coach := ai.NewMaster(s.Hub.HintBudget())
		an := frame.Analysis
		if an == nil {
			an = coach.Analyze(r.Context(), &masked, ra.Seat, true)
		}
		if r.Context().Err() != nil {
			return
		}
		if an == nil || len(an.Candidates) == 0 {
			writeErr(w, 500, "AI 分析失败，请重试")
			return
		}
		review := coach.Review(&masked, ra.Seat, an, action)
		ab, _ := json.Marshal(an)
		rb, _ := json.Marshal(review)
		if err := s.Store.SaveReplayAnalysis(r.Context(), gid, ra.Seat, seq, frame.Turn, string(ab), string(rb), review.PlayedWinRate, review.BestWinRate); err != nil {
			writeErr(w, 500, "保存复盘分析失败")
			return
		}
		frame.Analysis, frame.Review = an, &review
		writeJSON(w, 200, frame)
		return
	}
}

// Frame is the position after one recorded move.
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

// Replay is a finished game with every intermediate position.
type Replay struct {
	store.Game
	Initial engine.View `json:"initial"`
	Frames  []Frame     `json:"frames"`
}

func buildReplay(g store.Game) Replay {
	rp := Replay{Game: g, Frames: []Frame{}}
	s, err := engine.NewGame(g.PlayerCount, g.Seed)
	if err != nil {
		return rp
	}
	rp.Initial = s.ViewFor(-1)
	for _, ra := range g.Actions {
		var a engine.Action
		if json.Unmarshal([]byte(ra.Action), &a) != nil {
			break
		}
		turn, main := int(s.Turn), s.Phase == engine.PhaseMain
		events, err := s.Apply(a)
		if err != nil {
			break
		}
		f := Frame{Seq: ra.Seq, Seat: ra.Seat, Text: engine.Describe(a), View: s.ViewFor(-1), Events: events, Turn: turn, Main: main}
		if ra.Review != nil {
			var mr ai.MoveReview
			if json.Unmarshal([]byte(*ra.Review), &mr) == nil {
				f.Review = &mr
			}
		}
		rp.Frames = append(rp.Frames, f)
	}
	rp.Actions = nil
	return rp
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	uid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, 400, "用户 ID 无效")
		return
	}
	st, err := s.Store.UserStats(r.Context(), uid, 20)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "用户不存在")
		return
	}
	if err != nil {
		writeErr(w, 500, "读取战绩失败")
		return
	}
	writeJSON(w, 200, st)
}

// spa serves static files and falls back to index.html for client-side routes.
func spa(fsys fs.FS) http.Handler {
	files := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if f, err := fsys.Open(p); err == nil {
				f.Close()
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		b, err := fs.ReadFile(fsys, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(b)
	})
}

// Shutdown helper for graceful stop. Persisted rooms survive; the next server
// process restores them.
func Shutdown(srv *http.Server, hub *game.Hub) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hub.Shutdown()
	srv.Shutdown(ctx)
}
