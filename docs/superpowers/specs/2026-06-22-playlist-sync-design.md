# Playlist Sync — Design Spec

> Phase 2 / sub-project C. Import a **public Spotify playlist by URL** into a
> **Reverb-managed synced playlist**: Reverb stores the Spotify tracklist and
> computes have/missing live against the library, downloads what's missing in one
> click, and keeps the playlist fresh via manual "Sync now" **and** a background
> scheduler. Reuses the Phase-2-A completeness engine wholesale.

- **Status:** Approved design (brainstormed 2026-06-22), ready for implementation planning.
- **Author:** Reverb maintainer + Claude.
- **Builds on:** the completeness engine (matching, `match_cache`, batch download,
  album-page track rendering) and the sync-ready Playlist page from sub-project A.

---

## 1. Goals & Non-Goals

### Goals
1. **Import** a public Spotify playlist by pasting its URL → a Reverb-managed
   synced playlist showing every track as owned (play) or missing (download), in
   Spotify order, like the album page.
2. **Download missing** — one click queues every not-yet-owned track; as each
   downloads + scans, it flips to owned automatically (live matching, no hook).
3. **Keep fresh** — a **manual "Sync now"** and a **background scheduler**
   re-fetch the Spotify playlist (per-playlist interval), pick up added/removed
   tracks, and optionally auto-download new missing tracks.
4. **Integrated** — synced playlists appear in the left rail and Library grid
   alongside library playlists, visually marked, and open their own page.

### Non-Goals (this sub-project)
- **Spotify user OAuth** (Authorization Code flow). We use the existing
  **client-credentials** app token, so only **public** playlists are reachable.
  Private/own playlists and `GET /me/playlists` are a future sub-project.
- **Spotify editorial/algorithmic playlists** (Discover Weekly, Today's Top Hits,
  etc.) — Spotify cut these off for new apps on 2024-11-27; not accessible
  regardless of what we build. Out of scope by necessity.
- **Materializing into a Navidrome playlist.** The synced playlist lives in Reverb
  (stored tracklist + live matching), not as a Subsonic playlist. (A "save owned
  tracks as a library playlist" action could be a later add.)
- **Other sources** (Deezer, etc.) — Spotify only for now; the `PlaylistProvider`
  capability leaves room.

---

## 2. Locked Product Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Auth scope | **Public playlists by URL**, existing client-credentials | Ships now, no OAuth; covers user-created public playlists. |
| What sync produces | **Reverb-managed synced playlist** (stored Spotify tracklist + live have/missing matching) | Preserves Spotify order, shows missing inline, reuses the completeness engine; no Navidrome write. |
| Re-sync | **Manual "Sync now" + scheduled auto-sync** (per-playlist interval, optional auto-download) | User wants it fresh automatically; one shared sync function serves both. |
| Duplicate import | **Upsert** on `UNIQUE(source, external_id)` | Re-importing the same playlist updates it rather than duplicating. |
| Have/missing | **Computed live** per view via the matching service (`library_version`-invalidated), never stored | A downloaded track flips to owned with zero extra plumbing — same mechanism as `AlbumDetail`. |

---

## 3. Architecture — the sync engine

### 3.1 The synced playlist = stored tracklist + live matching
A `synced_playlists` row stores the Spotify playlist id, name, cover URL, schedule
settings, and the **ordered list of Spotify track refs** (`[]core.ExternalTrackRef`
as JSON). It does **not** store ownership. On every **view**, the detail endpoint
runs each track ref through the existing `Matcher` (cache-first, `library_version`
staleness) → owned (with library track id) / missing (with external ref), producing
a `SyncedPlaylistDetail` shaped exactly like `AlbumDetail`. So a downloaded track
appears as owned on the next view with no playlist mutation.

### 3.2 Three operations, one shared core
1. **Import** (`url`): `ParsePlaylistID(url)` → Spotify `GetPlaylist(id)` (meta +
   paginated tracks) → upsert the row (`UNIQUE(source, external_id)`) → return the
   live detail. Optional `downloadMissing` flag queues missing immediately.
2. **Sync** (called by **both** "Sync now" and the scheduler): re-fetch
   `GetPlaylist(id)` → replace `tracks_json` (mirrors Spotify: new tracks appear,
   removed tracks drop) → if `auto_download`, batch-queue newly-missing → stamp
   `last_synced_at`. On Spotify failure (404/timeout): keep the existing tracklist,
   record the error, do **not** wipe.
3. **View** (`Detail(id)`): stored tracks + live match → owned/missing in order.

### 3.3 The scheduler
A single background goroutine started at boot. Every **15 min** it selects
`synced_playlists` where `sync_enabled` and `last_synced_at + sync_interval_sec <=
now`, and runs **Sync** on each **sequentially** (no Spotify hammering). Failures
log and continue; the next tick retries. Intervals exposed to the user: **Manual**
(`sync_interval_sec = 0`, scheduler skips), **Daily** (`86400`), **Weekly**
(`604800`). The goroutine respects a context for clean shutdown.

### 3.4 Reuse ledger
- **Reused as-is:** matching service + `match_cache`, the batch-download manager,
  the album-page track rendering (`TrackRow` + the missing-row `DownloadAction`),
  `Cover` `coverSrc` (Spotify art), the Playlist-page shell, the modal pattern.
- **New:** Spotify `GetPlaylist` + `ParsePlaylistID`; the `synced_playlists` table;
  the sync service (`Import`/`List`/`Detail`/`Sync`/`UpdateSettings`/`Delete`); the
  scheduler goroutine; the import dialog + synced-playlist page + library wiring.

---

## 4. Data Model

### 4.1 New table (goose migration + sqlc)
```
synced_playlists(
  id                TEXT PRIMARY KEY,        -- Reverb-generated (uuid)
  source            TEXT NOT NULL,           -- "spotify"
  external_id       TEXT NOT NULL,           -- Spotify playlist id
  name              TEXT NOT NULL,
  cover_url         TEXT NOT NULL DEFAULT '',
  tracks_json       TEXT NOT NULL,           -- ordered []core.ExternalTrackRef
  sync_enabled      INTEGER NOT NULL DEFAULT 0,
  sync_interval_sec INTEGER NOT NULL DEFAULT 0,   -- 0=manual, 86400=daily, 604800=weekly
  auto_download     INTEGER NOT NULL DEFAULT 0,
  last_synced_at    INTEGER NOT NULL DEFAULT 0,
  created_at        INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_synced_playlists_source_external ON synced_playlists(source, external_id);
```
Queries: `Insert`/`Upsert`, `GetByID`, `GetBySourceExternal`, `List`, `ListDue(now)`
(`sync_enabled=1 AND sync_interval_sec>0 AND last_synced_at+sync_interval_sec<=now`),
`UpdateTracks(id, json, lastSyncedAt)`, `UpdateSettings(id, enabled, interval, auto)`,
`Delete(id)`.

### 4.2 Core types
```
ExternalPlaylist { Source, ExternalID, Name, CoverURL string; Tracks []ExternalResult }
SyncedPlaylist   { ID, Source, ExternalID, Name, CoverURL string
                   SyncEnabled bool; SyncIntervalSec int; AutoDownload bool
                   LastSyncedAt int64; TrackCount int }
SyncedPlaylistDetail {  // mirrors AlbumDetail
                   SyncedPlaylist
                   OwnedCount, TotalCount int
                   Tracks []AlbumDetailTrack }   // reuse the existing per-track type
```

---

## 5. API Surface (protected group)

| Method + path | Body | Returns |
|---|---|---|
| `POST /api/v1/synced-playlists` | `{url, downloadMissing?}` | `SyncedPlaylistDetail` |
| `GET /api/v1/synced-playlists` | — | `SyncedPlaylist[]` |
| `GET /api/v1/synced-playlists/{id}` | — | `SyncedPlaylistDetail` (live match) |
| `POST /api/v1/synced-playlists/{id}/sync` | — | `SyncedPlaylistDetail` |
| `POST /api/v1/synced-playlists/{id}/download-missing` | — | `[]core.DownloadJob` |
| `PUT /api/v1/synced-playlists/{id}/settings` | `{syncEnabled, intervalSec, autoDownload}` | `SyncedPlaylist` |
| `DELETE /api/v1/synced-playlists/{id}` | — | `{ok:true}` |

- Import validates the URL; a non-playlist URL → `400`; an inaccessible/private/
  editorial playlist (Spotify 403/404) → `422` with a friendly message.
- When no search adapter implements `PlaylistProvider` (no Spotify configured) →
  `503 "no Spotify source configured"`. Handlers `503` when the service is nil,
  mirroring the coverage handlers.
- OpenAPI documents all seven routes.

**Backend pieces:** `internal/search/spotify` gains `GetPlaylist` + `ParsePlaylistID`
+ the `search.PlaylistProvider` capability interface; a new `internal/playlistsync`
service + `Scheduler`; the `synced_playlists` store; `internal/api/synced_playlists.go`
handlers; wiring + scheduler start in `cmd/reverb`.

---

## 6. Frontend

**Routing:** `/synced-playlist/:id` → the synced playlist page. Library playlists
keep `/playlist/:id`.

**Data layer:** `web/src/lib/syncedPlaylistApi.ts` — `useSyncedPlaylists()`,
`useSyncedPlaylist(id)`, `importPlaylist(url, downloadMissing)`, `syncNow(id)`,
`downloadMissing(id)`, `updateSyncSettings(id, …)`, `deleteSyncedPlaylist(id)`; new
TS types mirroring §4.2 (`SyncedPlaylist`, `SyncedPlaylistDetail`). Invalidates
`['synced-playlists']` / `['synced-playlist', id]`.

**Import dialog** (reuse the existing admin modal pattern): an "Import from Spotify"
action in the Library playlists section (and the rail) → modal with a URL input →
**Fetch** → preview (cover, name, "50 tracks · 32 have · 18 missing") → **Import**
with a "download missing now" checkbox → navigates to `/synced-playlist/{id}`.
Inline errors for bad/inaccessible URLs and no-Spotify-configured.

**Synced playlist page** (`/synced-playlist/:id`) — the Album page, dressed for sync:
- Header: Spotify cover (`coverSrc`), a "Synced playlist" eyebrow + small synced
  badge, name, `"32 of 50 in library · 18 missing"`, and a `"Synced 2h ago"` line.
- Actions: **Play** (owned tracks), **Download all missing · N**, **Sync now**, and
  a **"⋯" menu** with the schedule settings + **Remove**.
- Tracklist: identical to the Album page — owned rows play, missing rows show the
  Download control, in Spotify order.

**Schedule settings** (in the "⋯" menu / inline panel): a **Sync** `Toggle`, an
**Interval** `Select` (Manual / Daily / Weekly), and an **Auto-download missing**
`Toggle` → `updateSyncSettings`.

**Library integration:** synced playlists render in **both** the left rail and the
Library grid alongside library playlists, with a small synced/Spotify badge, routing
to `/synced-playlist/:id`. A `useSyncedPlaylists` hook feeds these surfaces.

**Components reused:** the Album-page track rendering, `Cover` (`coverSrc`),
`Button`, `IconButton`, `Select`, `Toggle`, `Chip`, the modal pattern, `MediaCard`
(library grid). Design tokens only.

---

## 7. Edge Cases
- **Bad URL / non-playlist** → inline `400` error in the dialog.
- **Private / editorial / unknown playlist** (Spotify 403/404) → friendly "can't
  access — is it public?" (`422`).
- **No Spotify source configured** → import disabled with a clear message (`503`).
- **Empty playlist** → imports with 0 tracks, friendly empty state.
- **Large playlists** → follow Spotify `next` pagination (100/page).
- **Sync failure** (Spotify down/404) → keep the last-known tracklist + surface the
  error; never wipe.
- **Track removed on Spotify** → drops from the synced tracklist on next sync.
- **Scheduler** → sequential syncs, log-and-continue on failure, context-cancellable
  for clean shutdown; `Manual` interval is skipped.

---

## 8. Testing
- **Go unit:** `ParsePlaylistID` (all URL forms); `GetPlaylist` mapping + pagination;
  `Sync` diff (added/removed); `Detail` live-matching (owned/missing); `ListDue`
  selection; scheduler tick selects + syncs due playlists; import upsert.
- **FE component:** import dialog (fetch / preview / error / no-Spotify); synced
  playlist page states (owned/missing, sync-now, download-missing); schedule settings;
  library badge + routing.
- **Hermetic e2e:** import a stubbed public playlist → see have/missing → download a
  missing track → it flips to owned → "Sync now" picks up an added track.

---

## 9. Sequencing (for the plan)
1. **Spotify `GetPlaylist` + `ParsePlaylistID` + `PlaylistProvider`** (TDD).
2. **`synced_playlists` store** (migration + sqlc + round-trip).
3. **Sync service** — `Import`/`List`/`Detail`/`Sync`/`UpdateSettings`/`Delete`
   (reusing the matcher + batch download).
4. **Scheduler** goroutine + `ListDue`.
5. **API handlers + routes + wiring + scheduler start + OpenAPI.**
6. **FE data layer** (types + `syncedPlaylistApi`).
7. **Import dialog** + Library/rail entry point.
8. **Synced playlist page** + schedule settings.
9. **Library integration** (rail + grid badges + routing).
10. **e2e + edges.**

Each step verified before the next; whole-branch review before merge.
