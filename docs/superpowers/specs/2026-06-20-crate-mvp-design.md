# Crate — MVP Design Spec

> The self-hosted music app that knows what you have, and knows how to get what you don't.

- **Status:** Approved design, pre-implementation
- **Date:** 2026-06-20
- **Scope:** Phase 1 MVP (the first of several spec → plan → build cycles)
- **Product name:** **Crate** (formerly working-titled "Reverb"; renamed to avoid collision with Laravel Reverb / Reverb.com). The repository directory and `reverb-plan.md` are left unrenamed for now — an optional follow-up.

---

## 1. Overview & Scope

Crate unifies the self-hosted music ecosystem (Navidrome for serving, spotDL/Lidarr for acquiring) into one experience. The core loop:

1. Search for any song/album/artist — from your library or anywhere.
2. If you have it, play it. If you don't, download it — in one click.
3. It appears in your library and starts playing.

### Locked decisions (from brainstorming)

| Decision | Choice |
|---|---|
| Audience | OSS for the community → clean adapter abstractions, config UX, docs, broad compatibility |
| Backend | **Go**, single static binary |
| Frontend | **React + TypeScript + Vite + Tailwind** |
| Database | **SQLite** (sqlc-typed queries, goose migrations) |
| Streaming | HTTP **Range** requests, **proxied** through Crate |
| Library source (MVP) | **Subsonic/Navidrome**, behind an abstracted adapter |
| Search source (MVP) | **Spotify** |
| Downloader (MVP) | **spotDL** (shell-exec wrapper) |
| Auth | **Single-user**, bcrypt admin password, with an explicit opt-out |
| Real-time | **WebSocket** (events) + **SSE** (streaming search) |
| Architecture | Modular monolith, single binary, **embedded SPA** |

### MVP includes (Phase 1)

- Spotify-like music player against Navidrome (playback, queue, shuffle/repeat).
- Unified search with **My Library** and **Everywhere** modes; results distinguished by availability.
- One-click download via spotDL → queue → progress → auto-appear in library → optional auto-play.
- Config/settings UI + first-run setup wizard (no file editing required).

### Explicitly deferred (later cycles)

- Phase 2: artist discography pages, full download-queue panel + retry UI, playlist sync, Lidarr, smart library indicators.
- Phase 3+: standalone mode (folder-scan library), mobile apps, multi-user/social, plugin marketplace, encryption-at-rest, GitOps/declarative config, true waveform peaks.

The architecture below is built so every deferred item plugs into an existing seam rather than forcing a rewrite.

---

## 2. Architecture & Extension Seams

A single Go binary serves a versioned REST API, a WebSocket endpoint, an SSE search endpoint, and the embedded React SPA on one port.

```
┌─ Single Go binary (image: crate/crate) ────────────────────────┐
│  api/  ── REST (/api/v1/*) + WS (/api/v1/ws) + SSE (search)     │
│   │      API-first & versioned → future mobile reuses it         │
│   ▼                                                             │
│  ┌────────────────┐ ┌───────────────┐ ┌────────────────────┐   │
│  │ LibraryRegistry│ │ SearchRegistry│ │ DownloaderRegistry │   │  seams 1–3
│  │  [subsonic]    │ │  [spotify]    │ │  [spotdl]          │   │
│  └───────┬────────┘ └──────┬────────┘ └─────────┬──────────┘   │
│          └──── MatchingService (external⇄library) ───┘          │  reused by P2
│  ┌──────────────┐  ┌──────────┐  ┌───────────────────────────┐ │
│  │  EventBus    │  │  store/  │  │ config (DB-canonical +     │ │  seams 4–5
│  │ (pub/sub→WS) │  │ (SQLite) │  │ env secrets + wizard)      │ │
│  └──────────────┘  └──────────┘  └───────────────────────────┘ │
│  web/  ── embedded React SPA (embed.FS) / --dev proxies Vite    │
└─────────────────────────────────────────────────────────────────┘
```

### The five extension seams

1. **LibraryRegistry** — `LibraryAdapter`s. MVP: Subsonic. Future: folder-scan/standalone (P3).
2. **SearchRegistry** — `SearchSource`s, fan-out + merge. MVP: Spotify. Future: Deezer/MusicBrainz/Tidal (P2+).
3. **DownloaderRegistry** — `Downloader`s with capability probes + **ordered fallback chain**. MVP: spotDL. Future: Lidarr (P2).
4. **EventBus** — typed in-memory pub/sub backing the WebSocket. MVP: download + library events. Future: scan progress, now-playing sync, multi-user.
5. **Config** — DB-canonical, with **self-describing adapter config schemas** so the settings UI auto-renders forms + test-connection generically (this powers every new adapter and the eventual marketplace).

### Cross-cutting extensibility guarantees

- **API-first, versioned (`/api/v1`):** the SPA is just the first client; native mobile (P3) regenerates from the same OpenAPI spec. No business logic in the frontend.
- **Self-describing adapters:** each adapter advertises `Type`, `Name`, `ConfigSchema()`, `TestConnection()`, and behavioral **capabilities via optional interfaces** (below). The UI adapts to capabilities instead of hardcoding.
- **Registry is transport-agnostic & future-proof:** registration is allowed at **runtime**, not only `init()`. Interfaces are process-boundary-friendly (context-based, serializable data in/out, **no Go-func callbacks** — the `Downloader` uses `Status()` + EventBus, not `onComplete(handler)`). A P3 marketplace plugin becomes a `Plugin` impl proxying over a local socket/gRPC, registered at startup; the registry never knows the difference. Finicky `.so` Go plugins are explicitly rejected.

---

## 3. Backend Package Structure & Interfaces

```
cmd/crate/main.go             # wire dependencies, start server
internal/
  config/        # flags/env bootstrap + DB-stored settings + env secret injection
  store/         # SQLite (sqlc-generated), goose migrations
  events/        # typed EventBus (pub/sub) + WS hub
  library/       # LibraryAdapter interface + registry + conformance suite
    subsonic/
  search/        # SearchSource interface + registry + fan-out aggregator
    spotify/
  download/      # Downloader interface + registry + Manager (queue/workers/dedup/fallback/debounce)
    spotdl/
  matching/      # MatchingService + Normalize() (shared with dedup) + fixture corpus
  api/           # REST handlers (/api/v1), SSE + WS endpoints, DTOs, OpenAPI
  auth/          # middleware, sessions, login, setup
  core/          # shared domain types (Track, Album, Artist, ...)
web/             # React app; build output embedded via embed.FS
```

### Plugin base + adapter interfaces

```go
// Shared by ALL adapters — powers the registry, settings UI, and future marketplace.
type Plugin interface {
    Type() string                 // "library" | "search" | "downloader"
    Name() string                 // "subsonic", "spotify", "spotdl", ...
    ConfigSchema() ConfigSchema   // declared fields → UI auto-renders the form
    Init(cfg map[string]any) error
    TestConnection(ctx context.Context) error
}

type LibraryAdapter interface {
    Plugin
    Search(ctx context.Context, q string, types []EntityType) (SearchResults, error)
    GetArtist(ctx context.Context, id string) (Artist, error)
    GetAlbum(ctx context.Context, id string) (Album, error)
    GetPlaylists(ctx context.Context) ([]Playlist, error)
    Stream(ctx context.Context, trackID string, opts StreamOpts) (StreamHandle, error) // Range-aware
    CoverArt(ctx context.Context, id string, size int) (io.ReadCloser, string, error)
    StartScan(ctx context.Context) error
    ScanStatus(ctx context.Context) (ScanStatus, error)
}

type SearchSource interface {
    Plugin
    Search(ctx context.Context, q string, t EntityType) ([]ExternalResult, error)
    GetAlbum(ctx context.Context, externalID string) (ExternalAlbum, error)
}

type Downloader interface {
    Plugin
    CanDownload(ctx context.Context, item DownloadRequest) (bool, error) // cheap heuristic in search path
    Enqueue(ctx context.Context, item DownloadRequest) (DownloadJob, error)
    Status(ctx context.Context, jobID string) (DownloadJob, error)
    Cancel(ctx context.Context, jobID string) error
}
```

### Capabilities = optional interfaces (compile-time safe) + a runtime descriptor

Behavioral capabilities are **optional interfaces**, not boolean structs (idiomatic Go, compile-time contracts, no ever-growing struct):

```go
type DiscographyProvider     interface { GetArtistDiscography(ctx context.Context, externalID string) ([]ExternalAlbum, error) } // P2 artist pages
type QualityProfileDownloader interface { ListQualityProfiles(ctx context.Context) ([]QualityProfile, error) }                   // Lidarr (P2)
type MonitoringDownloader     interface { Monitor(ctx context.Context, artistID string) error }                                  // Lidarr (P2)

// usage: if d, ok := source.(DiscographyProvider); ok { ... }
```

The settings UI needs capabilities at **runtime**, so one helper bridges compile-time → runtime:

```go
func DescribeCapabilities(p Plugin) []string // runs type assertions, emits tags in the API DTO
```

**ISRC is not a capability — it is data presence.** A result either carries an ISRC or it doesn't; it lives as an optional field on `ExternalResult`/`Track` and the matcher uses it when non-empty. No `SearchCaps.ISRC` flag.

### Registry + factory

Each adapter package self-registers in `init()` (e.g. `library.Register("subsonic", factoryFn)`); `Register` is a mutex-guarded method also callable at runtime. The set of `adapter_instances` rows decides which registered adapters are instantiated and with what config. **Adding an adapter = new package + one `Register` call + implement the interface(s). Zero core changes** — identical for first-party and community adapters.

### Download Manager responsibilities

The `download.Manager` owns: the queue, a worker pool (size = setting), **dedup** (Q#3 — in-flight requests with the same normalized key join the existing job), the **fallback chain** (Q#4 — iterate downloaders by `priority` calling `CanDownload` until one accepts; configurable), **scan debounce** (coalesce completions over a ~5s window into one `StartScan`; uses an injectable clock), and **cancel/retry** mechanics. It publishes typed EventBus events.

---

## 4. Data Model & Config

**Principle:** the library (artists/albums/tracks/playlists) is **never duplicated** in SQLite — it lives in the adapter (Subsonic) and is the source of truth. The DB stores only Crate's own state, so standalone mode (P3) swaps the adapter, not the schema.

### Tables (SQLite)

| Table | Purpose | Key columns | Forward-compat |
|---|---|---|---|
| `schema_migrations` | migration tracking | version | — |
| `adapter_instances` | every configured integration is a row; the registry instantiates from these | id, type, name, enabled, `priority`, config_json | `priority` drives downloader fallback + search fan-out order; new integration = new row |
| `settings` | app prefs (k/v, JSON values) | key, value_json | `library_version`, `accent_color`, `auth_disabled`, `download.fallback`, `download.dedup`, worker count |
| `sessions` | auth | id, token_hash, expires_at, last_seen | bearer (mobile) + cookie (web) share it |
| `download_jobs` | queue + active + done/failed (one table, status lifecycle) | id, `dedup_key`, request_json, downloader_name, status, progress, error, output_path, library_track_id, priority, `requested_by`, attempts, created/started/finished_at | powers MVP tray **and** P2 queue panel/retry; `requested_by` nullable = P3 multi-user stub |
| `match_cache` | external⇄library results incl. **negative** matches | PK(source, external_id), library_track_id (nullable), method, confidence, isrc, mbid, duration_ms, `library_version`, matched_at | read-path for P2 artist pages / playlist sync / smart indicators |

**Deliberate omissions (YAGNI):** no `users` table (single-user) — `download_jobs.requested_by` reserved nullable for P3; no library mirror table; no separate `download_history` (it's `download_jobs` filtered by status).

### `library_version` (concrete definition)

A **monotonic integer** stored in `settings` (`library_version`), incremented on each **library-scan completion**. A `match_cache` row is stale iff `row.library_version < current`; stale rows re-match on next access. Targeted re-match of a single `external_id` happens on download-complete; the counter bumps on scan completion.

### `dedup_key` (concrete definition)

`hash( Normalize(artist) + "␟" + Normalize(title) + "␟" + Normalize(album) )`, computed via the **same `matching.Normalize()`** the matcher uses (never from raw `request_json`, which varies by source formatting). One normalization function means dedup and matching can never drift.

### Config model — DB-canonical, no YAML

```
Process bootstrap → CLI flags + env   (--port/CRATE_PORT, --db/CRATE_DB, --dev, log level)   [before DB exists]
Secrets           → env, optional     (CRATE_ADMIN_PASSWORD, CRATE_SPOTIFY_CLIENT_SECRET, CRATE_LIBRARY_PASSWORD, ...)
                    read at startup, injected into the relevant adapter config just before Init()
Everything else   → DB (settings + adapter_instances), edited via wizard/UI — single source of truth
```

- **DB is the only config store.** No file to drift, no precedence rules. `settings` + `adapter_instances` *are* the config.
- **Env = secrets only**, plain `os.Getenv()` at startup, merged into the adapter's config immediately before `Init()`. Not a general override layer.
- **First-run wizard** (no bootstrap file): fresh DB → API reports `setup_required` → SPA routes to the wizard: ① set admin password (or skip → auth disabled, with a loud warning), ② add library (Subsonic), ③ add search source (Spotify), ④ add downloader (spotDL). The wizard steps and the settings page are the **same `ConfigSchema()`-driven form + `TestConnection()` components**.

**Secrets honesty:** the admin password is bcrypt-hashed (verify-only). Retrievable secrets (Spotify client_secret, Lidarr API key) must be usable to call APIs, so they live in `config_json`, protected by file perms and overridable via env. Encryption-at-rest is a documented future item, not MVP.

**Future (documented, not built):** `GET /api/v1/config/export` + `POST /api/v1/config/import` (config-as-JSON; import writes to the DB). Import semantics are **"replace all" adapter instances** — predictable for restore/seed; this must be stated explicitly in the API contract when built. This restores GitOps-style reproducibility without reintroducing a live-file precedence system.

---

## 5. Core Flows

### Flow 1 — Library playback & streaming

**Streaming = proxy, not redirect.** The SPA plays from `GET /api/v1/stream/:trackId` (authed by our session); Crate proxies to the adapter's `Stream()`, forwarding **Range** both ways and passing through `Content-Type/Length`, `Accept-Ranges`, `Content-Range`, and `206`. Keeps Subsonic credentials server-side, keeps the adapter boundary clean (standalone mode streams from disk with no client change), and provides a seam for play-count/scrobble hooks. Transcoding stays Navidrome's job for MVP (passthrough; `StreamOpts.maxBitRate/format` reserved).

**Player engine = dual `<audio>` elements, outside React.** A framework-agnostic `AudioEngine` class wraps two HTML5 `<audio>` elements (native Range/seek via our proxy) and **preloads the next queue item** for near-gapless transitions (true Web Audio buffer scheduling would require fully decoding tracks, which fights streaming). The engine owns queue/shuffle/repeat and emits events; a Zustand store mirrors them into React. A global keyboard handler binds shortcuts to engine actions.

**Waveform scoping:** MVP ships a **waveform-styled seek bar with buffered-range indication**. A real `GET /api/v1/peaks/:trackId` endpoint (precomputed peaks) is a deferred fast-follow.

### Flow 2 — "Everywhere" search + matching (SSE)

```
SPA ──GET /api/v1/search/everywhere?q=&type=── (SSE) ──▶ Aggregator
        ├─ goroutine: spotify  (ctx deadline)
        ├─ goroutine: <future> (ctx deadline)
   ◀── event {source, status: ok|timeout|error, results[], cursor}   each result pre-matched
```

The aggregator fans out to enabled `SearchSource`s by `priority`, each in its own goroutine with an **individual `context.WithTimeout`**; a slow/down source never blocks the others. Results **stream back per-source as they arrive**. Before emitting, each result passes through `MatchingService` (cache-first), attaching `match:{status, libraryTrackId, method, confidence}` and an optional `download` field (active job by `external_id`+`source`, `dedup_key` fallback).

Local **My Library** search is a single fast REST response (`GET /api/v1/library/search`), not SSE.

Pagination is per-source via `cursor`. `CanDownload` in the search path is a **cheap heuristic** (the real check is at enqueue).

### Flow 3 — Download → queue → progress → library refresh

```
click ↓ ─POST /api/v1/downloads {externalId, source, downloader?, playWhenReady?}─▶ Manager
  1. build DownloadRequest (artist/title/album/isrc/source)
  2. dedup_key (normalized) ── active job exists? ──▶ JOIN it (Q#3)
  3. pick downloader: explicit, else iterate by priority via CanDownload (fallback chain, Q#4)
  4. insert download_jobs(queued) → worker pool
  5. spotDL adapter: shell-exec via injectable runner, parse stdout → publish download.progress → WS → tray
  6. success → debounced StartScan → poll ScanStatus → re-match external_id
              → set library_track_id, bump library_version (invalidate cache)
              → publish download.complete + library.updated
  7. SPA: tray live-updates; result flips ↓→✓ via in-place patch; if playWhenReady, auto-play
```

- **spotDL adapter** shells out through an **injectable exec runner**; stdout parsing **degrades gracefully** (unparseable → progress unknown → indeterminate ring, never an error). spotDL is **version-pinned in the Docker image**, with a comment in the adapter and a deployment-docs note flagging output-format fragility.
- **Scan debounce:** completions accumulate ~5s and coalesce into one `StartScan` (debounced timer fed by the EventBus, injectable clock) — prevents N scans on bulk "Download Missing" (P2).
- **Library-refresh:** `StartScan` → poll `getScanStatus` → re-match; time-boxed polling fallback. (Navidrome webhook = future.)
- **Cancel + retry** are built into the Manager now; MVP UI exposes **cancel in the tray**; the full queue panel + retry UI is P2 (backend already ready).
- **Typed EventBus events:** `download.queued|progress|complete|failed`, `library.updated` → WebSocket → SPA.

---

## 6. MatchingService (the crown jewel)

`Match(ctx, external) → MatchResult{Status, LibraryTrackID, Method, Confidence}` via a priority chain:

```
1. ISRC exact              (when both sides carry it)
2. MusicBrainz Recording ID (when both carry it)
3. Normalized fuzzy:  artist + title  ── disambiguate by ──▶ DURATION (±2–3s) + album
```

**Normalization (`Normalize()`)** removes cosmetic noise (case, punctuation, `feat.`/`featuring`, collapsed whitespace, unicode normalization) but **must not over-strip** version qualifiers — otherwise a live cut matches the studio cut. **Duration is the tiebreaker** that separates collisions (a live version is rarely within 2–3s of the studio version). `Normalize()` is a pure function, unit-tested hard, and shared with `dedup_key`.

Results (positive and negative) are written to `match_cache`, invalidated by `library_version`.

### Fixture corpus — enumerated, authored before the M2 TDD session

Stored as `internal/matching/testdata/*.json`, each with input pairs + expected verdict:

1. `feat-in-title.json` — `Song (feat. X)` vs `Song` (and `featuring` / `ft.` variants).
2. `remaster-deluxe-suffixes.json` — `(Remaster 2011)`, `(Deluxe Edition)`, `(2009 Remastered Version)`.
3. `live-vs-studio.json` — same title/artist, live vs studio, **separated by duration**.
4. `same-title-different-artist.json` — `Creep` by Radiohead vs `Creep` by TLC (must NOT match).
5. `non-latin-scripts.json` — Cyrillic/CJK/diacritics; unicode normalization.
6. `isrc-present-vs-absent.json` — ISRC exact path vs falling through to fuzzy.
7. `short-track-duration-ambiguity.json` — short tracks where fuzzy title is ambiguous and duration is decisive.
8. `punctuation-and-whitespace.json` — `Pt. 1` vs `Part 1`, `&` vs `and`, stray punctuation/whitespace.

---

## 7. Frontend Architecture & UX

**Stack:** React + TS + Vite + Tailwind. **Server state → TanStack Query**; **client/UI state → Zustand** (player mirror, download-queue overlay, UI toggles). The imperative `AudioEngine` lives outside React and feeds Zustand.

**API contract = OpenAPI spec served by the backend → generates the TS client + types.** The web app is the first generated client; native mobile (P3) regenerates from the same spec. No Go↔TS drift.

**Three explicit transports in the client layer** (kept distinct — different reconnection semantics, no unified abstraction):

| Transport | Used for | Reconnect |
|---|---|---|
| REST (typed fetch wrapper, auth) | browse, library search, mutations, settings | n/a |
| `SearchStream` (SSE) | `/search/everywhere` per-source results | `Last-Event-ID` |
| `RealtimeConnection` (WS) | download progress, `library.updated` | backoff + resubscribe |

### App shell, layout & panels

Spotify-like frame: left sidebar nav · scrollable main with a persistent top bar · fixed bottom player.

```
┌──────────┬───────────────────────────────────┐
│ Sidebar  │  Main (routed view)               │
│  Search  │   /search /library/* /album/:id   │   ⟵ ONE right-panel slot
│  Artists │   /artist/:id /playlist/:id       │      (Play Queue OR Download Tray)
│  Albums  │   /settings /setup                │
│  Playlists                                   │
│  ─────── │                                   │
│  ⟳ Downloads (badge)                         │
├──────────┴───────────────────────────────────┤
│ Player bar: art · title · transport · waveform-seek · vol · [Queue] [Downloads] │
└───────────────────────────────────────────────┘
```

**Two right-side panels, mutually exclusive (one slot — opening one closes the other):**
1. **Play Queue** — opened by the player-bar **Queue** button: up-next list, **drag-reorder, remove, now-playing header**.
2. **Download Tray** — opened by the **sidebar ⟳ entry** *and* the player-bar **Downloads** button (same panel): active/queued/done jobs, progress, cancel.

Naming is consistent throughout: **Play Queue** vs **Download Tray** are distinct; the sidebar ⟳ is the Download Tray.

Routing (React Router): `/search`, `/library/{artists,albums}`, `/album/:id`, `/artist/:id`, `/playlists`, `/playlist/:id`, `/settings`, `/setup`. A guard redirects to `/setup` while `setup_required`, and to login when unauthenticated.

The **Download Tray** and the future P2 **queue panel** share a `DownloadJobList` (tray = compact; panel = full + retry).

### Search UX (the core interaction)

- **Search bar = persistent in the top bar**, always visible; focus/typing routes to `/search`, text preserved.
- **My Library / Everywhere = segmented pill** on the right of the bar; switching re-runs the current query (sliding-pill animation, text preserved).
- **Streaming render = append-in-stable-sections, never reflow.** Library mode is one fast REST render. Everywhere mode is SSE: fixed sections (**Tracks / Albums / Artists**); each source's results **append within its section**, deduped across sources by ISRC/normalized key. **Already-shown rows never reorder** (no layout shift / misclicks). Pending sources show skeleton rows; **per-source status chips** ("Spotify ✓ · Deezer … · 1 timed out") keep partial results honest.
- **Result row state machine (4 states):**
  - **✓ In Library** (`match.libraryTrackId` set) → play
  - **⟳ Queued/Downloading** (active job) → ↓ icon sits in a **circular progress ring** (determinate when known; **indeterminate spinner when spotDL progress is unknown**)
  - **↓ Available** (no match; a downloader's cheap `CanDownload` says yes) → download popover
  - **(no icon) external-only** (nothing can fetch it)
  - The **⟳→✓ flip is an in-place patch** from the `download.complete` event — no refetch.

### Visual identity

- **Base = Spotify-like** (simple, functional, familiar).
- **Accent = configurable, default red `#F0354B`.** Driven by a `--color-accent` CSS custom property (RGB channels) on `<html>`, with Tailwind referencing it via `rgb(var(--color-accent) / <alpha-value>)`. **This token system is built in M0** — accent is a *system* color (active nav, buttons, focus rings), deliberately not the memorable element.
- **Signature = dynamic album background (committed).** Client-side palette extraction from the already-loaded cover (e.g. `node-vibrant`/canvas sampling, cached per album) sets a **subtle ambient background gradient** that shifts with the playing track. The **player bar uses the album's dominant color as a solid fill** with **computed-contrast text** (sample luminance → pick light/dark text, add a scrim only if needed) — *not* blur-over-art. Glassmorphism survives only on overlay panels over content, never over art.

### Mobile — decided now, not retrofitted

The shell is **responsive from day one** so it's never a rewrite:
- **Desktop (≥lg):** sidebar + main + bottom player + right slide-over panels.
- **Mobile (<md):** **bottom tab nav** (including a dedicated **Search tab → `/search` full page**), a **mini player** that **expands to a fullscreen now-playing** (iOS-Music style), panels become **full-screen sheets**, tap targets ≥44px.
- **Routes are identical**; only the chrome swaps via responsive layout components + a nav abstraction. **MVP builds desktop fully (M4b adds the mobile chrome as a thin follow-on).**

### Surgical cache invalidation (no jank)

Typed events carry IDs so invalidation is precise, never global:
- `download.complete` → `{jobId, libraryTrackId, artistId, albumId, source, externalId}`
- `library.updated` → `{artistIds[], albumIds[]}`

The client maps these to **targeted TanStack Query invalidations** (`['album', albumId]`, `['artist', artistId]`) and **in-place patches** the matching search result via `externalId`, avoiding visible refetch jank.

---

## 8. Auth

A first-class middleware seam, present from day one:

- `auth` package = middleware wrapping all `/api/v1/*` except `login`, `health`, and the `setup_required` probe.
- Single `admin_password`, **bcrypt-hashed**, in the secrets store. `POST /api/v1/auth/login` → **opaque random session token** stored hashed in `sessions` (revocable). Web keeps it in an httpOnly cookie; mobile sends `Authorization: Bearer` — the same middleware checks both → mobile-ready.
- **First-run setup** if no password is set.
- **Escape hatch:** `auth_disabled` setting (chosen in the wizard, or `CRATE_AUTH_DISABLED`) for trusted-LAN users who opt out explicitly — logs a loud startup warning.

---

## 9. Testing & Dev/Test Infra

**Dev stack (`docker-compose.dev.yml`):** Navidrome seeded with a few **bundled Creative-Commons tracks** (working library out-of-the-box for every contributor), spotDL **version-pinned** in the Crate image, Spotify creds via gitignored `.env`.

**Testability seams:** injectable **exec runner** (spotDL — feed canned stdout incl. malformed), injectable **HTTP client** (Spotify/Subsonic — record/replay cassettes via go-vcr, no live creds in CI), injectable **clock** (scan debounce), assertable in-memory **EventBus**.

**Test layers:**

| Layer | Tool | Targets |
|---|---|---|
| Unit (TDD-first) | Go test | **`matching`** (the enumerated fixture corpus), `download.Manager` (dedup join, fallback, debounce, cancel/retry), config/registry |
| **Adapter conformance** | Go test | shared `RunConformance(t, adapter)` suite every adapter must pass (community adapters self-verify) |
| Integration | Go + Docker | real Navidrome: stream-proxy Range, search, scan/refresh (CI service container) |
| API contract | Go | handlers validated against the OpenAPI spec |
| Frontend | Vitest + RTL | 4-state result row, mode toggle, progress-ring, queue reorder, palette/contrast logic |
| **E2E (money test)** | Playwright | the whole core loop (see below) |

**Playwright money test — explicit mock boundary:** only the *network fetch* is mocked. The injectable **spotDL exec runner emits canned realistic stdout** (progress lines, including one malformed line to exercise graceful degradation) **and** a **fixture audio file is dropped into the watched output dir**, so the test exercises real stdout parsing → WS progress events → `StartScan`/`getScanStatus` polling → re-match → ⟳→✓ flip → auto-play. Spotify search runs via cassette. The flow under test: Everywhere search → states render → click ↓ → ⟳ ring → completes → flips to ✓ → auto-plays.

**CI (GitHub Actions):** Go test + `golangci-lint`; frontend test + typecheck + lint; build SPA → embed → binary; build Docker image; optional Navidrome integration job.

---

## 10. Implementation Sequencing (vertical slice → fill out)

Each milestone is independently demoable; M1–M3 are the spine, M4–M5 productionize.

- **M0 — Foundation:** repo layout, config (flags/env), SQLite + goose + sqlc, the 3 registries + `Plugin`, EventBus, **auth seam** (sessions, login, `setup_required`), API server + OpenAPI scaffold, `--dev` Vite proxy / `embed.FS`, React + Tailwind app shell, **`--color-accent` token system**, `docker-compose.dev`. → *boots, login/setup works, empty shell renders.*
- **M1 — Library playback (the spine):** Subsonic adapter (+conformance), stream proxy (Range), `AudioEngine` (dual-audio, queue) + player bar + **Play Queue** panel, local search + album/artist pages. → *connect Navidrome, search your library, play with a real queue.*
- **M2 — Everywhere search + matching:** **author the matching fixture corpus first**, Spotify adapter (+conformance, ISRC), **`MatchingService`** (priority chain + duration disambiguation + `match_cache`/`library_version`, TDD), SSE aggregator (per-source deadlines), Everywhere UI (toggle, append-in-sections, per-source chips, ✓/↓/none). → *see what you have vs. don't.*
- **M3 — Download loop (closes the loop):** Downloader registry + **Manager** (dedup join, fallback chain, scan debounce, cancel/retry), spotDL adapter (+conformance), download endpoints + WS events, library-refresh (StartScan/poll/re-match), **Download Tray** + ⟳ result state + `playWhenReady` auto-play. → *search → download → appears → plays.*
- **M4a — Config UI + wizard (shippable checkpoint — unblocks real installation):** `ConfigSchema`-driven adapter forms + `TestConnection`, settings page (manage/enable/reorder `adapter_instances`), first-run wizard (same components), accent-color setting, keyboard shortcuts, empty/error/loading states. → *installable & configurable entirely via UI.*
- **M4b — Design identity + responsive + polish:** dynamic album palette extraction + contrast logic, responsive shell (mobile bottom nav incl. Search tab, fullscreen-expandable mini player, sheet panels). → *distinctive and mobile-ready.*
- **M5 — Package & ship:** multi-stage Docker image, user `docker-compose.yml`, published OpenAPI, README with **legal/ethical framing** (Q#5), deployment docs (spotDL pin note), Playwright e2e, green CI, v0.1 release.

---

## 11. Open Questions — Resolved

| # | Question | Resolution |
|---|---|---|
| Q1 | Go vs Node backend | **Go**, single binary |
| Q2 | Subsonic vs own server | **Subsonic-first**, abstracted behind `LibraryAdapter` from day one |
| Q3 | "already downloading" dedup | **Dedup-join** on normalized `dedup_key`; in-flight requests join the existing job |
| Q4 | Downloader fallback chain | **Configurable**; iterate by `priority` via `CanDownload` |
| Q5 | Legal/ethical framing | README framing in **M5**; user-configured downloaders, no shipped credentials |
| Q6 | Name collision | **Renamed to "Crate"** |

---

## Appendix — How later phases plug into the seams

- **Artist discography pages (P2):** `DiscographyProvider` optional interface on a `SearchSource`; reuses `MatchingService` + `match_cache` for "in library / download all / X-of-Y".
- **Download queue panel + retry (P2):** UI over the existing Manager (cancel/retry already built); shares `DownloadJobList`.
- **Playlist sync (P2):** new endpoints + reuse of `MatchingService` against a Spotify playlist; "Download Missing" → bulk enqueue (scan debounce already handles the burst).
- **Lidarr (P2):** new `Downloader` package implementing `QualityProfileDownloader`/`MonitoringDownloader`; UI adapts via `DescribeCapabilities`.
- **Standalone mode (P3):** new `LibraryAdapter` (folder scan + serve + transcode); streaming, matching, and schema unchanged.
- **Mobile apps (P3):** regenerate a client from the OpenAPI spec; bearer-token auth already supported.
- **Multi-user/social (P3):** add `users` table, backfill `download_jobs.requested_by`; per-user playlists/history; EventBus already supports targeted notifications.
- **Plugin marketplace (P3):** runtime registration + transport-agnostic interfaces already allow out-of-process adapters over a local socket/gRPC.
