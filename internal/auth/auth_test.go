package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gem/internal/store"
)

func newService(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := New(st, []byte("secret"))
	s.Cost = 4
	return s
}

func TestValidUsername(t *testing.T) {
	for name, want := range map[string]bool{
		"ab": false, "abc": true, "玩家一号": true, "user_01": true,
		"has space": false, "a-b-c": false, strings.Repeat("x", 20): true, strings.Repeat("x", 21): false,
	} {
		if ValidUsername(name) != want {
			t.Errorf("ValidUsername(%q) != %v", name, want)
		}
	}
}

func TestRegisterLogin(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	if _, _, err := s.Register(ctx, "bob", "123"); !errors.Is(err, ErrBadPassword) {
		t.Fatalf("short password: %v", err)
	}
	if _, _, err := s.Register(ctx, "b", "123456"); !errors.Is(err, ErrBadUsername) {
		t.Fatalf("short username: %v", err)
	}
	u, tok, err := s.Register(ctx, "bob", "123456")
	if err != nil || tok == "" {
		t.Fatal(err)
	}
	if u.PwHash == "123456" || !strings.HasPrefix(u.PwHash, "$2") {
		t.Fatalf("password not bcrypt-hashed: %q", u.PwHash)
	}
	if _, _, err := s.Register(ctx, "bob", "abcdef"); !errors.Is(err, ErrTaken) {
		t.Fatalf("duplicate: %v", err)
	}
	if _, _, err := s.Login(ctx, "bob", "wrong!"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("wrong password: %v", err)
	}
	if _, _, err := s.Login(ctx, "nobody", "123456"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown user: %v", err)
	}
	_, tok, err = s.Login(ctx, "bob", "123456")
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.Parse(tok)
	if err != nil || id.UserID != u.ID || id.Username != "bob" {
		t.Fatalf("parse: %+v %v", id, err)
	}
}

func TestTokenRejection(t *testing.T) {
	s := newService(t)
	u := store.User{ID: 7, Username: "eve"}
	tok, _ := s.Sign(u)
	other := New(s.Store, []byte("other-secret"))
	if _, err := other.Parse(tok); err == nil {
		t.Fatal("token accepted with the wrong secret")
	}
	s.TTL = -time.Minute
	expired, _ := s.Sign(u)
	if _, err := s.Parse(expired); err == nil {
		t.Fatal("expired token accepted")
	}
	if _, err := s.Parse(tok[:len(tok)-2] + "xx"); err == nil {
		t.Fatal("tampered token accepted")
	}
}

func TestMiddleware(t *testing.T) {
	s := newService(t)
	tok, _ := s.Sign(store.User{ID: 3, Username: "carol"})
	h := s.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := From(r.Context())
		if !ok || id.UserID != 3 {
			t.Errorf("identity: %+v", id)
		}
		w.WriteHeader(204)
	}))
	for _, tc := range []struct {
		header, query string
		code          int
	}{
		{"", "", 401},
		{"Bearer junk", "", 401},
		{"Bearer " + tok, "", 204},
		{"", tok, 204}, // WebSocket handshake style ?token=
	} {
		req := httptest.NewRequest("GET", "/x?token="+tc.query, nil)
		if tc.header != "" {
			req.Header.Set("Authorization", tc.header)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tc.code {
			t.Errorf("%+v: got %d", tc, rec.Code)
		}
	}
}
