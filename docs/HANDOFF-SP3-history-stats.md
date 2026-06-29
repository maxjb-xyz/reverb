# Handoff — Reverb SP3: Listening History & Stats (+ Scrobbling)

> **You are taking over Reverb to build SP3.** SP3 is the **Listening History & Stats** sub-project of the *Social / Multi-User* epic — record what users listen to, surface per-user stats (recently played, top tracks/artists, play counts, listening time), and **scrobble to Last.fm** (built in). This doc is your full briefing: the rules, what already exists (a lot — SP1/SP1.5/SP2 are done), the SP3 scope and its design tensions, and where SP3 sits in the roadmap. **Read it top to bottom, then brainstorm — do not start coding from this doc.**

---

## 0. How to work (read first — non-negotiable)

1. **Brainstorm → spec → plan → build.** SP3 has real design decisions (scrobble rules, an external Last.fm integration, track-identity resilience) — do **not** start coding from this doc. Use `superpowers:brainstorming` to turn the scope below into an agreed design saved under `docs/superpowers/specs/`, then `superpowers:writing-plans`, then `superpowers:subagent-driven-development` (fresh implementer per task, per-task review, **one whole-branch review before merge**). Every recent feature was built this way.
2. **Never `git push` without the user's explicit go-ahead.** Merging to **local `main`** is fine and expected; the user pushes + rebuilds the Docker image themselves. Local `main` is currently **+27 commits ahead** of `origin/main` (`origin` = `1eec7bc`, local = `b4465a0`) and that's normal.
3. **Branch first.** Never commit feature work directly on `main`. Branch, build, gate, review, fast-forward merge to local `main`.
4. **The gate must be green before any merge:**
   - Repo root: `go test ./...` && `go build ./...` && `go vet ./...`
   - From `web/`: `npx vitest run` && `npx tsc --noEmit` && `npm run build` && `npm run e2e`
   - `npm run build` (not just `tsc`) is part of the gate — it catches real breakage. e2e is currently **25/25** across 9 spec files; keep it green. `playlist-sync.spec.ts` (and occasionally other specs) has a known `net::ERR_ABORTED` flake under full parallel runs — re-run once; it passes in isolation.
5. **Frontend craft bar is high.** The UI must look like a polished Spotify-class app, not vibe-coded. **Design tokens only** — no raw hex, no `text-black`/`text-white`; accent surfaces use `text-on-accent`. Match existing components' density, idioms, and naming. (See `NotificationBell.tsx`, the player shell, and the detail pages for the bar.)
6. **Generated code is generated.** `sqlc` (`make generate`) owns `internal/store/db/*.sql.go` + `models.go` — never hand-edit; edit the `.sql` queries + regenerate. Migrations are `goose` files in `internal/store/migrations/` — **latest is `0018_notifications.sql`**; add new ones (`0019…`), never edit applied ones.
7. **The user tests against a live Docker image** built from GitHub `origin/main`, reachable at `soulkiller:8090` (a Tailscale host). You cannot SSH there. You **can** diagnose against the live app via the Claude-in-Chrome browser tools (screenshots, console, and `javascript_tool` `fetch` against the authenticated session — plain `curl` gets 401). A fix is **not live until the user pushes + rebuilds** — say so, don't assume.
8. **Commit message footer:** end commits with `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.

---

## 1. What Reverb is

A self-hosted music web app unifying a **Subsonic/Navidrome library + Spotify search + spotDL/Lidarr download** in one UI. Core loop: *search everywhere → if it's in your library, play it; if not, request/download it → it appears and plays.* Now multi-user with a request/approval system. OSS, AGPL-3.0.

**Stack:** Go single-binary modular monolith (chi router, modernc **cgo-free** sqlite, goose, sqlc, an EventBus, DB-canonical config) with an embedded **React 19 / TS** SPA (Vite, Tailwind w/ design tokens, TanStack Query, Zustand, Playwright e2e). Adapter seams via registries: **library / search / downloader**. Per-user WebSocket for live updates.

**Repo:** Go module `github.com/maxjb-xyz/reverb`; binary `reverb`; env prefix `REVERB_*`; image `ghcr.io/maxjb-xyz/reverb`. Working dir `/Users/maximusjb/Repos/reverb`.

---

## 2. What's already done (don't redo)

- **MVP (M0–M5)** + **Phase 2 (A detail pages / B download queue / C managed playlists / D Lidarr)** — all on `main`.
- **SP1 — Accounts & Identity Foundation** (multi-user: sessions, roles, bcrypt, registration modes). Done.
- **SP1.5 — Permissions model refinement** (capability set: `request`, `manage_requests`, `auto_approve`, `manage_library`, `manage_users`, `is_admin`). Done.
- **SP2 — Request system** + its full backlog, all merged to local `main` (NOT pushed):
  - The unified add-first **request → approval → fulfillment** flow, `/requests` page, per-user request WS events, the manager approval queue + pending badge.
  - **Configurable downloader granularity** (spotDL track+album, Lidarr album; two-column Song|Album ordering in **Admin → Providers → Downloaders**).
  - **Artist-level "Request all"** — fans the not-fully-owned discography into N album requests via `POST /requests/batch` (reuses `createOneRequest`; server-side dedup).
  - **Request quotas** — admin `max_pending_requests_per_user` (0=unlimited), enforced in `createOneRequest` for would-pend requests; single→429, batch→`quotaCapped`.
  - **Approval notifications — in-app center** — a `Notifier` (subscribes to request lifecycle events) writes `notifications` rows + publishes per-user `notification` WS events; user-scoped API; **TopBar bell + dropdown center**. (This is the closest precedent for SP3-style work: a new table + a bus-subscriber side-effect component + per-user WS + a FE store/UI. Study it.)
- **Deploy state:** `origin/main = 1eec7bc` (configurable-granularity — deployed + verified on soulkiller). **Local `main = b4465a0`, +27 commits ahead, NOT pushed.** The user pushes + rebuilds to deploy/verify.
- **No play-tracking, scrobbling, or history code exists yet** — SP3 is greenfield on top of the player + accounts that already exist.

---

## 3. SP3 scope — Listening History & Stats (+ Scrobbling)

From the roadmap: *"Listening history and stats (Last.fm scrobbling built in)."* Three pieces — pin the v1 cut in brainstorming; **consider splitting internal history+stats (3.1/3.2) from the external Last.fm integration (3.3)** into separate spec→plan→build cycles, as they're largely independent and 3.3 is its own external-service subsystem.

### 3.1 Play history (the foundation)
- Record each play: a `plays` / `listening_history` table — `user_id`, the track identity, `played_at`, and enough to be useful (ms played / completed-flag). Per-user (the accounts system is in place).
- **Where a "play" is detected:** the FE audio engine (`web/src/lib/audioEngine.ts`) + `playerStore.ts` know the current track + position. A play is reported to a new backend endpoint when it crosses a threshold (the de-facto standard is Last.fm's: track > 30s **and** played ≥ half its length **or** ≥ 4 minutes). Decide the threshold + whether the FE or a heartbeat reports it.

### 3.2 Stats (the payoff)
- Per-user aggregates over time windows (7d / 30d / year / all): **recently played**, **top tracks / artists / albums**, **play counts**, **total listening time**. New API + a stats/profile page (or a section on the user's page). This is the data layer **Reverb Wrapped** (a "Future Stuff" item) will later build on.

### 3.3 Last.fm scrobbling (the external integration)
- "Built in" = the user connects their Last.fm account and Reverb scrobbles on play (`track.scrobble`) + updates now-playing (`track.updateNowPlaying`). This is a genuine external-service integration: a Last.fm **API key + secret** (instance setting, admin-configured), the **desktop auth flow** (get token → user authorizes → fetch + store a **per-user session key**), and signed API calls. Reuse the settings/adapter + per-user-credential patterns.

### Design tensions to resolve in brainstorming (don't skip)
- **What counts as a play** + who reports it (FE on-threshold vs a now-playing heartbeat the server scores). Don't double-count seeks/restarts.
- **Track-identity resilience.** A play references a library track, but the library can be **re-matched / backend-swapped** (Navidrome ids churn — this exact class caused the cover-art bug). Store enough **stable metadata** (artist + title + album, and/or a stable external id like ISRC/Spotify id) so history + scrobbles survive id churn. **This is the highest-risk SP3 decision — get it right in the spec.**
- **Per-user everywhere.** History, stats, and the Last.fm connection are per-user. Don't leak one user's history to another (the notifications feature is the reference for airtight per-user scoping).
- **Last.fm scope + failure handling:** session-key storage per user, scrobble retry/queue on Last.fm downtime (scrobbles shouldn't be lost or block playback), and whether to scrobble streamed-from-library plays only or also previews.
- **v1 stats cut:** start with recently-played + top tracks/artists + play counts; defer richer cuts (genres, Wrapped) — note what's deferred.

---

## 4. Existing seams to extend (don't rebuild)

| Need | Reuse |
|---|---|
| Detect "a track played" + progress | `web/src/lib/audioEngine.ts` (Howler/audio engine), `web/src/lib/playerStore.ts` (current track + position), `web/src/components/shell/PlayerBar.tsx` |
| Per-user identity + scoping | `internal/auth/` (sessions, `CurrentUser`, capabilities); the notifications feature's SQL-level user-scoping is the model |
| A new table + service + side-effect consumer | The **notifications** feature (`internal/notification/` — migration + `Service` + `Notifier` bus-subscriber + per-user WS + FE store/UI) is the closest precedent — mirror its shape for a `plays`/`history` service |
| Live "now playing" / events | `internal/events/` EventBus + the per-user WebSocket (`internal/api/ws.go`, `web/src/lib/realtimeWiring.ts`) |
| External-service config + per-user credential | The adapter/settings pattern (`internal/api/settings.go` + `GetSetting`/`UpsertSetting`) for the Last.fm API key; per-user session-key storage alongside the user |
| Stable track identity across library churn | `internal/matching/` + the `library_version` mechanism — store stable metadata, don't trust raw library ids long-term |
| sqlc + migrations | `internal/store/queries/*.sql` + `make generate`; new goose migration `0019…` (latest is `0018`) |

---

## 5. Key file map (quick reference)

| Area | Path |
|---|---|
| HTTP routes / Deps | `internal/api/server.go` |
| Settings (GetSetting/UpsertSetting + DTO) | `internal/api/settings.go` |
| Auth / sessions / capabilities | `internal/auth/` |
| EventBus | `internal/events/` |
| Per-user WebSocket + `wsShouldForward` filter | `internal/api/ws.go`, `web/src/lib/realtimeWiring.ts` |
| Notifications subsystem (the SP3 precedent) | `internal/notification/`, `internal/api/notifications.go`, `web/src/lib/notificationApi.ts`, `web/src/components/NotificationBell.tsx` |
| Matching / library identity | `internal/matching/`, `internal/wiring/wiring.go` (`reconcile*Identity`) |
| FE audio engine + player store | `web/src/lib/audioEngine.ts`, `web/src/lib/playerStore.ts` |
| FE player UI | `web/src/components/shell/PlayerBar.tsx`, `NowPlayingPanel.tsx` |
| Composition root / wiring | `internal/wiring/`, `cmd/reverb/main.go` |
| sqlc queries / migrations | `internal/store/queries/`, `internal/store/migrations/` (latest `0018`) |
| FE design tokens | Tailwind config + `web/src/index.css` (token classes only) |
| Roadmap / future | `reverb-plan.md` (root) |

---

## 6. Where SP3 sits — and yes, there's a deferred SP4

The *Social / Multi-User* epic phasing (see `docs/superpowers/specs/2026-06-27-request-system-design.md` + `…-multiuser-accounts-foundation-design.md`):

- **SP1** Accounts & Identity — ✅ done
- **SP1.5** Permissions refinement — ✅ done
- **SP2** Request system (+ granularity, artist-request-all, quotas, notifications) — ✅ done (merged local, unpushed)
- **SP3** Listening History & Stats (+ scrobbling) — **← this handoff (next)**
- **SP4 — Richer Social — DEFERRED, real, and explicitly scoped:** **playlist sharing / collaboration, public profiles, and listening parties** (per `…-multiuser-accounts-foundation-design.md:279`). This is its own epic after SP3; it'll need its own brainstorm. The managed-playlists system (Phase-2 C) is the foundation playlist-sharing/collaboration will build on.

**Also deferred (the broader `reverb-plan.md` "Future Stuff" backlog, beyond the SP epics):** Reverb Wrapped (end-of-year review — **builds directly on SP3's history data**), Discover Weekly, SSO/OIDC, desktop apps (Tauri) + mobile apps, custom themed player bars, device control, "search Spotify playlists", federated features. And from the deferred-cleanup notes, a couple of tiny still-open nits (a `reqFromJob` helper dedup; `AddToPlaylistID` in one cross-restart reconstruction) — non-blocking.

One known scope flag carried over: the **approval notifications** feature shipped the **in-app** center only; **Web Push** (OS/browser push when the app is closed — service worker + VAPID + subscriptions) was deliberately deferred as its own subsystem. If the user wants true push, it layers onto the existing `Notifier` (a second delivery channel) and is a small standalone project — orthogonal to SP3.

---

## 7. Suggested order

1. **Brainstorm SP3** with the user — pin the v1 stats cut, the play/scrobble threshold, and (critically) the **track-identity-resilience** decision. Strongly consider **two sub-cycles**: (3a) internal play-history + stats, then (3b) Last.fm scrobbling (external integration).
2. For each: `brainstorming` → spec under `docs/superpowers/specs/` → `writing-plans` → `subagent-driven-development` (per-task review + one whole-branch review) → branch → gate green → merge to **local `main`** → tell the user to push + rebuild to verify on `soulkiller:8090`.
3. When in doubt about scope or a product call, **ask the user** — they're hands-on and decisive, and prefer a quick question over a wrong big build. (They recently said "just do everything" for the SP2 backlog and were happy to delegate design calls — but SP3's track-identity + Last.fm decisions are worth surfacing explicitly.)

*Reverb — own your music, again.*
