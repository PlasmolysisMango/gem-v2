// Package api exposes the REST and WebSocket interfaces.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gem/internal/auth"
	"gem/internal/engine"
	"gem/internal/game"
	"gem/internal/replay"
	"gem/internal/store"
)

// Server wires handlers together.
type Server struct {
	Auth   *auth.Service
	Store  *store.Store
	Hub    *game.Hub
	Static fs.FS // optional built web client (index.html at root)
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
		log.Printf("load replay game %d: %v", gid, err)
		writeErr(w, 500, "读取对局失败")
		return
	}
	if g.Status == "playing" {
		// Full records reveal deck order and hidden reserves; only allow after the game.
		writeErr(w, 409, "对局仍在进行，结束后才能复盘")
		return
	}
	rp, err := replay.Load(r.Context(), s.Store, g)
	if err != nil {
		writeErr(w, 500, "读取复盘分析失败")
		return
	}
	writeJSON(w, 200, rp)
}

func (s *Server) replayAnalysis(w http.ResponseWriter, r *http.Request) {
	gid, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	seq, seqErr := strconv.Atoi(r.PathValue("seq"))
	if err != nil || seqErr != nil || seq < 0 {
		writeErr(w, 400, "对局或着法 ID 无效")
		return
	}
	g, err := s.Store.Game(r.Context(), gid)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "对局不存在")
		return
	}
	if err != nil {
		log.Printf("load replay game %d: %v", gid, err)
		writeErr(w, 500, "读取对局失败")
		return
	}
	if g.Status == "playing" {
		writeErr(w, 409, replay.ErrPlaying.Error())
		return
	}
	frame, err := replay.Foreground(r.Context(), s.Store, gid, seq, s.Hub.HintBudget())
	if r.Context().Err() != nil {
		return
	}
	if errors.Is(err, replay.ErrNotMain) {
		writeErr(w, 400, err.Error())
		return
	}
	if err != nil {
		writeErr(w, 503, "分析暂停或失败，已保存的进度保留，请重试")
		return
	}
	writeJSON(w, 200, frame)
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
