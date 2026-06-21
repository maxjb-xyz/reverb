# Reverb M5 (Package & Ship) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Package Reverb as a single self-hosted Docker image (prod Go binary with embedded SPA + Python3 + ffmpeg + a pinned spotDL), document it (README + deployment + OpenAPI + legal/ethical framing), prove the core loop with a hermetic Playwright e2e, and wire CI + a (prepared-but-untriggered) GHCR release workflow.

**Architecture:** A multi-stage Dockerfile builds the web app (node), compiles a static `CGO_ENABLED=0 -tags prod` binary with the SPA embedded (golang), and ships it on `python:3.12-slim` with ffmpeg + a version-pinned spotDL, running as a non-root user. The binary is version-stamped via `-ldflags -X main.version`, which is surfaced at startup and at `GET /api/v1/version`. A production `docker-compose.yml` shares a `/music` volume between Reverb (spotDL `output_dir`) and a Subsonic/Navidrome library so downloads appear in the library. A Playwright spec drives the login → search-everywhere → download → flip-to-in-library → play loop entirely against route/WebSocket mocks (no real network, credentials, or money).

**Tech Stack:** Go 1.23 (chi v5, `modernc.org/sqlite`), React 19 / TypeScript ~6 (Vite 8, TanStack Query, zustand, react-router 6), Playwright (e2e), Docker multi-stage, GitHub Actions, GHCR.

## Global Constraints

- Single published image contains the prod binary (web embedded) + Python3 + ffmpeg + PINNED `spotdl==4.2.11`. Runtime base `python:3.12-slim`. Container runs as a **non-root** user.
- Go build is `CGO_ENABLED=0`, `-tags prod`, version injected via `-ldflags "-X main.version=$(VERSION)"`. The binary is fully static (sqlite driver is `modernc.org/sqlite`, cgo-free).
- The SPA is embedded ONLY when built with `-tags prod` AND `internal/api/dist` is populated first (copied from `web/dist`). `internal/api/embed_prod.go` has `//go:embed all:dist`; `embed.go` is the `!prod` stub.
- Go commands are ALWAYS scoped to `./cmd/... ./internal/...` (NEVER `./...` — the repo has vendored Go inside `web/node_modules` that breaks `./...`).
- Secrets ONLY via env in compose (never baked into the image or committed). `.env` is gitignored (root `.gitignore` already ignores `/.env`; compose's `.env` lives at repo root → covered). Only `.env.example` is committed.
- spotDL is PINNED. Bumping the pin REQUIRES re-validating the progress-parse regex `progressRe = (\d{1,3})\s*%` in `internal/download/spotdl/adapter.go` (there is an explicit code comment to this effect). Call this out in the Dockerfile comment and deployment docs.
- Playwright e2e is hermetic + fully mocked: serve the built `web/dist` via Playwright's `webServer` (`vite preview`), intercept ALL `/api/v1/*` HTTP routes and the `/api/v1/ws` WebSocket. No real backend, external API, credentials, media, or network egress.
- TS strict for any new TS (Playwright config/spec): the web tsconfig sets `verbatimModuleSyntax` and `erasableSyntaxOnly` → use `import type` for type-only imports, NO enums, NO constructor parameter-properties.
- No regressions to M0–M4: existing 208 Go tests + 150 FE tests stay green; existing CI `backend` + `frontend` jobs stay intact.
- Legal/ethical framing is REQUIRED in the README.
- Config surface (verbatim, do not re-derive): flags `--port` (default 8090), `--db` (default `./data/reverb.db`), `--dev`, `--log-level` (default `info`). Env: `REVERB_PORT`, `REVERB_DB`, `REVERB_DEV=1`, `REVERB_ADMIN_PASSWORD`, `REVERB_AUTH_DISABLED=1|true`. Adapter secrets via env: `REVERB_LIBRARY_PASSWORD`, `REVERB_SPOTIFY_CLIENT_SECRET`. Flags win over env, env wins over defaults.

## License Decision

Adopt **AGPL-3.0-only**. Rationale: Reverb is a network-served, self-hosted app that bundles a third-party downloader (spotDL, itself GPL-family) and connects to user-provided services; AGPL keeps modifications open for a service that users reach over a network, matches the GPL-family tooling it ships, and is the conventional license for self-hosted media servers (Navidrome/Jellyfin lineage). The `LICENSE` file is added in Task 6, and the README states the choice + reasoning in one line.

## File Structure

New files:
- `cmd/reverb/version.go` — `var version = "dev"` + `Version()` accessor (own file so the `-X main.version` symbol is obvious and version logic is isolated).
- `internal/api/version.go` — `GET /api/v1/version` handler returning `{"version": "..."}`.
- `internal/api/version_test.go` — httptest for the version handler.
- `Dockerfile` — 3-stage build (node → golang → python runtime).
- `.dockerignore` — keep build context small + secrets/artifacts out.
- `docker-compose.yml` — production compose (Reverb + commented Navidrome, shared `/music`).
- `.env.example` — committed secret/template env file.
- `web/playwright.config.ts` — Playwright config (webServer = `vite preview`).
- `web/e2e/core-loop.spec.ts` — the hermetic "money test".
- `web/e2e/mocks.ts` — the route + WebSocket mock helpers (shared, typed).
- `README.md` — root project README (what/why/quick-start/config/legal/architecture/license).
- `LICENSE` — AGPL-3.0 full text.
- `docs/deployment.md` — reverse-proxy/TLS, volumes/backups, upgrade steps, spotDL pin note.
- `.github/workflows/release.yml` — release-published GHCR publish + GitHub Release (committed, NOT triggered).

Modified files:
- `cmd/reverb/main.go` — log the version at startup; thread `version` into `api.Deps.Version`.
- `internal/api/server.go` — add `Version string` to `Deps`; register `GET /api/v1/version`.
- `internal/api/openapi.yaml` — expand to document the real `/api/v1` surface (incl. `/version`).
- `internal/api/openapi_test.go` — assert content-type + the `openapi: 3.0.3` body (extend existing).
- `Makefile` — `VERSION ?= dev`; `build` injects `-ldflags "-X main.version=$(VERSION)"` and stays `CGO_ENABLED=0`.
- `web/package.json` — add Playwright devDependency + `e2e` script.
- `.github/workflows/ci.yml` — keep `backend` + `frontend`; ADD `docker` + `e2e` jobs.

---

## Task 1: Version stamping (Go + API + Makefile)

**Files:**
- Create: `cmd/reverb/version.go`
- Create: `internal/api/version.go`
- Create: `internal/api/version_test.go`
- Modify: `internal/api/server.go` (add `Version string` to `Deps`; register route)
- Modify: `cmd/reverb/main.go` (log version at startup; set `deps.Version`)
- Modify: `Makefile` (`VERSION ?= dev`; ldflags + `CGO_ENABLED=0`)

**Interfaces:**
- Produces:
  - `main.version` (package `main`, `var version = "dev"`) — the `-ldflags -X main.version=…` target.
  - `api.Deps.Version string` — version surfaced by the API; defaults to `"dev"` when empty.
  - `GET /api/v1/version` → `200 application/json` body `{"version":"<value>"}`.

- [ ] **Step 1: Write the failing API test**

Create `internal/api/version_test.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVersionEndpoint(t *testing.T) {
	tests := []struct {
		name string
		deps Deps
		want string
	}{
		{name: "explicit version", deps: Deps{Version: "1.2.3"}, want: "1.2.3"},
		{name: "empty defaults to dev", deps: Deps{}, want: "dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := NewServer(tt.deps)
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/version", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Fatalf("content-type = %q, want application/json", ct)
			}
			var body struct {
				Version string `json:"version"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.Version != tt.want {
				t.Fatalf("version = %q, want %q", body.Version, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/api/ -run TestVersionEndpoint -count=1 -v`
Expected: FAIL — compile error `deps.Version undefined` / no `/api/v1/version` route (404).

- [ ] **Step 3: Add `Version` to `Deps`**

In `internal/api/server.go`, add the field to the `Deps` struct (place it right after `Dev bool`):

```go
type Deps struct {
	Auth             *auth.Service
	Library          library.LibraryAdapter
	SearchAggregator Streamer
	Search           *registry.Registry
	Downloader       *registry.Registry
	Lib              *registry.Registry
	Downloads        DownloadManager
	Events           EventSubscriber
	Adapters         AdapterStore
	ConfigDirty      ConfigDirty
	Dev              bool
	Version          string
}
```

- [ ] **Step 4: Register the version route**

In `internal/api/server.go`, in `routes()`, add the public route right after the `/openapi.yaml` line:

```go
		r.Get("/openapi.yaml", s.handleOpenAPI)
		r.Get("/version", s.handleVersion)
```

- [ ] **Step 5: Implement the version handler**

Create `internal/api/version.go`:

```go
package api

import "net/http"

// handleVersion reports the build version. Empty Deps.Version (e.g. zero-value
// Deps in tests, or a build without -ldflags) reports "dev".
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	v := s.deps.Version
	if v == "" {
		v = "dev"
	}
	writeJSON(w, http.StatusOK, map[string]string{"version": v})
}
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./internal/api/ -run TestVersionEndpoint -count=1 -v`
Expected: PASS (both subtests).

- [ ] **Step 7: Add the `main.version` variable**

Create `cmd/reverb/version.go`:

```go
package main

// version is the build version. It is overridden at build time via
// -ldflags "-X main.version=<value>" (see the Makefile and Dockerfile).
// It defaults to "dev" for `go run` / un-stamped builds.
var version = "dev"
```

- [ ] **Step 8: Log the version at startup and thread it into Deps**

In `cmd/reverb/main.go`, add a startup log line as the FIRST statement inside `main()` (before `config.Load`):

```go
func main() {
	log.Printf("reverb %s starting", version)

	cfg, err := config.Load(os.Args[1:], os.Getenv)
```

Then add `Version: version,` to the `deps := api.Deps{...}` literal (place it right after `Dev: cfg.Dev,`):

```go
	deps := api.Deps{
		Auth:        authSvc,
		Library:     libAdapter,
		Lib:         libraryReg,
		Search:      searchReg,
		Downloader:  downloaderReg,
		Adapters:    st.Q(),
		Events:      bus,
		ConfigDirty: dirty,
		Dev:         cfg.Dev,
		Version:     version,
	}
```

- [ ] **Step 9: Update the Makefile build target to inject VERSION**

In `Makefile`, change the `build` target and add a `VERSION` default at the top. Replace:

```make
build: web
	go build -tags prod -o reverb ./cmd/reverb
```

with:

```make
VERSION ?= dev

build: web
	CGO_ENABLED=0 go build -tags prod -ldflags "-X main.version=$(VERSION)" -o reverb ./cmd/reverb
```

- [ ] **Step 10: Verify the binary builds and stamps the version**

Run: `make build VERSION=t-1.0.0 && ./reverb --help 2>&1 | head -n 1 ; echo "exit:$?"`
Expected: build succeeds; `--help` exits non-zero is fine — the goal is the binary exists. Then verify the stamp without a long-running server:
Run: `go build -tags prod -ldflags "-X main.version=t-1.0.0" -o /tmp/reverb-vtest ./cmd/reverb && /tmp/reverb-vtest --port 0 --db /tmp/reverb-vtest.db & pid=$!; sleep 1; kill "$pid" 2>/dev/null; true`
Expected: among the startup logs, the line `reverb t-1.0.0 starting` appears.
(Note: `--help` is not a defined flag; `flag` will error and exit — that only confirms the binary runs. The version log line is the real check. PID capture is used instead of `kill %1` because job-control is unreliable in non-interactive shells.)

- [ ] **Step 11: Run the full Go API test package**

Run: `go test ./internal/api/ -count=1`
Expected: PASS (ok github.com/maxjb-xyz/reverb/internal/api).

- [ ] **Step 12: Commit**

```bash
git add cmd/reverb/version.go cmd/reverb/main.go internal/api/version.go internal/api/version_test.go internal/api/server.go Makefile
git commit -m "feat(version): stamp build version, log at startup, expose GET /api/v1/version"
```

---

## Task 2: OpenAPI — expand the spec (serving already exists)

**Files:**
- Modify: `internal/api/openapi.yaml` (expand to the real surface, add `/version`)
- Modify: `internal/api/openapi_test.go` (assert content-type + the version path is documented)

**Interfaces:**
- Consumes: `GET /api/v1/openapi.yaml` (already wired in `server.go` + `internal/api/openapi.go`, which embeds `openapi.yaml` via `//go:embed openapi.yaml`).
- Produces: a documented `/api/v1` surface served verbatim from the embedded file (ships in the binary).

> Context: `internal/api/openapi.go` already does `//go:embed openapi.yaml` and serves it at `GET /api/v1/openapi.yaml` with `Content-Type: application/yaml`. `internal/api/openapi_test.go` already asserts `200` + body contains `openapi: 3.0.3`. This task expands the YAML content and tightens the test. No new handler is needed.

- [ ] **Step 1: Write the stronger failing test**

Replace the contents of `internal/api/openapi_test.go` with:

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
	if ct := rec.Header().Get("Content-Type"); ct != "application/yaml" {
		t.Fatalf("content-type = %q, want application/yaml", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"openapi: 3.0.3",
		"/version:",
		"/search/everywhere:",
		"/downloads:",
		"/ws:",
		"/stream/{id}:",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("spec missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/api/ -run TestServesOpenAPI -count=1 -v`
Expected: FAIL — body does not yet contain `/version:`, `/search/everywhere:`, `/downloads:`, `/ws:`, `/stream/{id}:`.

- [ ] **Step 3: Expand the OpenAPI document**

Replace the entire contents of `internal/api/openapi.yaml` with:

```yaml
openapi: 3.0.3
info:
  title: Reverb API
  version: 0.1.0
  description: >
    Reverb is a self-hosted music app unifying a Subsonic/Navidrome library,
    Spotify search, and spotDL one-click download. All paths are served under
    /api/v1. Session auth is a cookie issued by /setup/admin or /auth/login;
    protected routes return 401 without it (unless auth is disabled via env).
servers:
  - url: /api/v1
paths:
  /health:
    get:
      summary: Liveness probe
      responses: { "200": { description: ok } }
  /version:
    get:
      summary: Build version
      responses:
        "200":
          description: version string
          content:
            application/json:
              schema:
                type: object
                properties: { version: { type: string } }
  /openapi.yaml:
    get:
      summary: This OpenAPI document
      responses: { "200": { description: yaml } }
  /setup/status:
    get:
      summary: Whether first-run setup is required
      responses: { "200": { description: "{ setupRequired: bool }" } }
  /setup/admin:
    post:
      summary: Set the admin password during first-run setup
      requestBody:
        required: true
        content:
          application/json:
            schema: { type: object, properties: { password: { type: string } }, required: [password] }
      responses: { "200": { description: session issued }, "400": { description: password required }, "409": { description: already set up } }
  /auth/login:
    post:
      summary: Log in with the admin password
      requestBody:
        required: true
        content:
          application/json:
            schema: { type: object, properties: { password: { type: string } }, required: [password] }
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
  /adapters:
    get:
      summary: List configured adapter instances (secrets redacted)
      responses: { "200": { description: ok }, "401": { description: unauthorized } }
    post:
      summary: Create an adapter instance
      responses: { "200": { description: created }, "400": { description: invalid } }
  /adapters/{id}:
    put:
      summary: Update an adapter instance
      parameters: [{ name: id, in: path, required: true, schema: { type: string } }]
      responses: { "200": { description: updated }, "404": { description: not found } }
    delete:
      summary: Delete an adapter instance
      parameters: [{ name: id, in: path, required: true, schema: { type: string } }]
      responses: { "200": { description: deleted }, "404": { description: not found } }
  /adapters/test:
    post:
      summary: Test an adapter configuration (TestConnection) without persisting
      responses: { "200": { description: "{ ok: bool, error?: string }" } }
  /settings:
    get:
      summary: Read app settings
      responses: { "200": { description: ok } }
    put:
      summary: Update app settings (sets the restart-to-apply flag)
      responses: { "200": { description: ok, pendingRestart flag included } }
  /config/pending-restart:
    get:
      summary: Whether config changed since startup (restart-to-apply banner)
      responses: { "200": { description: "{ pending: bool }" } }
  /library/search:
    get:
      summary: Fast REST search of the local library (tracks/albums/artists)
      parameters: [{ name: q, in: query, required: true, schema: { type: string } }]
      responses: { "200": { description: SearchResults } }
  /library/artists:
    get:
      summary: List library artists
      responses: { "200": { description: ok } }
  /library/artist/{id}:
    get:
      summary: One artist with albums
      parameters: [{ name: id, in: path, required: true, schema: { type: string } }]
      responses: { "200": { description: ok }, "404": { description: not found } }
  /library/album/{id}:
    get:
      summary: One album with tracks
      parameters: [{ name: id, in: path, required: true, schema: { type: string } }]
      responses: { "200": { description: ok }, "404": { description: not found } }
  /library/albums:
    get:
      summary: List albums
      parameters: [{ name: type, in: query, required: false, schema: { type: string } }]
      responses: { "200": { description: ok } }
  /library/playlists:
    get:
      summary: List playlists
      responses: { "200": { description: ok } }
  /stream/{id}:
    get:
      summary: Stream/transcode-proxy a library track by id (Range supported)
      parameters: [{ name: id, in: path, required: true, schema: { type: string } }]
      responses: { "200": { description: audio bytes }, "206": { description: partial content }, "404": { description: not found } }
  /cover/{id}:
    get:
      summary: Cover art proxy by id
      parameters:
        - { name: id, in: path, required: true, schema: { type: string } }
        - { name: size, in: query, required: false, schema: { type: integer } }
      responses: { "200": { description: image bytes } }
  /search/everywhere:
    get:
      summary: >
        Search-everywhere as Server-Sent Events. Streams one envelope per source
        (text/event-stream); each event data is a JSON SearchEnvelope with
        per-result library match status.
      parameters:
        - { name: q, in: query, required: true, schema: { type: string } }
        - { name: type, in: query, required: false, schema: { type: string, enum: [track, album, artist] } }
      responses:
        "200":
          description: SSE stream of search envelopes
          content:
            text/event-stream:
              schema: { type: string }
  /downloads:
    get:
      summary: List download jobs
      responses: { "200": { description: array of DownloadJob } }
    post:
      summary: Enqueue a one-click download (deduped)
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                source: { type: string }
                externalId: { type: string }
                artist: { type: string }
                title: { type: string }
                album: { type: string }
                isrc: { type: string }
                playWhenReady: { type: boolean }
              required: [source, externalId, artist, title]
      responses: { "200": { description: DownloadJob }, "400": { description: invalid } }
  /downloads/{id}/cancel:
    post:
      summary: Cancel a queued/running download
      parameters: [{ name: id, in: path, required: true, schema: { type: string } }]
      responses: { "200": { description: ok }, "404": { description: not found } }
  /downloads/{id}/retry:
    post:
      summary: Retry a failed/canceled download
      parameters: [{ name: id, in: path, required: true, schema: { type: string } }]
      responses: { "200": { description: DownloadJob }, "404": { description: not found } }
  /ws:
    get:
      summary: >
        WebSocket of live events. Each frame is JSON { type, payload } where type
        is the EventBus topic (download.queued, download.progress,
        download.complete, download.failed, library.updated). Used by the client
        to flip a result to in-library, drive progress rings, and resync.
      responses:
        "101": { description: Switching Protocols (WebSocket upgrade) }
        "401": { description: unauthorized }
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/api/ -run TestServesOpenAPI -count=1 -v`
Expected: PASS.

- [ ] **Step 5: Sanity-check the YAML is well-formed**

Run: `go test ./internal/api/ -count=1`
Expected: PASS (the whole api package; if the YAML were malformed the embed still compiles, so also do a quick lint):
Run: `python3 -c "import sys,yaml" 2>/dev/null && python3 -c "import yaml,sys; yaml.safe_load(open('internal/api/openapi.yaml')); print('yaml ok')" || echo "pyyaml not installed; skip (go embed serves it verbatim)"`
Expected: `yaml ok` (or the skip message — the Go test is authoritative).

- [ ] **Step 6: Commit**

```bash
git add internal/api/openapi.yaml internal/api/openapi_test.go
git commit -m "docs(api): expand OpenAPI to the real /api/v1 surface incl. /version"
```

---

## Task 3: Multi-stage Dockerfile + .dockerignore

**Files:**
- Create: `Dockerfile`
- Create: `.dockerignore`

**Interfaces:**
- Consumes: `Makefile`/build conventions (`-tags prod`, `CGO_ENABLED=0`, `-ldflags -X main.version`), the embed contract (`web/dist` → `internal/api/dist`), the spotDL runtime needs (Python3 + ffmpeg), `ENV REVERB_DB` default.
- Produces: an image `reverb:test` whose `ENTRYPOINT ["reverb"]` runs the prod binary as a non-root user; `EXPOSE 8090`; `VOLUME ["/data"]`; spotDL pinned at `4.2.11`.

- [ ] **Step 1: Create `.dockerignore`**

Create `.dockerignore`:

```
# Build artifacts and dependencies (re-installed/rebuilt inside the image)
web/node_modules
web/dist
internal/api/dist
node_modules

# Local data / databases
data
*.db

# The locally built binary (the image builds its own)
/reverb

# VCS, tooling, scratch
.git
.github
.gitignore
.superpowers
.claude
dev

# Docs and plans (not needed to build the image)
docs
README.md
reverb-plan.md

# Secrets must never enter the build context
.env
```

- [ ] **Step 2: Create the Dockerfile**

Create `Dockerfile`:

```dockerfile
# syntax=docker/dockerfile:1

# ---------- Stage 1: build the web SPA ----------
FROM node:22-slim AS web
WORKDIR /app/web
# Install deps first (cache layer) — copy lockfile + manifest only.
COPY web/package.json web/package-lock.json ./
RUN npm ci
# Then the source, and build.
COPY web/ ./
RUN npm run build

# ---------- Stage 2: build the Go binary with the SPA embedded ----------
FROM golang:1.23 AS gobuild
ARG VERSION=dev
WORKDIR /src
# Module cache layer.
COPY go.mod go.sum ./
RUN go mod download
# Source.
COPY . .
# The prod embed requires internal/api/dist to be populated BEFORE the build.
COPY --from=web /app/web/dist ./internal/api/dist
# Static, cgo-free, prod-embedded, version-stamped.
RUN CGO_ENABLED=0 go build -tags prod \
      -ldflags "-X main.version=${VERSION}" \
      -o /out/reverb ./cmd/reverb

# ---------- Stage 3: runtime ----------
FROM python:3.12-slim AS runtime
# ffmpeg is a spotDL runtime dependency.
RUN apt-get update \
 && apt-get install -y --no-install-recommends ffmpeg \
 && rm -rf /var/lib/apt/lists/*
# VERSION PIN: spotDL output formatting is fragile. Reverb's spotdl adapter parses
# progress with the regex `(\d{1,3})\s*%` in internal/download/spotdl/adapter.go.
# Bumping this pin REQUIRES re-validating that regex against the new output.
RUN pip install --no-cache-dir "spotdl==4.2.11"
# Non-root user.
RUN useradd --create-home --uid 10001 reverb
COPY --from=gobuild /out/reverb /usr/local/bin/reverb
ENV REVERB_DB=/data/reverb.db
VOLUME ["/data"]
EXPOSE 8090
USER reverb
ENTRYPOINT ["reverb"]
```

- [ ] **Step 3: Build the image (executor's local Docker check)**

Run: `docker build -t reverb:test --build-arg VERSION=t-1.0.0 .`
Expected: build succeeds — final line `naming to docker.io/library/reverb:test` (or `Successfully tagged reverb:test`). Each stage (web npm build, go build, pip install spotdl==4.2.11, apt ffmpeg) completes without error.
Note: this step needs a working Docker daemon on the executor. If Docker is unavailable in the sandbox, mark the step as "deferred to CI (Task 8 `docker` job runs the same build)" and proceed — the Dockerfile content is the deliverable.

- [ ] **Step 4: Smoke-run the image prints the version and serves /health**

Run:
```bash
docker run -d --name reverb-smoke -p 18090:8090 reverb:test --db /tmp/smoke.db
sleep 2
docker logs reverb-smoke 2>&1 | grep "reverb t-1.0.0 starting"
curl -fsS http://localhost:18090/api/v1/version
docker rm -f reverb-smoke
```
Expected: the log line `reverb t-1.0.0 starting` is present; the curl prints `{"version":"t-1.0.0"}`. (If Docker is unavailable, defer to CI as in Step 3.)

- [ ] **Step 5: Commit**

```bash
git add Dockerfile .dockerignore
git commit -m "build(docker): multi-stage image (node->go prod embed->python3+ffmpeg+spotdl pin), non-root"
```

---

## Task 4: Production docker-compose.yml + .env.example

**Files:**
- Create: `docker-compose.yml`
- Create: `.env.example`

**Interfaces:**
- Consumes: the Dockerfile (`build: .`), the config env surface, the shared-`/music` requirement (spotDL `output_dir` = `/music`; Navidrome scans the same volume), `EXPOSE 8090`, `VOLUME ["/data"]`, `ENV REVERB_DB=/data/reverb.db`.
- Produces: a compose project that `docker compose config -q` parses clean; secrets sourced only from `.env` (gitignored), with `.env.example` as the committed template.

- [ ] **Step 1: Create `.env.example`**

Create `.env.example`:

```dotenv
# Copy this file to `.env` (which is gitignored) and fill in real values.
# These are read by docker-compose.yml via `env_file`. NEVER commit `.env`.

# Admin password seeded on first run if setup has not been completed yet.
# Leave blank to use the in-app first-run wizard instead.
REVERB_ADMIN_PASSWORD=

# Spotify search adapter — your Spotify app's Client Secret.
# (The Client ID is non-secret and is set in the in-app Settings UI.)
REVERB_SPOTIFY_CLIENT_SECRET=

# Subsonic/Navidrome library adapter password (overrides the stored config_json
# secret at runtime). Set this if you point Reverb at an existing Navidrome.
REVERB_LIBRARY_PASSWORD=
```

- [ ] **Step 2: Create `docker-compose.yml`**

Create `docker-compose.yml`:

```yaml
# Production-ish single-host deployment for Reverb.
#
# Reverb writes downloads (via spotDL) into the shared /music volume. Point your
# Subsonic/Navidrome library adapter at the SAME music so downloads appear in the
# library after a scan. See the commented `navidrome` service below for a ready
# pairing. Secrets come ONLY from .env (copy .env.example -> .env).

services:
  reverb:
    build:
      context: .
      args:
        VERSION: "${REVERB_VERSION:-dev}"
    # Or pull a published image instead of building:
    # image: ghcr.io/maxjb-xyz/reverb:latest
    ports:
      - "8090:8090"
    env_file:
      - .env
    volumes:
      # SQLite DB + app state (REVERB_DB defaults to /data/reverb.db in the image).
      - ./data:/data
      # Shared music dir. Set the spotDL adapter's `output_dir` to /music in the
      # Settings UI so downloads land where the library scans.
      - ./music:/music
    restart: unless-stopped

  # ---------------------------------------------------------------------------
  # Optional: a Navidrome that scans the SAME ./music volume. Uncomment to run
  # the full pairing. In Reverb's Settings UI add a Subsonic library adapter with
  # base URL http://navidrome:4533 (and REVERB_LIBRARY_PASSWORD in .env).
  # ---------------------------------------------------------------------------
  # navidrome:
  #   image: deluan/navidrome:latest
  #   ports:
  #     - "4533:4533"
  #   environment:
  #     ND_LOGLEVEL: info
  #   volumes:
  #     - ./music:/music:ro
  #     - ./navidrome-data:/data
  #   restart: unless-stopped
```

- [ ] **Step 3: Verify compose parses clean**

Run: `docker compose config -q`
Expected: no output, exit code 0. (`-q` prints nothing on success.)
Note: `docker compose config` reads `.env` for variable substitution. `${REVERB_VERSION:-dev}` has a default so it parses even without `.env`. If the daemon/CLI is unavailable, defer to CI (Task 8 `docker` job can add the same check); the compose content is the deliverable.

- [ ] **Step 4: Confirm `.env` is gitignored (no new ignore needed)**

Run: `git check-ignore .env ; echo "exit:$?"`
Expected: prints `.env` and `exit:0` (root `.gitignore` already has `/.env`; `.env` is at repo root). If it does NOT match, add a line `.env` to `.gitignore` and commit it with this task. (It is expected to already match.)

- [ ] **Step 5: Commit**

```bash
git add docker-compose.yml .env.example
git commit -m "deploy(compose): production docker-compose with shared /music + .env.example"
```

---

## Task 5: Playwright e2e of the core loop (the money test)

**Files:**
- Modify: `web/package.json` (add `@playwright/test` devDependency + `e2e` script)
- Create: `web/playwright.config.ts`
- Create: `web/e2e/mocks.ts`
- Create: `web/e2e/core-loop.spec.ts`

**Interfaces:**
- Consumes: the built SPA (`web/dist` served by `vite preview`), and the real client API contracts:
  - `GET /api/v1/setup/status` → `{ setupRequired: boolean }`
  - `GET /api/v1/me` → `200` (authenticated) or `401`
  - `POST /api/v1/auth/login` → `200 { ok: true }`
  - `GET /api/v1/search/everywhere?q=…&type=track` → `text/event-stream` of `data: <SearchEnvelope JSON>`
  - `POST /api/v1/downloads` → `DownloadJob` JSON
  - `GET /api/v1/downloads` → `DownloadJob[]`
  - `WS /api/v1/ws` → frames `{ type, payload }` (e.g. `download.complete` with `payload.libraryTrackId`)
  - `GET /api/v1/stream/{id}` → audio bytes
  - DOM: search "Everywhere" toggle button text `Everywhere`; a result row with a `Download <title>` aria-label button (from `ExternalRow`); after flip, an in-library row is a `<button>` that calls `playTrackList`; the player Pause button has `aria-label="Pause"` and the PlayerBar shows the track title (PlayerBar is `md:flex` → needs a ≥768px viewport).
- Produces: `npm run e2e` (Playwright) green; fully hermetic.

> Design note (deterministic flip-to-in-library): clicking Download fires `POST /downloads` (mock returns a `queued` job). The app's WebSocket (`useRealtime` in `AppShell`) is mocked via `page.routeWebSocket` (which is async — must be `await`ed). `installWsMock` does NOT send any frame on connect; instead it captures the `WebSocketRoute` and returns a trigger object with an async `complete()` method. The spec calls `await ws.complete()` ONLY AFTER clicking the Download button (which has already fired `POST /downloads` and upserted the queued job-1). `complete()` sends the `download.complete` frame so `downloadStore.applyEvent` flips job-1 to `completed` + `libraryTrackId`; `ExternalRow` then renders the in-library `✓` button. Clicking that button calls `playTrackList`, which sets `playing = true` and the PlayerBar shows the title + a Pause button. The `/stream/{id}` route is mocked so no real media/network is touched (the audio element's play() may reject in headless Chromium, but the store sets `playing` optimistically — we assert UI state, not audio output). The initial `GET /api/v1/downloads` (the WS onOpen resync) returns an EMPTY array `[]` so the Everywhere result row starts with a visible Download button — not pre-completed.

- [ ] **Step 1: Add the Playwright devDependency and the e2e script**

Run (from repo root): `cd web && npm install -D @playwright/test@^1.49.0`
Expected: `@playwright/test` appears under `devDependencies` in `web/package.json` and `web/package-lock.json` updates.

Then edit `web/package.json` `scripts` to add the `e2e` script (keep existing scripts):

```json
  "scripts": {
    "dev": "vite",
    "build": "tsc -b && vite build",
    "lint": "eslint .",
    "preview": "vite preview",
    "test": "vitest run",
    "e2e": "playwright test"
  },
```

- [ ] **Step 2: Install the Playwright browser (Chromium only)**

Run (from `web/`): `npx playwright install --with-deps chromium`
Expected: Chromium downloads/installs successfully. (CI re-runs this in Task 8; locally it is required for `npm run e2e`.)

- [ ] **Step 3: Create the Playwright config**

Create `web/playwright.config.ts`:

```ts
import { defineConfig, devices } from '@playwright/test'

// Hermetic e2e: serve the built SPA via `vite preview` and intercept ALL
// /api/v1/* HTTP + the /api/v1/ws WebSocket in the spec. No real backend.
export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? 'github' : 'list',
  use: {
    baseURL: 'http://localhost:4173',
    trace: 'on-first-retry',
    // PlayerBar is `hidden md:flex`; use a desktop viewport so it renders.
    viewport: { width: 1280, height: 800 },
  },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
  ],
  webServer: {
    // `vite preview` serves the production build on :4173 (Vite's default
    // preview port). Build first so dist exists.
    command: 'npm run build && npm run preview -- --port 4173 --strictPort',
    url: 'http://localhost:4173',
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
  },
})
```

- [ ] **Step 4: Create the mock helpers**

Create `web/e2e/mocks.ts`:

```ts
import type { Page, Route, WebSocketRoute } from '@playwright/test'

// One external track that is NOT in the library yet (match.status = not_in_library).
export const externalTrack = {
  source: 'spotify',
  externalId: 'ext-1',
  title: 'Test Anthem',
  artist: 'Mock Artist',
  album: 'Mock Album',
  durationMs: 200_000,
  isrc: 'TESTISRC0001',
  type: 'track' as const,
  match: { status: 'not_in_library' as const, libraryTrackId: '', method: 'none' as const, confidence: 0 },
}

// The library track id the completed download resolves to (flips the row).
export const libraryTrackId = 'lib-track-1'

// The queued job returned by POST /downloads.
function queuedJob() {
  return {
    id: 'job-1',
    dedupKey: `isrc:${externalTrack.isrc.toLowerCase()}`,
    status: 'queued',
    progress: 0,
    downloaderName: 'spotdl',
    priority: 0,
    attempts: 0,
    source: externalTrack.source,
    externalId: externalTrack.externalId,
    artist: externalTrack.artist,
    title: externalTrack.title,
    album: externalTrack.album,
    isrc: externalTrack.isrc,
    playWhenReady: false,
    createdAt: Date.now() / 1000,
    startedAt: 0,
    finishedAt: 0,
  }
}

// installApiMocks intercepts every /api/v1/* HTTP call. `authed` is a mutable box
// so the session flips to authenticated after login (the app calls
// /setup/status then /me on load, and reloads after POST /auth/login).
export async function installApiMocks(page: Page, authed: { value: boolean }) {
  await page.route('**/api/v1/setup/status', (route: Route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ setupRequired: false }) }),
  )

  await page.route('**/api/v1/me', (route: Route) =>
    authed.value
      ? route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ authenticated: true }) })
      : route.fulfill({ status: 401, contentType: 'application/json', body: JSON.stringify({ error: 'unauthorized' }) }),
  )

  await page.route('**/api/v1/auth/login', (route: Route) => {
    authed.value = true
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ok: true }) })
  })

  // Everywhere search: a finite SSE body delivers the data: frame to onmessage then
  // closes; EventSource auto-reconnects and the persistent route re-fulfills the same
  // body — harmless because everywhereStore.appendSection dedups by dedupKey. Do NOT
  // "fix" this by switching to a hanging body: that would make onmessage never fire.
  await page.route('**/api/v1/search/everywhere**', (route: Route) => {
    const envelope = { source: 'spotify', status: 'ok', results: [externalTrack] }
    const body = `data: ${JSON.stringify(envelope)}\n\n`
    return route.fulfill({ status: 200, contentType: 'text/event-stream', body })
  })

  // Library search (library mode) — empty; the spec uses Everywhere mode.
  await page.route('**/api/v1/library/search**', (route: Route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ tracks: [], albums: [], artists: [] }) }),
  )

  // GET /downloads: return EMPTY array so the result row starts with a visible
  // Download button (not pre-completed). POST /downloads enqueues and returns the
  // queued job; the WS completion frame (sent by ws.complete() after the click)
  // then flips the row to in-library.
  await page.route('**/api/v1/downloads', (route: Route) => {
    if (route.request().method() === 'POST') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(queuedJob()) })
    }
    // GET /downloads (WS onOpen resync) → empty: no pre-existing jobs.
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
  })

  // Stream proxy → tiny audio body so the <audio> src resolves (no real media).
  await page.route('**/api/v1/stream/**', (route: Route) =>
    route.fulfill({ status: 200, contentType: 'audio/mpeg', body: '' }),
  )

  // Cover proxy → 1x1 transparent png-ish bytes (never actually displayed in assertions).
  await page.route('**/api/v1/cover/**', (route: Route) =>
    route.fulfill({ status: 200, contentType: 'image/png', body: '' }),
  )
}

// WsTrigger lets the spec fire the completion frame at the right moment.
export type WsTrigger = { complete: () => Promise<void> }

// installWsMock intercepts the realtime WebSocket. On connect it does NOT send any
// frame; instead it captures the WebSocketRoute and returns a WsTrigger. The spec
// calls await ws.complete() ONLY AFTER clicking the Download button (which fires
// POST /downloads and upserts the queued job-1). complete() sends the
// download.complete frame so applyEvent flips job-1 to completed+libraryTrackId
// and the row becomes in-library. page.routeWebSocket is async — always await it.
export async function installWsMock(page: Page): Promise<WsTrigger> {
  let capturedWs: WebSocketRoute | null = null

  await page.routeWebSocket('**/api/v1/ws', (ws: WebSocketRoute) => {
    // Fully mocked — do not connectToServer(). Capture for later use.
    capturedWs = ws
  })

  return {
    complete: () =>
      new Promise<void>((resolve, reject) => {
        const deadline = Date.now() + 5000
        const poll = () => {
          if (capturedWs) {
            const frame = {
              type: 'download.complete',
              payload: {
                jobId: 'job-1',
                dedupKey: `isrc:${externalTrack.isrc.toLowerCase()}`,
                status: 'completed',
                progress: 100,
                source: externalTrack.source,
                externalId: externalTrack.externalId,
                libraryTrackId,
              },
            }
            capturedWs.send(JSON.stringify(frame))
            resolve()
          } else if (Date.now() > deadline) {
            reject(new Error('installWsMock: WebSocket never opened within 5 s'))
          } else {
            setTimeout(poll, 20)
          }
        }
        poll()
      }),
  }
}
```

- [ ] **Step 5: Create the core-loop spec**

Create `web/e2e/core-loop.spec.ts`:

```ts
import { test, expect } from '@playwright/test'
import { installApiMocks, installWsMock, externalTrack } from './mocks'

test('core loop: login -> search everywhere -> download -> in-library -> play', async ({ page }) => {
  const authed = { value: false }
  // Install HTTP mocks first (GET /downloads returns [] so no pre-existing job).
  await installApiMocks(page, authed)
  // Install WS mock and get the trigger object; does NOT send any frame yet.
  const ws = await installWsMock(page)

  // 1) Load: setup not required, not authed -> Login screen.
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Log in to Reverb' })).toBeVisible()

  // 2) Log in. The app reloads on success; /me now returns authed.
  await page.getByPlaceholder('Admin password').fill('correct horse')
  await page.getByRole('button', { name: 'Log in' }).click()

  // 3) After reload we land on /search (default route). Switch to Everywhere.
  await expect(page.getByRole('button', { name: 'Everywhere' })).toBeVisible()
  await page.getByRole('button', { name: 'Everywhere' }).click()

  // 4) Search; the SSE mock returns one not-in-library track.
  await page.getByPlaceholder('Search your library…').fill(externalTrack.title)
  await expect(page.getByText(externalTrack.title)).toBeVisible()

  // The Download button is present (row is NOT in library — GET /downloads was []).
  const downloadBtn = page.getByRole('button', { name: `Download ${externalTrack.title}` })
  await expect(downloadBtn).toBeVisible()

  // 5) Click Download -> POST /downloads -> queued job-1 upserted into the store.
  //    NOW send the WS completion frame to flip job-1 to completed+libraryTrackId.
  await downloadBtn.click()
  await ws.complete()

  // The Download button disappears; the in-library ✓ row appears.
  await expect(downloadBtn).toHaveCount(0)
  await expect(page.getByTitle('In library')).toBeVisible()

  // 6) Play: clicking the in-library row plays the synthesized track. The player
  //    bar shows the title and a Pause button (playing flipped true).
  await page.getByRole('button', { name: new RegExp(externalTrack.title) }).first().click()
  await expect(page.getByTestId('player-bar').getByText(externalTrack.title)).toBeVisible()
  await expect(page.getByRole('button', { name: 'Pause' })).toBeVisible()
})
```

- [ ] **Step 6: Run the e2e suite**

Run (from `web/`): `npm run e2e`
Expected: `1 passed` — the `core-loop` test is green. Playwright auto-builds (`npm run build`) and serves `vite preview` on :4173 via `webServer`, then runs the spec against the built SPA with all routes mocked.
If the play step is flaky because `audio.play()` rejects before `playing` flips: the player store sets `playing = true` synchronously in `loadCurrent`/`play` BEFORE awaiting the play() promise, so the Pause button should appear regardless. If a specific environment still flakes, the deterministic assertion is `page.getByTestId('player-bar').getByText(externalTrack.title)` (the now-playing title), which does not depend on the audio promise — keep both asserts.

- [ ] **Step 7: Type-check the new TS via a dedicated e2e tsconfig (strict gate)**

The e2e files (`web/e2e/*.ts` and `web/playwright.config.ts`) are outside every existing tsconfig project's `include` (`tsconfig.app.json` covers `src/`; `tsconfig.node.json` covers `vite.config.ts`), so a bare `npx tsc --noEmit` does NOT validate them. Add a dedicated config:

Create `web/tsconfig.e2e.json`:

```json
{
  "extends": "./tsconfig.app.json",
  "include": ["e2e", "playwright.config.ts"],
  "compilerOptions": {
    "noEmit": true,
    "types": ["node"]
  }
}
```

Run (from `web/`): `npx tsc -p tsconfig.e2e.json --noEmit`
Expected: no errors. (The spec/config/mocks use `import type` for type-only imports — `Page`, `Route`, `WebSocketRoute` — and contain no enums or constructor parameter-properties, satisfying `verbatimModuleSyntax` + `erasableSyntaxOnly`.)

- [ ] **Step 8: Ignore Playwright artifacts**

Append to `web/.gitignore` (do NOT touch root `.gitignore`):

```
/test-results/
/playwright-report/
/playwright/.cache/
```

(Verify `web/.gitignore` exists first; it does. These keep generated e2e artifacts out of git.)

- [ ] **Step 9: Commit**

```bash
git add web/package.json web/package-lock.json web/playwright.config.ts web/tsconfig.e2e.json web/e2e/ web/.gitignore
git commit -m "test(e2e): hermetic Playwright core-loop (login->search->download->in-library->play)"
```

---

## Task 6: README (legal/ethical framing) + LICENSE + deployment docs

**Files:**
- Create: `README.md`
- Create: `LICENSE`
- Create: `docs/deployment.md`

**Interfaces:**
- Consumes: the config surface (env + flags), the quick-start (docker compose from Task 4), the design spec at `docs/superpowers/specs/2026-06-20-reverb-mvp-design.md`, the spotDL pin note.
- Produces: human-facing docs. Verification is a section-presence checklist (no automated test).

- [ ] **Step 1: Create the LICENSE file (AGPL-3.0)**

Run (from repo root): `curl -fsSL https://www.gnu.org/licenses/agpl-3.0.txt -o LICENSE`
Expected: `LICENSE` exists and its first line is `                    GNU AFFERO GENERAL PUBLIC LICENSE`.
Verify: `head -n 1 LICENSE` shows the AGPL title.
If there is no network in the executor environment: create `LICENSE` containing the full AGPL-3.0 text from a local copy (the canonical text is at https://www.gnu.org/licenses/agpl-3.0.txt). Do NOT ship a stub — the full text is required.

- [ ] **Step 2: Create `docs/deployment.md`**

Create `docs/deployment.md`:

```markdown
# Deploying Reverb

Reverb ships as a single Docker image: a static Go binary with the web UI
embedded, plus Python 3, ffmpeg, and a pinned spotDL. This guide covers a
production-ish single-host deployment.

## Quick start

```bash
cp .env.example .env      # fill in secrets
docker compose up -d      # builds + starts Reverb on :8090
```

Open http://localhost:8090 and complete the first-run wizard (set an admin
password unless you provided `REVERB_ADMIN_PASSWORD` in `.env`), then add your
adapters in Settings:

- **Library** (Subsonic/Navidrome): point it at your existing server.
- **Search** (Spotify): set the Client ID in Settings; the Client Secret comes
  from `REVERB_SPOTIFY_CLIENT_SECRET` in `.env`.
- **Downloader** (spotDL): set `output_dir` to `/music`.

## The shared music volume

Reverb's spotDL downloader writes into `/music`. For downloads to appear in your
library, your Subsonic/Navidrome server MUST scan the SAME directory. The
provided `docker-compose.yml` mounts `./music:/music` into Reverb and (in the
commented Navidrome service) `./music:/music:ro` into Navidrome. After a
download completes, Reverb triggers a library scan and the track becomes
playable.

## Reverse proxy + TLS

Run Reverb behind a TLS-terminating reverse proxy. Reverb serves plain HTTP on
8090 and uses a same-origin session cookie + a WebSocket at `/api/v1/ws`, so the
proxy MUST forward Upgrade/Connection headers.

### Caddy (simplest)

```
music.example.com {
    reverse_proxy localhost:8090
}
```

Caddy obtains/renews certificates automatically and proxies WebSockets out of
the box.

### nginx

```nginx
server {
    listen 443 ssl;
    server_name music.example.com;
    ssl_certificate     /etc/letsencrypt/live/music.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/music.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8090;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

## Volumes & backups

- `./data` → `/data` holds the SQLite database (`/data/reverb.db`, set via
  `REVERB_DB`) plus app state. This is the only stateful Reverb volume.
- `./music` → `/music` holds downloaded audio (shared with the library server).

**Backup:** stop the container (or use SQLite's online backup) and copy
`./data/reverb.db`. A simple cold backup:

```bash
docker compose stop reverb
cp ./data/reverb.db ./backups/reverb-$(date +%F).db
docker compose start reverb
```

## Upgrades

```bash
git pull                  # or: docker compose pull (if using a published image)
docker compose build      # rebuild from the new source
docker compose up -d      # recreate the container
```

Reverb runs SQLite migrations automatically on startup. Back up `./data/reverb.db`
before a major upgrade.

## spotDL version pin

The image pins `spotdl==4.2.11`. spotDL's stdout formatting is fragile and
Reverb parses download progress with the regex `(\d{1,3})\s*%`
(`internal/download/spotdl/adapter.go`). **Bumping the spotDL pin requires
re-validating that regex against the new output format** before shipping —
otherwise progress may silently degrade to "indeterminate".
```

- [ ] **Step 3: Create `README.md`**

Create `README.md`:

```markdown
# Reverb

**Reverb** is a self-hosted music app that unifies your existing music library, the
broader catalog you can search online, and one-click downloading — in a single
fast web UI. It is a Go single-binary modular monolith with an embedded
React/TypeScript SPA.

> Reverb is for personal use with music you have the legal right to download. See
> [Legal & ethical use](#legal--ethical-use).

## The core loop

1. **Search everywhere** — one search box spans your library and online sources
   (e.g. Spotify) at once, streaming results as each source responds.
2. **See what you already have** — results are matched against your library
   (by ISRC/metadata), so you instantly know what is missing.
3. **One-click download** — missing tracks download via spotDL into your music
   folder; live progress streams over a WebSocket.
4. **It just appears** — when the download finishes, Reverb rescans your library
   and the track flips to in-library — ready to play, in the same row.

## Features

- Unified library browsing (artists / albums / playlists) backed by a
  Subsonic/Navidrome server.
- Gapless-feeling web player with queue, shuffle, repeat, seek, and keyboard
  shortcuts.
- "Search Everywhere" with live per-source streaming (SSE) and library matching.
- One-click spotDL downloads with live progress and auto play-when-ready.
- Pluggable adapters (library / search / downloader) configured in-app, with a
  first-run setup wizard.
- Single static binary, SPA embedded; ships as one Docker image (Python3 +
  ffmpeg + pinned spotDL included).
- Responsive UI (desktop + mobile).

## Screenshots

<!-- TODO: add screenshots of Search Everywhere, a download in progress, and the player. -->
_Screenshots coming soon._

## Quick start (Docker Compose)

```bash
git clone https://github.com/maxjb-xyz/reverb.git
cd reverb
cp .env.example .env        # fill in secrets (see Configuration)
docker compose up -d        # builds + runs on http://localhost:8090
```

Open http://localhost:8090 and complete the **first-run wizard**: set an admin
password (unless you set `REVERB_ADMIN_PASSWORD` in `.env`), then add your
library, search, and downloader adapters in Settings. Point the spotDL
downloader's `output_dir` at `/music` so downloads land where your library
scans. Full details: [docs/deployment.md](docs/deployment.md).

## Configuration reference

Reverb is configured by flags, environment variables, and the in-app Settings UI.
**Precedence: flags > environment > defaults.**

### Flags

| Flag | Default | Description |
| --- | --- | --- |
| `--port` | `8090` | HTTP listen port |
| `--db` | `./data/reverb.db` | SQLite database path |
| `--dev` | `false` | Dev mode (proxies the Vite dev server) |
| `--log-level` | `info` | Log level |

### Environment variables

| Variable | Description |
| --- | --- |
| `REVERB_PORT` | HTTP listen port (same as `--port`) |
| `REVERB_DB` | SQLite path (same as `--db`); the Docker image defaults this to `/data/reverb.db` |
| `REVERB_DEV=1` | Enable dev mode |
| `REVERB_ADMIN_PASSWORD` | Seed the admin password on first run (if setup not yet complete) |
| `REVERB_AUTH_DISABLED=1` (or `true`) | Disable auth entirely — **trusted LAN only**, all routes become unauthenticated |
| `REVERB_SPOTIFY_CLIENT_SECRET` | Spotify search adapter Client Secret (overrides stored config) |
| `REVERB_LIBRARY_PASSWORD` | Subsonic/Navidrome library adapter password (overrides stored config) |

Secrets (`REVERB_*_SECRET`, `REVERB_*_PASSWORD`, `REVERB_ADMIN_PASSWORD`) should be
provided via environment / `.env` only — never committed. `.env` is gitignored;
`.env.example` is the committed template.

### First-run wizard

On first launch Reverb detects that no admin password is set and shows a setup
screen. Set a password (or pre-seed `REVERB_ADMIN_PASSWORD`), then configure
adapters in Settings. Adapter config changes that require a restart surface a
"Restart to apply" banner.

## Legal & ethical use

Reverb is a tool for **personal use with content you have the legal right to
access and download**. By using Reverb you agree that:

- You are responsible for complying with the laws of your jurisdiction and the
  **terms of service** of every service you connect Reverb to (your music server,
  Spotify, etc.). Reverb does not grant any rights to content.
- **spotDL is a separate, third-party tool** that Reverb invokes. Reverb does not
  host, distribute, or provide any copyrighted content; it orchestrates tools you
  configure. How you use spotDL is your responsibility.
- Reverb is intended for downloading music **you own or are otherwise legally
  entitled to** (e.g. content you have purchased or that is freely licensed). Do
  not use Reverb to infringe copyright.
- Reverb is provided **"as is", without warranty of any kind**. The authors are
  not liable for misuse. See the [LICENSE](LICENSE).

## Architecture overview

Reverb is a **modular monolith**: a single Go binary organized around clean
**adapter seams** — `library` (Subsonic/Navidrome), `search` (Spotify), and
`downloader` (spotDL) — each registered explicitly at the composition root (no
`init()` side-effects). The frontend is a React/TypeScript SPA embedded into the
binary at build time (`-tags prod`). State and events flow through an in-process
EventBus that backs both the WebSocket and the download manager. The full design
rationale is in
[docs/superpowers/specs/2026-06-20-reverb-mvp-design.md](docs/superpowers/specs/2026-06-20-reverb-mvp-design.md).
The HTTP API is documented in OpenAPI, served live at `/api/v1/openapi.yaml`.

## Development & contributing

```bash
# Backend tests (scoped — never use ./... ; web/node_modules has vendored Go)
go test ./cmd/... ./internal/...

# Frontend (from web/)
cd web && npm install && npm run test

# Run locally (two shells)
cd web && npm run dev          # shell 1: Vite dev server
go run ./cmd/reverb --dev       # shell 2: Go server proxying Vite

# End-to-end (hermetic, mocked)
cd web && npm run e2e
```

Contributions are welcome. Please open an issue to discuss substantial changes
first, keep tests green (`make test`), and follow the existing adapter/seam
patterns. New adapters should register at the composition root and ship with
tests.

## License

**AGPL-3.0-only** — chosen because Reverb is a network-served, self-hosted app
that bundles GPL-family tooling (spotDL); AGPL keeps modifications open for a
networked service and matches the self-hosted-media-server tradition. See
[LICENSE](LICENSE).
```

- [ ] **Step 4: Verify the required README sections exist (checklist)**

Run: `grep -nE "^#|^##" README.md`
Expected: the output includes (in order) headings for `# Reverb`, `## The core loop`, `## Features`, `## Screenshots`, `## Quick start (Docker Compose)`, `## Configuration reference`, `## Legal & ethical use`, `## Architecture overview`, `## Development & contributing`, `## License`.
Run: `grep -c "REVERB_" README.md`
Expected: ≥ 6 (every documented env var present).

- [ ] **Step 5: Verify deployment doc sections**

Run: `grep -nE "^#|^##" docs/deployment.md`
Expected: includes `## Reverse proxy + TLS`, `## Volumes & backups`, `## Upgrades`, `## spotDL version pin`.

- [ ] **Step 6: Commit**

```bash
git add README.md LICENSE docs/deployment.md
git commit -m "docs: README (legal/ethical framing + config + arch), AGPL-3.0 LICENSE, deployment guide"
```

---

## Task 7: CI extension — add `docker` + `e2e` jobs

**Files:**
- Modify: `.github/workflows/ci.yml` (keep `backend` + `frontend`; ADD `docker` + `e2e`)

**Interfaces:**
- Consumes: the Dockerfile (Task 3), the e2e `npm run e2e` script (Task 5).
- Produces: a CI workflow that builds the image (VERSION from the git sha) and runs the hermetic e2e — without breaking the existing jobs. YAML is `actionlint`-clean.

- [ ] **Step 1: Append the `docker` and `e2e` jobs**

Replace the entire contents of `.github/workflows/ci.yml` with (existing `backend` + `frontend` preserved verbatim, two jobs added):

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
      - run: go vet ./cmd/... ./internal/...
      - run: go test ./cmd/... ./internal/... -count=1
  frontend:
    runs-on: ubuntu-latest
    defaults: { run: { working-directory: web } }
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with: { node-version: '22' }
      - run: npm ci
      - run: npx tsc --noEmit
      - run: npm run test
      - run: npm run build
  docker:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: docker/setup-buildx-action@v3
      - name: Build image
        uses: docker/build-push-action@v6
        with:
          context: .
          push: false
          load: true
          tags: reverb:ci
          build-args: |
            VERSION=${{ github.sha }}
      - name: Smoke-test the image
        run: |
          docker run -d --name reverb-ci -p 18090:8090 reverb:ci --db /tmp/ci.db
          for i in $(seq 1 20); do
            if curl -fsS http://localhost:18090/api/v1/version; then ok=1; break; fi
            sleep 1
          done
          docker logs reverb-ci
          test "${ok:-0}" = "1"
          curl -fsS http://localhost:18090/api/v1/version | grep "${GITHUB_SHA}"
          docker rm -f reverb-ci
  e2e:
    runs-on: ubuntu-latest
    defaults: { run: { working-directory: web } }
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with: { node-version: '22' }
      - run: npm ci
      - run: npx playwright install --with-deps chromium
      - run: npm run e2e
```

- [ ] **Step 2: Lint the workflow YAML**

Run: `actionlint .github/workflows/ci.yml`
Expected: no output (clean). If `actionlint` is not installed:
Run: `go run github.com/rhysd/actionlint/cmd/actionlint@latest .github/workflows/ci.yml`
Expected: no output (clean). As a fallback YAML well-formedness check:
Run: `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml')); print('yaml ok')"`
Expected: `yaml ok`.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: add docker (build+smoke) and e2e (playwright) jobs"
```

---

## Task 8: Release automation (prepared, NOT triggered)

**Files:**
- Create: `.github/workflows/release.yml`

**Interfaces:**
- Consumes: the Dockerfile (Task 3), GHCR (`ghcr.io/maxjb-xyz/reverb` — intended owner is `maxjb-xyz`; the workflow uses `${{ github.repository_owner }}` so it resolves correctly for whatever account the repo lives under), `GITHUB_TOKEN`.
- Produces: a release-published workflow that builds + pushes the image to GHCR (tagged with the version + `latest`) and fires on a published GitHub Release.

> **HUMAN ACTION — DEFERRED:** Actually publishing the `v0.1.0` GitHub Release
> is an OUTWARD-FACING action that requires the user's explicit go-ahead. The
> executor commits this workflow but MUST NOT create/push any `v*` tag and MUST
> NOT publish any GitHub Release. The workflow only runs when the user later
> publishes a GitHub Release named `v0.1.0` (or any `v*` name) through the
> GitHub UI or API.

- [ ] **Step 1: Create the release workflow**

Create `.github/workflows/release.yml`:

```yaml
name: Release
# Triggers when a GitHub Release is published (via the GitHub UI or API).
# To fire this for the first time: publish a GitHub Release named "v0.1.0".
# Intended GHCR owner: maxjb-xyz (resolved at runtime via github.repository_owner).
on:
  release:
    types: [published]
permissions:
  contents: write
  packages: write
jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Derive version from release tag
        id: ver
        run: |
          TAG=${{ github.event.release.tag_name }}
          echo "version=${TAG#v}" >> "$GITHUB_OUTPUT"
      - uses: docker/setup-buildx-action@v3
      - name: Log in to GHCR
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      - name: Build and push image
        uses: docker/build-push-action@v6
        with:
          context: .
          push: true
          tags: |
            ghcr.io/${{ github.repository_owner }}/reverb:${{ steps.ver.outputs.version }}
            ghcr.io/${{ github.repository_owner }}/reverb:latest
          build-args: |
            VERSION=${{ steps.ver.outputs.version }}
      - name: Attach notes to GitHub Release
        uses: softprops/action-gh-release@v2
        with:
          generate_release_notes: true
```

- [ ] **Step 2: Lint the release workflow YAML**

Run: `actionlint .github/workflows/release.yml`
Expected: no output (clean). Fallbacks (same as Task 7 Step 2): `go run github.com/rhysd/actionlint/cmd/actionlint@latest .github/workflows/release.yml` (clean), or `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/release.yml')); print('yaml ok')"` → `yaml ok`.

- [ ] **Step 3: Confirm no tag is created (do NOT trigger)**

Run: `git tag --list 'v*'`
Expected: empty output — the executor has created NO `v*` tag. (Pushing `v0.1.0` is deferred to the user.)

- [ ] **Step 4: Commit (the workflow only; no tag)**

```bash
git add .github/workflows/release.yml
git commit -m "ci: prepared GHCR release workflow (release-published trigger; not yet triggered)"
```

---

## Task 9: Final ship smoke + Definition of Done

**Files:** none created/modified — this task runs verification commands across the whole milestone.

**Interfaces:**
- Consumes: everything from Tasks 1–8.
- Produces: a green end-to-end signal and the M5 Definition of Done.

- [ ] **Step 1: Prod build with VERSION stamps and logs the version**

Run: `make build VERSION=0.1.0`
Expected: web build + `CGO_ENABLED=0 ... -ldflags "-X main.version=0.1.0"` succeed; `./reverb` exists.
Run: `go build -tags prod -ldflags "-X main.version=0.1.0" -o /tmp/reverb-ship ./cmd/reverb && /tmp/reverb-ship --port 0 --db /tmp/reverb-ship.db & pid=$!; sleep 1; kill "$pid" 2>/dev/null; true`
Expected: startup logs include `reverb 0.1.0 starting`. (PID capture avoids relying on `%1` job control in non-interactive shells.)

- [ ] **Step 2: Full test suite green (Go scoped + FE)**

Run: `make test`
Expected: `go test ./cmd/... ./internal/...` → all `ok` (≥ 208 Go tests incl. the new version + openapi assertions); `cd web && npm run test` → all FE tests pass (≥ 150).

- [ ] **Step 3: Docker build succeeds**

Run: `docker build -t reverb:test --build-arg VERSION=0.1.0 .`
Expected: build succeeds. (If Docker is unavailable on the executor, the CI `docker` job from Task 7 is the authoritative check — note that explicitly.)

- [ ] **Step 4: Compose parses clean**

Run: `docker compose config -q`
Expected: exit 0, no output. (If Docker CLI unavailable, defer to CI / note it.)

- [ ] **Step 5: e2e green**

Run (from `web/`): `npm run e2e`
Expected: `1 passed` (the core-loop spec).

- [ ] **Step 6: CI YAML lints**

Run: `actionlint .github/workflows/ci.yml .github/workflows/release.yml`
Expected: no output (clean). (Use the `go run github.com/rhysd/actionlint/cmd/actionlint@latest …` fallback if the binary is absent.)

- [ ] **Step 7: Confirm no release was triggered**

Run: `git tag --list 'v*'`
Expected: empty. The release is the user's call.

- [ ] **Step 8: Record the Definition of Done**

M5 is DONE when ALL of the following hold:

- The build is version-stamped: `make build VERSION=x` produces a binary that logs `reverb x starting` and serves `GET /api/v1/version` → `{"version":"x"}`.
- A single multi-stage Docker image builds (`docker build`), runs as non-root, embeds the SPA, and bundles Python3 + ffmpeg + `spotdl==4.2.11`; `docker run … reverb:test` serves `/api/v1/version`.
- `docker-compose.yml` + `.env.example` exist; `docker compose config -q` is clean; secrets come only from `.env` (gitignored).
- `internal/api/openapi.yaml` documents the real `/api/v1` surface and is served at `/api/v1/openapi.yaml` (200, `application/yaml`, contains `openapi: 3.0.3`).
- A hermetic, fully-mocked Playwright `core-loop` spec passes via `npm run e2e` (login → search everywhere → download → flip to in-library → play).
- `README.md` exists with the required sections (incl. **Legal & ethical use**), `LICENSE` is full AGPL-3.0, and `docs/deployment.md` covers reverse-proxy/TLS, volumes/backups, upgrades, and the spotDL pin.
- CI (`.github/workflows/ci.yml`) keeps `backend` + `frontend` and adds `docker` + `e2e`; the release workflow (`.github/workflows/release.yml`) is committed but NOT triggered (no GitHub Release has been published).
- No M0–M4 regressions: `make test` is fully green.
- **Deferred to the user:** publishing a GitHub Release named `v0.1.0` to trigger the GHCR publish (pushes `ghcr.io/maxjb-xyz/reverb:0.1.0` and `:latest`).

- [ ] **Step 9: Commit (if any final touch-ups were needed)**

```bash
git add -A
git commit -m "chore(m5): final ship smoke — version-stamped build, docker, e2e, docs all green"
```

(If nothing changed in this task, skip the commit.)

---

## Self-Review

**1. Spec coverage:** Each of the 9 required M5 deliverables maps to a task —
(1) version stamping → Task 1; (2) Dockerfile + .dockerignore → Task 3;
(3) production compose + .env.example → Task 4; (4) OpenAPI expand + serve →
Task 2 (serving already existed; this task expands the YAML + tightens the test);
(5) Playwright e2e → Task 5; (6) README + legal/ethical + LICENSE + deployment
docs → Task 6; (7) CI extension → Task 7; (8) release automation (prepared, not
triggered) → Task 8; (9) final ship smoke + DoD → Task 9. All Global Constraints
appear in the dedicated section and are reflected per task (non-root, CGO_ENABLED=0,
-tags prod, scoped Go tests, secrets via env only, spotDL pin note, hermetic e2e,
TS strict).

**2. Placeholder scan:** No "TBD"/"add appropriate X"/"implement later" in code
or content steps. The only literal `TODO`/`coming soon` are intentional README
content (the screenshots placeholder is explicitly required by the spec as a
"screenshots placeholder"). Every code/file step contains complete content.

**3. Type consistency:** `api.Deps.Version` is defined in Task 1 (server.go) and
consumed by `handleVersion` (Task 1) and asserted in `version_test.go` (Task 1);
`main.version` is defined in `cmd/reverb/version.go` (Task 1) and referenced by
both `main.go` and the Makefile/Dockerfile `-X main.version` (Tasks 1, 3). The
e2e mock shapes (`SearchEnvelope`, `ExternalResult`, `DownloadJob`,
`download.complete` frame `{type,payload}`) match `web/src/lib/types.ts` and the
`useRealtime`/`downloadStore`/`ExternalRow` consumers verified during planning.
The OpenAPI `/version` path matches the actual route registered in `server.go`.

**Resolved ambiguities (named assumptions):**
- **ASSUMPTION (e2e flip mechanism):** The deterministic in-library flip is driven
  by a mocked WebSocket `download.complete` frame (via `page.routeWebSocket`)
  carrying `payload.libraryTrackId`, because `ExternalRow` treats a completed job
  with a `libraryTrackId` as in-library. This avoids depending on a re-fetch race.
- **ASSUMPTION (OpenAPI serving pre-exists):** `internal/api/openapi.go` already
  embeds + serves the spec and a test already exists; Task 4 of the spec is
  therefore scoped to expanding the YAML and strengthening the test, not adding a
  handler (re-adding one would conflict). Stated explicitly in Task 2.
- **ASSUMPTION (vite preview port):** `vite preview` serves on `:4173`; the
  Playwright `webServer.url`/`baseURL` use `:4173` with `--strictPort`.

**Hard-to-make-TDD note (for controller pre-flight):** Tasks 3, 4, 7, 8 (Docker /
compose / CI YAML / release) are NOT TDD-shaped — their "test" is a build/parse/lint
invocation (`docker build`, `docker compose config -q`, `actionlint`), not a
red→green unit cycle, and several require a Docker daemon that may be absent in the
executor sandbox (each such step includes an explicit "defer to CI" fallback).
Tasks 1, 2, 5 are genuinely TDD (failing test first). The controller should
pre-flight: (a) Docker availability in the executor (else the `docker`/compose
verification steps run only in CI), (b) network access for `curl …agpl-3.0.txt`
(Task 6 Step 1) and `npx playwright install` (Task 5 Step 2), and (c) that
`@playwright/test@^1.49.0` supports `page.routeWebSocket` (it does; added in 1.48).
