package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const sessionCookieName = "pzctl_session"

type Auth struct {
	password   string
	signingKey []byte
	ttl        time.Duration
}

func NewAuth(password string, signingKey []byte, ttl time.Duration) *Auth {
	return &Auth{password: password, signingKey: signingKey, ttl: ttl}
}

func (a *Auth) Verify(submitted string) bool {
	return hmac.Equal([]byte(submitted), []byte(a.password))
}

func (a *Auth) IssueToken(now time.Time) string {
	expiry := strconv.FormatInt(now.Add(a.ttl).Unix(), 10)
	mac := hmac.New(sha256.New, a.signingKey)
	mac.Write([]byte(expiry))
	sig := hex.EncodeToString(mac.Sum(nil))
	return expiry + "." + sig
}

func (a *Auth) ValidateToken(token string, now time.Time) bool {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return false
	}
	expiry, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return false
	}
	if now.Unix() > expiry {
		return false
	}
	mac := hmac.New(sha256.New, a.signingKey)
	mac.Write([]byte(parts[0]))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(parts[1]), []byte(expected))
}

func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookieName)
		if err != nil || !a.ValidateToken(c.Value, time.Now()) {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// SetSessionCookie writes the session cookie on the response.
func (a *Auth) SetSessionCookie(w http.ResponseWriter, now time.Time, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    a.IssueToken(now),
		Path:     "/",
		Expires:  now.Add(a.ttl),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}
