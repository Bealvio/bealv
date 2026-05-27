package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeKube struct {
	status      PodStatus
	statusErr   error
	logs        string
	logsErr     error
	restartErr  error
	restartHits int32
}

func (f *fakeKube) PodStatus(ctx context.Context) (PodStatus, error) {
	return f.status, f.statusErr
}
func (f *fakeKube) PodLogs(ctx context.Context, tail int64) (string, error) {
	return f.logs, f.logsErr
}
func (f *fakeKube) RestartDeployment(ctx context.Context) error {
	atomic.AddInt32(&f.restartHits, 1)
	return f.restartErr
}

func newTestServer(t *testing.T, fk *fakeKube) *Server {
	t.Helper()
	return &Server{
		Kube:         fk,
		Auth:         NewAuth("hunter2", []byte("test-signing-key-32-bytes-pad"), time.Hour),
		Cooldown:     NewCooldown(2 * time.Minute),
		SecureCookie: false,
	}
}

func withSession(s *Server, r *http.Request) {
	tok := s.Auth.IssueToken(time.Now())
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tok})
}

func TestStatus_RequiresAuth(t *testing.T) {
	s := newTestServer(t, &fakeKube{})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/status", nil)
	s.Router().ServeHTTP(w, r)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
}

func TestStatus_ReturnsJSON(t *testing.T) {
	fk := &fakeKube{status: PodStatus{Name: "p", Phase: "Running", Ready: true, Restarts: 1}}
	s := newTestServer(t, fk)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/status", nil)
	withSession(s, r)
	s.Router().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var got PodStatus
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Phase != "Running" || !got.Ready || got.Restarts != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestLogs_ReturnsText(t *testing.T) {
	fk := &fakeKube{logs: "line1\nline2\n"}
	s := newTestServer(t, fk)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/logs", nil)
	withSession(s, r)
	s.Router().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "line1") {
		t.Fatalf("body = %q", w.Body.String())
	}
}

func TestRestart_TriggersKubeRestart(t *testing.T) {
	fk := &fakeKube{}
	s := newTestServer(t, fk)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/restart", nil)
	withSession(s, r)
	s.Router().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", w.Code, w.Body.String())
	}
	if atomic.LoadInt32(&fk.restartHits) != 1 {
		t.Fatalf("restartHits = %d, want 1", fk.restartHits)
	}
}

func TestRestart_SecondCallBlockedByCooldown(t *testing.T) {
	fk := &fakeKube{}
	s := newTestServer(t, fk)
	w1 := httptest.NewRecorder()
	r1 := httptest.NewRequest("POST", "/api/restart", nil)
	withSession(s, r1)
	s.Router().ServeHTTP(w1, r1)
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("POST", "/api/restart", nil)
	withSession(s, r2)
	s.Router().ServeHTTP(w2, r2)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("second call status = %d, want 429", w2.Code)
	}
	if atomic.LoadInt32(&fk.restartHits) != 1 {
		t.Fatalf("restartHits = %d, want 1", fk.restartHits)
	}
}

func TestRestart_PropagatesKubeError(t *testing.T) {
	fk := &fakeKube{restartErr: errors.New("boom")}
	s := newTestServer(t, fk)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/restart", nil)
	withSession(s, r)
	s.Router().ServeHTTP(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	s := newTestServer(t, &fakeKube{})
	form := url.Values{"password": {"wrong"}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	s.Router().ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestLogin_CorrectPasswordSetsCookie(t *testing.T) {
	s := newTestServer(t, &fakeKube{})
	form := url.Values{"password": {"hunter2"}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	s.Router().ServeHTTP(w, r)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != sessionCookieName {
		t.Fatalf("expected session cookie, got %v", cookies)
	}
}
