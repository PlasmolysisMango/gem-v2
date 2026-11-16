package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"gem/internal/auth"
	"gem/internal/game"
)

// Protocol types are defined next to the room actor and re-exported here for clients.
type (
	Envelope        = game.Envelope
	RoomState       = game.RoomState
	RoomSummary     = game.RoomSummary
	RoomSettings    = game.RoomSettings
	SeatInfo        = game.SeatInfo
	SetSeatPayload  = game.SetSeatPayload
	ActionPayload   = game.ActionPayload
	ChatPayload     = game.ChatPayload
	GameViewPayload = game.GameViewPayload
	EventsPayload   = game.EventsPayload
	LoggedEvent     = game.LoggedEvent
	GameOverPayload = game.GameOverPayload
	HintPayload     = game.HintPayload
	ReviewPayload   = game.ReviewPayload
	ErrorPayload    = game.ErrorPayload
)

// ws upgrades an authenticated connection (?token=) and routes messages to rooms.
func (s *Server) ws(w http.ResponseWriter, r *http.Request) {
	id, err := s.Auth.Parse(auth.TokenFrom(r))
	if err != nil {
		writeErr(w, 401, err.Error())
		return
	}
	// Tokens (not cookies) authenticate the socket, so cross-origin upgrades are harmless.
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	c.SetReadLimit(64 << 10)
	sess := game.NewSession(id.UserID, id.Username)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go func() {
		defer cancel()
		ping := time.NewTicker(25 * time.Second)
		defer ping.Stop()
		for {
			select {
			case e := <-sess.Out():
				wctx, wc := context.WithTimeout(ctx, 10*time.Second)
				err := wsjson.Write(wctx, c, e)
				wc()
				if err != nil {
					return
				}
			case <-ping.C:
				pctx, pc := context.WithTimeout(ctx, 10*time.Second)
				err := c.Ping(pctx)
				pc()
				if err != nil {
					return
				}
			case <-sess.Done():
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	var room *game.Room
	defer func() {
		sess.Close()
		if room != nil {
			room.Leave(sess, false)
		}
		c.Close(websocket.StatusNormalClosure, "")
	}()
	for {
		var e game.Envelope
		if err := wsjson.Read(ctx, c, &e); err != nil {
			return
		}
		switch e.Type {
		case game.MsgJoinRoom:
			target := s.Hub.Get(e.RoomID)
			if target == nil {
				sess.SendError(e.RoomID, e.ReqID, "房间不存在")
				continue
			}
			if room != nil && room != target {
				room.Leave(sess, false)
			}
			room = target
			room.Join(sess)
		case game.MsgLeaveRoom:
			if room != nil {
				room.Handle(sess, e)
				room = nil
			}
		default:
			if room == nil || (e.RoomID != "" && e.RoomID != room.ID()) {
				sess.SendError(e.RoomID, e.ReqID, "请先加入房间")
				continue
			}
			room.Handle(sess, e)
		}
	}
}

// Marshal builds an envelope with a JSON payload (helper for Go clients).
func Marshal(typ, roomID string, payload any) Envelope {
	var raw json.RawMessage
	if payload != nil {
		raw, _ = json.Marshal(payload)
	}
	return Envelope{Type: typ, RoomID: roomID, Payload: raw}
}
