// Package auth handles password hashing, JWT issuance and HTTP authentication.
package auth

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"gem/internal/store"
)

var (
	ErrBadUsername = errors.New("用户名需为 3-20 个字符，只能包含字母、数字、下划线或中文")
	ErrBadPassword = errors.New("密码至少 6 位")
	ErrTaken       = errors.New("用户名已被占用")
	ErrInvalid     = errors.New("用户名或密码错误")
	ErrToken       = errors.New("登录已失效，请重新登录")
)

// Identity is the authenticated principal carried in tokens.
type Identity struct {
	UserID   int64  `json:"id"`
	Username string `json:"username"`
}

// Service issues and verifies credentials.
type Service struct {
	Store  *store.Store
	Secret []byte
	TTL    time.Duration
	Cost   int // bcrypt cost
}

func New(st *store.Store, secret []byte) *Service {
	return &Service{Store: st, Secret: secret, TTL: 24 * time.Hour, Cost: bcrypt.DefaultCost}
}

// ValidUsername checks length (3-20 runes) and allowed characters.
func ValidUsername(name string) bool {
	n := utf8.RuneCountInString(name)
	if n < 3 || n > 20 {
		return false
	}
	for _, r := range name {
		if !(r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return false
		}
	}
	return true
}

// Register creates an account and returns a token.
func (s *Service) Register(ctx context.Context, username, password string) (store.User, string, error) {
	username = strings.TrimSpace(username)
	if !ValidUsername(username) {
		return store.User{}, "", ErrBadUsername
	}
	if len(password) < 6 {
		return store.User{}, "", ErrBadPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), s.Cost)
	if err != nil {
		return store.User{}, "", err
	}
	u, err := s.Store.CreateUser(ctx, username, string(hash))
	if errors.Is(err, store.ErrExists) {
		return u, "", ErrTaken
	}
	if err != nil {
		return u, "", err
	}
	tok, err := s.Sign(u)
	return u, tok, err
}

// Login checks the password and returns a token.
func (s *Service) Login(ctx context.Context, username, password string) (store.User, string, error) {
	u, err := s.Store.UserByName(ctx, strings.TrimSpace(username))
	if errors.Is(err, store.ErrNotFound) {
		return u, "", ErrInvalid
	}
	if err != nil {
		return u, "", err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PwHash), []byte(password)) != nil {
		return u, "", ErrInvalid
	}
	tok, err := s.Sign(u)
	return u, tok, err
}

type claims struct {
	Username string `json:"name"`
	jwt.RegisteredClaims
}

// Sign issues an HS256 token for u.
func (s *Service) Sign(u store.User) (string, error) {
	now := time.Now()
	c := claims{Username: u.Username, RegisteredClaims: jwt.RegisteredClaims{
		Subject:   strconv.FormatInt(u.ID, 10),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(s.TTL)),
	}}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(s.Secret)
}

// Parse verifies a token.
func (s *Service) Parse(tok string) (Identity, error) {
	var c claims
	_, err := jwt.ParseWithClaims(tok, &c, func(t *jwt.Token) (any, error) { return s.Secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil {
		return Identity{}, ErrToken
	}
	id, err := strconv.ParseInt(c.Subject, 10, 64)
	if err != nil {
		return Identity{}, ErrToken
	}
	return Identity{UserID: id, Username: c.Username}, nil
}

type ctxKey struct{}

// WithIdentity stores id in ctx.
func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// From returns the identity set by Middleware.
func From(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(ctxKey{}).(Identity)
	return id, ok
}

// TokenFrom extracts a bearer token from the Authorization header or the token query parameter
// (the latter is used by WebSocket handshakes, which cannot set headers in browsers).
func TokenFrom(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return r.URL.Query().Get("token")
}

// Middleware rejects requests without a valid token.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := s.Parse(TokenFrom(r))
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"` + err.Error() + `"}`))
			return
		}
		next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), id)))
	})
}
