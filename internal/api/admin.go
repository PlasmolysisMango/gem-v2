package api

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"time"

	"gem/internal/auth"
	"gem/internal/store"
)

// AdminSettings is the runtime configuration exposed to admins.
type AdminSettings struct {
	IdleCloseSeconds int64 `json:"idleCloseSeconds"`
}

// adminOnly authenticates the request and requires an admin account.
func (s *Server) adminOnly(next http.HandlerFunc) http.Handler {
	return s.Auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := auth.From(r.Context())
		if !ok {
			writeErr(w, 401, auth.ErrToken.Error())
			return
		}
		if id.IsAdmin() {
			next(w, r)
			return
		}
		u, err := s.Store.UserByID(r.Context(), id.UserID)
		if err != nil {
			writeErr(w, 401, auth.ErrToken.Error())
			return
		}
		if !u.IsAdmin {
			writeErr(w, 403, "需要管理员权限")
			return
		}
		next(w, r)
	}))
}

func (s *Server) adminGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, AdminSettings{IdleCloseSeconds: int64(s.Hub.IdleClose() / time.Second)})
}

func (s *Server) adminPutSettings(w http.ResponseWriter, r *http.Request) {
	var cfg AdminSettings
	if json.NewDecoder(r.Body).Decode(&cfg) != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	if cfg.IdleCloseSeconds < 0 || cfg.IdleCloseSeconds > math.MaxInt64/int64(time.Second) {
		writeErr(w, 400, "清理时长必须是非负秒数")
		return
	}
	if err := s.Store.SetSetting(r.Context(), store.SettingIdleClose, strconv.FormatInt(cfg.IdleCloseSeconds, 10)); err != nil {
		writeErr(w, 500, "保存设置失败")
		return
	}
	s.Hub.SetIdleClose(time.Duration(cfg.IdleCloseSeconds) * time.Second)
	writeJSON(w, 200, cfg)
}

func (s *Server) adminCloseRoom(w http.ResponseWriter, r *http.Request) {
	if !s.Hub.CloseRoom(r.PathValue("id")) {
		writeErr(w, 404, "房间不存在")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
