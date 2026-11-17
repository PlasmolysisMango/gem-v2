package entryauth

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// Identity is the authenticated user injected by the nginx gateway.
type Identity struct {
	UID         string
	Username    string
	DisplayName string
	Role        string
	Grants      string
}

type contextKey struct{}

func FromRequest(r *http.Request) (Identity, bool) {
	id, ok := r.Context().Value(contextKey{}).(Identity)
	return id, ok
}

// Gateway validates headers written by nginx after auth_request succeeds.
type Gateway struct {
	key      string
	service  string
	origin   string
}

// New returns nil when ENTRY_AUTH_MODE is not "gateway", keeping legacy auth.
func New(service string) (*Gateway, error) {
	if strings.TrimSpace(os.Getenv("ENTRY_AUTH_MODE")) != "gateway" {
		return nil, nil
	}
	key := strings.TrimSpace(os.Getenv("ENTRY_PROXY_KEY"))
	if key == "" {
		path := strings.TrimSpace(os.Getenv("ENTRY_PROXY_KEY_FILE"))
		if path == "" {
			path = "/etc/entry/proxy.key"
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, errors.New("读取 ENTRY_PROXY_KEY_FILE 失败: " + err.Error())
		}
		key = strings.TrimSpace(string(data))
	}
	if key == "" {
		return nil, errors.New("gateway 模式需要 ENTRY_PROXY_KEY")
	}
	origin := strings.TrimSpace(os.Getenv("ENTRY_PUBLIC_ORIGIN"))
	if origin == "" {
		origin = "https://47.99.132.211"
	}
	return &Gateway{key: key, service: service, origin: strings.TrimRight(origin, "/")}, nil
}

func (g *Gateway) Identity(r *http.Request) (Identity, error) {
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Entry-Proxy-Key")), []byte(g.key)) != 1 {
		return Identity{}, errors.New("缺少网关头")
	}
	if strings.TrimSpace(r.Header.Get("X-Entry-Service")) != g.service {
		return Identity{}, errors.New("服务身份不匹配")
	}
	if !sameOrigin(r, g.origin) {
		return Identity{}, errors.New("来源校验失败")
	}
	uid := strings.TrimSpace(r.Header.Get("X-Entry-User-Id"))
	role := strings.TrimSpace(r.Header.Get("X-Entry-Role"))
	if uid == "" || role == "" {
		return Identity{}, errors.New("缺少用户身份")
	}
	id := Identity{
		UID:         uid,
		Username:    decodeHeader(r.Header.Get("X-Entry-Username")),
		DisplayName: decodeHeader(r.Header.Get("X-Entry-Display-Name")),
		Role:        role,
		Grants:      r.Header.Get("X-Entry-Grants"),
	}
	if id.Username == "" {
		id.Username = uid
	}
	if id.DisplayName == "" {
		id.DisplayName = id.Username
	}
	return id, nil
}

// Wrap stores a validated identity in the request context.
func (g *Gateway) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := g.Identity(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, id)))
	})
}

func sameOrigin(r *http.Request, origin string) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
		return false
	}
	if got := strings.TrimRight(strings.TrimSpace(r.Header.Get("Origin")), "/"); got != "" {
		return got == origin
	}
	if ref := strings.TrimSpace(r.Header.Get("Referer")); ref != "" {
		u, err := url.Parse(ref)
		return err == nil && strings.TrimRight(u.Scheme+"://"+u.Host, "/") == origin
	}
	return true
}

func decodeHeader(value string) string {
	if value == "" {
		return ""
	}
	if decoded, err := url.QueryUnescape(value); err == nil {
		return decoded
	}
	return value
}
