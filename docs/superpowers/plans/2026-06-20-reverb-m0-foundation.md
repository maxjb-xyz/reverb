# Reverb M0 — Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up Reverb's bootable skeleton — a single Go binary that serves a versioned API and an embedded React shell, with config, SQLite, an event bus, the adapter registry, and a working auth/first-run-setup flow.

**Architecture:** Modular-monolith Go binary (`cmd/reverb`) with domain packages under `internal/`. It serves REST under `/api/v1`, gates protected routes behind session-cookie auth, and serves the React SPA either from `embed.FS` (prod) or by reverse-proxying Vite (`--dev`). State lives in SQLite (goose migrations, sqlc-typed queries). No real adapters yet — the registry + self-describing `Plugin` contract are proven with a fake adapter.

**Tech Stack:** Go 1.23, chi v5, modernc.org/sqlite (pure-Go, cgo-free), pressly/goose v3, sqlc, x/crypto/bcrypt; React + TypeScript + Vite + Tailwind 3.4, React Router 6, TanStack Query 5, Zustand 4, Vitest. (As-built versions from `npm create vite@latest`: React 19, TypeScript ~5.9, Vite 8, Vitest 4, Node ≥22 — CI pins Node 22.)

## Global Constraints

- Go module path: `github.com/maximusjb/reverb` (verbatim in every `import`).
- Go version floor: `go 1.23`.
- SQLite driver: `modernc.org/sqlite` (driver name `"sqlite"`) — **no cgo** (keeps the single static binary cross-compilable). Never add `mattn/go-sqlite3`.
- API base path: `/api/v1` for every endpoint.
- Default port `8090`; env override `REVERB_PORT`; flag `--port`.
- Default DB path `./data/reverb.db`; env `REVERB_DB`; flag `--db`.
- Dev mode: flag `--dev` (or env `REVERB_DEV=1`) → SPA requests reverse-proxy to `http://localhost:5173`.
- Secrets come only from env at startup: `REVERB_ADMIN_PASSWORD`, plus adapter secrets in later milestones. Never store the admin password in plaintext — bcrypt only.
- Accent color is a runtime CSS custom property `--color-accent` (space-separated RGB channels), default red `240 53 75` (`#F0354B`); Tailwind references it via `rgb(var(--color-accent) / <alpha-value>)`.
- Settings keys used in M0: `admin_password_hash`, `auth_disabled`, `accent_color`, `dynamic_background`.
- TDD always: failing test → confirm red → minimal code → confirm green → commit. Conventional-commit messages.

---

## File Structure

**Go (backend):**
- `go.mod`, `go.sum` — module + deps.
- `cmd/reverb/main.go` — entrypoint: load config, open store, build server, listen.
- `internal/config/config.go` — `Config` + `Load()` (flags + env).
- `internal/store/store.go` — open DB, run goose migrations, expose `*Queries`.
- `internal/store/migrations/0001_init.sql` — settings, sessions, adapter_instances.
- `internal/store/queries/*.sql` — sqlc query source.
- `internal/store/db/*.go` — sqlc-generated (do not hand-edit).
- `sqlc.yaml` — sqlc config.
- `internal/events/bus.go` — typed in-memory pub/sub.
- `internal/registry/registry.go` — `Plugin`, `ConfigSchema`, `Registry`, capability probes, `DescribeCapabilities`.
- `internal/auth/auth.go` — password hashing, `Service` (sessions, setup-required).
- `internal/api/server.go` — chi router, route mounting, server struct.
- `internal/api/middleware.go` — auth middleware.
- `internal/api/handlers.go` — health/setup/auth/me/adapters handlers.
- `internal/api/static.go` — embed.FS / Vite-proxy SPA handler.
- `internal/api/openapi.yaml` — hand-authored OpenAPI for M0 endpoints (served at `/api/v1/openapi.yaml`; co-located with the api package because `//go:embed` cannot reference parent directories).
- `Makefile` — build/test/dev/gen targets.

**React (frontend) under `web/`:**
- `web/package.json`, `web/vite.config.ts`, `web/tsconfig.json`, `web/tailwind.config.js`, `web/postcss.config.js`, `web/index.html`.
- `web/src/main.tsx`, `web/src/App.tsx`, `web/src/index.css` (Tailwind + `--color-accent`).
- `web/src/lib/api.ts` — fetch wrapper.
- `web/src/lib/session.ts` — setup/auth status hooks.
- `web/src/components/AppShell.tsx`, `Sidebar.tsx`, `PlayerBar.tsx`.
- `web/src/routes/{Search,Library,Settings,Login,Setup}.tsx`.
- `web/src/App.test.tsx` — Vitest smoke test.
- `web/src/setupTests.ts`, `web/vitest.config.ts`.

**Embed bridge (build-tagged so `go test ./...` never needs a built SPA):**
- `internal/api/embed.go` (`//go:build !prod`) — dev stub handler (used by tests/CI).
- `internal/api/embed_prod.go` (`//go:build prod`) — `//go:embed all:dist` real handler (used by `make build`, which passes `-tags prod`).

**Infra:**
- `docker-compose.dev.yml` — Navidrome dev service.
- `.github/workflows/ci.yml` — Go + frontend CI.
- `.gitignore`.

---

## Task 1: Go module + HTTP server skeleton + health

**Files:**
- Create: `go.mod`, `.gitignore`, `cmd/reverb/main.go`, `internal/api/server.go`, `internal/api/handlers.go`
- Test: `internal/api/handlers_test.go`

**Interfaces:**
- Produces: `api.NewServer(deps Deps) *Server` where `type Deps struct{}` (grows in later tasks); `(*Server) Handler() http.Handler`; `(*Server) Routes()` mounts `GET /api/v1/health` → `200 {"status":"ok"}`.

- [ ] **Step 1: Initialize the module and gitignore**

Run:
```bash
go mod init github.com/maximusjb/reverb
printf '%s\n' '/data/' '/web/dist/' '/web/node_modules/' '*.db' '/.env' '/reverb' '/.superpowers/' > .gitignore
go get github.com/go-chi/chi/v5@v5.1.0
```

- [ ] **Step 2: Write the failing test**

Create `internal/api/handlers_test.go`:
```go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealth(t *testing.T) {
	srv := NewServer(Deps{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status field = %q, want ok", body["status"])
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestHealth -v`
Expected: FAIL — `undefined: NewServer` / `Deps`.

- [ ] **Step 4: Write minimal implementation**

Create `internal/api/server.go`:
```go
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Deps holds the server's dependencies. It grows across M0 tasks.
type Deps struct{}

type Server struct {
	deps   Deps
	router chi.Router
}

func NewServer(deps Deps) *Server {
	s := &Server{deps: deps, router: chi.NewRouter()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.router }

func (s *Server) routes() {
	s.router.Route("/api/v1", func(r chi.Router) {
		r.Get("/health", s.handleHealth)
	})
}
```

Create `internal/api/handlers.go`:
```go
package api

import (
	"encoding/json"
	"net/http"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
```

- [ ] **Step 5: Add the entrypoint**

Create `cmd/reverb/main.go`:
```go
package main

import (
	"log"
	"net/http"

	"github.com/maximusjb/reverb/internal/api"
)

func main() {
	srv := api.NewServer(api.Deps{})
	addr := ":8090"
	log.Printf("reverb listening on %s", addr)
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/api/ -run TestHealth -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
go mod tidy
git add go.mod go.sum .gitignore cmd internal
git commit -m "feat(api): bootable server skeleton with health endpoint"
```

---

## Task 2: Config bootstrap (flags + env)

**Files:**
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`
- Modify: `cmd/reverb/main.go`

**Interfaces:**
- Produces: `type Config struct { Port int; DBPath string; Dev bool; LogLevel string; AdminPassword string }` and `func Load(args []string, getenv func(string) string) (Config, error)`. `AdminPassword` is sourced from `REVERB_ADMIN_PASSWORD` only.

- [ ] **Step 1: Write the failing test**

Create `internal/config/config_test.go`:
```go
package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	c, err := Load(nil, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != 8090 || c.DBPath != "./data/reverb.db" || c.Dev {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}

func TestLoadFlagsOverrideDefaults(t *testing.T) {
	c, err := Load([]string{"--port", "9000", "--dev"}, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != 9000 || !c.Dev {
		t.Fatalf("flags not applied: %+v", c)
	}
}

func TestEnvFillsPortWhenNoFlag(t *testing.T) {
	env := map[string]string{"REVERB_PORT": "7000", "REVERB_ADMIN_PASSWORD": "secret"}
	c, err := Load(nil, func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != 7000 || c.AdminPassword != "secret" {
		t.Fatalf("env not applied: %+v", c)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -v`
Expected: FAIL — `undefined: Load`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/config/config.go`:
```go
package config

import (
	"flag"
	"io"
	"strconv"
)

type Config struct {
	Port          int
	DBPath        string
	Dev           bool
	LogLevel      string
	AdminPassword string
}

// Load resolves config: flags win over env, env wins over defaults.
func Load(args []string, getenv func(string) string) (Config, error) {
	c := Config{Port: 8090, DBPath: "./data/reverb.db", LogLevel: "info"}

	if v := getenv("REVERB_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			c.Port = p
		}
	}
	if v := getenv("REVERB_DB"); v != "" {
		c.DBPath = v
	}
	if getenv("REVERB_DEV") == "1" {
		c.Dev = true
	}
	c.AdminPassword = getenv("REVERB_ADMIN_PASSWORD")

	fs := flag.NewFlagSet("reverb", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.IntVar(&c.Port, "port", c.Port, "HTTP port")
	fs.StringVar(&c.DBPath, "db", c.DBPath, "SQLite path")
	fs.BoolVar(&c.Dev, "dev", c.Dev, "dev mode (proxy Vite)")
	fs.StringVar(&c.LogLevel, "log-level", c.LogLevel, "log level")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	return c, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -v`
Expected: PASS (all three).

- [ ] **Step 5: Wire config into main**

Replace `cmd/reverb/main.go`:
```go
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/maximusjb/reverb/internal/api"
	"github.com/maximusjb/reverb/internal/config"
)

func main() {
	cfg, err := config.Load(os.Args[1:], os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	srv := api.NewServer(api.Deps{})
	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("reverb listening on %s (dev=%v)", addr, cfg.Dev)
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}
```

- [ ] **Step 6: Verify build + commit**

Run: `go build ./... && go test ./...`
Expected: build OK, tests PASS.
```bash
git add internal/config cmd/reverb/main.go
git commit -m "feat(config): flag+env bootstrap configuration"
```

---

## Task 3: SQLite store — migrations + sqlc + settings

**Files:**
- Create: `sqlc.yaml`, `internal/store/store.go`, `internal/store/migrations/0001_init.sql`, `internal/store/queries/settings.sql`, `internal/store/queries/sessions.sql`, `internal/store/queries/adapters.sql`
- Generated: `internal/store/db/*` (via `sqlc generate`)
- Test: `internal/store/store_test.go`

**Interfaces:**
- Produces: `func Open(path string) (*Store, error)`; `(*Store) Migrate() error`; `(*Store) Close() error`; `(*Store) Q() *db.Queries`. sqlc generates `db.Queries` with: `GetSetting(ctx, key) (string, error)`, `UpsertSetting(ctx, UpsertSettingParams{Key, Value})`, `CreateSession(ctx, CreateSessionParams{ID, TokenHash, ExpiresAt})`, `GetSession(ctx, tokenHash) (Session, error)`, `DeleteSession(ctx, tokenHash) error`, `CreateAdapterInstance`, `ListAdapterInstances(ctx) ([]AdapterInstance, error)`, `DeleteAdapterInstance(ctx, id) error`.

- [ ] **Step 1: Add deps and the migration SQL**

Run:
```bash
go get modernc.org/sqlite@v1.34.1
go get github.com/pressly/goose/v3@v3.22.1
```

Create `internal/store/migrations/0001_init.sql`:
```sql
-- +goose Up
CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE sessions (
    id         TEXT PRIMARY KEY,
    token_hash TEXT NOT NULL UNIQUE,
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    expires_at INTEGER NOT NULL,
    last_seen  INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE TABLE adapter_instances (
    id         TEXT PRIMARY KEY,
    type       TEXT NOT NULL,
    name       TEXT NOT NULL,
    enabled    INTEGER NOT NULL DEFAULT 1,
    priority   INTEGER NOT NULL DEFAULT 0,
    config_json TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at INTEGER NOT NULL DEFAULT (unixepoch())
);

-- +goose Down
DROP TABLE adapter_instances;
DROP TABLE sessions;
DROP TABLE settings;
```

- [ ] **Step 2: Author sqlc query files**

Create `internal/store/queries/settings.sql`:
```sql
-- name: GetSetting :one
SELECT value FROM settings WHERE key = ?;

-- name: UpsertSetting :exec
INSERT INTO settings (key, value) VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value;
```

Create `internal/store/queries/sessions.sql`:
```sql
-- name: CreateSession :exec
INSERT INTO sessions (id, token_hash, expires_at) VALUES (?, ?, ?);

-- name: GetSession :one
SELECT * FROM sessions WHERE token_hash = ?;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = ?;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at < ?;
```

Create `internal/store/queries/adapters.sql`:
```sql
-- name: CreateAdapterInstance :exec
INSERT INTO adapter_instances (id, type, name, enabled, priority, config_json)
VALUES (?, ?, ?, ?, ?, ?);

-- name: ListAdapterInstances :many
SELECT * FROM adapter_instances ORDER BY type, priority;

-- name: DeleteAdapterInstance :exec
DELETE FROM adapter_instances WHERE id = ?;
```

- [ ] **Step 3: Configure sqlc and generate**

Create `sqlc.yaml`:
```yaml
version: "2"
sql:
  - engine: "sqlite"
    schema: "internal/store/migrations"
    queries: "internal/store/queries"
    gen:
      go:
        package: "db"
        out: "internal/store/db"
        emit_json_tags: true
```

Run:
```bash
go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.27.0 generate
```
Expected: creates `internal/store/db/{db.go,models.go,*.sql.go}`.

- [ ] **Step 4: Write the failing store test**

Create `internal/store/store_test.go`:
```go
package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMigrateAndSettingsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := st.Q().UpsertSetting(ctx, db.UpsertSettingParams{Key: "k", Value: "v1"}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Q().GetSetting(ctx, "k")
	if err != nil || got != "v1" {
		t.Fatalf("get = %q err=%v", got, err)
	}
	// upsert overwrites
	_ = st.Q().UpsertSetting(ctx, db.UpsertSettingParams{Key: "k", Value: "v2"})
	got, _ = st.Q().GetSetting(ctx, "k")
	if got != "v2" {
		t.Fatalf("upsert did not overwrite: %q", got)
	}
}
```

Add the import line `"github.com/maximusjb/reverb/internal/store/db"` to the test's import block.

- [ ] **Step 5: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestMigrateAndSettingsRoundTrip -v`
Expected: FAIL — `undefined: Open`.

- [ ] **Step 6: Implement the store with embedded goose migrations**

Create `internal/store/store.go`:
```go
package store

import (
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/maximusjb/reverb/internal/store/db"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type Store struct {
	sql *sql.DB
	q   *db.Queries
}

func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	conn, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	return &Store{sql: conn, q: db.New(conn)}, nil
}

func (s *Store) Migrate() error {
	goose.SetBaseFS(migrationFS)
	if err := goose.SetDialect("sqlite"); err != nil {
		return err
	}
	return goose.Up(s.sql, "migrations")
}

func (s *Store) Q() *db.Queries { return s.q }
func (s *Store) Close() error   { return s.sql.Close() }
```

- [ ] **Step 7: Run test to verify it passes**

Run: `go test ./internal/store/ -v`
Expected: PASS.

- [ ] **Step 8: Wire store into main**

Edit `cmd/reverb/main.go` — after config load, before building the server:
```go
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		log.Fatal(err)
	}
```
Add import `"github.com/maximusjb/reverb/internal/store"`.

- [ ] **Step 9: Commit**

Run: `go build ./... && go test ./...`
```bash
git add sqlc.yaml internal/store cmd/reverb/main.go go.mod go.sum
git commit -m "feat(store): sqlite store with goose migrations and sqlc queries"
```

---

## Task 4: EventBus

**Files:**
- Create: `internal/events/bus.go`
- Test: `internal/events/bus_test.go`

**Interfaces:**
- Produces: `type Event struct { Topic string; Payload any }`; `func New() *Bus`; `(*Bus) Subscribe(topic string) (<-chan Event, func())` (returns channel + unsubscribe); `(*Bus) Publish(ev Event)` (non-blocking; drops to slow subscribers' buffered channel). Topics used later: `download.queued|progress|complete|failed`, `library.updated`.

- [ ] **Step 1: Write the failing test**

Create `internal/events/bus_test.go`:
```go
package events

import (
	"testing"
	"time"
)

func TestSubscribeReceivesPublished(t *testing.T) {
	b := New()
	ch, cancel := b.Subscribe("download.progress")
	defer cancel()

	b.Publish(Event{Topic: "download.progress", Payload: 42})

	select {
	case ev := <-ch:
		if ev.Payload.(int) != 42 {
			t.Fatalf("payload = %v", ev.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	b := New()
	ch, cancel := b.Subscribe("t")
	cancel()
	b.Publish(Event{Topic: "t", Payload: 1})
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("received after cancel")
		}
	case <-time.After(100 * time.Millisecond):
		// no delivery — acceptable
	}
}

func TestPublishToOtherTopicIgnored(t *testing.T) {
	b := New()
	ch, cancel := b.Subscribe("a")
	defer cancel()
	b.Publish(Event{Topic: "b", Payload: 1})
	select {
	case <-ch:
		t.Fatal("got event for wrong topic")
	case <-time.After(100 * time.Millisecond):
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/events/ -v`
Expected: FAIL — `undefined: New`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/events/bus.go`:
```go
package events

import "sync"

type Event struct {
	Topic   string
	Payload any
}

type subscriber struct {
	topic string
	ch    chan Event
}

type Bus struct {
	mu   sync.Mutex
	subs map[int]*subscriber
	next int
}

func New() *Bus { return &Bus{subs: map[int]*subscriber{}} }

// Subscribe returns a buffered channel for a topic and an unsubscribe func.
func (b *Bus) Subscribe(topic string) (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.next
	b.next++
	s := &subscriber{topic: topic, ch: make(chan Event, 32)}
	b.subs[id] = s
	return s.ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if cur, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(cur.ch)
		}
	}
}

// Publish delivers to matching subscribers without blocking the caller.
func (b *Bus) Publish(ev Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, s := range b.subs {
		if s.topic != ev.Topic {
			continue
		}
		select {
		case s.ch <- ev:
		default: // drop if subscriber is full; progress is coalescible
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/events/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/events
git commit -m "feat(events): in-memory typed pub/sub event bus"
```

---

## Task 5: Plugin contract + registry + capability probes

**Files:**
- Create: `internal/registry/registry.go`
- Test: `internal/registry/registry_test.go`

**Interfaces:**
- Produces:
  - `type ConfigField struct { Key, Label, Type string; Required, Secret bool }`
  - `type ConfigSchema struct { Fields []ConfigField }`
  - `type Plugin interface { Type() string; Name() string; ConfigSchema() ConfigSchema; Init(cfg map[string]any) error; TestConnection(ctx context.Context) error }`
  - `type Factory func() Plugin`
  - `type Registry struct{...}`; `func NewRegistry(kind string) *Registry`; `(*Registry) Register(name string, f Factory)`; `(*Registry) Create(name string) (Plugin, error)`; `(*Registry) Names() []string`
  - Capability probes: `func RegisterCapability(name string, probe func(Plugin) bool)`; `func DescribeCapabilities(p Plugin) []string`

- [ ] **Step 1: Write the failing test**

Create `internal/registry/registry_test.go`:
```go
package registry

import (
	"context"
	"testing"
)

type fakeAdapter struct{ initialized bool }

func (f *fakeAdapter) Type() string                              { return "library" }
func (f *fakeAdapter) Name() string                              { return "fake" }
func (f *fakeAdapter) ConfigSchema() ConfigSchema                { return ConfigSchema{Fields: []ConfigField{{Key: "url", Label: "URL", Type: "string", Required: true}}} }
func (f *fakeAdapter) Init(cfg map[string]any) error             { f.initialized = true; return nil }
func (f *fakeAdapter) TestConnection(ctx context.Context) error  { return nil }

// optional capability interface (mimics future DiscographyProvider)
type discographyProvider interface{ Discography() }

func (f *fakeAdapter) Discography() {}

func TestRegisterCreateNames(t *testing.T) {
	reg := NewRegistry("library")
	reg.Register("fake", func() Plugin { return &fakeAdapter{} })

	if got := reg.Names(); len(got) != 1 || got[0] != "fake" {
		t.Fatalf("names = %v", got)
	}
	p, err := reg.Create("fake")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "fake" {
		t.Fatalf("name = %q", p.Name())
	}
	if _, err := reg.Create("missing"); err == nil {
		t.Fatal("expected error for unknown adapter")
	}
}

func TestDescribeCapabilities(t *testing.T) {
	RegisterCapability("discography", func(p Plugin) bool {
		_, ok := p.(discographyProvider)
		return ok
	})
	caps := DescribeCapabilities(&fakeAdapter{})
	found := false
	for _, c := range caps {
		if c == "discography" {
			found = true
		}
	}
	if !found {
		t.Fatalf("discography not detected: %v", caps)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/registry/ -v`
Expected: FAIL — `undefined: NewRegistry`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/registry/registry.go`:
```go
package registry

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

type ConfigField struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
	Secret   bool   `json:"secret"`
}

type ConfigSchema struct {
	Fields []ConfigField `json:"fields"`
}

type Plugin interface {
	Type() string
	Name() string
	ConfigSchema() ConfigSchema
	Init(cfg map[string]any) error
	TestConnection(ctx context.Context) error
}

type Factory func() Plugin

type Registry struct {
	kind      string
	mu        sync.RWMutex
	factories map[string]Factory
}

func NewRegistry(kind string) *Registry {
	return &Registry{kind: kind, factories: map[string]Factory{}}
}

// Register adds a factory. Safe to call at init() or at runtime.
func (r *Registry) Register(name string, f Factory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[name] = f
}

func (r *Registry) Create(name string) (Plugin, error) {
	r.mu.RLock()
	f, ok := r.factories[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("registry %q: unknown adapter %q", r.kind, name)
	}
	return f(), nil
}

func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.factories))
	for n := range r.factories {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// --- capability probes ---

var (
	capMu    sync.RWMutex
	capProbes = map[string]func(Plugin) bool{}
)

// RegisterCapability registers a runtime probe that detects an optional
// interface. Later milestones (M2/M3) register probes for DiscographyProvider,
// QualityProfileDownloader, etc. — the registry stays domain-agnostic.
func RegisterCapability(name string, probe func(Plugin) bool) {
	capMu.Lock()
	defer capMu.Unlock()
	capProbes[name] = probe
}

func DescribeCapabilities(p Plugin) []string {
	capMu.RLock()
	defer capMu.RUnlock()
	var out []string
	for name, probe := range capProbes {
		if probe(p) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/registry/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/registry
git commit -m "feat(registry): plugin contract, registry, capability probes"
```

---

## Task 6: Auth core — password hashing, sessions, setup-required

**Files:**
- Create: `internal/auth/auth.go`
- Test: `internal/auth/auth_test.go`

**Interfaces:**
- Consumes: `store.Store` (via a narrow interface), `store/db` params.
- Produces:
  - `func HashPassword(pw string) (string, error)`; `func VerifyPassword(hash, pw string) bool`
  - `type Service struct{...}`; `func NewService(st Querier, now func() time.Time) *Service`
  - `type Querier interface { GetSetting(ctx, key) (string, error); UpsertSetting(ctx, db.UpsertSettingParams) error; CreateSession(ctx, db.CreateSessionParams) error; GetSession(ctx, tokenHash string) (db.Session, error); DeleteSession(ctx, tokenHash string) error }`
  - `(*Service) SetAdminPassword(ctx, pw string) error`
  - `(*Service) IsSetupRequired(ctx) (bool, error)` — true when no `admin_password_hash` AND `auth_disabled` != "true"
  - `(*Service) CheckLogin(ctx, pw string) (bool, error)`
  - `(*Service) CreateSession(ctx) (token string, err error)` — random token, stores sha256 hash, 30-day expiry
  - `(*Service) ValidateToken(ctx, token string) (bool, error)`
  - `(*Service) Logout(ctx, token string) error`

- [ ] **Step 1: Add bcrypt and write the failing test**

Run: `go get golang.org/x/crypto/bcrypt@v0.31.0`

Create `internal/auth/auth_test.go`:
```go
package auth

import (
	"context"
	"testing"
	"time"

	"github.com/maximusjb/reverb/internal/store"
	"github.com/maximusjb/reverb/internal/store/db"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/a.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	return NewService(st.Q(), time.Now)
}

func TestPasswordHashVerify(t *testing.T) {
	h, err := HashPassword("hunter2")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(h, "hunter2") || VerifyPassword(h, "wrong") {
		t.Fatal("verify mismatch")
	}
}

func TestSetupRequiredLifecycle(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	req, _ := s.IsSetupRequired(ctx)
	if !req {
		t.Fatal("fresh DB should require setup")
	}
	if err := s.SetAdminPassword(ctx, "pw"); err != nil {
		t.Fatal(err)
	}
	req, _ = s.IsSetupRequired(ctx)
	if req {
		t.Fatal("setup should be complete after password set")
	}
}

func TestLoginAndSession(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	_ = s.SetAdminPassword(ctx, "pw")

	ok, _ := s.CheckLogin(ctx, "pw")
	if !ok {
		t.Fatal("login should succeed")
	}
	if bad, _ := s.CheckLogin(ctx, "nope"); bad {
		t.Fatal("login should fail")
	}

	tok, err := s.CreateSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if valid, _ := s.ValidateToken(ctx, tok); !valid {
		t.Fatal("token should validate")
	}
	if err := s.Logout(ctx, tok); err != nil {
		t.Fatal(err)
	}
	if valid, _ := s.ValidateToken(ctx, tok); valid {
		t.Fatal("token should be invalid after logout")
	}
}

var _ = db.Session{}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/ -v`
Expected: FAIL — `undefined: NewService` / `HashPassword`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/auth/auth.go`:
```go
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/maximusjb/reverb/internal/store/db"
	"golang.org/x/crypto/bcrypt"
)

const (
	keyAdminHash    = "admin_password_hash"
	keyAuthDisabled = "auth_disabled"
	sessionTTL      = 30 * 24 * time.Hour
)

func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

func VerifyPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

type Querier interface {
	GetSetting(ctx context.Context, key string) (string, error)
	UpsertSetting(ctx context.Context, arg db.UpsertSettingParams) error
	CreateSession(ctx context.Context, arg db.CreateSessionParams) error
	GetSession(ctx context.Context, tokenHash string) (db.Session, error)
	DeleteSession(ctx context.Context, tokenHash string) error
}

type Service struct {
	q   Querier
	now func() time.Time
}

func NewService(q Querier, now func() time.Time) *Service {
	return &Service{q: q, now: now}
}

func (s *Service) SetAdminPassword(ctx context.Context, pw string) error {
	h, err := HashPassword(pw)
	if err != nil {
		return err
	}
	return s.q.UpsertSetting(ctx, db.UpsertSettingParams{Key: keyAdminHash, Value: h})
}

func (s *Service) IsSetupRequired(ctx context.Context) (bool, error) {
	if v, _ := s.q.GetSetting(ctx, keyAuthDisabled); v == "true" {
		return false, nil
	}
	_, err := s.q.GetSetting(ctx, keyAdminHash)
	if err != nil {
		return true, nil // no row → setup needed
	}
	return false, nil
}

func (s *Service) CheckLogin(ctx context.Context, pw string) (bool, error) {
	h, err := s.q.GetSetting(ctx, keyAdminHash)
	if err != nil {
		return false, errors.New("admin password not set")
	}
	return VerifyPassword(h, pw), nil
}

func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

func (s *Service) CreateSession(ctx context.Context) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	tok := base64.RawURLEncoding.EncodeToString(raw)
	err := s.q.CreateSession(ctx, db.CreateSessionParams{
		ID:        uuid.NewString(),
		TokenHash: hashToken(tok),
		ExpiresAt: s.now().Add(sessionTTL).Unix(),
	})
	if err != nil {
		return "", err
	}
	return tok, nil
}

func (s *Service) ValidateToken(ctx context.Context, tok string) (bool, error) {
	if tok == "" {
		return false, nil
	}
	sess, err := s.q.GetSession(ctx, hashToken(tok))
	if err != nil {
		return false, nil
	}
	if sess.ExpiresAt < s.now().Unix() {
		return false, nil
	}
	return true, nil
}

func (s *Service) Logout(ctx context.Context, tok string) error {
	return s.q.DeleteSession(ctx, hashToken(tok))
}
```

Run: `go get github.com/google/uuid@v1.6.0`

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/auth/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
go mod tidy
git add internal/auth go.mod go.sum
git commit -m "feat(auth): password hashing, sessions, setup-required logic"
```

---

## Task 7: API wiring — middleware, auth/setup/me/adapters endpoints, SPA handler

**Files:**
- Modify: `internal/api/server.go`, `internal/api/handlers.go`
- Create: `internal/api/middleware.go`, `internal/api/static.go`
- Test: `internal/api/auth_flow_test.go`

**Interfaces:**
- Consumes: `*auth.Service`, `*registry.Registry` (×3), `*store.Store`, `config.Config`.
- Produces (extends `Deps`):
  ```go
  type Deps struct {
      Auth       *auth.Service
      Library    *registry.Registry
      Search     *registry.Registry
      Downloader *registry.Registry
      Dev        bool
  }
  ```
- Endpoints: `GET /api/v1/health`, `GET /api/v1/setup/status` → `{"setupRequired":bool}`, `POST /api/v1/setup/admin` `{"password":...}` → sets password + returns session (only when setup required), `POST /api/v1/auth/login` `{"password":...}` → session cookie + `{"ok":true}`, `POST /api/v1/auth/logout`, `GET /api/v1/me` (protected) → `{"authenticated":true}`, `GET /api/v1/adapters/available` (protected) → `[{type,name,configSchema,capabilities}]`. Session cookie name: `reverb_session`.

- [ ] **Step 1: Write the failing flow test**

Create `internal/api/auth_flow_test.go`:
```go
package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/maximusjb/reverb/internal/auth"
	"github.com/maximusjb/reverb/internal/registry"
	"github.com/maximusjb/reverb/internal/store"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/api.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	return NewServer(Deps{
		Auth:       auth.NewService(st.Q(), time.Now),
		Library:    registry.NewRegistry("library"),
		Search:     registry.NewRegistry("search"),
		Downloader: registry.NewRegistry("downloader"),
	})
}

func TestSetupThenProtectedAccess(t *testing.T) {
	srv := testServer(t)
	h := srv.Handler()

	// setup required initially
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/setup/status", nil))
	if rec.Body.String() == "" || rec.Code != 200 {
		t.Fatalf("setup status failed: %d %s", rec.Code, rec.Body.String())
	}

	// /me is 401 before auth
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/me", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("/me before auth = %d, want 401", rec.Code)
	}

	// complete setup → expect a session cookie
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/setup/admin", bytes.NewBufferString(`{"password":"pw"}`))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup admin = %d %s", rec.Code, rec.Body.String())
	}
	cookie := rec.Result().Cookies()
	if len(cookie) == 0 {
		t.Fatal("expected session cookie")
	}

	// /me with cookie is 200
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.AddCookie(cookie[0])
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/me with cookie = %d", rec.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestSetupThenProtectedAccess -v`
Expected: FAIL — `Deps` has no field `Auth`.

- [ ] **Step 3: Extend Deps and routes**

Replace `internal/api/server.go`:
```go
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/maximusjb/reverb/internal/auth"
	"github.com/maximusjb/reverb/internal/registry"
)

type Deps struct {
	Auth       *auth.Service
	Library    *registry.Registry
	Search     *registry.Registry
	Downloader *registry.Registry
	Dev        bool
}

type Server struct {
	deps   Deps
	router chi.Router
}

func NewServer(deps Deps) *Server {
	s := &Server{deps: deps, router: chi.NewRouter()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.router }

func (s *Server) routes() {
	s.router.Use(middleware.Recoverer)

	s.router.Route("/api/v1", func(r chi.Router) {
		// public
		r.Get("/health", s.handleHealth)
		r.Get("/setup/status", s.handleSetupStatus)
		r.Post("/setup/admin", s.handleSetupAdmin)
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/logout", s.handleLogout)

		// protected
		r.Group(func(pr chi.Router) {
			pr.Use(s.requireAuth)
			pr.Get("/me", s.handleMe)
			pr.Get("/adapters/available", s.handleAdaptersAvailable)
		})
	})

	// SPA (embed.FS in prod, Vite proxy in --dev) — must be last.
	s.router.Handle("/*", s.spaHandler())
}
```

Run: `go get github.com/go-chi/chi/v5/middleware@v5.1.0` (already part of chi v5; `go mod tidy` will resolve).

- [ ] **Step 4: Add middleware**

Create `internal/api/middleware.go`:
```go
package api

import (
	"net/http"
)

const sessionCookie = "reverb_session"

func (s *Server) tokenFromRequest(r *http.Request) string {
	if c, err := r.Cookie(sessionCookie); err == nil {
		return c.Value
	}
	const p = "Bearer "
	if h := r.Header.Get("Authorization"); len(h) > len(p) && h[:len(p)] == p {
		return h[len(p):]
	}
	return ""
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok, _ := s.deps.Auth.ValidateToken(r.Context(), s.tokenFromRequest(r))
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string, dev bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   !dev,
		MaxAge:   30 * 24 * 3600,
	})
}
```

- [ ] **Step 5: Add the handlers**

Append to `internal/api/handlers.go`:
```go
import (
	"context"
	"github.com/maximusjb/reverb/internal/registry"
)

type passwordBody struct {
	Password string `json:"password"`
}

func decode(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	req, _ := s.deps.Auth.IsSetupRequired(r.Context())
	writeJSON(w, http.StatusOK, map[string]bool{"setupRequired": req})
}

func (s *Server) handleSetupAdmin(w http.ResponseWriter, r *http.Request) {
	req, _ := s.deps.Auth.IsSetupRequired(r.Context())
	if !req {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "setup already complete"})
		return
	}
	var body passwordBody
	if err := decode(r, &body); err != nil || body.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password required"})
		return
	}
	if err := s.deps.Auth.SetAdminPassword(r.Context(), body.Password); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not set password"})
		return
	}
	s.issueSession(w, r.Context())
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body passwordBody
	if err := decode(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	ok, _ := s.deps.Auth.CheckLogin(r.Context(), body.Password)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	s.issueSession(w, r.Context())
}

func (s *Server) issueSession(w http.ResponseWriter, ctx context.Context) {
	tok, err := s.deps.Auth.CreateSession(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "session error"})
		return
	}
	s.setSessionCookie(w, tok, s.deps.Dev)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "token": tok})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	_ = s.deps.Auth.Logout(r.Context(), s.tokenFromRequest(r))
	s.setSessionCookie(w, "", s.deps.Dev)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": true})
}

type adapterInfo struct {
	Type         string              `json:"type"`
	Name         string              `json:"name"`
	ConfigSchema registry.ConfigSchema `json:"configSchema"`
	Capabilities []string            `json:"capabilities"`
}

func (s *Server) handleAdaptersAvailable(w http.ResponseWriter, r *http.Request) {
	var out []adapterInfo
	for _, reg := range []*registry.Registry{s.deps.Library, s.deps.Search, s.deps.Downloader} {
		for _, name := range reg.Names() {
			p, err := reg.Create(name)
			if err != nil {
				continue
			}
			out = append(out, adapterInfo{
				Type:         p.Type(),
				Name:         p.Name(),
				ConfigSchema: p.ConfigSchema(),
				Capabilities: registry.DescribeCapabilities(p),
			})
		}
	}
	writeJSON(w, http.StatusOK, out)
}
```
(Merge the new `import` block into the existing one at the top of `handlers.go` rather than adding a second `import` statement.)

- [ ] **Step 6: Add the SPA handler (dev proxy now; embed wired in Task 11)**

Create `internal/api/static.go`:
```go
package api

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

// spaHandler serves the frontend. In dev it proxies to Vite; in prod it serves
// the embedded build (wired in Task 11 via setEmbeddedFS).
func (s *Server) spaHandler() http.Handler {
	if s.deps.Dev {
		target, _ := url.Parse("http://localhost:5173")
		return httputil.NewSingleHostReverseProxy(target)
	}
	return s.embeddedSPA()
}
```

Create a temporary stub so the package compiles before Task 11. Create `internal/api/embed.go`:
```go
package api

import "net/http"

// embeddedSPA is replaced in Task 11 with a real embed.FS handler.
func (s *Server) embeddedSPA() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"app": "reverb", "note": "frontend not embedded yet"})
	})
}
```

- [ ] **Step 7: Run test to verify it passes**

Run: `go mod tidy && go test ./internal/api/ -v`
Expected: PASS (`TestHealth`, `TestSetupThenProtectedAccess`).

- [ ] **Step 8: Wire everything in main**

Replace `cmd/reverb/main.go`:
```go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/maximusjb/reverb/internal/api"
	"github.com/maximusjb/reverb/internal/auth"
	"github.com/maximusjb/reverb/internal/config"
	"github.com/maximusjb/reverb/internal/registry"
	"github.com/maximusjb/reverb/internal/store"
)

func main() {
	cfg, err := config.Load(os.Args[1:], os.Getenv)
	if err != nil {
		log.Fatal(err)
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		log.Fatal(err)
	}

	authSvc := auth.NewService(st.Q(), time.Now)
	// Seed admin password from env if provided and not yet set.
	if cfg.AdminPassword != "" {
		if req, _ := authSvc.IsSetupRequired(context.Background()); req {
			_ = authSvc.SetAdminPassword(context.Background(), cfg.AdminPassword)
		}
	}

	srv := api.NewServer(api.Deps{
		Auth:       authSvc,
		Library:    registry.NewRegistry("library"),
		Search:     registry.NewRegistry("search"),
		Downloader: registry.NewRegistry("downloader"),
		Dev:        cfg.Dev,
	})

	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("reverb listening on %s (dev=%v)", addr, cfg.Dev)
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}
```
- [ ] **Step 9: Build, test, commit**

Run: `go build ./... && go test ./...`
Expected: PASS.
```bash
git add internal/api cmd/reverb/main.go go.mod go.sum
git commit -m "feat(api): auth middleware, setup/login/me/adapters endpoints, SPA handler"
```

---

## Task 8: OpenAPI scaffold

**Files:**
- Create: `internal/api/openapi.yaml`
- Modify: `internal/api/server.go` (serve the spec), `internal/api/handlers.go`
- Test: `internal/api/openapi_test.go`

**Interfaces:**
- Produces: `GET /api/v1/openapi.yaml` → `200`, `Content-Type: application/yaml`, body embedded from `api/openapi.yaml`.

- [ ] **Step 1: Author the spec**

Create `internal/api/openapi.yaml`:
```yaml
openapi: 3.0.3
info:
  title: Reverb API
  version: 0.1.0
servers:
  - url: /api/v1
paths:
  /health:
    get:
      summary: Liveness probe
      responses: { "200": { description: ok } }
  /setup/status:
    get:
      summary: Whether first-run setup is required
      responses: { "200": { description: ok } }
  /setup/admin:
    post:
      summary: Set the admin password during first-run setup
      responses: { "200": { description: session issued }, "409": { description: already set up } }
  /auth/login:
    post:
      summary: Log in with the admin password
      responses: { "200": { description: session issued }, "401": { description: invalid } }
  /auth/logout:
    post:
      summary: Invalidate the current session
      responses: { "200": { description: ok } }
  /me:
    get:
      summary: Current session check
      responses: { "200": { description: authenticated }, "401": { description: unauthorized } }
  /adapters/available:
    get:
      summary: Registered adapter types with config schemas and capabilities
      responses: { "200": { description: ok } }
```

- [ ] **Step 2: Write the failing test**

Create `internal/api/openapi_test.go`:
```go
package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServesOpenAPI(t *testing.T) {
	srv := NewServer(Deps{})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/openapi.yaml", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "openapi: 3.0.3") {
		t.Fatal("spec body not served")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestServesOpenAPI -v`
Expected: FAIL — 404.

- [ ] **Step 4: Embed and serve the spec**

Create `internal/api/openapi.go`:
```go
package api

import (
	_ "embed"
	"net/http"
)

//go:embed openapi.yaml
var openapiSpec []byte

func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(openapiSpec)
}
```
Add inside the `/api/v1` route group in `server.go`, with the other public routes:
```go
		r.Get("/openapi.yaml", s.handleOpenAPI)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/api/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api
git commit -m "feat(api): serve hand-authored OpenAPI spec for M0 endpoints"
```

---

## Task 9: Frontend scaffold — Vite + Tailwind + accent tokens + shell

**Files:**
- Create: `web/package.json`, `web/vite.config.ts`, `web/tsconfig.json`, `web/tsconfig.node.json`, `web/tailwind.config.js`, `web/postcss.config.js`, `web/index.html`, `web/vitest.config.ts`, `web/src/main.tsx`, `web/src/index.css`, `web/src/App.tsx`, `web/src/components/{AppShell,Sidebar,PlayerBar}.tsx`, `web/src/setupTests.ts`, `web/src/App.test.tsx`

**Interfaces:**
- Produces: a Vite app that builds to `web/dist`, renders `<AppShell>` (sidebar + main outlet + player bar), with `--color-accent` driving Tailwind's `accent` color. `npm run build`, `npm run dev` (port 5173), `npm run test`.

- [ ] **Step 1: Scaffold the app**

Run:
```bash
mkdir -p web
cd web
npm create vite@latest . -- --template react-ts
npm install
npm install -D tailwindcss@3.4.14 postcss autoprefixer vitest@2 @testing-library/react @testing-library/jest-dom jsdom
npm install react-router-dom@6 @tanstack/react-query@5 zustand@4
npx tailwindcss init -p
cd ..
```

- [ ] **Step 2: Configure Tailwind with the accent token**

Replace `web/tailwind.config.js`:
```js
/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        accent: 'rgb(var(--color-accent) / <alpha-value>)',
        base: '#0D0D0F',
      },
    },
  },
  plugins: [],
}
```

Replace `web/src/index.css`:
```css
@tailwind base;
@tailwind components;
@tailwind utilities;

:root {
  /* default accent: red #F0354B as space-separated RGB channels */
  --color-accent: 240 53 75;
}

html, body, #root { height: 100%; }
body { @apply bg-base text-neutral-200; margin: 0; }
```

- [ ] **Step 3: Build the shell components**

Create `web/src/components/Sidebar.tsx`:
```tsx
import { NavLink } from 'react-router-dom'

const items = [
  { to: '/search', label: 'Search' },
  { to: '/library', label: 'Library' },
  { to: '/settings', label: 'Settings' },
]

export function Sidebar() {
  return (
    <nav className="w-56 shrink-0 border-r border-neutral-800 p-4 space-y-1">
      <div className="text-xl font-bold mb-4 text-accent">Reverb</div>
      {items.map((i) => (
        <NavLink
          key={i.to}
          to={i.to}
          className={({ isActive }) =>
            `block rounded px-3 py-2 ${isActive ? 'bg-accent/20 text-accent' : 'hover:bg-neutral-800'}`
          }
        >
          {i.label}
        </NavLink>
      ))}
    </nav>
  )
}
```

Create `web/src/components/PlayerBar.tsx`:
```tsx
export function PlayerBar() {
  return (
    <div className="h-20 border-t border-neutral-800 px-4 flex items-center text-sm text-neutral-400">
      Nothing playing
    </div>
  )
}
```

Create `web/src/components/AppShell.tsx`:
```tsx
import { Outlet } from 'react-router-dom'
import { Sidebar } from './Sidebar'
import { PlayerBar } from './PlayerBar'

export function AppShell() {
  return (
    <div className="flex flex-col h-full">
      <div className="flex flex-1 min-h-0">
        <Sidebar />
        <main className="flex-1 overflow-auto p-6">
          <Outlet />
        </main>
      </div>
      <PlayerBar />
    </div>
  )
}
```

- [ ] **Step 4: Minimal App + main (routing fleshed out in Task 10)**

Replace `web/src/App.tsx`:
```tsx
import { AppShell } from './components/AppShell'

export default function App() {
  return <AppShell />
}
```

Replace `web/src/main.tsx`:
```tsx
import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter, Routes, Route } from 'react-router-dom'
import App from './App'
import './index.css'

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter>
      <Routes>
        <Route path="/*" element={<App />} />
      </Routes>
    </BrowserRouter>
  </React.StrictMode>,
)
```

- [ ] **Step 5: Configure Vitest + smoke test**

Create `web/vitest.config.ts`:
```ts
import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  test: { environment: 'jsdom', globals: true, setupFiles: './src/setupTests.ts' },
})
```

Create `web/src/setupTests.ts`:
```ts
import '@testing-library/jest-dom'
```

Create `web/src/App.test.tsx`:
```tsx
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import App from './App'

test('renders the Reverb brand in the shell', () => {
  render(
    <MemoryRouter>
      <App />
    </MemoryRouter>,
  )
  expect(screen.getByText('Reverb')).toBeInTheDocument()
})
```

Add to `web/package.json` `"scripts"`: `"test": "vitest run"`.

- [ ] **Step 6: Run the smoke test**

Run: `cd web && npm run test && cd ..`
Expected: 1 passed.

- [ ] **Step 7: Verify build + commit**

Run: `cd web && npm run build && cd ..`
Expected: `web/dist` created.
```bash
git add web/package.json web/package-lock.json web/*.ts web/*.js web/*.json web/index.html web/src
git commit -m "feat(web): vite+tailwind shell with runtime accent token"
```

---

## Task 10: Frontend routing, guards, login + setup pages, API client

**Files:**
- Create: `web/src/lib/api.ts`, `web/src/lib/session.ts`, `web/src/routes/{Search,Library,Settings,Login,Setup}.tsx`
- Modify: `web/src/main.tsx`, `web/src/App.tsx`
- Test: `web/src/routes/Setup.test.tsx`

**Interfaces:**
- Produces: `api.get<T>(path)`, `api.post<T>(path, body)` (credentials: 'include'); `useSessionStatus()` → `{ loading, setupRequired, authenticated }`. Routes: `/search`, `/library`, `/settings`, `/login`, `/setup`. A `<Guard>` redirects to `/setup` when `setupRequired`, to `/login` when not authenticated.

- [ ] **Step 1: API client + session hook**

Create `web/src/lib/api.ts`:
```ts
const BASE = '/api/v1'

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(BASE + path, {
    method,
    credentials: 'include',
    headers: body ? { 'Content-Type': 'application/json' } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  })
  if (!res.ok) throw new Error(`${method} ${path} -> ${res.status}`)
  const text = await res.text()
  return (text ? JSON.parse(text) : null) as T
}

export const api = {
  get: <T>(p: string) => request<T>('GET', p),
  post: <T>(p: string, b?: unknown) => request<T>('POST', p, b),
}
```

Create `web/src/lib/session.ts`:
```ts
import { useEffect, useState } from 'react'
import { api } from './api'

export interface SessionStatus {
  loading: boolean
  setupRequired: boolean
  authenticated: boolean
}

export function useSessionStatus(): SessionStatus {
  const [s, setS] = useState<SessionStatus>({ loading: true, setupRequired: false, authenticated: false })
  useEffect(() => {
    ;(async () => {
      try {
        const setup = await api.get<{ setupRequired: boolean }>('/setup/status')
        let authenticated = false
        if (!setup.setupRequired) {
          try {
            await api.get('/me')
            authenticated = true
          } catch {
            authenticated = false
          }
        }
        setS({ loading: false, setupRequired: setup.setupRequired, authenticated })
      } catch {
        setS({ loading: false, setupRequired: false, authenticated: false })
      }
    })()
  }, [])
  return s
}
```

- [ ] **Step 2: Page components**

Create `web/src/routes/Search.tsx`:
```tsx
export default function Search() {
  return <h1 className="text-2xl font-bold">Search</h1>
}
```
Create `web/src/routes/Library.tsx`:
```tsx
export default function Library() {
  return <h1 className="text-2xl font-bold">Library</h1>
}
```
Create `web/src/routes/Settings.tsx`:
```tsx
export default function Settings() {
  return <h1 className="text-2xl font-bold">Settings</h1>
}
```
Create `web/src/routes/Login.tsx`:
```tsx
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '../lib/api'

export default function Login() {
  const [pw, setPw] = useState('')
  const [err, setErr] = useState('')
  const nav = useNavigate()
  async function submit(e: React.FormEvent) {
    e.preventDefault()
    try {
      await api.post('/auth/login', { password: pw })
      nav('/search')
      window.location.reload()
    } catch {
      setErr('Invalid password')
    }
  }
  return (
    <form onSubmit={submit} className="max-w-sm mx-auto mt-24 space-y-4">
      <h1 className="text-2xl font-bold">Log in to Reverb</h1>
      <input
        type="password"
        value={pw}
        onChange={(e) => setPw(e.target.value)}
        className="w-full rounded bg-neutral-900 border border-neutral-700 px-3 py-2"
        placeholder="Admin password"
      />
      {err && <p className="text-accent text-sm">{err}</p>}
      <button className="w-full rounded bg-accent py-2 font-medium text-white">Log in</button>
    </form>
  )
}
```
Create `web/src/routes/Setup.tsx`:
```tsx
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '../lib/api'

export default function Setup() {
  const [pw, setPw] = useState('')
  const nav = useNavigate()
  async function submit(e: React.FormEvent) {
    e.preventDefault()
    await api.post('/setup/admin', { password: pw })
    nav('/search')
    window.location.reload()
  }
  return (
    <form onSubmit={submit} className="max-w-sm mx-auto mt-24 space-y-4">
      <h1 className="text-2xl font-bold">Welcome to Reverb</h1>
      <p className="text-neutral-400 text-sm">Set an admin password to get started.</p>
      <input
        type="password"
        value={pw}
        onChange={(e) => setPw(e.target.value)}
        className="w-full rounded bg-neutral-900 border border-neutral-700 px-3 py-2"
        placeholder="Choose a password"
      />
      <button className="w-full rounded bg-accent py-2 font-medium text-white">Continue</button>
    </form>
  )
}
```

- [ ] **Step 3: Guard + routing**

Replace `web/src/App.tsx`:
```tsx
import { Navigate, Route, Routes } from 'react-router-dom'
import { AppShell } from './components/AppShell'
import { useSessionStatus } from './lib/session'
import Search from './routes/Search'
import Library from './routes/Library'
import Settings from './routes/Settings'
import Login from './routes/Login'
import Setup from './routes/Setup'

export default function App() {
  const s = useSessionStatus()
  if (s.loading) return <div className="p-6 text-neutral-500">Loading…</div>
  if (s.setupRequired) return <Setup />
  if (!s.authenticated) return <Login />
  return (
    <Routes>
      <Route element={<AppShell />}>
        <Route path="/search" element={<Search />} />
        <Route path="/library" element={<Library />} />
        <Route path="/settings" element={<Settings />} />
        <Route path="*" element={<Navigate to="/search" replace />} />
      </Route>
    </Routes>
  )
}
```

- [ ] **Step 4: Write the failing route test**

Create `web/src/routes/Setup.test.tsx`:
```tsx
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Setup from './Setup'

test('setup page prompts for an admin password', () => {
  render(
    <MemoryRouter>
      <Setup />
    </MemoryRouter>,
  )
  expect(screen.getByText('Welcome to Reverb')).toBeInTheDocument()
  expect(screen.getByPlaceholderText('Choose a password')).toBeInTheDocument()
})
```

- [ ] **Step 5: Run frontend tests**

Run: `cd web && npm run test && cd ..`
Expected: all pass (App smoke + Setup).

- [ ] **Step 6: Commit**

```bash
git add web/src
git commit -m "feat(web): session-guarded routing with setup and login flows"
```

---

## Task 11: Embed the built SPA + Makefile

**Files:**
- Modify: `internal/api/embed.go` (replace stub with real embed)
- Create: `Makefile`
- Test: manual run (documented expected output)

**Interfaces:**
- Produces: `(*Server) embeddedSPA()` serves files from embedded `web/dist` with SPA fallback to `index.html`. `make build` produces `./reverb` with the SPA inside.

- [ ] **Step 1: Tag the dev stub `!prod` and add the prod embed**

Add a build constraint as the **first line** of the existing `internal/api/embed.go` (keep the rest of the dev stub unchanged):
```go
//go:build !prod

package api

import "net/http"

// embeddedSPA (dev/test stub) — replaced by embed_prod.go under -tags prod.
func (s *Server) embeddedSPA() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"app": "reverb", "note": "frontend not embedded yet"})
	})
}
```

Create `internal/api/embed_prod.go`:
```go
//go:build prod

package api

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var distFS embed.FS

// embeddedSPA serves the built React app with history-API fallback.
func (s *Server) embeddedSPA() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := fs.Stat(sub, trimLeadingSlash(r.URL.Path)); err != nil {
			// not a real file → serve index.html for client-side routing
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

func trimLeadingSlash(p string) string {
	if len(p) > 0 && p[0] == '/' {
		return p[1:]
	}
	return p
}
```

Why build tags: `go test ./...` and CI compile **without** `-tags prod`, so they use the stub and never require `internal/api/dist` to exist. Only `make build` (which runs `make web` first) passes `-tags prod` and embeds the real SPA.

- [ ] **Step 2: Makefile**

Create `Makefile`:
```make
.PHONY: gen test build web dev clean

gen:
	go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.27.0 generate

test:
	go test ./...
	cd web && npm run test

web:
	cd web && npm install && npm run build
	rm -rf internal/api/dist
	cp -r web/dist internal/api/dist

build: web
	go build -tags prod -o reverb ./cmd/reverb

dev:
	@echo "Run in two shells:"
	@echo "  1) cd web && npm run dev"
	@echo "  2) go run ./cmd/reverb --dev"

clean:
	rm -rf reverb web/dist internal/api/dist
```

Add `/internal/api/dist/` to `.gitignore`:
```bash
printf '%s\n' '/internal/api/dist/' >> .gitignore
```

- [ ] **Step 3: Build the full binary**

Run: `make build`
Expected: `web/dist` built, copied to `internal/api/dist`, `./reverb` produced with no errors.

- [ ] **Step 4: Smoke-run prod mode**

Run:
```bash
rm -f data/reverb.db
./reverb &
sleep 1
curl -s localhost:8090/api/v1/health
curl -s localhost:8090/api/v1/setup/status
curl -s -o /dev/null -w "%{http_code}\n" localhost:8090/
kill %1
```
Expected: health → `{"status":"ok"}`; setup/status → `{"setupRequired":true}`; `/` → `200` (index.html served from embed).

- [ ] **Step 5: Commit**

```bash
git add internal/api/embed.go Makefile .gitignore
git commit -m "build: embed built SPA into the binary with history fallback"
```

---

## Task 12: docker-compose.dev — Navidrome for M1

**Files:**
- Create: `docker-compose.dev.yml`, `dev/music/.gitkeep`, `dev/README.md`

**Interfaces:**
- Produces: `docker compose -f docker-compose.dev.yml up` brings up Navidrome on `:4533` reading from `./dev/music`.

- [ ] **Step 1: Compose file**

Create `docker-compose.dev.yml`:
```yaml
services:
  navidrome:
    image: deluan/navidrome:0.53.3
    ports:
      - "4533:4533"
    environment:
      ND_LOGLEVEL: info
      ND_SCANSCHEDULE: 1m
      ND_BASEURL: ""
    volumes:
      - ./dev/music:/music:ro
      - ./dev/navidrome:/data
```

- [ ] **Step 2: Dev docs + sample-music placeholder**

Run:
```bash
mkdir -p dev/music dev/navidrome
touch dev/music/.gitkeep
printf '%s\n' '/dev/navidrome/' >> .gitignore
```

Create `dev/README.md`:
```markdown
# Reverb dev environment

1. Drop a few Creative-Commons audio files into `dev/music/` (e.g. tracks from
   https://freemusicarchive.org or the Navidrome demo set). They are gitignored
   except `.gitkeep`.
2. `docker compose -f docker-compose.dev.yml up` → Navidrome at http://localhost:4533
   (first run: create an admin user in the Navidrome UI).
3. Run Reverb against it (M1 adds the Subsonic adapter):
   - `cd web && npm run dev`
   - `go run ./cmd/reverb --dev`
   Open http://localhost:8090.
```

Add `dev/music/*` (except `.gitkeep`) to `.gitignore`:
```bash
printf '%s\n' 'dev/music/*' '!dev/music/.gitkeep' >> .gitignore
```

- [ ] **Step 3: Verify Navidrome boots**

Run: `docker compose -f docker-compose.dev.yml up -d && sleep 5 && curl -s -o /dev/null -w "%{http_code}\n" localhost:4533/ping ; docker compose -f docker-compose.dev.yml down`
Expected: `200` (Navidrome ping responds).

- [ ] **Step 4: Commit**

```bash
git add docker-compose.dev.yml dev/README.md dev/music/.gitkeep .gitignore
git commit -m "chore(dev): navidrome docker-compose dev environment"
```

---

## Task 13: CI pipeline

**Files:**
- Create: `.github/workflows/ci.yml`

**Interfaces:**
- Produces: CI that runs Go vet/test and frontend typecheck/test/build on push + PR.

- [ ] **Step 1: Workflow**

Create `.github/workflows/ci.yml`:
```yaml
name: CI
on:
  push:
    branches: [main]
  pull_request:
jobs:
  backend:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.23' }
      - run: go vet ./...
      - run: go test ./... -count=1
  frontend:
    runs-on: ubuntu-latest
    defaults: { run: { working-directory: web } }
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with: { node-version: '20' }
      - run: npm ci
      - run: npx tsc --noEmit
      - run: npm run test
      - run: npm run build
```

- [ ] **Step 2: Validate the backend job locally**

Run: `go vet ./... && go test ./... -count=1`
Expected: PASS (mirrors CI backend job).

- [ ] **Step 3: Validate the frontend job locally**

Run: `cd web && npx tsc --noEmit && npm run test && npm run build && cd ..`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: go + frontend test/build pipeline"
```

---

## Definition of Done (M0)

- `make build` produces a single `./reverb` binary with the SPA embedded.
- Fresh run → `/api/v1/setup/status` reports `setupRequired: true`; the SPA shows the setup screen.
- Setting an admin password logs you in (session cookie) and renders the empty app shell (sidebar + Search/Library/Settings + player bar).
- Restart → setup no longer required; `/login` accepts the password; wrong password is rejected; `/me` is 401 without a session.
- `--dev` proxies the SPA to Vite (HMR); prod serves it from `embed.FS`.
- `go test ./...` and `cd web && npm run test` are green; CI mirrors both.
- `docker compose -f docker-compose.dev.yml up` brings up Navidrome for M1.

---

## Self-Review

**Spec coverage (M0 line items):** repo layout ✓ (T1) · config flags/env ✓ (T2) · SQLite+goose+sqlc ✓ (T3) · EventBus ✓ (T4) · 3 registries + Plugin ✓ (T5) · auth seam: sessions/login/setup_required ✓ (T6–T7) · API server + OpenAPI scaffold ✓ (T7–T8) · `--dev` Vite proxy / embed.FS ✓ (T7, T11) · React+Tailwind app shell ✓ (T9) · `--color-accent` token system ✓ (T9) · docker-compose.dev ✓ (T12). Capability probes (DescribeCapabilities) ✓ (T5). CI added ✓ (T13).

**Deferred-by-design (not M0):** `match_cache`/`download_jobs` tables (M2/M3 migrations) · real adapters (M1+) · dynamic palette/responsive/settings-UI (M4) · OpenAPI→TS codegen (introduced when the client consumes typed data, M1+).

**Placeholder scan:** every code step contains complete, runnable content; no TBD/TODO. The Task 7 embed stub is intentional and explicitly replaced in Task 11.

**Type consistency:** `Deps` fields (`Auth`, `Library`, `Search`, `Downloader`, `Dev`) are introduced in T7 and used consistently in handlers/main. `auth.Querier` matches the sqlc-generated method set from T3. `registry.Plugin`/`ConfigSchema`/`DescribeCapabilities` names are consistent across T5, T7. Frontend `api.get/post` and `useSessionStatus` signatures match their consumers in T10.
