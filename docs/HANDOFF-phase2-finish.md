# Handoff — Finish Reverb Phase 2 (Subprojects B & D)

> **You are taking over Reverb to finish Phase 2.** Two subprojects remain: **B — Download Queue Panel** and **D — Lidarr integration**. This doc is your full briefing: what Reverb is, the rules you must follow, what already exists for each subproject (a lot — extend, don't rebuild), and how to proceed. Read it top to bottom before touching code.

---

## 0. How to work (read first — these are non-negotiable)

1. **Brainstorm → spec → plan → build.** Do **not** start coding B or D from this doc. Each needs a short design pass first (especially D — see its section). Use the `superpowers:brainstorming` skill to turn the scope below into an agreed design, then `superpowers:writing-plans`, then `superpowers:subagent-driven-development` to execute. The prior agent built all of Phase-2 C this way.
2. **Never `git push` without the user's explicit go-ahead.** Merging to **local `main`** is fine and expected; the user pushes + rebuilds the Docker image themselves. Local `main` is routinely many commits ahead of `origin/main`.
3. **Branch first.** Never commit feature work directly on `main`. Branch, build, gate, review, then fast-forward merge to local `main`.
4. **The gate must be green before any merge:**
   - From repo root: `go test ./...` && `go build ./...` && `go vet ./...`
   - From `web/`: `npx vitest run` && `npx tsc --noEmit` && `npm run build` && `npm run e2e` (e2e must stay **3/3**)
   - `npm run build` (not just `tsc`) is part of the gate — it has caught real breakage.
5. **Frontend craft bar is high.** The UI must look like a polished Spotify-class app, not vibe-coded. **Design tokens only** — no raw hex, no `text-black`/`text-white`. Accent is red; use `text-on-accent` on accent surfaces. Match the existing components' density, idioms, and naming.
6. **Generated code is generated.** `sqlc` (`make gen`) owns `internal/store/*.sql.go` + `models.go` — never hand-edit them; edit the `.sql` queries + run `make gen`. DB migrations are `goose` files in `internal/store/migrations/` (latest is `0011`); add new ones, never edit applied ones.
7. **The user tests against a live Docker image** built from GitHub `origin/main`, reachable at `soulkiller:8090` (a Tailscale host). You cannot SSH there. You **can** diagnose against the live app through the Claude-in-Chrome browser tools (screenshots, console, and `javascript_tool` `fetch` against the authenticated session — plain `curl` gets 401). A fix is **not live until the user pushes + rebuilds** — say so, don't assume.
8. **Commit message footer:** end commits with `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.

---

## 1. What Reverb is

A self-hosted music web app that unifies a **Subsonic/Navidrome library + Spotify search + spotDL one-click download** in one UI. Core loop: *search everywhere → if it's in your library, play it; if not, download it → it appears and plays.* OSS, AGPL-3.0.

**Stack:** Go single-binary modular monolith (chi router, modernc **cgo-free** sqlite, goose, sqlc, an EventBus, DB-canonical config) with an embedded **React 19 / TS** SPA (Vite, Tailwind w/ design tokens, TanStack Query, Zustand, Playwright e2e). Adapter seams via registries: **library / search / downloader**.

**Repo:** Go module `github.com/maxjb-xyz/reverb`; binary `reverb`; env prefix `REVERB_*`; image `ghcr.io/maxjb-xyz/reverb`. Working dir `/Users/maximusjb/Repos/reverb`.

---

## 2. What's already done (don't redo)

- **MVP (M0–M5):** foundation, library+player, everywhere-search+matching, the download manager, config/admin UI, packaging/Docker/CI. All on `main`.
- **Phase 2 — A (Detail pages + library completeness):** artist / album / playlist detail pages, discography coverage, dynamic album-palette header. **Done.**
- **Phase 2 — C (Playlist Sync → full Managed-Playlists system):** **Done, and large.** ALL playlists are now Reverb-managed in its own DB (`synced_playlists` table). Navidrome's playlist path is eliminated. Supports create / one-time Spotify import (with cover + per-track downloading/failed state) / synced mirror; editing = add/remove/rename/delete/custom-cover-upload/drag-reorder; the service runs without Spotify; existing Navidrome playlists migrate in on first startup; URLs are `/playlist/{uuid}` and the API is `/api/v1/playlists` (the word "synced" is now just a *mode* flag). This was deliberately built as the foundation for a future **own library engine (primary), Navidrome (secondary)** — keep that direction in mind.

**Remaining Phase 2 = B and D only.**

---

## 3. Subproject B — Download Queue Panel

**Goal (firm direction from the user — not an either/or):** deliver **both** surfaces.
1. **Better the existing right-panel pane** (`DownloadTray`) — polish and strengthen the at-a-glance tray that lives in the app shell's right column.
2. **Build a new, dedicated, more-advanced full page** (a `/downloads` route) — the power-user queue-management surface with the heavier features that don't belong in a narrow side pane.

The pane is the quick glance; the page is the control center. They share the same live store + API, so build the page as a superset and have the pane link into it ("See all" / "Open queue").

### What already exists (extend this — most of the backend is here)
- **Backend API** (`internal/api/server.go`): `POST /downloads`, `POST /downloads/batch`, `GET /downloads` (list), `POST /downloads/{id}/cancel`, `POST /downloads/{id}/retry`.
- **Manager** (`internal/download/manager.go`): `Enqueue`, `List`, `Cancel`, `Retry(jobID, manualURL)`. 2 worker goroutines; dedup; debounced library scan; post-download re-match + `library_version` bump. Jobs persist in `internal/download/sqlstore.go`.
- **Live updates:** the Manager publishes events on an EventBus (`publishEvent(TopicQueued, …)`, `publishComplete(…)`, progress) → the FE receives them over a realtime WebSocket (`web/src/lib/realtimeWiring.ts`) and updates a Zustand store live.
- **FE plumbing:** `web/src/lib/downloadApi.ts` (`getDownloads`, `cancelDownload`, `retryDownload`, `postDownload`, `postBatchDownload`), `web/src/lib/downloadStore.ts` (Zustand store keyed by `(source, externalId)`), and a **basic right-panel tray** `web/src/components/DownloadTray.tsx` (shows active/recent + friendly failure copy). `DownloadAction.tsx` is the per-row download button/state machine (in-library / downloading / completed / failed-with-retry / available).

### The gap (what B actually builds)
Mostly **frontend**, on top of the existing live store + API — split across the two surfaces:

**The pane (improve `DownloadTray`):** tighten the existing right-panel tray — clearer live progress, grouping by status, friendlier empty/active/failed states, and a prominent link into the full page ("See all"). It stays compact; it's the glance. (`AppShell.tsx` already has the `rightPanel === 'downloads'` slot wired.)

**The full page (new `/downloads` route — the advanced surface):** the power-user control center. Candidate features (pin the v1 set in brainstorming):
- Everything in flight + queued + recently completed/failed, grouped and **filterable/searchable**.
- Per-item **and bulk** actions: cancel, retry, clear/dismiss.
- A **history** view of completed/failed downloads.
- **Reorder / prioritize** the queue and **pause/resume** — these are the advanced bits that justify a full page.

**Backend note:** `List`/`Cancel`/`Retry` + live events already exist. The only features needing **new backend** are **reorder/prioritize** and **pause/resume** — the Manager is FIFO across 2 workers today with no `Reorder`/`Pause`. These touch `manager.go` + the job store + new API routes + new events; scope them explicitly in the plan (they're the main reason the full page is a "subproject" and not an afternoon).

**Recommendation:** brainstorm the v1 feature split between pane and page first, then plan the backend additions (reorder/pause) and the FE for both surfaces. Much of the pane work + the read/cancel/retry parts of the page are FE polish against a backend that already streams live state; reorder/pause are the real backend additions.

---

## 4. Subproject D — Lidarr integration

**Goal:** let Reverb use **Lidarr** as a download source alongside spotDL.

### The seam already exists (this is the good news)
- **Downloader interface** — `internal/download/download.go:24`:
  ```go
  type Downloader interface {
      // …registry.Plugin (Name() string, ConfigSchema(), Configure(config)…)
      Start(ctx context.Context, req core.DownloadRequest, onProgress func(int)) (outputPath string, err error)
  }
  ```
- **Reference implementation:** `internal/download/spotdl/adapter.go` — `New()`, `Name() "spotdl"`, `ConfigSchema()` (drives the admin config form), config validation, and `Start()` (shells out, streams progress, returns the written file path). **Copy this shape.**
- **Registry + wiring:** downloaders register via `internal/registry` (`reg.Register("spotdl", func() registry.Plugin { return spotdl.New() })`, see `internal/wiring/`). Admin configures a named instance with a config JSON (URL, API key, etc.) validated against `ConfigSchema()`.
- **Multi-downloader UI already works:** when more than one downloader is enabled, `web/src/components/download/DownloadPopover.tsx` lets the user pick which one. Its tests already use **"Lidarr"** as the placeholder second downloader — so the picker is built and waiting for a real adapter.

### The design tension (must resolve in brainstorming — don't skip)
The `Start(ctx, req) → outputPath` contract is **synchronous and per-track**: "download this one track, return the file path." **Lidarr does not work that way.** Lidarr manages **artists/albums**, hands releases to *its own* download clients/indexers, and completes **asynchronously** via its own queue — there's no "give me this one track's file right now." So a naive `Start()` that blocks until a file appears is the wrong model.

Decisions to make first:
- Does Reverb push an **artist/album** to Lidarr's wanted list and then **poll/track** Lidarr's queue (mapping Lidarr queue state → Reverb `DownloadJob` states)? That likely needs more than the current `Downloader` interface exposes (it may need a new capability — note `registry.RegisterCapability(…)` exists for exactly this kind of optional-capability probe).
- How does a **track-level** Reverb request map onto Lidarr's **album-level** model? (Match the track's album → request that album?)
- Where do finished files land so Reverb's matcher picks them up (Lidarr's import path = Reverb's `/music`)?
- Auth/config fields: base URL, API key, root folder, quality profile.

**Recommendation:** treat D as a genuine design problem, not a copy-paste of spotdl. Brainstorm the async model + whether the `Downloader` interface needs extending (a `Start`-that-returns-immediately + async status, via a capability), **then** plan + build. Keep changes additive — spotdl must keep working unchanged.

---

## 5. Key file map (quick reference)

| Area | Path |
|---|---|
| HTTP routes | `internal/api/server.go` |
| Download API handlers | `internal/api/downloads*.go` / handlers in `internal/api/` |
| Download manager + workers | `internal/download/manager.go` |
| Download job store | `internal/download/sqlstore.go`, queries in `internal/store/queries/` |
| Downloader interface | `internal/download/download.go` |
| spotdl adapter (reference) | `internal/download/spotdl/adapter.go` |
| Registry / capabilities | `internal/registry/registry.go` |
| Composition root / wiring | `internal/wiring/`, `cmd/reverb/main.go` |
| EventBus | `internal/events/` |
| FE download API/store | `web/src/lib/downloadApi.ts`, `web/src/lib/downloadStore.ts` |
| FE realtime WS | `web/src/lib/realtimeWiring.ts` |
| FE download UI | `web/src/components/DownloadTray.tsx`, `web/src/components/download/*` |
| App shell / panels | `web/src/components/AppShell.tsx` |
| FE design tokens | Tailwind config + `web/src/index.css` (use token classes only) |
| Existing Phase-2 specs | `docs/superpowers/specs/` (see the detail-pages + playlist-sync designs for the bar) |

---

## 6. Suggested order

1. **B first** (lower risk, mostly FE on an existing live backend) to re-acclimate to the codebase + conventions, then **D** (needs real design work).
2. For each: `brainstorming` → save a design under `docs/superpowers/specs/` → `writing-plans` → `subagent-driven-development` (fresh implementer per task, review each, whole-branch review before merge) → branch → gate green → merge to **local `main`** → tell the user to push + rebuild to verify on `soulkiller:8090`.

When in doubt about scope or a product call, **ask the user** — they are hands-on and decisive, and prefer a quick question over a wrong big build.
