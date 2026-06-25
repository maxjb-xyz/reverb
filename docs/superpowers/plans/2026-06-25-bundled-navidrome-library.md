# Bundled Navidrome Library Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make a fresh Reverb install a single zero-dependency container by bundling Navidrome and running it as a supervised child process; "external Subsonic" stays a first-class secondary mode.

**Architecture:** A new `internal/library/embedded` package owns a **Supervisor** that execs and lifecycle-manages a bundled `navidrome` binary **iff** backend mode is built-in. The existing, conformance-tested `subsonic` adapter is reused unchanged — in built-in mode it is constructed pointed at `http://127.0.0.1:4533` with an auto-provisioned `admin` credential. Backend mode is a key-value setting; mode is inferred (external if a library adapter already exists, else built-in) when unset, so existing deployments are untouched. `main.go` gains graceful shutdown so the child is SIGTERM'd cleanly.

**Tech Stack:** Go 1.23 (modernc/cgo-free sqlite, chi, sqlc, goose), React 19 + TypeScript + Vite + Tailwind + TanStack Query + Zustand, Docker (multi-stage, `python:3.12-slim` runtime), Navidrome v0.62.0.

## Global Constraints

- **Go module:** `github.com/maxjb-xyz/reverb`. Binary `reverb`. Env prefix `REVERB_*`.
- **Navidrome version (pinned):** `0.62.0`. Release asset URL: `https://github.com/navidrome/navidrome/releases/download/v0.62.0/navidrome_0.62.0_linux_${TARGETARCH}.tar.gz` where `${TARGETARCH}` ∈ {`amd64`,`arm64`}. The tarball contains the `navidrome` binary at its root.
- **Navidrome config (env-only, no config file):** `ND_MUSICFOLDER`, `ND_DATAFOLDER`, `ND_ADDRESS=127.0.0.1` (localhost-only, never exposed), `ND_PORT=4533`, `ND_DEVAUTOCREATEADMINPASSWORD=<generated>` (always creates an `admin` user with that password), `ND_SCANSCHEDULE="@every 1h"`. The admin username is always `admin`.
- **Resource invariant:** Navidrome runs **iff** effective backend mode is `built-in`. External mode execs nothing.
- **Settings keys (key-value `settings` table, NO migration needed):** `library_backend_mode` (`built-in`|`external`), `navidrome_admin_password` (generated secret).
- **Default port:** Reverb `8090`; bundled Navidrome `127.0.0.1:4533`.
- **License:** Navidrome is GPL-3.0; shipped as an unmodified separate-process binary (mere aggregation) — compatible with Reverb's AGPL-3.0-only.
- **Build gate (must pass before any task is "done"):** `cd web && npm run build` + `go build ./... && go test -race ./...` + `cd web && npm test`. The e2e gate (`cd web && npm run e2e`) runs after the FE tasks.
- **Commit after every task.** Conventional commits (`feat(...)`, `test(...)`, `docs(...)`).

---

## File Structure

**New package `internal/library/embedded/`:**
- `embedded.go` — shared types (`Mode`, `Health`, `Credentials`, `AdminUsername`) + `ResolveMode` (pure).
- `credentials.go` — `EnsureInternalCredentials` (generate + persist the admin password).
- `naviconfig.go` — `BuildNavidromeEnv` (deterministic env slice) + `MusicDir` helper.
- `supervisor.go` — `Supervisor` (Start/Ready/Health/Shutdown), `Process`/`Runner`/`Probe` seams, `ExecRunner`, `PingProbe`.
- `*_test.go` — unit tests per file.

**Modified Go:**
- `internal/wiring/wiring.go` — `BuildLibraryAdapter` becomes mode-aware; `ServiceBundle` gains `Supervisor`; `Build` constructs it.
- `cmd/reverb/main.go` — read mode/creds, construct+Start supervisor, graceful shutdown.
- `cmd/reverb/serve.go` (new) — testable `serveWithShutdown` helper.
- `internal/api/settings.go` — `library_backend_mode` GET/PUT.
- `internal/api/library.go` + `internal/api/server.go` — `GET /library/status` handler + route.
- `internal/api/server.go` `Deps` — add `LibraryStatus func() (mode, state string)`.

**Modified FE (`web/src/`):**
- `lib/settingsApi.ts` — `libraryBackendMode` on `AppSettings`.
- `routes/Settings.tsx` — backend-mode control.
- `routes/Setup.tsx` — library step: built-in vs external.
- `lib/libraryApi.ts` (new) — `getLibraryStatus` + hook.
- `routes/Library.tsx` (or shell) — "library starting" indicator.

**Packaging:**
- `Dockerfile` — download+bundle Navidrome; add `tini` as PID 1.
- `docker-compose.yml`, `README.md` — built-in default docs.

---

### Task 1: Embedded types + `ResolveMode`

**Files:**
- Create: `internal/library/embedded/embedded.go`
- Test: `internal/library/embedded/embedded_test.go`

**Interfaces:**
- Produces: `type Mode string` (`ModeBuiltIn="built-in"`, `ModeExternal="external"`); `type Health string` (`HealthExternal`, `HealthStarting`, `HealthReady`, `HealthDegraded`); `type Credentials struct{ Username, Password string }`; `const AdminUsername = "admin"`; `func ResolveMode(setting string, hasEnabledLibraryInstance bool) Mode`.

- [ ] **Step 1: Write the failing test**

```go
package embedded

import "testing"

func TestResolveMode(t *testing.T) {
	cases := []struct {
		name        string
		setting     string
		hasLibInst  bool
		want        Mode
	}{
		{"explicit built-in", "built-in", true, ModeBuiltIn},
		{"explicit external", "external", false, ModeExternal},
		{"unset with existing library instance -> external (untouched)", "", true, ModeExternal},
		{"unset fresh install -> built-in", "", false, ModeBuiltIn},
		{"garbage value falls back to inference", "nonsense", false, ModeBuiltIn},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ResolveMode(c.setting, c.hasLibInst); got != c.want {
				t.Errorf("ResolveMode(%q,%v) = %q, want %q", c.setting, c.hasLibInst, got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/library/embedded/ -run TestResolveMode`
Expected: FAIL — package/identifiers undefined.

- [ ] **Step 3: Write minimal implementation**

```go
// Package embedded bundles and supervises a Navidrome child process so Reverb
// works out of the box with no external library server. It writes no library
// data itself — the existing subsonic adapter talks to the child over HTTP.
package embedded

type Mode string

const (
	ModeBuiltIn  Mode = "built-in"
	ModeExternal Mode = "external"
)

// Health is the supervisor's view of the child process.
type Health string

const (
	HealthExternal Health = "external" // not managing a child (external mode)
	HealthStarting Health = "starting"
	HealthReady    Health = "ready"
	HealthDegraded Health = "degraded"
)

// AdminUsername is the fixed username Navidrome auto-creates via
// ND_DEVAUTOCREATEADMINPASSWORD; the subsonic adapter authenticates as this.
const AdminUsername = "admin"

// Credentials are the internal admin credentials for the bundled Navidrome.
type Credentials struct {
	Username string
	Password string
}

// ResolveMode determines the effective backend mode. An explicit, valid setting
// wins. When unset/invalid, presence of an enabled library adapter instance
// implies external (so existing deployments are untouched); otherwise built-in.
func ResolveMode(setting string, hasEnabledLibraryInstance bool) Mode {
	switch Mode(setting) {
	case ModeBuiltIn:
		return ModeBuiltIn
	case ModeExternal:
		return ModeExternal
	default:
		if hasEnabledLibraryInstance {
			return ModeExternal
		}
		return ModeBuiltIn
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/library/embedded/ -run TestResolveMode`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/library/embedded/embedded.go internal/library/embedded/embedded_test.go
git commit -m "feat(embedded): backend Mode/Health types + ResolveMode inference"
```

---

### Task 2: Internal credential provisioning

**Files:**
- Create: `internal/library/embedded/credentials.go`
- Test: `internal/library/embedded/credentials_test.go`

**Interfaces:**
- Consumes: a settings store — define the minimal interface locally.
- Produces:
  - `type SettingStore interface { GetSetting(ctx context.Context, key string) (string, error); UpsertSetting(ctx context.Context, arg db.UpsertSettingParams) error }`
  - `const settingKeyAdminPassword = "navidrome_admin_password"`
  - `func EnsureInternalCredentials(ctx context.Context, s SettingStore) (Credentials, error)` — returns `{Username:"admin", Password:<persisted-or-newly-generated>}`. Generates a 32-byte hex password on first call and persists it; idempotent thereafter.

- [ ] **Step 1: Write the failing test**

```go
package embedded

import (
	"context"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/store"
)

func TestEnsureInternalCredentials_GeneratesOnceAndPersists(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	c1, err := EnsureInternalCredentials(ctx, st.Q())
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if c1.Username != AdminUsername {
		t.Errorf("username = %q, want %q", c1.Username, AdminUsername)
	}
	if len(c1.Password) < 16 {
		t.Errorf("password too short: %q", c1.Password)
	}

	c2, err := EnsureInternalCredentials(ctx, st.Q())
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if c2.Password != c1.Password {
		t.Errorf("password changed across calls: %q vs %q", c1.Password, c2.Password)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/library/embedded/ -run TestEnsureInternalCredentials`
Expected: FAIL — `EnsureInternalCredentials` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
package embedded

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/maxjb-xyz/reverb/internal/store/db"
)

const settingKeyAdminPassword = "navidrome_admin_password"

// SettingStore is the slice of the DB queries this package needs.
type SettingStore interface {
	GetSetting(ctx context.Context, key string) (string, error)
	UpsertSetting(ctx context.Context, arg db.UpsertSettingParams) error
}

// EnsureInternalCredentials returns the internal admin credentials for the
// bundled Navidrome, generating and persisting a strong random password the
// first time. The username is always AdminUsername.
func EnsureInternalCredentials(ctx context.Context, s SettingStore) (Credentials, error) {
	if pw, err := s.GetSetting(ctx, settingKeyAdminPassword); err == nil && pw != "" {
		return Credentials{Username: AdminUsername, Password: pw}, nil
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return Credentials{}, fmt.Errorf("embedded: generate password: %w", err)
	}
	pw := hex.EncodeToString(buf)
	if err := s.UpsertSetting(ctx, db.UpsertSettingParams{Key: settingKeyAdminPassword, Value: pw}); err != nil {
		return Credentials{}, fmt.Errorf("embedded: persist password: %w", err)
	}
	return Credentials{Username: AdminUsername, Password: pw}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/library/embedded/ -run TestEnsureInternalCredentials`
Expected: PASS

> Note: `store.Open`, `st.Migrate()`, `st.Q()` and `db.UpsertSettingParams` are the exact APIs used in `internal/api/settings_test.go`. `GetSetting` returns a non-nil error (`sql.ErrNoRows`) when the key is absent — the `err == nil && pw != ""` guard handles that.

- [ ] **Step 5: Commit**

```bash
git add internal/library/embedded/credentials.go internal/library/embedded/credentials_test.go
git commit -m "feat(embedded): EnsureInternalCredentials generates+persists admin password"
```

---

### Task 3: Navidrome env generation

**Files:**
- Create: `internal/library/embedded/naviconfig.go`
- Test: `internal/library/embedded/naviconfig_test.go`

**Interfaces:**
- Produces:
  - `type NaviOptions struct { MusicDir, DataDir, Address string; Port int; AdminPassword, ScanSchedule string }`
  - `func BuildNavidromeEnv(o NaviOptions) []string` — returns `ND_*` env entries.
  - `func MusicDir(getenv func(string) string) string` — `getenv("REVERB_DOWNLOAD_DIR")` or `/music`.
  - `func DefaultNaviOptions(dataDir, musicDir, adminPassword string) NaviOptions` — fills `Address="127.0.0.1"`, `Port=4533`, `ScanSchedule="@every 1h"`, `DataDir=filepath.Join(dataDir,"navidrome")`.

- [ ] **Step 1: Write the failing test**

```go
package embedded

import (
	"strings"
	"testing"
)

func envMap(entries []string) map[string]string {
	m := map[string]string{}
	for _, e := range entries {
		if i := strings.IndexByte(e, '='); i >= 0 {
			m[e[:i]] = e[i+1:]
		}
	}
	return m
}

func TestBuildNavidromeEnv_LocalhostAndCreds(t *testing.T) {
	o := DefaultNaviOptions("/data", "/music", "s3cret")
	m := envMap(BuildNavidromeEnv(o))

	if m["ND_ADDRESS"] != "127.0.0.1" {
		t.Errorf("ND_ADDRESS = %q, want 127.0.0.1 (localhost-only)", m["ND_ADDRESS"])
	}
	if m["ND_PORT"] != "4533" {
		t.Errorf("ND_PORT = %q, want 4533", m["ND_PORT"])
	}
	if m["ND_MUSICFOLDER"] != "/music" {
		t.Errorf("ND_MUSICFOLDER = %q", m["ND_MUSICFOLDER"])
	}
	if m["ND_DATAFOLDER"] != "/data/navidrome" {
		t.Errorf("ND_DATAFOLDER = %q, want /data/navidrome", m["ND_DATAFOLDER"])
	}
	if m["ND_DEVAUTOCREATEADMINPASSWORD"] != "s3cret" {
		t.Errorf("ND_DEVAUTOCREATEADMINPASSWORD = %q", m["ND_DEVAUTOCREATEADMINPASSWORD"])
	}
}

func TestMusicDir_DefaultsToMusic(t *testing.T) {
	if got := MusicDir(func(string) string { return "" }); got != "/music" {
		t.Errorf("MusicDir default = %q, want /music", got)
	}
	if got := MusicDir(func(k string) string {
		if k == "REVERB_DOWNLOAD_DIR" {
			return "/songs"
		}
		return ""
	}); got != "/songs" {
		t.Errorf("MusicDir override = %q, want /songs", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/library/embedded/ -run 'TestBuildNavidromeEnv|TestMusicDir'`
Expected: FAIL — undefined identifiers.

- [ ] **Step 3: Write minimal implementation**

```go
package embedded

import (
	"os"
	"path/filepath"
	"strconv"
)

// NaviOptions are the inputs needed to launch the bundled Navidrome.
type NaviOptions struct {
	MusicDir      string
	DataDir       string // Navidrome's own data/DB dir
	Address       string
	Port          int
	AdminPassword string
	ScanSchedule  string
}

// DefaultNaviOptions returns localhost-bound options with Navidrome's data dir
// nested under Reverb's data dir.
func DefaultNaviOptions(reverbDataDir, musicDir, adminPassword string) NaviOptions {
	return NaviOptions{
		MusicDir:      musicDir,
		DataDir:       filepath.Join(reverbDataDir, "navidrome"),
		Address:       "127.0.0.1",
		Port:          4533,
		AdminPassword: adminPassword,
		ScanSchedule:  "@every 1h",
	}
}

// BuildNavidromeEnv renders the ND_* environment for the child process. The
// process inherits the parent env plus these (later entries win in os/exec).
func BuildNavidromeEnv(o NaviOptions) []string {
	env := append([]string{}, os.Environ()...)
	return append(env,
		"ND_MUSICFOLDER="+o.MusicDir,
		"ND_DATAFOLDER="+o.DataDir,
		"ND_ADDRESS="+o.Address,
		"ND_PORT="+strconv.Itoa(o.Port),
		"ND_DEVAUTOCREATEADMINPASSWORD="+o.AdminPassword,
		"ND_SCANSCHEDULE="+o.ScanSchedule,
	)
}

// MusicDir resolves the music folder (shared with the download output dir).
func MusicDir(getenv func(string) string) string {
	if d := getenv("REVERB_DOWNLOAD_DIR"); d != "" {
		return d
	}
	return "/music"
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/library/embedded/ -run 'TestBuildNavidromeEnv|TestMusicDir'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/library/embedded/naviconfig.go internal/library/embedded/naviconfig_test.go
git commit -m "feat(embedded): BuildNavidromeEnv + localhost-only Navidrome options"
```

---

### Task 4: Supervisor — start, readiness, shutdown, mode-gating

**Files:**
- Create: `internal/library/embedded/supervisor.go`
- Test: `internal/library/embedded/supervisor_test.go`

**Interfaces:**
- Produces:
  - `type Process interface { Wait() error }`
  - `type Runner func(ctx context.Context, env []string) (Process, error)`
  - `type Probe func(ctx context.Context) error`
  - `type Options struct { Mode Mode; Env []string; Runner Runner; Probe Probe; ProbeEvery, RestartDelay time.Duration; MaxRestarts int }`
  - `func New(o Options) *Supervisor`
  - `func (s *Supervisor) Start()` — non-blocking; no-op (Health=external) when `Mode != ModeBuiltIn`.
  - `func (s *Supervisor) Ready() bool`, `func (s *Supervisor) Health() Health`
  - `func (s *Supervisor) Shutdown(ctx context.Context) error`
  - `func ExecRunner(binaryPath string) Runner`, `func PingProbe(baseURL string, hc *http.Client) Probe`

- [ ] **Step 1: Write the failing test**

```go
package embedded

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeProcess returns from Wait when its ctx is canceled or crash is signaled.
type fakeProcess struct {
	ctx   context.Context
	crash chan struct{}
}

func (p *fakeProcess) Wait() error {
	select {
	case <-p.ctx.Done():
		return p.ctx.Err()
	case <-p.crash:
		return errors.New("crashed")
	}
}

func TestSupervisor_ExternalMode_RunsNothing(t *testing.T) {
	var started bool
	s := New(Options{
		Mode:   ModeExternal,
		Runner: func(ctx context.Context, _ []string) (Process, error) { started = true; return nil, nil },
		Probe:  func(context.Context) error { return nil },
	})
	s.Start()
	if started {
		t.Fatal("external mode must not start a child")
	}
	if s.Health() != HealthExternal {
		t.Errorf("health = %q, want external", s.Health())
	}
	if err := s.Shutdown(context.Background()); err != nil {
		t.Errorf("shutdown: %v", err)
	}
}

func TestSupervisor_BuiltIn_BecomesReadyThenShutsDown(t *testing.T) {
	var mu sync.Mutex
	var proc *fakeProcess
	s := New(Options{
		Mode: ModeBuiltIn,
		Runner: func(ctx context.Context, _ []string) (Process, error) {
			mu.Lock()
			proc = &fakeProcess{ctx: ctx, crash: make(chan struct{})}
			mu.Unlock()
			return proc, nil
		},
		Probe:      func(context.Context) error { return nil }, // immediately ready
		ProbeEvery: time.Millisecond,
	})
	s.Start()

	deadline := time.Now().Add(2 * time.Second)
	for !s.Ready() && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if !s.Ready() {
		t.Fatalf("never became ready; health=%q", s.Health())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/library/embedded/ -run TestSupervisor`
Expected: FAIL — `New`/`Supervisor` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
package embedded

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// Process is a running child (test seam).
type Process interface{ Wait() error }

// Runner starts a child process with env and returns it.
type Runner func(ctx context.Context, env []string) (Process, error)

// Probe reports nil when the child is serving.
type Probe func(ctx context.Context) error

type Options struct {
	Mode         Mode
	Env          []string
	Runner       Runner
	Probe        Probe
	ProbeEvery   time.Duration
	RestartDelay time.Duration
	MaxRestarts  int
}

type Supervisor struct {
	opts     Options
	mu       sync.Mutex
	health   Health
	sawReady bool
	cancel   context.CancelFunc
	done     chan struct{}
}

func New(o Options) *Supervisor {
	if o.ProbeEvery == 0 {
		o.ProbeEvery = 500 * time.Millisecond
	}
	if o.RestartDelay == 0 {
		o.RestartDelay = time.Second
	}
	if o.MaxRestarts == 0 {
		o.MaxRestarts = 5
	}
	h := HealthStarting
	if o.Mode != ModeBuiltIn {
		h = HealthExternal
	}
	return &Supervisor{opts: o, health: h, done: make(chan struct{})}
}

func (s *Supervisor) Health() Health { s.mu.Lock(); defer s.mu.Unlock(); return s.health }
func (s *Supervisor) Ready() bool    { return s.Health() == HealthReady }

func (s *Supervisor) setHealth(h Health) { s.mu.Lock(); s.health = h; s.mu.Unlock() }

// Start launches the supervise loop. No-op (beyond external health) when not built-in.
func (s *Supervisor) Start() {
	if s.opts.Mode != ModeBuiltIn {
		close(s.done)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	go s.supervise(ctx)
}

func (s *Supervisor) supervise(ctx context.Context) {
	defer close(s.done)
	restarts := 0
	for {
		proc, err := s.opts.Runner(ctx, s.opts.Env)
		if err != nil {
			log.Printf("navidrome: start failed: %v", err)
		} else {
			s.mu.Lock()
			s.sawReady = false
			s.mu.Unlock()
			readyCtx, stopReady := context.WithCancel(ctx)
			go s.waitReady(readyCtx)
			werr := proc.Wait()
			stopReady()
			if ctx.Err() != nil {
				return // shutting down
			}
			log.Printf("navidrome: exited: %v", werr)
		}
		if ctx.Err() != nil {
			return
		}
		s.mu.Lock()
		hadReady := s.sawReady
		s.mu.Unlock()
		if hadReady {
			restarts = 0 // a previously-healthy instance crashed: fresh budget
		} else {
			restarts++
		}
		if restarts >= s.opts.MaxRestarts {
			s.setHealth(HealthDegraded)
			log.Printf("navidrome: %d consecutive failures — degraded; stopping restarts", restarts)
			return
		}
		s.setHealth(HealthStarting)
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.opts.RestartDelay * time.Duration(restarts+1)):
		}
	}
}

func (s *Supervisor) waitReady(ctx context.Context) {
	t := time.NewTicker(s.opts.ProbeEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.opts.Probe(ctx); err == nil {
				s.mu.Lock()
				if s.health != HealthDegraded {
					s.health = HealthReady
				}
				s.sawReady = true
				s.mu.Unlock()
				return
			}
		}
	}
}

// Shutdown cancels the supervise loop (which SIGTERMs the child via ExecRunner's
// cmd.Cancel) and waits for it to exit, or until ctx is done.
func (s *Supervisor) Shutdown(ctx context.Context) error {
	if s.cancel != nil {
		s.cancel()
	}
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ExecRunner runs the real navidrome binary. Context cancel sends SIGTERM (via
// cmd.Cancel), then SIGKILL after WaitDelay — a graceful child shutdown.
func ExecRunner(binaryPath string) Runner {
	return func(ctx context.Context, env []string) (Process, error) {
		cmd := exec.CommandContext(ctx, binaryPath)
		cmd.Env = env
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
		cmd.WaitDelay = 10 * time.Second
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return execProcess{cmd}, nil
	}
}

type execProcess struct{ cmd *exec.Cmd }

func (p execProcess) Wait() error { return p.cmd.Wait() }

// PingProbe returns a Probe that hits the Subsonic ping endpoint (auth omitted —
// any HTTP response means the server is up and accepting connections).
func PingProbe(baseURL string, hc *http.Client) Probe {
	if hc == nil {
		hc = &http.Client{Timeout: 3 * time.Second}
	}
	return func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/rest/ping", nil)
		if err != nil {
			return err
		}
		resp, err := hc.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()
		return nil
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/library/embedded/ -run TestSupervisor -race`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/library/embedded/supervisor.go internal/library/embedded/supervisor_test.go
git commit -m "feat(embedded): Supervisor start/ready/shutdown + mode-gating + Exec/Ping seams"
```

---

### Task 5: Supervisor — restart with backoff, degraded after cap

**Files:**
- Modify: `internal/library/embedded/supervisor_test.go` (add tests)

**Interfaces:**
- Consumes: `New`, `Options`, `Supervisor`, `fakeProcess` from Task 4.
- Produces: no new symbols — verifies restart/backoff/degraded behavior already implemented in Task 4's `supervise`.

- [ ] **Step 1: Write the failing test**

```go
func TestSupervisor_CrashLoop_GoesDegraded(t *testing.T) {
	var starts int
	var mu sync.Mutex
	s := New(Options{
		Mode: ModeBuiltIn,
		Runner: func(ctx context.Context, _ []string) (Process, error) {
			mu.Lock()
			starts++
			mu.Unlock()
			// crash immediately: Wait returns at once
			crash := make(chan struct{})
			close(crash)
			return &fakeProcess{ctx: ctx, crash: crash}, nil
		},
		Probe:        func(context.Context) error { return errors.New("never ready") },
		ProbeEvery:   time.Millisecond,
		RestartDelay: time.Millisecond,
		MaxRestarts:  3,
	})
	s.Start()

	deadline := time.Now().Add(2 * time.Second)
	for s.Health() != HealthDegraded && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if s.Health() != HealthDegraded {
		t.Fatalf("health = %q, want degraded", s.Health())
	}
	mu.Lock()
	got := starts
	mu.Unlock()
	if got != 3 {
		t.Errorf("runner started %d times, want 3 (MaxRestarts)", got)
	}
	if err := s.Shutdown(context.Background()); err != nil {
		t.Errorf("shutdown after degraded: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails (or passes if Task 4 is correct)**

Run: `go test ./internal/library/embedded/ -run TestSupervisor_CrashLoop -race`
Expected: PASS (Task 4 implemented the behavior). If it FAILS, fix `supervise` in `supervisor.go` until green — the loop must stop after exactly `MaxRestarts` starts and set `HealthDegraded`.

- [ ] **Step 3: (only if red) adjust `supervise`**

No new code expected. If the count is off-by-one, the fix is the loop ordering already shown in Task 4 (increment then compare `>= MaxRestarts`).

- [ ] **Step 4: Run the whole package with race**

Run: `go test ./internal/library/embedded/ -race`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/library/embedded/supervisor_test.go
git commit -m "test(embedded): supervisor crash-loops to degraded after MaxRestarts"
```

---

### Task 6: Mode-aware library adapter construction

**Files:**
- Modify: `internal/wiring/wiring.go` (`BuildLibraryAdapter`, `ServiceBundle`, `Build`)
- Test: `internal/wiring/wiring_test.go` (add a test; create the file if absent)

**Interfaces:**
- Consumes: `embedded.ResolveMode`, `embedded.Mode`, `embedded.Credentials`, `embedded.AdminUsername`; `subsonic.New()`; `db.AdapterInstance`.
- Produces:
  - Updated `func BuildLibraryAdapter(ctx, reg *registry.Registry, instances []db.AdapterInstance, getenv func(string) string, mode embedded.Mode, creds embedded.Credentials) (library.LibraryAdapter, error)` — when `mode == ModeBuiltIn`, builds a `subsonic` adapter with `url="http://127.0.0.1:4533"`, `username=creds.Username`, `password=creds.Password`, ignoring `instances`. Otherwise unchanged behavior.
  - `ServiceBundle.Supervisor *embedded.Supervisor` (set in a later task; field added here).

- [ ] **Step 1: Write the failing test**

```go
package wiring

import (
	"context"
	"testing"

	"github.com/maxjb-xyz/reverb/internal/library/embedded"
	"github.com/maxjb-xyz/reverb/internal/library/subsonic"
	"github.com/maxjb-xyz/reverb/internal/registry"
	"github.com/maxjb-xyz/reverb/internal/store/db"
)

func libReg() *registry.Registry {
	reg := registry.NewRegistry("library")
	reg.Register("subsonic", func() registry.Plugin { return subsonic.New() })
	return reg
}

func TestBuildLibraryAdapter_BuiltIn_IgnoresInstancesUsesLocalhost(t *testing.T) {
	// No library instances at all, built-in mode -> still get a configured adapter.
	lib, err := BuildLibraryAdapter(
		context.Background(), libReg(), nil, func(string) string { return "" },
		embedded.ModeBuiltIn, embedded.Credentials{Username: "admin", Password: "pw"},
	)
	if err != nil {
		t.Fatalf("built-in build: %v", err)
	}
	if lib == nil {
		t.Fatal("built-in mode must synthesize a library adapter")
	}
	if lib.Name() != "subsonic" {
		t.Errorf("adapter = %q, want subsonic", lib.Name())
	}
}

func TestBuildLibraryAdapter_External_UsesInstanceConfig(t *testing.T) {
	inst := []db.AdapterInstance{{
		ID: "x", Type: "library", Name: "subsonic", Enabled: 1,
		ConfigJson: `{"url":"http://nav.example:4533","username":"u","password":"p"}`,
	}}
	lib, err := BuildLibraryAdapter(
		context.Background(), libReg(), inst, func(string) string { return "" },
		embedded.ModeExternal, embedded.Credentials{},
	)
	if err != nil {
		t.Fatalf("external build: %v", err)
	}
	if lib == nil {
		t.Fatal("external mode with a configured instance must build an adapter")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/wiring/ -run TestBuildLibraryAdapter`
Expected: FAIL — `BuildLibraryAdapter` signature mismatch (too few args).

- [ ] **Step 3: Write minimal implementation**

In `internal/wiring/wiring.go`, replace the existing `BuildLibraryAdapter` (currently lines ~28–69) with the mode-aware version:

```go
// BuildLibraryAdapter builds the active library adapter. In built-in mode it
// synthesizes a subsonic adapter pointed at the bundled localhost Navidrome with
// the internal admin credentials, ignoring stored instances. In external mode it
// uses the first enabled "library" adapter instance (legacy behavior).
func BuildLibraryAdapter(
	ctx context.Context,
	reg *registry.Registry,
	instances []db.AdapterInstance,
	getenv func(string) string,
	mode embedded.Mode,
	creds embedded.Credentials,
) (library.LibraryAdapter, error) {
	if mode == embedded.ModeBuiltIn {
		plugin, err := reg.Create("subsonic")
		if err != nil {
			return nil, fmt.Errorf("built-in library: %w", err)
		}
		lib, ok := plugin.(library.LibraryAdapter)
		if !ok {
			return nil, fmt.Errorf("built-in library: subsonic is not a LibraryAdapter")
		}
		if err := lib.Init(map[string]any{
			"url":      "http://127.0.0.1:4533",
			"username": creds.Username,
			"password": creds.Password,
		}); err != nil {
			return nil, fmt.Errorf("built-in library init: %w", err)
		}
		return lib, nil
	}

	// external mode (unchanged behavior)
	var inst *db.AdapterInstance
	for i := range instances {
		if instances[i].Type == "library" && instances[i].Enabled == 1 {
			inst = &instances[i]
			break
		}
	}
	if inst == nil {
		return nil, nil
	}
	plugin, err := reg.Create(inst.Name)
	if err != nil {
		return nil, fmt.Errorf("library adapter %q: %w", inst.Name, err)
	}
	lib, ok := plugin.(library.LibraryAdapter)
	if !ok {
		return nil, fmt.Errorf("library adapter %q: not a LibraryAdapter", inst.Name)
	}
	cfg := map[string]any{}
	if inst.ConfigJson != "" {
		if err := json.Unmarshal([]byte(inst.ConfigJson), &cfg); err != nil {
			return nil, fmt.Errorf("library adapter %q config: %w", inst.Name, err)
		}
	}
	if pw := getenv("REVERB_LIBRARY_PASSWORD"); pw != "" {
		cfg["password"] = pw
	}
	if err := lib.Init(cfg); err != nil {
		return nil, fmt.Errorf("library adapter %q init: %w", inst.Name, err)
	}
	return lib, nil
}
```

Add the import `"github.com/maxjb-xyz/reverb/internal/library/embedded"` to `wiring.go`. Add a field to `ServiceBundle` (struct near line ~226):

```go
	Supervisor *embedded.Supervisor // bundled Navidrome supervisor; nil in external mode wiring helpers
```

Update the **single** call site inside `Builder.Build` (currently `libAdapter, err := BuildLibraryAdapter(ctx, b.libraryReg, instances, b.getenv)`) to resolve mode + creds first:

```go
	// Resolve effective backend mode and (if built-in) ensure internal creds.
	modeSetting, _ := b.queries.GetSetting(ctx, "library_backend_mode")
	hasLibInst := false
	for i := range instances {
		if instances[i].Type == "library" && instances[i].Enabled == 1 {
			hasLibInst = true
			break
		}
	}
	mode := embedded.ResolveMode(modeSetting, hasLibInst)
	var creds embedded.Credentials
	if mode == embedded.ModeBuiltIn {
		creds, err = embedded.EnsureInternalCredentials(ctx, b.queries)
		if err != nil {
			return bundle, fmt.Errorf("built-in library credentials: %w", err)
		}
	}
	libAdapter, err := BuildLibraryAdapter(ctx, b.libraryReg, instances, b.getenv, mode, creds)
```

> `b.queries` is the `*db.Queries` already held by `Builder`. `bundle` and `err` are already in scope at that point in `Build`. Keep the existing `bundle.Library = libAdapter` and subsequent logic unchanged.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/wiring/ -run TestBuildLibraryAdapter && go build ./...`
Expected: PASS + build clean.

- [ ] **Step 5: Commit**

```bash
git add internal/wiring/wiring.go internal/wiring/wiring_test.go
git commit -m "feat(wiring): mode-aware BuildLibraryAdapter (built-in -> localhost Navidrome)"
```

---

### Task 7: Compose supervisor into main + graceful shutdown

**Files:**
- Create: `cmd/reverb/serve.go`
- Test: `cmd/reverb/serve_test.go`
- Modify: `cmd/reverb/main.go`
- Modify: `internal/wiring/wiring.go` (`Build` constructs `bundle.Supervisor`)

**Interfaces:**
- Consumes: `embedded.New/Options/ExecRunner/PingProbe/BuildNavidromeEnv/DefaultNaviOptions/MusicDir`, `embedded.Supervisor.Shutdown`, `http.Server`.
- Produces: `func serveWithShutdown(srv *http.Server, ln net.Listener, stop <-chan struct{}, onShutdown func(context.Context) error) error` in `cmd/reverb/serve.go`.

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServeWithShutdown_RunsHookOnStop(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.NewServeMux()}
	stop := make(chan struct{})
	hookRan := make(chan struct{})

	go func() {
		_ = serveWithShutdown(srv, ln, stop, func(context.Context) error {
			close(hookRan)
			return nil
		})
	}()

	time.Sleep(20 * time.Millisecond)
	close(stop)

	select {
	case <-hookRan:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown hook did not run after stop")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/reverb/ -run TestServeWithShutdown`
Expected: FAIL — `serveWithShutdown` undefined.

- [ ] **Step 3: Write minimal implementation**

Create `cmd/reverb/serve.go`:

```go
package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"
)

// serveWithShutdown serves until `stop` is closed, then gracefully shuts the
// HTTP server down and runs onShutdown (e.g. to SIGTERM the Navidrome child).
func serveWithShutdown(srv *http.Server, ln net.Listener, stop <-chan struct{}, onShutdown func(context.Context) error) error {
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case err := <-errCh:
		return err
	case <-stop:
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	if onShutdown != nil {
		_ = onShutdown(ctx)
	}
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
```

In `cmd/reverb/main.go`, replace the final blocking listen (currently `if err := http.ListenAndServe(addr, srv.Handler()); err != nil { log.Fatal(err) }`) with signal-driven serving + supervisor shutdown. Add imports `net`, `os/signal`, `syscall`. Construct the supervisor near where `bundle.Manager` is started and start it BEFORE serving:

```go
	// Start the bundled-library supervisor (no-op in external mode).
	if bundle.Supervisor != nil {
		bundle.Supervisor.Start()
	}

	// ... existing deps/server construction ...

	addr := fmt.Sprintf(":%d", cfg.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("reverb listening on %s (dev=%v)", addr, cfg.Dev)

	stop := make(chan struct{})
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sig; close(stop) }()

	httpSrv := &http.Server{Handler: srv.Handler()}
	if err := serveWithShutdown(httpSrv, ln, stop, func(ctx context.Context) error {
		if bundle.Supervisor != nil {
			return bundle.Supervisor.Shutdown(ctx)
		}
		return nil
	}); err != nil {
		log.Fatal(err)
	}
```

In `internal/wiring/wiring.go` `Build`, after the library adapter is built, construct the supervisor (only meaningful in built-in mode; in external mode it is a no-op supervisor):

First add a `naviBin()` helper to `Builder` (so an empty `REVERB_NAVIDROME_BIN` defaults to the bundled binary on PATH rather than failing to exec):

```go
func (b *Builder) naviBin() string {
	if v := b.getenv("REVERB_NAVIDROME_BIN"); v != "" {
		return v
	}
	return "navidrome" // resolved on PATH (bundled at /usr/local/bin/navidrome)
}
```

Then construct the supervisor (uses the new `b.dataDir` field — see wiring note below):

```go
	var naviEnv []string
	if mode == embedded.ModeBuiltIn {
		opts := embedded.DefaultNaviOptions(b.dataDir, embedded.MusicDir(b.getenv), creds.Password)
		naviEnv = embedded.BuildNavidromeEnv(opts)
	}
	bundle.Supervisor = embedded.New(embedded.Options{
		Mode:   mode,
		Env:    naviEnv,
		Runner: embedded.ExecRunner(b.naviBin()),
		Probe:  embedded.PingProbe("http://127.0.0.1:4533", nil),
	})
```

The Builder doesn't currently know Reverb's data dir, so thread it in: add a `dataDir string` field to `Builder`, set it in `NewBuilder` from a new trailing parameter, and pass `filepath.Dir(cfg.DBPath)` from `main.go`. Navidrome's data dir then nests under it (`DefaultNaviOptions` does the `filepath.Join(dataDir, "navidrome")`).

> **Wiring the new `NewBuilder` parameter:** update `NewBuilder` to accept `dataDir string` as the last argument and store it; update the single call site in `main.go` (currently `wiring.NewBuilder(libraryReg, searchReg, downloaderReg, st.Q(), st, bus, download.RealClock{}, os.Getenv)`) to append `filepath.Dir(cfg.DBPath)`. `filepath` is already imported in `main.go` (used for `DataDir`).

- [ ] **Step 4: Run tests + build**

Run: `go build ./... && go test ./cmd/reverb/ ./internal/wiring/ -race`
Expected: PASS + build clean.

- [ ] **Step 5: Commit**

```bash
git add cmd/reverb/serve.go cmd/reverb/serve_test.go cmd/reverb/main.go internal/wiring/wiring.go
git commit -m "feat(reverb): supervise bundled Navidrome + graceful shutdown (SIGTERM child)"
```

---

### Task 8: Settings API — `library_backend_mode`

**Files:**
- Modify: `internal/api/settings.go`
- Test: `internal/api/settings_test.go` (add a test mirroring `TestDefaultDownloaderSetting`)

**Interfaces:**
- Consumes: `db.UpsertSettingParams`, `GetSetting` (via `s.deps.Adapters`).
- Produces: settings DTO field `libraryBackendMode`; PUT validation accepting only `""`, `"built-in"`, `"external"`; constant `keyLibraryBackendMode = "library_backend_mode"`.

- [ ] **Step 1: Write the failing test**

```go
func TestLibraryBackendModeSetting(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/s.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	authSvc := auth.NewService(st.Q(), time.Now)
	_ = authSvc.SetAdminPassword(context.Background(), "pw")
	tok, _ := authSvc.CreateSession(context.Background())
	srv := NewServer(Deps{Auth: authSvc, Adapters: st.Q()})
	cookie := &http.Cookie{Name: sessionCookie, Value: tok}

	put := func(body string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", bytes.NewBufferString(body))
		req.AddCookie(cookie)
		srv.Handler().ServeHTTP(rec, req)
		return rec.Code
	}
	if code := put(`{"libraryBackendMode":"external"}`); code != http.StatusOK {
		t.Fatalf("set external = %d", code)
	}
	if code := put(`{"libraryBackendMode":"built-in"}`); code != http.StatusOK {
		t.Fatalf("set built-in = %d", code)
	}
	if code := put(`{"libraryBackendMode":"bogus"}`); code != http.StatusBadRequest {
		t.Fatalf("bogus mode = %d, want 400", code)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	req.AddCookie(cookie)
	srv.Handler().ServeHTTP(rec, req)
	var dto struct {
		LibraryBackendMode string `json:"libraryBackendMode"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &dto)
	if dto.LibraryBackendMode != "built-in" {
		t.Fatalf("mode = %q, want built-in", dto.LibraryBackendMode)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestLibraryBackendModeSetting`
Expected: FAIL — field never set, GET returns empty.

- [ ] **Step 3: Write minimal implementation**

In `internal/api/settings.go`: add the key constant alongside the others:

```go
	keyLibraryBackendMode = "library_backend_mode"
```

Add to `settingsDTO`:

```go
	LibraryBackendMode string `json:"libraryBackendMode"`
```

In `currentSettings`, after the other reads:

```go
	if v, err := s.deps.Adapters.GetSetting(r.Context(), keyLibraryBackendMode); err == nil && v != "" {
		out.LibraryBackendMode = v
	}
```

Add to `putSettingsBody`:

```go
	LibraryBackendMode *string `json:"libraryBackendMode"`
```

In `handlePutSettings`, before the final `writeJSON`:

```go
	if body.LibraryBackendMode != nil {
		mode := *body.LibraryBackendMode
		if mode != "" && mode != "built-in" && mode != "external" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "libraryBackendMode must be empty, \"built-in\", or \"external\""})
			return
		}
		if err := s.deps.Adapters.UpsertSetting(r.Context(), db.UpsertSettingParams{Key: keyLibraryBackendMode, Value: mode}); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not save settings"})
			return
		}
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/api/ -run TestLibraryBackendModeSetting`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/settings.go internal/api/settings_test.go
git commit -m "feat(settings): library_backend_mode setting (built-in|external, validated)"
```

---

### Task 9: Library status endpoint

**Files:**
- Modify: `internal/api/server.go` (`Deps` + route)
- Create: `internal/api/library_status.go`
- Test: `internal/api/library_status_test.go`
- Modify: `cmd/reverb/main.go` (provide `LibraryStatus` closure)

**Interfaces:**
- Consumes: `s.deps.LibraryStatus func() (mode string, state string)`.
- Produces: `GET /api/v1/library/status` → `{"mode":"built-in|external","state":"starting|ready|degraded|external|unconfigured"}`. When `deps.LibraryStatus == nil`, fall back to `{mode:"external", state:"ready"}` if a library is present else `unconfigured`.

- [ ] **Step 1: Write the failing test**

```go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLibraryStatus_ReportsSupervisorState(t *testing.T) {
	srv := NewServer(Deps{
		LibraryStatus: func() (string, string) { return "built-in", "starting" },
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/library/status", nil)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var dto struct{ Mode, State string }
	_ = json.Unmarshal(rec.Body.Bytes(), &dto)
	if dto.Mode != "built-in" || dto.State != "starting" {
		t.Fatalf("got %+v, want built-in/starting", dto)
	}
}
```

> Confirm whether `/library/status` should require auth by checking how `/library/*` routes are mounted in `server.go` (the protected router `pr`). Mount it on the same router as the other `/library` routes; if those are protected, add the session cookie to the test as `settings_test.go` does. The snippet above assumes it is public-readable; adjust to match neighboring `/library` routes.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestLibraryStatus`
Expected: FAIL — route 404 / `LibraryStatus` field undefined.

- [ ] **Step 3: Write minimal implementation**

Add to `Deps` in `server.go`:

```go
	// LibraryStatus reports (mode, state) for the bundled-library status endpoint.
	// nil in tests/legacy — handler falls back.
	LibraryStatus func() (mode string, state string)
```

Create `internal/api/library_status.go`:

```go
package api

import "net/http"

func (s *Server) handleLibraryStatus(w http.ResponseWriter, r *http.Request) {
	if s.deps.LibraryStatus != nil {
		mode, state := s.deps.LibraryStatus()
		writeJSON(w, http.StatusOK, map[string]string{"mode": mode, "state": state})
		return
	}
	if s.library() != nil {
		writeJSON(w, http.StatusOK, map[string]string{"mode": "external", "state": "ready"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"mode": "external", "state": "unconfigured"})
}
```

Register the route next to the other `/library` routes in `server.go` (mirror their router + auth):

```go
	pr.Get("/library/status", s.handleLibraryStatus)
```

In `cmd/reverb/main.go`, populate the closure when building `deps` (after `bundle.Supervisor` exists):

```go
	if bundle.Supervisor != nil {
		sup := bundle.Supervisor
		modeStr := "external"
		if h := sup.Health(); h != embedded.HealthExternal {
			modeStr = "built-in"
		}
		deps.LibraryStatus = func() (string, string) {
			h := sup.Health()
			if h == embedded.HealthExternal {
				if bundle.Library != nil {
					return "external", "ready"
				}
				return "external", "unconfigured"
			}
			return modeStr, string(h)
		}
	}
```

Add the `embedded` import to `main.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/api/ -run TestLibraryStatus && go build ./...`
Expected: PASS + build clean.

- [ ] **Step 5: Commit**

```bash
git add internal/api/server.go internal/api/library_status.go internal/api/library_status_test.go cmd/reverb/main.go
git commit -m "feat(api): GET /library/status reports bundled-library state"
```

---

### Task 10: Frontend — backend-mode control in Settings

**Files:**
- Modify: `web/src/lib/settingsApi.ts`
- Modify: `web/src/routes/Settings.tsx`
- Test: `web/src/routes/Settings.test.tsx`

**Interfaces:**
- Consumes: `useSettings`, `useUpdateSettings`, `Select` (`{ value, options, onChange, label }`).
- Produces: `AppSettings.libraryBackendMode: string`; a "Library backend" `Select` in the Appearance tab saving `{ libraryBackendMode }`.

- [ ] **Step 1: Write the failing test**

Add to `web/src/routes/Settings.test.tsx` (mirror the `defaultDownloader` test). Ensure the `useSettings` mock returns `libraryBackendMode`:

```typescript
it('shows a Library backend select and saves the choice', () => {
  wrap(<Settings />)
  const select = screen.getByLabelText('Library backend')
  fireEvent.change(select, { target: { value: 'external' } })
  expect(mockMutate).toHaveBeenCalledWith({ libraryBackendMode: 'external' })
})
```

If the file's `useSettings` mock has an explicit `data` object, add `libraryBackendMode: 'built-in'` to it.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/routes/Settings.test.tsx -t "Library backend"`
Expected: FAIL — label not found.

- [ ] **Step 3: Write minimal implementation**

In `web/src/lib/settingsApi.ts`, extend the interface:

```typescript
export interface AppSettings {
  accentColor: string
  dynamicBackground: boolean
  defaultDownloader: string
  libraryBackendMode: string // 'built-in' | 'external'
}
```

In `web/src/routes/Settings.tsx`, add a row in the Appearance tab (after the Default downloader row), mirroring its structure:

```tsx
<Select
  label="Library backend"
  value={settings.data?.libraryBackendMode ?? 'built-in'}
  options={[
    { value: 'built-in', label: 'Built-in (bundled)' },
    { value: 'external', label: 'External Subsonic' },
  ]}
  onChange={(v) => updateSettings.mutate({ libraryBackendMode: v })}
/>
```

Add an explanatory caption next to it: "Built-in runs a managed music server for your folder. External connects to your own Navidrome/Subsonic server (configure it under Admin). Changing this takes effect after a restart."

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run src/routes/Settings.test.tsx`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/settingsApi.ts web/src/routes/Settings.tsx web/src/routes/Settings.test.tsx
git commit -m "feat(web): Library backend mode control in Settings"
```

---

### Task 11: Frontend — setup wizard library step (built-in vs external)

**Files:**
- Modify: `web/src/routes/Setup.tsx`
- Test: `web/src/routes/Setup.test.tsx`

**Interfaces:**
- Consumes: `api.put('/settings', { libraryBackendMode })`, `createAdapter`, `AdapterForm`.
- Produces: a library step that offers "Built-in" (sets `libraryBackendMode='built-in'` then advances) and "Connect existing Subsonic" (sets `libraryBackendMode='external'`, shows the subsonic `AdapterForm`, creates the adapter, advances).

- [ ] **Step 1: Write the failing test**

Add to `web/src/routes/Setup.test.tsx`:

```typescript
it('library step offers a Built-in option that sets built-in mode and advances', async () => {
  renderSetup()
  // advance past password
  fireEvent.change(screen.getByPlaceholderText('Choose a password'), { target: { value: 'hunter2' } })
  fireEvent.click(screen.getByRole('button', { name: /continue/i }))
  await screen.findByText(/add a library/i)

  fireEvent.click(screen.getByRole('button', { name: /use built-in/i }))
  await waitFor(() =>
    expect(api.put).toHaveBeenCalledWith('/settings', { libraryBackendMode: 'built-in' }),
  )
})
```

Ensure `api.put` is part of the existing `vi.mock('../lib/api', ...)` in this test file (add `put: vi.fn(() => Promise.resolve({}))` if missing).

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/routes/Setup.test.tsx -t "Built-in"`
Expected: FAIL — no "Use built-in" button.

- [ ] **Step 3: Write minimal implementation**

In `web/src/routes/Setup.tsx`, in the library-step render branch (where `copy.type === 'library'`), render the built-in choice ABOVE the existing provider list. Keep the existing external flow (provider buttons + `AdapterForm`) for "Connect existing Subsonic":

```tsx
{step === 'library' && !chosen && (
  <div className="space-y-4">
    <button
      type="button"
      onClick={async () => {
        await api.put('/settings', { libraryBackendMode: 'built-in' })
        advance()
      }}
      className="w-full rounded-xl border border-border-subtle bg-raised p-4 text-left"
    >
      <div className="font-semibold text-text-primary">Use built-in library (recommended)</div>
      <div className="text-sm text-text-secondary">Reverb manages a music server for your folder — no setup.</div>
    </button>
    <div className="text-xs uppercase tracking-wide text-text-muted">or connect an existing server</div>
    {/* existing provider buttons remain here */}
  </div>
)}
```

When the user picks the subsonic provider and submits `AdapterForm`, set external mode before/after creating the adapter. In that `onSubmit`:

```tsx
onSubmit={async (config) => {
  await api.put('/settings', { libraryBackendMode: 'external' })
  await createAdapter({ type: 'library', name: chosen.name, enabled: true, priority: 0, config })
  advance()
}}
```

> Only change the **library** step. Search and downloader steps keep their current behavior.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd web && npx vitest run src/routes/Setup.test.tsx`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web/src/routes/Setup.tsx web/src/routes/Setup.test.tsx
git commit -m "feat(web): setup wizard built-in vs external library choice"
```

---

### Task 12: Frontend — "library starting" indicator

**Files:**
- Create: `web/src/lib/libraryApi.ts`
- Modify: `web/src/routes/Library.tsx`
- Test: `web/src/routes/Library.test.tsx` (create if absent) or `web/src/lib/libraryApi.test.ts`

**Interfaces:**
- Produces: `interface LibraryStatus { mode: string; state: string }`, `getLibraryStatus(): Promise<LibraryStatus>`, `useLibraryStatus()` (TanStack Query, `refetchInterval: 3000` while `state==='starting'`). `Library.tsx` shows a banner when `state==='starting'` ("Library starting…") or `state==='degraded'` ("Library unavailable — the bundled server failed to start").

- [ ] **Step 1: Write the failing test**

```typescript
import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import Library from './Library'

vi.mock('../lib/libraryApi', () => ({
  useLibraryStatus: vi.fn(() => ({ data: { mode: 'built-in', state: 'starting' } })),
}))
// (mock any other hooks Library uses to render minimally)

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>)
}

describe('Library starting indicator', () => {
  it('shows a starting banner when the bundled library is starting', () => {
    wrap(<Library />)
    expect(screen.getByText(/library starting/i)).toBeInTheDocument()
  })
})
```

> Inspect `Library.tsx`'s existing hook imports first and mock them so the component renders in isolation (follow the mock style already used in the FE test suite).

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/routes/Library.test.tsx`
Expected: FAIL — no starting banner.

- [ ] **Step 3: Write minimal implementation**

Create `web/src/lib/libraryApi.ts`:

```typescript
import { useQuery } from '@tanstack/react-query'
import { api } from './api'

export interface LibraryStatus {
  mode: string
  state: string // 'starting' | 'ready' | 'degraded' | 'external' | 'unconfigured'
}

export function getLibraryStatus(): Promise<LibraryStatus> {
  return api.get<LibraryStatus>('/library/status')
}

export function useLibraryStatus() {
  return useQuery({
    queryKey: ['library', 'status'],
    queryFn: getLibraryStatus,
    refetchInterval: (q) => (q.state.data?.state === 'starting' ? 3000 : false),
  })
}
```

In `web/src/routes/Library.tsx`, near the top of the rendered content:

```tsx
const libStatus = useLibraryStatus()
// ...
{libStatus.data?.state === 'starting' && (
  <div className="rounded-lg border border-border-subtle bg-raised px-4 py-2 text-sm text-text-secondary">
    Library starting… the bundled music server is coming up.
  </div>
)}
{libStatus.data?.state === 'degraded' && (
  <div className="rounded-lg border border-red-500/40 bg-red-500/10 px-4 py-2 text-sm text-red-300">
    Library unavailable — the bundled server failed to start. Check logs or switch to an external server in Settings.
  </div>
)}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run src/routes/Library.test.tsx`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/libraryApi.ts web/src/routes/Library.tsx web/src/routes/Library.test.tsx
git commit -m "feat(web): library starting/degraded indicator"
```

---

### Task 13: Packaging — bundle Navidrome + tini + docs

**Files:**
- Modify: `Dockerfile`
- Modify: `docker-compose.yml`
- Modify: `README.md`

**Interfaces:**
- Produces: an image containing `/usr/local/bin/navidrome` (v0.62.0, arch-correct) and `tini` as PID 1; `REVERB_NAVIDROME_BIN` defaulting to `navidrome` on PATH; compose + README documenting the single-container built-in default.

- [ ] **Step 1: Add the Navidrome download to the runtime stage**

In the `runtime` stage of `Dockerfile`, after ffmpeg install and BEFORE `USER reverb`, add (note `TARGETARCH` is provided automatically by buildx):

```dockerfile
# --- Bundled Navidrome (GPL-3.0, shipped unmodified as a separate process) ---
ARG TARGETARCH
ARG NAVIDROME_VERSION=0.62.0
RUN apt-get update \
 && apt-get install -y --no-install-recommends tini wget ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && wget -O /tmp/navidrome.tar.gz \
      "https://github.com/navidrome/navidrome/releases/download/v${NAVIDROME_VERSION}/navidrome_${NAVIDROME_VERSION}_linux_${TARGETARCH}.tar.gz" \
 && mkdir -p /tmp/nd \
 && tar -xzf /tmp/navidrome.tar.gz -C /tmp/nd \
 && install -m 0755 /tmp/nd/navidrome /usr/local/bin/navidrome \
 && rm -rf /tmp/navidrome.tar.gz /tmp/nd
```

- [ ] **Step 2: Make tini PID 1 and ensure the Navidrome data dir is writable**

Change the data-dir prep + entrypoint. Where the image currently does `mkdir -p /data /music && chown -R reverb:reverb /data /music`, also create `/data/navidrome`:

```dockerfile
RUN useradd --create-home --uid 1000 reverb \
 && mkdir -p /data /data/navidrome /music \
 && chown -R reverb:reverb /data /music
```

Replace `ENTRYPOINT ["reverb"]` with:

```dockerfile
ENTRYPOINT ["/usr/bin/tini", "--", "reverb"]
```

- [ ] **Step 3: Verify the image builds and bundles both binaries**

Run:
```bash
docker build -t reverb:navidrome-test .
docker run --rm --entrypoint sh reverb:navidrome-test -c "navidrome --version && which tini && which reverb"
```
Expected: prints a Navidrome `0.62.0` version line, a tini path, and a reverb path — confirming all three are present.

- [ ] **Step 4: Update compose + README**

In `docker-compose.yml`, ensure the `reverb` service mounts a music volume and document that built-in needs nothing else. Update the commented Navidrome service into a clearly-labeled "ONLY if you set Library backend = External" note. Add to `README.md` a "Library backends" section:

```markdown
### Library backends

By default Reverb runs a **bundled music server** (Navidrome) inside the same
container — just mount your music at `/music` and start it. Nothing else to set up.

Prefer your own server? In **Settings → Library backend**, switch to **External
Subsonic** and add your Navidrome/Subsonic server under **Admin**. In external
mode nothing extra runs inside the Reverb container.
```

- [ ] **Step 5: Commit**

```bash
git add Dockerfile docker-compose.yml README.md
git commit -m "feat(docker): bundle Navidrome v0.62.0 + tini PID 1; built-in default docs"
```

---

## Final Verification (run before declaring the milestone done)

- [ ] **Go gate:** `go build ./... && go test -race ./...` — all green.
- [ ] **FE gate:** `cd web && npm run build && npm test` — all green.
- [ ] **E2e gate:** `cd web && npm run e2e` — core loop + completeness + playlist-sync specs green. If the wizard's library step changed assertions, update the e2e mocks (`web/e2e/mocks.ts`) to stub `PUT /api/v1/settings` and `GET /api/v1/library/status` (`{mode:'built-in',state:'ready'}`).
- [ ] **Image gate:** `docker build -t reverb:bundled .` succeeds; `docker run --rm --entrypoint sh reverb:bundled -c "navidrome --version"` prints `0.62.0`.
- [ ] **Manual smoke (optional, real container):** `docker run -p 8090:8090 -v $PWD/dev/music:/music reverb:bundled`, open `http://localhost:8090`, complete setup choosing built-in, confirm `GET /api/v1/library/status` flips `starting → ready` and the library populates after the scan.

---

## Spec Coverage Check

- Bundle Navidrome as supervised child → Tasks 4, 5, 7, 13.
- Reuse subsonic adapter pointed at localhost → Task 6.
- Replace/pick-one (no federation) → Task 6 (single adapter; mode chooses source).
- Resource invariant (runs iff built-in) → Tasks 4 (mode-gate), 6, 7.
- Invisible Navidrome (localhost bind, internal creds) → Tasks 2, 3, 13.
- Mode setting + existing-deployments-untouched inference → Tasks 1, 6, 8.
- Config/onboarding UX (Settings + wizard) → Tasks 10, 11.
- Library starting/degraded surface → Tasks 4, 9, 12.
- Graceful shutdown / clean child reap → Tasks 7, 13 (tini).
- Packaging (pinned multi-arch binary, tini, version) → Task 13.
- Failure posture (degraded-but-alive) → Tasks 5, 9, 12.
- No forced migration → Task 1 (`ResolveMode` infers external for existing installs).
