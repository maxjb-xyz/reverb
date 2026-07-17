# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Reverb is a self-hosted music app: a Go single-binary modular monolith with an
embedded React/TypeScript SPA. It unifies an existing music library (Subsonic/
Navidrome), online search (Deezer/Spotify), and one-click downloading (spotDL)
in one web UI. License is AGPL-3.0-only.

## Commands

```bash
# Backend tests — NEVER use ./... ; web/node_modules contains vendored Go
go test ./cmd/... ./internal/...

# Frontend (from web/)
cd web && npm install
npm run test          # vitest unit/component tests
npm run e2e            # Playwright, hermetic/mocked
npm run lint            # eslint

# make targets
make gen     # regenerate sqlc code (queries -> Go) into internal/store/db
make web     # build the SPA, copy web/dist -> internal/api/dist
make build   # web + production binary (-tags prod) -> ./reverb
make test    # backend tests + frontend unit tests
make clean   # remove build artifacts

# Run locally (two shells, hot reload)
cd web && npm run dev          # shell 1: Vite dev server
go run ./cmd/reverb --dev       # shell 2: Go server proxying Vite, prints URL (default :8090)

# Run a single Go test
go test ./internal/download/... -run TestName -v

# Run a single frontend test file
cd web && npx vitest run src/lib/downloadApi.test.ts
```

Go 1.23+, Node 22+. gofmt-clean is required (`gofmt -w` before committing).
Conventional Commits (`feat(scope): …`, `fix(scope): …`, `test(scope): …`, etc.).
TDD is the norm — git history shows RED-phase `test(...)` commits followed by
implementation commits; keep suites green.

## Architecture

### Adapter/seam pattern (the core design)

Reverb has three pluggable seams, each with the same shape:

- **`library`** (Subsonic/Navidrome) — `internal/library/library.go` defines the
  interface; `internal/library/subsonic` and `internal/library/embedded` are
  concrete adapters; `internal/library/conformance.go` is a shared test suite
  every adapter must pass.
- **`search`** (Deezer / Spotify) — `internal/search/search.go` interface;
  `internal/search/deezer`, `internal/search/spotify` adapters;
  `internal/search/aggregator.go` fans a query out to all enabled sources
  concurrently (SSE streaming); `internal/search/conformance.go`.
- **`downloader`** (spotDL) — `internal/download/download.go` interface;
  `internal/download/spotdl`, `internal/download/lidarr` adapters;
  `internal/download/conformance.go`.

New adapters register explicitly at the composition root — **no `init()`
side-effects**. The registry (`internal/registry/registry.go`) holds
constructors by name; `internal/wiring/wiring.go` builds the active
library/search/download services from the enabled `adapter_instance` DB rows.
Wiring is invoked both at startup (`cmd/reverb`) and on live adapter
config changes via the API (no restart required) — see `cmd/reverb/reload.go`
and `internal/api` adapter mutation handlers.

Library backend has two modes (`internal/library/embedded`): **built-in**
(bundles Navidrome as a supervised child process against the same music dir)
and **external** (points at a user-provided Subsonic/Navidrome server). Only
built-in mode wires a local music dir into the subsonic adapter (enables the
waveform-peaks endpoint via local file access).

### Composition & control flow

- `cmd/reverb/main.go` / `serve.go` — composition root: opens the store, runs
  migrations, builds adapters via `wiring`, starts the HTTP server.
- `internal/core` — cross-cutting domain types and orchestration
  (`download.go`, `coverage.go`, `request.go`, `playlistsync.go`,
  `notification.go`, `external.go`) that coordinate across the seams.
- `internal/events/bus.go` — in-process EventBus; backs both the WebSocket
  (`internal/api/stream.go`) and the download manager's progress events. This
  is the primary way backend state changes reach the frontend live.
- `internal/matching` — matches search results against the library (by
  ISRC/metadata) to determine what's already owned.
- `internal/resolver` — resolves adapter/track identity across sources.
- `internal/store` — SQLite (via `modernc.org/sqlite`), migrations in
  `internal/store/migrations/*.sql` (goose), generated query code in
  `internal/store/db` (via sqlc — run `make gen` after editing
  `internal/store/queries`).
- `internal/api` — HTTP handlers (chi router). API is documented in OpenAPI,
  served live at `/api/v1/openapi.yaml` — keep it in sync with handler
  changes. `internal/api/embed.go` embeds the built SPA (`-tags prod`).
- `internal/auth` — session/cookie auth; `internal/registry` roles system
  files live alongside in `internal/api/roles.go`, `users.go`.

### Frontend (`web/`)

React 19 + TypeScript, Vite, TanStack Query, Zustand, Tailwind, react-router.
- `web/src/routes/` — one file per page (Home, Library, Search, Album, Artist,
  Downloads, Requests, Settings, Admin, Stats, ...), each with a co-located
  `*.test.tsx`.
- `web/src/lib/` — API clients (`*Api.ts`, thin fetch wrappers per backend
  resource), Zustand stores (`*Store.ts`: player, download, coverage, auth,
  library revision, everywhere/search, now-playing, pending-play), and
  cross-cutting utilities (`audioEngine.ts`, `mediaSession.ts`,
  `playTracker.ts`, `realtime.ts` for the WebSocket connection,
  `paletteService.ts`/`paletteWorker.ts` for cover-art color extraction).
- `web/src/components/` — shared UI components.
- Dev mode: Go proxies Vite (`--dev` flag) for hot reload; production build
  embeds the SPA into the Go binary via `-tags prod`.

### Configuration

Flags > environment variables > defaults. See README.md for the full flag/env
reference (`--port`/`REVERB_PORT`, `--db`/`REVERB_DB`, `--dev`/`REVERB_DEV`,
`REVERB_DOWNLOAD_DIR`, `REVERB_ADMIN_PASSWORD` (first-run only),
`REVERB_SPOTIFY_CLIENT_ID/SECRET`, `REVERB_LIBRARY_PASSWORD`,
`REVERB_SPOTDL_PATH`, `REVERB_NAVIDROME_BIN`). Secrets are env/`.env`-only,
never committed; `.env` is gitignored, `.env.example` is the template.

### Linting

`.golangci.yml` enables errcheck, govet, ineffassign, misspell, staticcheck,
unconvert. Deferred-`Close()` errcheck ignores are pre-configured for common
`io.Closer` types. staticcheck runs "all" minus a few disabled stylistic
checks (QF1001, QF1003, ST1000, ST1003 — the last because existing naming uses
initialisms like `CoverUrl` pervasively, not `CoverURL`).
