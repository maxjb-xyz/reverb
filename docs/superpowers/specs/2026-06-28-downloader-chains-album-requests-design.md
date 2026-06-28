# Granularity-Aware Downloader Chains + Album Requests — Design

**Status:** Approved (brainstorm 2026-06-28)

**Goal:** Replace the per-add "choose a downloader" picker with admin-ordered, **granularity-aware downloader chains** — a song-level chain tried in order with failure-fallback, and a separate album-level chain — and extend the request system to support **album requests** so Lidarr's whole-album grab flows through the same request/approval queue.

**Tech stack:** Go modular monolith (chi, modernc sqlite, goose, sqlc) + React 19/TS SPA. Builds on SP2 (the request system).

---

## Background — current state (concrete)

- **Selection** `internal/download/manager.go:pick()` (line ~425): if `req.Downloader != ""` use that named downloader (the picker override), else iterate `m.downloaders` in order and pick the first whose `CanDownload` returns true.
- **Order already admin-driven:** `wiring.go:BuildDownloaders` returns enabled downloader adapter_instances "ordered by (type, priority)" — so the slice is already in fallback-chain order, and `adapter_instances.priority` is settable via the adapters API (`internal/api/adapters.go` Priority field).
- **Failure-fallback is a stub:** `manager.go` (~line 701-714) has a `FUTURE:` comment — today a single downloader is picked and if it fails the job goes straight to `DownloadFailed`. The manual "download from a link" (`DownloadRequest.ManualURL`) is the deliberate last resort, gated behind all-providers-failed.
- **Lidarr is excluded via a hack:** Lidarr's `CanDownload` returns `false` so the auto-pick loop never selects it; it is reachable ONLY via the picker (`DownloadPopover`) setting `req.Downloader = "lidarr"`, which maps the clicked track to its album and grabs the whole album (with a "fetches the whole album" disclosure modal in `DownloadAction.tsx`). Lidarr is an `AsyncDownloader` (`download.go:43`, Submit/Poll) driven by a reconciler goroutine.
- **The picker:** `web/src/components/download/DownloadPopover.tsx` (the split-button list) + a `default_downloader` admin setting + the `req.Downloader` override.
- **Request system (SP2):** track-level only. Every add → `POST /requests`; `auto_approve` → approve+enqueue, `request`-only → pending in the `manage_requests` Approval queue; `request.Tracker` flips a request `fulfilled`/`failed` when its linked download job completes/fails. Requests table = migration `0014_requests` (source, external_id, title, artist, album, isrc, duration_ms, cover_art_id, cover_url, status, …).

---

## Architecture

### 1. Downloader granularity

Add a **granularity** to each downloader: `track` | `album`. Mechanism: a method on the `Downloader` interface (`internal/download/download.go:26`), e.g. `Granularity() core.DownloadGranularity`. spotDL returns `track`; Lidarr returns `album`. The conformance suite (`internal/download/conformance.go`) asserts every downloader declares one.

This **replaces the `CanDownload=false` exclusion hack** for Lidarr — granularity now partitions which chain a downloader belongs to. `CanDownload(ctx, req)` stays, narrowed to its real meaning: "can this downloader attempt *this specific item* right now?" (a per-request filter applied *within* a chain), not "should I be auto-picked at all."

### 2. Two chains, partitioned by granularity

- **Song chain** = enabled `track`-granularity downloaders, in `(priority)` order. Drives every **track** request.
- **Album chain** = enabled `album`-granularity downloaders (Lidarr today), in `(priority)` order. Drives every **album** request.

`pick()` becomes **granularity-scoped**: given a request's granularity, it iterates only that chain, in order, returning the first whose `CanDownload` accepts the item. `core.DownloadRequest` gains a `Granularity` field (`track` | `album`, default `track`); the `Downloader` override field and `pick()`'s explicit-name branch are **removed** (the picker is gone — nothing sets it). `ManualURL` (the last-resort) is unchanged.

**A track request can never select an album downloader**, by construction — this is the guarantee that prevents the surprise whole-album grab.

### 3. Failure-fallback (build the `FUTURE:` stub)

In the worker's failure branch (`manager.go` ~701): when the chosen downloader can't produce the file, **re-pick the next downloader in the same chain** (same granularity, after the one that failed) whose `CanDownload` accepts the item, and retry it. Only when the chain is **exhausted** does the job reach `DownloadFailed` — and then the existing `ManualURL` last-resort condition is unchanged. The job logs each downloader attempted. (With only spotDL today the song chain has one link, so fallback is dormant until a second `track` downloader is added — this is the infrastructure for that, which is the point of the redesign.)

### 4. Remove the per-add picker

Delete `web/src/components/download/DownloadPopover.tsx` (the split-button) and its tests, remove the `default_downloader` admin setting + its UI + all reads, and remove the per-add `req.Downloader` override path. A track add becomes a single button → the song chain. No per-add downloader choice remains anywhere in the UI.

### 5. Album requests (extend the request system to albums)

- **`kind` on requests** (`track` | `album`, migration adds the column default `'track'`). An **album request** reuses the existing request item fields — `source`, `external_id` (the **album** id), `title` (album name), `artist`, `cover_art_id`/`cover_url` — with the track-only fields (`isrc`, `duration_ms`) null. Dedup is per `(requested_by, source, external_id)` exactly as tracks (`GetOpenRequestByItem` unchanged — the album id occupies `external_id`).
- **Entry point:** a **"Request album"** action on the album detail page (`web/src/routes/Album.tsx`) → `POST /requests` with `{kind:"album", source, externalId:<albumId>, title:<albumName>, artist, coverUrl/coverArtId}`, carrying the "this fetches the whole album via <album downloader>" disclosure (relocated from the current per-track Lidarr modal). Same capability gating as track requests: visible with `request`, skips the queue with `auto_approve`.
- **Flow:** `auto_approve` → approve + `Enqueue(Granularity=album)` → album chain (Lidarr submit) immediately; `request`-only → a **pending album request** in the Approval queue → manager approves → enqueue. The Lidarr adapter's `Submit` accepts an `album`-granularity request and adds/monitors **that album directly** (cleaner than the current track→album mapping; the album id is the unit).
- **Fulfillment:** an album request maps to exactly **one** album-chain job. The existing `request.Tracker` flips the request `fulfilled`/`failed` when that job's `download.complete`/`download.failed` fires — no new fulfillment path, just an album-shaped payload. (Lidarr performs the multi-track import; one request ↔ one album job.)
- **Rendering:** `/requests` (My Requests) rows and the manager Approval queue render album requests with an **"Album · ~N tracks"** cue (N from the album's track count when known) so a manager sees it is a larger grab than a single track. Album-vs-track is read from `kind`.

### 6. Admin order config

`adapter_instances.priority` already orders the chains. Surface a clear ordering control for downloaders in Settings (`web/src/routes/Settings.tsx`): the admin reorders downloaders (which writes `priority`), and each downloader shows its **granularity read-only** (so the admin understands a `track` reorder vs an `album` reorder are independent chains). If a usable reorder affordance already exists for adapters, reuse it; otherwise add a minimal up/down reorder. The order takes effect on the next manager (re)build.

---

## Data flow

- **Track add** → `POST /requests {kind:track}` → (`auto_approve`) `Enqueue(Granularity=track)` → song-chain `pick` → download → (on failure) next track downloader → … → tracker → `fulfilled`.
- **Album request** → `POST /requests {kind:album}` → (`auto_approve`) `Enqueue(Granularity=album)` → album chain (Lidarr `Submit` of the album) → reconciler Poll → `download.complete` → tracker → `fulfilled`. (`request`-only inserts a pending album request first; a manager approve triggers the same enqueue.)

## Error handling

- **Song-chain exhaustion** → job `DownloadFailed` → request `failed` (existing tracker path). `ManualURL` last-resort unchanged.
- **Album grab failure** (Lidarr import fails / errors) → the album job fails → request `failed`.
- **Granularity guarantee:** a `track` request provably never reaches an `album` downloader; an `album` request only ever uses the album chain (covered by tests).

## Migration / compatibility

- Migration adds `requests.kind TEXT NOT NULL DEFAULT 'track'` (existing requests become track requests).
- Granularity is **code-derived from downloader type** (no data migration): spotDL=`track`, Lidarr=`album`.
- Drop the `default_downloader` setting and remove its reads; remove `req.Downloader` plumbing.
- Existing download jobs and requests are otherwise untouched.

## Out of scope (explicit — do-next or not-planned)

**Do-next (natural follow-ups):**
- **Push notifications for approvals** — alerting a manager that an approval is waiting (the album-request approval works *now* via the existing Approval queue; notifications are the enhancement, deferred).
- **Artist-level "request all"** — request a whole artist's discography. Album-level only now.

**Maybe-later:**
- **Track→album auto-escalation** — when the song chain is exhausted for a track, offer/auto-create an approval-gated "grab the whole album instead?" fallback. Not now; a track request just fails, and the user can explicitly Request album.
- **Partial-album ownership optimization** — Reverb-side skipping of already-owned album tracks. Out of scope; Lidarr's import handles existing files.

**Not planned here:**
- Multiple **track**-level downloaders shipped in this project — the fallback infra is built and tested with fakes, but spotDL remains the only real `track` downloader; a second is a separate addition.

## Testing

- **Granularity partition (the guarantee):** a `track` request never selects an `album` downloader; an `album` request only uses the album chain. Asserted directly against `pick()` with fake track + album downloaders.
- **Failure-fallback:** two fake `track` downloaders, first fails → second is used; both fail → job `DownloadFailed`; `ManualURL` still surfaces only after exhaustion. Single-downloader case unchanged.
- **`pick()` granularity-scoped order** respects `priority`.
- **Picker removed:** a track add still enqueues via the song chain (no regression); no `default_downloader` / `req.Downloader` reads remain (grep-clean + a test).
- **Album requests:** create (`auto_approve`→enqueue album chain; `request`→pending), dedup per album id, approve→enqueue, fulfillment flips on the album job's complete/failed, `/requests` + Approval render the "Album · ~N tracks" cue, gating (`request`/`auto_approve`).
- **Admin reorder** persists `priority` and changes chain order on rebuild.
- **e2e (hermetic):** Request album → (manager) approve → fulfilled toast; a normal track add is unaffected by the picker removal.
