# Project Zomboid Control Page Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a small password-protected web page at `pz.bealv.io` that lets friends view the Project Zomboid server status, tail its logs, and trigger a restart (with cooldown) without needing `kubectl` access.

**Architecture:** Single Go binary serves an embedded HTML/CSS/JS page plus a small JSON API. It talks to the Kubernetes API in-cluster via `client-go` using a tightly-scoped `ServiceAccount` (`get/patch` on the `project-zomboid` Deployment, `get/list` on its Pods, `get` on Pods/log). Auth is a shared password from Vault; sessions are HMAC-signed cookies. A `Cooldown` struct enforces a 2-minute minimum between restarts. Deployed in the `project-zomboid` namespace, exposed via the existing kgateway `https` Gateway.

**Tech Stack:** Go 1.22 · `k8s.io/client-go` · standard library `net/http` · Kustomize · Flux · kgateway / Gateway API · External Secrets (Vault) · Zot registry (`zot.bealv.io`)

---

## Assumptions and conventions (do not skip — confirm before starting)

- Vault path for the new dedicated password: `secrets-bealv/pz-control/auth`, key `password`.
- Vault path for the cookie signing key: `secrets-bealv/pz-control/auth`, key `signing-key` (32 random bytes hex-encoded).
- Image: `zot.bealv.io/public/pz-control:v0.1.0` (bump as needed).
- The pz-control pod runs in namespace `project-zomboid` (same as the game server) so RBAC stays a `Role`, not a `ClusterRole`.
- Flux manages deployment via a new `Kustomization` at `gitops/kustomizations/pz-control.yaml`.
- Go source lives at `gitops/apps/pz-control/app/` (kept inside the gitops repo for now; it's <500 LOC and can be extracted to its own repo later if it grows).

## File structure

```
gitops/
├── apps/pz-control/
│   ├── app/                          # Go source + Dockerfile
│   │   ├── go.mod
│   │   ├── go.sum
│   │   ├── main.go                   # wiring: load config, build deps, http.ListenAndServe
│   │   ├── cooldown.go               # rate-limit restart calls
│   │   ├── cooldown_test.go
│   │   ├── auth.go                   # password verify + HMAC session token + middleware
│   │   ├── auth_test.go
│   │   ├── kube.go                   # KubeClient interface + real client-go impl
│   │   ├── handlers.go               # HTTP handlers (status, logs, restart, login)
│   │   ├── handlers_test.go          # uses fakeKube
│   │   ├── web/                      # embedded static assets
│   │   │   ├── index.html
│   │   │   ├── login.html
│   │   │   ├── style.css
│   │   │   └── app.js
│   │   └── Dockerfile                # multi-stage: golang:1.22 → distroless/static
│   ├── namespace.yaml                # (reuses project-zomboid namespace; this file is just a marker — see Task 9)
│   ├── rbac.yaml                     # ServiceAccount + Role + RoleBinding
│   ├── external-secret.yml           # password + signing-key from Vault
│   ├── deployment.yaml               # 1 replica, runs the pz-control image
│   ├── service.yaml                  # ClusterIP :8080
│   ├── httproute.yaml                # pz.bealv.io → service
│   └── kustomization.yaml
└── kustomizations/pz-control.yaml    # Flux entry
```

**File responsibilities:**
- `cooldown.go`: pure logic, no I/O — easy to unit-test.
- `auth.go`: password compare (constant-time), HMAC-SHA256 token issue/validate, `http.Handler` middleware. No external deps.
- `kube.go`: defines `KubeClient` interface (`PodStatus`, `PodLogs`, `RestartDeployment`) and a real impl backed by `client-go`. The interface is what `handlers.go` depends on, so tests can substitute a fake.
- `handlers.go`: builds an `http.ServeMux` from a `KubeClient`, `*Auth`, `*Cooldown`. No global state.
- `main.go`: reads env vars (`PASSWORD`, `SIGNING_KEY`, `TARGET_NAMESPACE`, `TARGET_DEPLOYMENT`), constructs everything, starts the server.

---

## Task 1: Scaffold app folder and Go module

**Files:**
- Create: `gitops/apps/pz-control/app/go.mod`
- Create: `gitops/apps/pz-control/app/main.go` (placeholder)

- [ ] **Step 1: Initialize Go module**

```bash
cd gitops/apps/pz-control/app
go mod init github.com/neferites/pz-control
```

Expected: a `go.mod` file containing `module github.com/neferites/pz-control` and `go 1.22` (or similar).

- [ ] **Step 2: Write a placeholder `main.go` so the module compiles**

```go
package main

func main() {}
```

- [ ] **Step 3: Verify it builds**

Run: `go build ./...`
Expected: no output, exit code 0.

- [ ] **Step 4: Commit**

```bash
git add gitops/apps/pz-control/app/go.mod gitops/apps/pz-control/app/main.go
git commit -m "feat(pz-control): scaffold Go module"
```

---

## Task 2: Cooldown (TDD)

**Why:** Prevent friends from spamming the restart button and thrashing the pod. Pure logic — easy to test first.

**Files:**
- Create: `gitops/apps/pz-control/app/cooldown_test.go`
- Create: `gitops/apps/pz-control/app/cooldown.go`

- [ ] **Step 1: Write the failing tests**

`cooldown_test.go`:

```go
package main

import (
	"testing"
	"time"
)

func TestCooldown_FirstCallAllowed(t *testing.T) {
	c := NewCooldown(2 * time.Minute)
	if !c.Try(time.Unix(1000, 0)) {
		t.Fatal("first Try should be allowed")
	}
}

func TestCooldown_SecondCallWithinWindowBlocked(t *testing.T) {
	c := NewCooldown(2 * time.Minute)
	c.Try(time.Unix(1000, 0))
	if c.Try(time.Unix(1060, 0)) {
		t.Fatal("Try within window should be blocked")
	}
}

func TestCooldown_CallAfterWindowAllowed(t *testing.T) {
	c := NewCooldown(2 * time.Minute)
	c.Try(time.Unix(1000, 0))
	if !c.Try(time.Unix(1121, 0)) {
		t.Fatal("Try after window should be allowed")
	}
}

func TestCooldown_RemainingZeroWhenIdle(t *testing.T) {
	c := NewCooldown(2 * time.Minute)
	if got := c.Remaining(time.Unix(1000, 0)); got != 0 {
		t.Fatalf("Remaining on fresh cooldown = %v, want 0", got)
	}
}

func TestCooldown_RemainingDecreases(t *testing.T) {
	c := NewCooldown(2 * time.Minute)
	c.Try(time.Unix(1000, 0))
	got := c.Remaining(time.Unix(1030, 0))
	want := 90 * time.Second
	if got != want {
		t.Fatalf("Remaining = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail to compile**

Run: `go test ./...`
Expected: build error (`undefined: NewCooldown`).

- [ ] **Step 3: Implement `cooldown.go`**

```go
package main

import (
	"sync"
	"time"
)

type Cooldown struct {
	mu       sync.Mutex
	last     time.Time
	duration time.Duration
}

func NewCooldown(d time.Duration) *Cooldown {
	return &Cooldown{duration: d}
}

// Try records the action and returns true if it is permitted.
// Returns false if a previous Try happened within the cooldown window.
func (c *Cooldown) Try(now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.last.IsZero() && now.Sub(c.last) < c.duration {
		return false
	}
	c.last = now
	return true
}

// Remaining returns the duration until the cooldown expires, or 0 if ready.
func (c *Cooldown) Remaining(now time.Time) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.last.IsZero() {
		return 0
	}
	r := c.duration - now.Sub(c.last)
	if r < 0 {
		return 0
	}
	return r
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./...`
Expected: `PASS`, all five tests green.

- [ ] **Step 5: Commit**

```bash
git add gitops/apps/pz-control/app/cooldown.go gitops/apps/pz-control/app/cooldown_test.go
git commit -m "feat(pz-control): add restart cooldown logic"
```

---

## Task 3: Auth (TDD)

**Why:** Shared dedicated password + signed session cookie. Constant-time comparison and HMAC signature so tampering / brute-force is harder.

**Files:**
- Create: `gitops/apps/pz-control/app/auth_test.go`
- Create: `gitops/apps/pz-control/app/auth.go`

- [ ] **Step 1: Write the failing tests**

`auth_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to confirm failure**

Run: `go test ./...`
Expected: build errors (`undefined: NewAuth`, `undefined: sessionCookieName`).

- [ ] **Step 3: Implement `auth.go`**

```go
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
```

- [ ] **Step 4: Run tests**

Run: `go test ./...`
Expected: PASS for all auth + cooldown tests.

- [ ] **Step 5: Commit**

```bash
git add gitops/apps/pz-control/app/auth.go gitops/apps/pz-control/app/auth_test.go
git commit -m "feat(pz-control): add password auth + HMAC session cookies"
```

---

## Task 4: Kubernetes client wrapper

**Why:** Defines the boundary between business logic and the cluster. `handlers.go` depends only on the `KubeClient` interface, so tests can use a fake.

**Files:**
- Create: `gitops/apps/pz-control/app/kube.go`

- [ ] **Step 1: Add client-go dependency**

Run:
```bash
cd gitops/apps/pz-control/app
go get k8s.io/client-go@v0.30.3
go get k8s.io/api@v0.30.3
go get k8s.io/apimachinery@v0.30.3
```

Expected: `go.sum` populated, no errors.

- [ ] **Step 2: Write `kube.go`**

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type PodStatus struct {
	Name      string    `json:"name"`
	Phase     string    `json:"phase"`
	Ready     bool      `json:"ready"`
	StartedAt time.Time `json:"started_at"`
	Restarts  int32     `json:"restarts"`
}

type KubeClient interface {
	PodStatus(ctx context.Context) (PodStatus, error)
	PodLogs(ctx context.Context, tail int64) (string, error)
	RestartDeployment(ctx context.Context) error
}

type kubeClient struct {
	cs         kubernetes.Interface
	namespace  string
	deployment string
	podLabel   string // label selector, e.g. "app=project-zomboid"
}

func NewKubeClient(namespace, deployment, podLabel string) (KubeClient, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("in-cluster config: %w", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("clientset: %w", err)
	}
	return &kubeClient{cs: cs, namespace: namespace, deployment: deployment, podLabel: podLabel}, nil
}

func (k *kubeClient) currentPod(ctx context.Context) (*corev1.Pod, error) {
	pods, err := k.cs.CoreV1().Pods(k.namespace).List(ctx, metav1.ListOptions{LabelSelector: k.podLabel})
	if err != nil {
		return nil, err
	}
	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("no pods matching %q in %q", k.podLabel, k.namespace)
	}
	// Deployment uses replicas:1 + strategy:Recreate, so there is at most one.
	return &pods.Items[0], nil
}

func (k *kubeClient) PodStatus(ctx context.Context) (PodStatus, error) {
	pod, err := k.currentPod(ctx)
	if err != nil {
		return PodStatus{}, err
	}
	s := PodStatus{
		Name:  pod.Name,
		Phase: string(pod.Status.Phase),
	}
	if pod.Status.StartTime != nil {
		s.StartedAt = pod.Status.StartTime.Time
	}
	for _, c := range pod.Status.ContainerStatuses {
		s.Restarts += c.RestartCount
		if c.Ready {
			s.Ready = true
		} else {
			s.Ready = false
			break
		}
	}
	return s, nil
}

func (k *kubeClient) PodLogs(ctx context.Context, tail int64) (string, error) {
	pod, err := k.currentPod(ctx)
	if err != nil {
		return "", err
	}
	req := k.cs.CoreV1().Pods(k.namespace).GetLogs(pod.Name, &corev1.PodLogOptions{TailLines: &tail})
	rc, err := req.Stream(ctx)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// RestartDeployment triggers a rolling restart by patching
// .spec.template.metadata.annotations["kubectl.kubernetes.io/restartedAt"].
func (k *kubeClient) RestartDeployment(ctx context.Context) error {
	patch := map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]string{
						"kubectl.kubernetes.io/restartedAt": time.Now().UTC().Format(time.RFC3339),
					},
				},
			},
		},
	}
	body, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	_, err = k.cs.AppsV1().Deployments(k.namespace).Patch(ctx, k.deployment, types.StrategicMergePatchType, body, metav1.PatchOptions{})
	return err
}
```

- [ ] **Step 3: Verify compile**

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add gitops/apps/pz-control/app/go.mod gitops/apps/pz-control/app/go.sum gitops/apps/pz-control/app/kube.go
git commit -m "feat(pz-control): add Kubernetes client wrapper"
```

---

## Task 5: HTTP handlers (TDD)

**Why:** This is where auth, cooldown, and the Kube client come together. We test against a fake `KubeClient` so no cluster is needed.

**Files:**
- Create: `gitops/apps/pz-control/app/handlers_test.go`
- Create: `gitops/apps/pz-control/app/handlers.go`

- [ ] **Step 1: Write the failing tests**

`handlers_test.go`:

```go
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
		Kube:      fk,
		Auth:      NewAuth("hunter2", []byte("test-signing-key-32-bytes-pad"), time.Hour),
		Cooldown:  NewCooldown(2 * time.Minute),
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
	// First call
	w1 := httptest.NewRecorder()
	r1 := httptest.NewRequest("POST", "/api/restart", nil)
	withSession(s, r1)
	s.Router().ServeHTTP(w1, r1)
	// Second call immediately after
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
```

- [ ] **Step 2: Run tests to confirm failure**

Run: `go test ./...`
Expected: build errors (`undefined: Server`).

- [ ] **Step 3: Implement `handlers.go`**

```go
package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"time"
)

//go:embed web/*
var webFS embed.FS

type Server struct {
	Kube         KubeClient
	Auth         *Auth
	Cooldown     *Cooldown
	SecureCookie bool // set false for local tests, true in production behind TLS
	indexTpl     *template.Template
	loginTpl     *template.Template
}

func (s *Server) Router() http.Handler {
	if s.indexTpl == nil {
		s.indexTpl = template.Must(template.ParseFS(webFS, "web/index.html"))
		s.loginTpl = template.Must(template.ParseFS(webFS, "web/login.html"))
	}
	mux := http.NewServeMux()

	// Public
	mux.HandleFunc("GET /login", s.getLogin)
	mux.HandleFunc("POST /login", s.postLogin)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(mustSubFS(webFS, "web"))))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	// Protected
	protected := http.NewServeMux()
	protected.HandleFunc("GET /", s.getIndex)
	protected.HandleFunc("GET /api/status", s.getStatus)
	protected.HandleFunc("GET /api/logs", s.getLogs)
	protected.HandleFunc("POST /api/restart", s.postRestart)
	mux.Handle("/", s.Auth.Middleware(protected))

	return mux
}

func (s *Server) getIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.indexTpl.Execute(w, nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) getLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.loginTpl.Execute(w, map[string]any{"Error": r.URL.Query().Get("error")}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) postLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if !s.Auth.Verify(r.FormValue("password")) {
		http.Error(w, "wrong password", http.StatusUnauthorized)
		return
	}
	s.Auth.SetSessionCookie(w, time.Now(), s.SecureCookie)
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) getStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	st, err := s.Kube.PodStatus(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(st)
}

func (s *Server) getLogs(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	out, err := s.Kube.PodLogs(ctx, 500)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(out))
}

func (s *Server) postRestart(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	if !s.Cooldown.Try(now) {
		remaining := s.Cooldown.Remaining(now)
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(remaining.Seconds())+1))
		http.Error(w, fmt.Sprintf("cooldown: %s remaining", remaining.Truncate(time.Second)), http.StatusTooManyRequests)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := s.Kube.RestartDeployment(ctx); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("restart triggered"))
}

func mustSubFS(efs embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(efs, dir)
	if err != nil {
		panic(err)
	}
	return sub
}
```

- [ ] **Step 4: Stub the embedded templates so the build succeeds**

Create `gitops/apps/pz-control/app/web/index.html`:

```html
<!doctype html>
<html><body>stub</body></html>
```

Create `gitops/apps/pz-control/app/web/login.html`:

```html
<!doctype html>
<html><body>stub login</body></html>
```

- [ ] **Step 5: Run tests**

Run: `go test ./...`
Expected: all tests PASS.

- [ ] **Step 6: Commit**

```bash
git add gitops/apps/pz-control/app/handlers.go \
        gitops/apps/pz-control/app/handlers_test.go \
        gitops/apps/pz-control/app/web/index.html \
        gitops/apps/pz-control/app/web/login.html
git commit -m "feat(pz-control): add HTTP handlers with auth + cooldown"
```

---

## Task 6: Real embedded UI

**Why:** Replace the stub HTML with the actual page friends will see. Single-page UI: status badge, scrollable logs, refresh button, restart button with confirm dialog.

**Files:**
- Modify: `gitops/apps/pz-control/app/web/index.html`
- Modify: `gitops/apps/pz-control/app/web/login.html`
- Create: `gitops/apps/pz-control/app/web/style.css`
- Create: `gitops/apps/pz-control/app/web/app.js`

- [ ] **Step 1: Write `web/index.html`**

```html
<!doctype html>
<html lang="fr">
<head>
  <meta charset="utf-8">
  <title>Project Zomboid — Control</title>
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <link rel="stylesheet" href="/static/style.css">
</head>
<body>
  <header>
    <h1>GLYNV-HARD</h1>
    <span id="status-badge" class="badge unknown">…</span>
  </header>

  <section id="info">
    <dl>
      <dt>Pod</dt><dd id="pod-name">—</dd>
      <dt>Démarré</dt><dd id="started-at">—</dd>
      <dt>Restarts</dt><dd id="restarts">—</dd>
    </dl>
    <button id="restart-btn">Redémarrer le serveur</button>
    <p id="restart-msg"></p>
  </section>

  <section id="logs">
    <h2>Logs (500 dernières lignes)</h2>
    <pre id="logs-pre">chargement…</pre>
  </section>

  <script src="/static/app.js"></script>
</body>
</html>
```

- [ ] **Step 2: Write `web/login.html`**

```html
<!doctype html>
<html lang="fr">
<head>
  <meta charset="utf-8">
  <title>PZ Control — login</title>
  <link rel="stylesheet" href="/static/style.css">
</head>
<body class="login">
  <form method="post" action="/login">
    <h1>Project Zomboid Control</h1>
    {{ if .Error }}<p class="error">Mot de passe incorrect.</p>{{ end }}
    <input type="password" name="password" placeholder="mot de passe" autofocus>
    <button type="submit">Entrer</button>
  </form>
</body>
</html>
```

- [ ] **Step 3: Write `web/style.css`**

```css
* { box-sizing: border-box; }
body {
  font-family: system-ui, sans-serif;
  background: #1d1d1d;
  color: #ddd;
  margin: 0;
  padding: 1.5rem;
  max-width: 900px;
  margin-inline: auto;
}
header { display: flex; align-items: center; gap: 1rem; }
h1 { margin: 0; font-size: 1.4rem; }
.badge { padding: .2rem .6rem; border-radius: 999px; font-size: .85rem; font-weight: bold; }
.badge.ok { background: #2d6a3a; color: #fff; }
.badge.warn { background: #8a6d1f; color: #fff; }
.badge.bad { background: #8a2929; color: #fff; }
.badge.unknown { background: #555; color: #ccc; }

dl { display: grid; grid-template-columns: max-content 1fr; gap: .3rem 1rem; }
dt { color: #888; }

button {
  background: #b03030;
  color: white;
  border: 0;
  padding: .6rem 1.2rem;
  border-radius: 6px;
  cursor: pointer;
  font-size: 1rem;
}
button:hover { background: #c44040; }
button:disabled { background: #555; cursor: not-allowed; }

#logs-pre {
  background: #000;
  color: #cfd;
  padding: 1rem;
  border-radius: 6px;
  max-height: 50vh;
  overflow: auto;
  font-size: .8rem;
  line-height: 1.3;
}

#restart-msg.error { color: #ff8080; }
#restart-msg.ok { color: #80ff80; }

body.login {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 100vh;
}
body.login form { display: flex; flex-direction: column; gap: 1rem; max-width: 300px; }
body.login input { padding: .6rem; border-radius: 6px; border: 1px solid #444; background: #222; color: #eee; }
body.login .error { color: #ff8080; margin: 0; }
```

- [ ] **Step 4: Write `web/app.js`**

```javascript
const $ = (id) => document.getElementById(id);

async function refreshStatus() {
  try {
    const r = await fetch("/api/status");
    if (!r.ok) throw new Error("status http " + r.status);
    const s = await r.json();
    const badge = $("status-badge");
    badge.textContent = s.phase + (s.ready ? " · ready" : "");
    badge.className = "badge " + (s.ready ? "ok" : (s.phase === "Running" ? "warn" : "bad"));
    $("pod-name").textContent = s.name || "—";
    $("started-at").textContent = s.started_at ? new Date(s.started_at).toLocaleString() : "—";
    $("restarts").textContent = s.restarts ?? "—";
  } catch (e) {
    $("status-badge").textContent = "error";
    $("status-badge").className = "badge bad";
  }
}

async function refreshLogs() {
  try {
    const r = await fetch("/api/logs");
    const txt = await r.text();
    $("logs-pre").textContent = txt;
    $("logs-pre").scrollTop = $("logs-pre").scrollHeight;
  } catch (e) {
    $("logs-pre").textContent = "Impossible de récupérer les logs: " + e;
  }
}

async function doRestart() {
  if (!confirm("Redémarrer le serveur ? Tous les joueurs connectés seront déconnectés.")) return;
  const btn = $("restart-btn");
  const msg = $("restart-msg");
  btn.disabled = true;
  msg.textContent = "Redémarrage en cours…";
  msg.className = "";
  try {
    const r = await fetch("/api/restart", { method: "POST" });
    if (r.ok) {
      msg.textContent = "Restart déclenché.";
      msg.className = "ok";
    } else {
      const t = await r.text();
      msg.textContent = "Erreur: " + t;
      msg.className = "error";
    }
  } catch (e) {
    msg.textContent = "Erreur réseau: " + e;
    msg.className = "error";
  } finally {
    setTimeout(() => { btn.disabled = false; }, 5000);
  }
}

$("restart-btn").addEventListener("click", doRestart);
refreshStatus();
refreshLogs();
setInterval(refreshStatus, 5000);
setInterval(refreshLogs, 5000);
```

- [ ] **Step 5: Verify tests still pass (templates were re-parsed)**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add gitops/apps/pz-control/app/web/
git commit -m "feat(pz-control): add UI (status, logs, restart)"
```

---

## Task 7: Main entrypoint

**Why:** Wire env → Auth → Kube → Server → ListenAndServe. Fail fast on missing config.

**Files:**
- Modify: `gitops/apps/pz-control/app/main.go`

- [ ] **Step 1: Replace `main.go`**

```go
package main

import (
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"time"
)

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("missing required env var: %s", key)
	}
	return v
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	password := mustEnv("PZCTL_PASSWORD")
	signingHex := mustEnv("PZCTL_SIGNING_KEY")
	signingKey, err := hex.DecodeString(signingHex)
	if err != nil || len(signingKey) < 16 {
		log.Fatalf("PZCTL_SIGNING_KEY must be hex-encoded and >= 16 bytes: %v", err)
	}

	namespace := envOr("PZCTL_NAMESPACE", "project-zomboid")
	deployment := envOr("PZCTL_DEPLOYMENT", "project-zomboid")
	podLabel := envOr("PZCTL_POD_LABEL", "app=project-zomboid")
	addr := envOr("PZCTL_ADDR", ":8080")

	kube, err := NewKubeClient(namespace, deployment, podLabel)
	if err != nil {
		log.Fatalf("kube client: %v", err)
	}

	srv := &Server{
		Kube:         kube,
		Auth:         NewAuth(password, signingKey, 24*time.Hour),
		Cooldown:     NewCooldown(2 * time.Minute),
		SecureCookie: true,
	}

	log.Printf("pz-control listening on %s (ns=%s deploy=%s)", addr, namespace, deployment)
	log.Fatal(http.ListenAndServe(addr, srv.Router()))
}
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 3: Run tests**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add gitops/apps/pz-control/app/main.go
git commit -m "feat(pz-control): wire main entrypoint"
```

---

## Task 8: Dockerfile

**Why:** Multi-stage build: a Go builder produces a static binary, then we copy into a `distroless/static` image (tiny, no shell, no package manager).

**Files:**
- Create: `gitops/apps/pz-control/app/Dockerfile`
- Create: `gitops/apps/pz-control/app/.dockerignore`

- [ ] **Step 1: Write `Dockerfile`**

```dockerfile
# syntax=docker/dockerfile:1.7

FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/pz-control .

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /out/pz-control /pz-control
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/pz-control"]
```

- [ ] **Step 2: Write `.dockerignore`**

```
*_test.go
Dockerfile
.dockerignore
```

- [ ] **Step 3: Build the image locally**

Run:
```bash
cd gitops/apps/pz-control/app
docker buildx build --platform linux/amd64 -t zot.bealv.io/public/pz-control:v0.1.0 --load .
```

Expected: image built; `docker images | grep pz-control` shows the tag.

- [ ] **Step 4: Smoke-test the binary inside the image**

Run:
```bash
docker run --rm -e PZCTL_PASSWORD=test -e PZCTL_SIGNING_KEY=$(openssl rand -hex 32) zot.bealv.io/public/pz-control:v0.1.0
```

Expected: log line about needing in-cluster config (`kube client: in-cluster config: ...`) — that's the expected failure outside a pod and proves env parsing works.

- [ ] **Step 5: Push to Zot**

Run: `docker push zot.bealv.io/public/pz-control:v0.1.0`
Expected: push succeeds.

- [ ] **Step 6: Commit**

```bash
git add gitops/apps/pz-control/app/Dockerfile gitops/apps/pz-control/app/.dockerignore
git commit -m "feat(pz-control): add Dockerfile"
```

---

## Task 9: Kubernetes manifests

**Why:** Deployment, Service, RBAC, External Secret, HTTPRoute, Kustomize. All in the existing `project-zomboid` namespace.

**Files:**
- Create: `gitops/apps/pz-control/rbac.yaml`
- Create: `gitops/apps/pz-control/external-secret.yml`
- Create: `gitops/apps/pz-control/deployment.yaml`
- Create: `gitops/apps/pz-control/service.yaml`
- Create: `gitops/apps/pz-control/httproute.yaml`
- Create: `gitops/apps/pz-control/kustomization.yaml`

- [ ] **Step 1: Write `rbac.yaml`**

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: pz-control
  namespace: project-zomboid
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: pz-control
  namespace: project-zomboid
rules:
  - apiGroups: ["apps"]
    resources: ["deployments"]
    resourceNames: ["project-zomboid"]
    verbs: ["get", "patch"]
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list"]
  - apiGroups: [""]
    resources: ["pods/log"]
    verbs: ["get"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: pz-control
  namespace: project-zomboid
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: pz-control
subjects:
  - kind: ServiceAccount
    name: pz-control
    namespace: project-zomboid
```

- [ ] **Step 2: Provision the Vault secret out-of-band**

Before applying, populate Vault at `secrets-bealv/pz-control/auth` with:
- `password`: chosen shared password for friends
- `signing-key`: `openssl rand -hex 32`

This is a one-time manual step; document it but don't script it in this plan.

- [ ] **Step 3: Write `external-secret.yml`**

```yaml
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: pz-control-secrets
  namespace: project-zomboid
spec:
  refreshInterval: '1h'
  secretStoreRef:
    name: vault-backend
    kind: ClusterSecretStore
  target:
    name: pz-control-secrets
  data:
    - secretKey: password
      remoteRef:
        key: secrets-bealv/pz-control/auth
        property: password
    - secretKey: signing-key
      remoteRef:
        key: secrets-bealv/pz-control/auth
        property: signing-key
```

- [ ] **Step 4: Write `deployment.yaml`**

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: pz-control
  namespace: project-zomboid
  labels:
    app: pz-control
spec:
  replicas: 1
  selector:
    matchLabels:
      app: pz-control
  template:
    metadata:
      labels:
        app: pz-control
    spec:
      serviceAccountName: pz-control
      containers:
        - name: pz-control
          image: zot.bealv.io/public/pz-control:v0.1.0
          imagePullPolicy: IfNotPresent
          ports:
            - containerPort: 8080
              name: http
          env:
            - name: PZCTL_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: pz-control-secrets
                  key: password
            - name: PZCTL_SIGNING_KEY
              valueFrom:
                secretKeyRef:
                  name: pz-control-secrets
                  key: signing-key
          readinessProbe:
            httpGet: { path: /healthz, port: 8080 }
            periodSeconds: 5
          livenessProbe:
            httpGet: { path: /healthz, port: 8080 }
            periodSeconds: 30
          resources:
            requests: { cpu: "10m", memory: "32Mi" }
            limits:   { memory: "128Mi" }
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            runAsNonRoot: true
            runAsUser: 65532
            capabilities:
              drop: ["ALL"]
```

- [ ] **Step 5: Write `service.yaml`**

```yaml
apiVersion: v1
kind: Service
metadata:
  name: pz-control
  namespace: project-zomboid
spec:
  type: ClusterIP
  selector:
    app: pz-control
  ports:
    - name: http
      port: 8080
      targetPort: 8080
```

- [ ] **Step 6: Write `httproute.yaml`**

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: pz-control-http
  namespace: project-zomboid
spec:
  parentRefs:
    - name: https
      namespace: kgateway-system
      sectionName: http
  hostnames:
    - pz.bealv.io
  rules:
    - filters:
        - type: RequestRedirect
          requestRedirect:
            scheme: https
            statusCode: 301
---
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: pz-control
  namespace: project-zomboid
spec:
  parentRefs:
    - name: https
      namespace: kgateway-system
      sectionName: https
  hostnames:
    - pz.bealv.io
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /
      backendRefs:
        - name: pz-control
          port: 8080
```

- [ ] **Step 7: Write `kustomization.yaml`**

```yaml
namespace: project-zomboid
resources:
  - rbac.yaml
  - external-secret.yml
  - deployment.yaml
  - service.yaml
  - httproute.yaml
```

- [ ] **Step 8: Validate the manifests render**

Run: `kubectl kustomize gitops/apps/pz-control/`
Expected: full YAML output of all resources, no errors.

- [ ] **Step 9: Commit**

```bash
git add gitops/apps/pz-control/rbac.yaml \
        gitops/apps/pz-control/external-secret.yml \
        gitops/apps/pz-control/deployment.yaml \
        gitops/apps/pz-control/service.yaml \
        gitops/apps/pz-control/httproute.yaml \
        gitops/apps/pz-control/kustomization.yaml
git commit -m "feat(pz-control): add k8s manifests"
```

---

## Task 10: Register with Flux

**Why:** The other apps are reconciled by Flux via a `Kustomization` in `gitops/kustomizations/`. Without this entry, the manifests just sit in git.

**Files:**
- Create: `gitops/kustomizations/pz-control.yaml`

- [ ] **Step 1: Write the Flux Kustomization**

```yaml
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: pz-control
  namespace: flux-system
spec:
  interval: 5m
  path: ./gitops/apps/pz-control
  prune: true
  sourceRef:
    kind: GitRepository
    name: infra
```

- [ ] **Step 2: Commit**

```bash
git add gitops/kustomizations/pz-control.yaml
git commit -m "feat(pz-control): register Flux Kustomization"
```

---

## Task 11: Deploy and smoke-test

**Why:** Catch real-world issues (RBAC missing a verb, secret mount, ingress propagation) before declaring done.

- [ ] **Step 1: Push the branch and let Flux reconcile**

Run:
```bash
git push origin <branch-name>
```

Wait ~5 min for Flux to pick it up, or force it:

```bash
flux reconcile kustomization pz-control --with-source --kubeconfig ~/.kube/kubeconfigs/prod-k8s.yml
```

- [ ] **Step 2: Verify the pod is running**

Run:
```bash
kubectl --kubeconfig ~/.kube/kubeconfigs/prod-k8s.yml -n project-zomboid get pod -l app=pz-control
```

Expected: 1 pod `Running`, ready 1/1. If `CreateContainerConfigError`, the ExternalSecret hasn't synced yet — check with `kubectl get externalsecret -n project-zomboid pz-control-secrets`.

- [ ] **Step 3: Verify the HTTPRoute is accepted**

Run:
```bash
kubectl --kubeconfig ~/.kube/kubeconfigs/prod-k8s.yml -n project-zomboid describe httproute pz-control
```

Expected: status condition `Accepted: True`.

- [ ] **Step 4: Hit the page from the browser**

Open `https://pz.bealv.io` — expect the login page. Enter the wrong password → error. Enter the right one → main page loads, status badge shows `Running · ready` within a few seconds, logs appear.

- [ ] **Step 5: Test restart**

Click "Redémarrer le serveur", confirm. Within ~5s, the status badge should flip away from `ready`; new pod comes up after ~30-60s.

- [ ] **Step 6: Test cooldown**

Click restart again immediately. Expect a 429 / error message ("cooldown: ~2m remaining").

- [ ] **Step 7: If everything works, open a PR**

```bash
gh pr create --title "feat(pz-control): self-serve restart page for Project Zomboid" \
  --body "Adds pz.bealv.io with shared password, status, logs, restart-with-cooldown. RBAC scoped to the project-zomboid deployment."
```

---

## Open questions to resolve before starting

1. **Vault path / signing-key generation**: confirm `secrets-bealv/pz-control/auth` is the right path and that you'll provision both `password` and `signing-key` (32-byte hex) before the ExternalSecret tries to sync.
2. **Image push**: confirm you can push to `zot.bealv.io/public/...` from your dev machine.
3. **Module path**: `github.com/neferites/pz-control` is a guess — feel free to use a different name in `go mod init`.
4. **Session TTL**: 24h cookie is the default in `main.go`. Tell me if you'd prefer shorter.
