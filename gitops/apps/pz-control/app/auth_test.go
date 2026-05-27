package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestAuth() *Auth {
	return NewAuth("hunter2", []byte("test-signing-key-32-bytes-padding"), time.Hour)
}

func TestAuth_VerifyCorrectPassword(t *testing.T) {
	a := newTestAuth()
	if !a.Verify("hunter2") {
		t.Fatal("Verify(correct) should be true")
	}
}

func TestAuth_VerifyWrongPassword(t *testing.T) {
	a := newTestAuth()
	if a.Verify("nope") {
		t.Fatal("Verify(wrong) should be false")
	}
}

func TestAuth_TokenRoundtrip(t *testing.T) {
	a := newTestAuth()
	now := time.Unix(1000, 0)
	tok := a.IssueToken(now)
	if !a.ValidateToken(tok, now.Add(30*time.Minute)) {
		t.Fatal("freshly issued token should validate within TTL")
	}
}

func TestAuth_TokenExpired(t *testing.T) {
	a := newTestAuth()
	now := time.Unix(1000, 0)
	tok := a.IssueToken(now)
	if a.ValidateToken(tok, now.Add(2*time.Hour)) {
		t.Fatal("expired token should not validate")
	}
}

func TestAuth_TokenTampered(t *testing.T) {
	a := newTestAuth()
	now := time.Unix(1000, 0)
	tok := a.IssueToken(now)
	tampered := tok[:len(tok)-2] + "00"
	if a.ValidateToken(tampered, now) {
		t.Fatal("tampered token should not validate")
	}
}

func TestAuth_MiddlewareRedirectsWithoutCookie(t *testing.T) {
	a := newTestAuth()
	h := a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be reached")
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	h.ServeHTTP(w, r)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/login" {
		t.Fatalf("Location = %q, want /login", loc)
	}
}

func TestAuth_MiddlewareAllowsValidCookie(t *testing.T) {
	a := newTestAuth()
	tok := a.IssueToken(time.Now())
	called := false
	h := a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tok})
	h.ServeHTTP(w, r)
	if !called {
		t.Fatal("handler should have been called")
	}
}
