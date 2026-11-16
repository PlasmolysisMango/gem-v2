package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"gem/internal/engine"
	"gem/internal/game"
	"gem/internal/store"
)

// DefaultServer is used when no server has been saved.
const DefaultServer = "http://localhost:8080"

// ConfigDir is where the token and server address are stored (~/.config/gem).
// GEM_CONFIG_DIR overrides it.
func ConfigDir() string {
	if d := os.Getenv("GEM_CONFIG_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "gem")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "gem")
}

func readConfig(name string) string {
	b, _ := os.ReadFile(filepath.Join(ConfigDir(), name))
	return strings.TrimSpace(string(b))
}

// LoadClient builds a client from the saved server and token.
func LoadClient() *Client {
	srv := os.Getenv("GEM_SERVER")
	if srv == "" {
		srv = readConfig("server")
	}
	if srv == "" {
		srv = DefaultServer
	}
	return &Client{Server: srv, Token: readConfig("token")}
}

// Save stores the server and token for later commands.
func (c *Client) Save() error {
	dir := ConfigDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "server"), []byte(c.Server+"\n"), 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "token"), []byte(c.Token+"\n"), 0o600)
}

// Client talks to the REST API.
type Client struct {
	Server string
	Token  string
	User   store.User
	HTTP   *http.Client
}

type apiError struct {
	Error string `json:"error"`
}

func (c *Client) do(method, path string, body, out any) error {
	var rd *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, strings.TrimRight(c.Server, "/")+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("无法连接服务器 %s：%w", c.Server, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e apiError
		json.NewDecoder(resp.Body).Decode(&e)
		if resp.StatusCode == http.StatusUnauthorized && e.Error == "" {
			e.Error = "未登录或登录已过期，请先执行 gem-cli login"
		}
		if e.Error == "" {
			e.Error = resp.Status
		}
		return errors.New(e.Error)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// Login authenticates (or registers) and keeps the token.
func (c *Client) Login(username, password string, register bool) error {
	path := "/api/auth/login"
	if register {
		path = "/api/auth/register"
	}
	var res struct {
		Token string     `json:"token"`
		User  store.User `json:"user"`
	}
	if err := c.do("POST", path, map[string]string{"username": username, "password": password}, &res); err != nil {
		return err
	}
	c.Token, c.User = res.Token, res.User
	return nil
}

// Me returns the logged-in user.
func (c *Client) Me() (store.User, error) {
	var u store.User
	err := c.do("GET", "/api/me", nil, &u)
	return u, err
}

// Rooms lists the lobby.
func (c *Client) Rooms() ([]game.RoomSummary, error) {
	var rs []game.RoomSummary
	err := c.do("GET", "/api/rooms", nil, &rs)
	return rs, err
}

// CreateRoom opens a room owned by the user.
func (c *Client) CreateRoom(set game.RoomSettings) (game.RoomSummary, error) {
	var r game.RoomSummary
	err := c.do("POST", "/api/rooms", set, &r)
	return r, err
}

func (c *Client) wsURL() (string, error) {
	u, err := url.Parse(strings.TrimRight(c.Server, "/") + "/ws")
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}
	q := u.Query()
	q.Set("token", c.Token)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Dial opens the WebSocket for a room; the join is sent when the backend starts.
func (c *Client) Dial(ctx context.Context, roomID string) (*Online, error) {
	addr, err := c.wsURL()
	if err != nil {
		return nil, err
	}
	dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(dctx, addr, nil)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			return nil, errors.New("未登录或登录已过期，请先执行 gem-cli login")
		}
		return nil, fmt.Errorf("连接 WebSocket 失败：%w", err)
	}
	conn.SetReadLimit(4 << 20)
	o := &Online{conn: conn, room: roomID}
	o.ctx, o.cancel = context.WithCancel(ctx)
	return o, nil
}

// Online is a Backend bound to one server room.
type Online struct {
	conn    *websocket.Conn
	room    string
	ctx     context.Context
	cancel  context.CancelFunc
	playing atomic.Bool
	done    chan struct{}
}

func (o *Online) write(typ string, payload any) error {
	e := game.Envelope{Type: typ, RoomID: o.room}
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		e.Payload = raw
	}
	ctx, cancel := context.WithTimeout(o.ctx, 10*time.Second)
	defer cancel()
	return wsjson.Write(ctx, o.conn, e)
}

// Start implements Backend: joins the room and pumps server messages into the UI.
func (o *Online) Start(send func(tea.Msg)) {
	o.done = make(chan struct{})
	go func() {
		defer close(o.done)
		if err := o.write(game.MsgJoinRoom, nil); err != nil {
			send(ClosedMsg{Err: err})
			return
		}
		for {
			var e game.Envelope
			if err := wsjson.Read(o.ctx, o.conn, &e); err != nil {
				if o.ctx.Err() == nil {
					send(ClosedMsg{Err: err})
				}
				return
			}
			if msg := o.decode(e); msg != nil {
				send(msg)
			}
		}
	}()
}

func (o *Online) decode(e game.Envelope) tea.Msg {
	switch e.Type {
	case game.MsgRoomState:
		var p game.RoomState
		if json.Unmarshal(e.Payload, &p) == nil {
			o.playing.Store(p.Status == "playing")
			return RoomMsg{State: p}
		}
	case game.MsgGameView:
		var p game.GameViewPayload
		if json.Unmarshal(e.Payload, &p) == nil {
			return ViewMsg{GameID: p.GameID, View: p.View}
		}
	case game.MsgEvents:
		var p game.EventsPayload
		if json.Unmarshal(e.Payload, &p) == nil {
			var lines []string
			for _, ev := range p.Events {
				lines = append(lines, ev.Text)
			}
			return LogMsg{Lines: lines, Replace: p.Replace}
		}
	case game.MsgHint:
		var p game.HintPayload
		if json.Unmarshal(e.Payload, &p) == nil {
			return HintMsg{Turn: p.Turn, Analysis: p.Analysis}
		}
	case game.MsgMoveReview:
		var p game.ReviewPayload
		if json.Unmarshal(e.Payload, &p) == nil {
			return ReviewMsg{Review: p.Review}
		}
	case game.MsgGameOver:
		var p game.GameOverPayload
		if json.Unmarshal(e.Payload, &p) == nil {
			return OverMsg{Over: p}
		}
	case game.MsgChat:
		var p game.ChatPayload
		if json.Unmarshal(e.Payload, &p) == nil {
			return LogMsg{Lines: []string{fmt.Sprintf("[聊天] %s：%s", p.From, p.Text)}}
		}
	case game.MsgError:
		var p game.ErrorPayload
		if json.Unmarshal(e.Payload, &p) == nil {
			return ErrMsg{Err: p.Message}
		}
	}
	return nil
}

// Write failures need no handling here: they surface through the read loop's ClosedMsg.

// Act implements Backend.
func (o *Online) Act(a engine.Action) { go o.write(game.MsgAction, game.ActionPayload{Action: a}) }

// Hint implements Backend.
func (o *Online) Hint() { go o.write(game.MsgRequestHint, nil) }

// StartGame implements Backend (room owner only).
func (o *Online) StartGame() { go o.write(game.MsgStart, nil) }

// Chat sends a chat line to the room.
func (o *Online) Chat(text string) { go o.write(game.MsgChat, game.ChatPayload{Text: text}) }

// Close implements Backend. Leaving a waiting room frees the seat; leaving a running
// game keeps the seat for reconnection (an AI takes over after the grace period).
func (o *Online) Close() {
	if !o.playing.Load() {
		o.write(game.MsgLeaveRoom, nil)
	}
	o.cancel()
	o.conn.Close(websocket.StatusNormalClosure, "")
	if o.done != nil {
		<-o.done
	}
}
