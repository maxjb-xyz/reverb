# Lidarr Integration — Design Spec

> Phase 2 / sub-project D. Let Reverb acquire music through **Lidarr** as a second
> downloader alongside spotDL. Because Lidarr is **asynchronous + album/artist-level**
> (vs. the synchronous, per-track `Downloader.Start` contract), this introduces a
> reusable **async-downloader capability** + a background **reconciler**. Lidarr is
> **opt-in** (never auto-picked); choosing it for a track honestly acquires that
> track's whole album. Adds a **default-downloader** admin setting so the picker
> doesn't nag once there are two sources.

- **Status:** Approved design (brainstormed 2026-06-24), ready for implementation planning.
- **Author:** Reverb maintainer + Claude.
- **Builds on:** the downloader seam (`internal/download/download.go` `Downloader`, `internal/registry` `Plugin` + `RegisterCapability`), the spotDL adapter (reference shape), the Manager's scan-debounce + post-download rematch, the playlist-sync **scheduler goroutine** (precedent for a background reconciler), the multi-downloader picker (`DownloadPopover`, whose tests already use "Lidarr"), and the Phase-2-B queue surfaces (the `/downloads` page + `DownloadTray` render any `DownloadJob`, so Lidarr jobs appear there for free).

---

## 1. Goals & Non-Goals

### Goals
1. **Lidarr as a downloader adapter** mirroring spotDL's shape (`ConfigSchema` → admin form, `Init`, `TestConnection`), registered at the composition root and configured per-instance.
2. **Async acquisition without pinning a worker:** a new optional `AsyncDownloader` capability — the Lidarr adapter **submits** an album to Lidarr and returns immediately; a background **reconciler** polls Lidarr, maps its state/progress onto the `DownloadJob`, and fires the **existing** scan + rematch when Lidarr imports.
3. **Honest per-track → album UX:** choosing Lidarr on a track maps the track → its album, monitors + searches that album in Lidarr, and discloses up front that the **whole album** will be acquired. The clicked track flips to owned via the normal scan/rematch.
4. **Default-downloader setting:** an admin setting so a normal download click can go straight to a chosen default (no popover), with a one-off override still available.
5. **Opt-in only:** Lidarr is never silently auto-selected by the fallback chain (so a playlist-import batch never grabs dozens of albums via Lidarr).

### Non-Goals (this sub-project)
- **A native acquisition pipeline** (Reverb directly orchestrating MusicBrainz + Prowlarr/Jackett + qBittorrent + import). This re-implements Lidarr's hard 80% (release scoring, file→release import-matching, failure handling) and is a separate, much larger project tied to the **own-library-engine** direction. Recorded as a deliberate future bet, not built here. The `AsyncDownloader` seam built now is exactly the machinery such a pipeline would reuse.
- **Single-track acquisition via Lidarr.** Lidarr acquires album *releases*; it cannot fetch one track. spotDL remains the surgical single-track tool.
- **Dynamic config pickers** (fetching Lidarr quality profiles / root folders into dropdowns). v1 uses manual IDs validated on `TestConnection`; dynamic pickers are deferred (the static `ConfigSchema` form doesn't support fetched options yet).
- **Album-match disambiguation UI.** v1 takes Lidarr's best ranked lookup match or fails with a clear message; a user-facing "which album did you mean?" picker is deferred.
- **Pausing Lidarr from Reverb.** The Phase-2-B "pause queue" gates only the spotDL worker lane; Lidarr runs its own queue. Out of scope to gate Lidarr.
- **Sonarr/Radarr or other *arr** — Lidarr only; the `AsyncDownloader` capability leaves room.

---

## 2. Locked Product Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Direct Lidarr vs native pipeline | **Integrate Lidarr** | Reuses the user's configured indexers/download-client/profiles; Reverb stays a thin, robust orchestrator. Native pipeline = build Lidarr; deferred. |
| Surface | **Per-track picker, opt-in, with honest album disclosure** | Keeps the unified one-control loop; transparency resolves the "album for one song" tension without a separate surface. |
| Async model | **`AsyncDownloader` capability + background reconciler** | Never pins a worker for hours; reusable seam; Lidarr jobs surface live in the B queue UI. |
| `CanDownload` | **Returns false (opt-in only)** | Lidarr never auto-picked by the fallback chain or batch/playlist imports. |
| Config | **Manual Lidarr IDs, validated on TestConnection** | Static `ConfigSchema`; dynamic pickers deferred. |
| Album resolution | **Best ranked Lidarr lookup match, else fail with a friendly error** | YAGNI; disambiguation UI deferred. |
| Cross-album dedup | **Rely on Lidarr idempotency** | Two tracks of one album → two Submits → Lidarr no-ops the duplicate add; both Reverb jobs reconcile on import. No special Reverb dedup. |
| Default downloader | **Admin setting + split-button override** | Avoids the popover nagging once two sources exist; override + "Always ask" keep flexibility. |

---

## 3. Architecture

### 3.1 The async-downloader capability (new, reusable)
A new optional interface in `internal/download`, detected at runtime via the existing
`registry.RegisterCapability` probe — **spotDL is unchanged** and remains a plain
synchronous `Downloader`:

```go
// AsyncDownloader is an optional capability: the adapter hands the request off to
// an external manager (e.g. Lidarr) and reports progress via polling, instead of
// blocking in Start. Detected via registry capability probe "async".
type AsyncDownloader interface {
    // Submit hands req off to the external system and returns a ref to track it.
    // Must NOT block on completion. Returns an error if the request can't be placed
    // (e.g. album not found) — the job then fails.
    Submit(ctx context.Context, req core.DownloadRequest) (ref string, err error)

    // Poll reports the current state of a submitted job. State == DownloadCompleted
    // means the files have been imported into the library folder (the Manager then
    // runs the normal scan + rematch). State == DownloadFailed carries Error.
    Poll(ctx context.Context, ref string) (AsyncStatus, error)

    // CancelAsync best-effort removes/abandons the external job.
    CancelAsync(ctx context.Context, ref string) error
}

type AsyncStatus struct {
    State    core.DownloadStatus // queued | running | completed | failed
    Progress int                 // 0-100, or -1 when unknown
    Error    string              // set when State == failed
}
```

Registered once at the composition root:
```go
registry.RegisterCapability("async", func(p registry.Plugin) bool { _, ok := p.(download.AsyncDownloader); return ok })
```

### 3.2 Manager: the async lane + reconciler
The Manager gains an **async path** parallel to its sync worker pool:
- **Enqueue:** after `pick`, if the chosen downloader implements `AsyncDownloader`, the Manager calls `Submit(req)` (quick), persists the returned `ref` on the job, sets the job `running`, publishes `download.progress`, and **does NOT enqueue it to the worker channel** — no worker is pinned. A `Submit` error fails the job.
- **Reconciler goroutine** (one, started with the Manager, modeled on the playlist-sync scheduler; context-cancellable for clean shutdown): every `ReconcileEvery` (~10s) it lists in-flight async jobs (status `running` with a non-empty `ref`), calls `Poll(ref)` on each, and:
  - updates `progress` + publishes `download.progress` when it changes;
  - on `Poll → completed` (Lidarr imported): sets `OutputPath` to the configured root folder, marks the job `completed`, publishes `download.complete`, and calls the **existing** `scheduleScan(jobID)` → the normal Navidrome scan + rematch flips the track to owned;
  - on `Poll → failed`: marks `failed` with the reason, publishes `download.failed`;
  - on `Poll` transport error: leaves the job untouched and retries next tick (Lidarr may be briefly unreachable).
- **Restart-safe:** jobs + refs are persisted; on startup the reconciler resumes polling in-flight async jobs (and the existing request rehydration applies). A generous **max-age** (e.g. 7 days, `AsyncMaxAge`) gives up on a stuck job → `failed`.
- **Cancel:** `Manager.Cancel` for an async job calls `CancelAsync(ref)` (best-effort) and marks it `canceled`.
- **Pause:** unchanged — the B pause gate only governs the sync worker lane; async/Lidarr jobs are not gated (Lidarr owns its queue).

### 3.3 Track → album → Lidarr (the Lidarr adapter's `Submit`)
Reverb holds Spotify metadata; Lidarr is MusicBrainz-keyed. `Submit`:
1. **Resolve the album:** call Lidarr **album lookup** (`GET /api/v1/album/lookup?term=<artist> <album>`) → take the **top-ranked result** (foreign album id + artist foreign id). **Empty results → return an error → job fails** ("couldn't find *album* in Lidarr").
2. **Ensure the artist exists, UNMONITORED:** if not already in Lidarr, **add** it (`POST /api/v1/artist`) with the configured `root_folder`, `quality_profile_id`, `metadata_profile_id`, **`monitored: false` and `addOptions.monitor: "none"`**. This is critical — adding an artist monitored would make Lidarr chase the artist's **entire discography**; we only ever want the requested album. Lidarr returns the existing artist if already present (idempotent); an already-monitored existing artist is left as the user configured it.
3. **Monitor ONLY the target album + search it:** set the resolved album `monitored: true` (`PUT /api/v1/album/monitor` with just that album id), then trigger an **album search for that album only** (`POST /api/v1/command {name:"AlbumSearch", albumIds:[id]}`). No other album is monitored or searched.
4. **Return ref** = the Lidarr album id (sufficient for `Poll`/`CancelAsync`).

`Poll(ref)` reads Lidarr's **queue** (`GET /api/v1/queue` — progress while downloading) and the **album import status** (`GET /api/v1/album/{id}` statistics `trackFileCount`/`trackCount`, and/or `GET /api/v1/history` import records). Mapping:

| Lidarr observation | `AsyncStatus.State` / Progress |
|---|---|
| album monitored, no queue item yet (searching) | `running`, progress `-1` |
| queue item downloading | `running`, progress = `100·(size−sizeleft)/size` |
| queue item importing / import pending | `running`, progress `100` |
| album fully imported (trackFileCount ≥ trackCount, or import history) | `completed` |
| search returned no release / queue failed / removed | `failed` (reason) |

`CancelAsync(ref)` removes any active queue item for the album (`DELETE /api/v1/queue/{id}`) and optionally unmonitors the album; best-effort.

### 3.4 Completion → owned (reuses everything)
Lidarr's `root_folder` **is the music folder Navidrome serves**. On import, the
Manager runs the same `scheduleScan → StartScan → waitForScan → rematch →
library_track_id` path spotDL uses. The clicked track flips to owned; the rest of
the album appears in the library via the scan (not tracked as jobs — expected).

### 3.5 Reuse ledger
- **Reused as-is:** the registry + capability seam, the Manager's scan-debounce + rematcher + EventBus + WS, the `DownloadJob` lifecycle + the B queue surfaces (`/downloads` page, `DownloadTray`, cancel/clear), the `DownloadPopover` picker, the `/adapters` config flow + admin form, the `/settings` store.
- **New:** the `AsyncDownloader` interface + capability probe; the Manager async lane + reconciler goroutine; the `lidarr` adapter (+ a small Lidarr HTTP client); the `downloader_ref` column; the `default_downloader` setting; the picker default/override + album disclosure.

---

## 4. The Lidarr adapter

`internal/download/lidarr/` — mirrors `internal/download/spotdl/`:
- `adapter.go`: `New()`, `Type() "downloader"`, `Name() "lidarr"`, `ConfigSchema()`, `Init(cfg)`, `TestConnection(ctx)`, `CanDownload() → false`, and the `AsyncDownloader` methods `Submit`/`Poll`/`CancelAsync`.
- `client.go`: a thin Lidarr REST client (base URL + `X-Api-Key`), with a `Doer`/`http.Client` seam injectable for tests (mirrors spotDL's `Runner` test seam).

**ConfigSchema fields:** `url` (string, required), `api_key` (string, secret, required), `root_folder` (string, required), `quality_profile_id` (number, required), `metadata_profile_id` (number, required). (No `monitor` field — the adapter always adds the artist **unmonitored** and monitors only the requested album, so a single-track request can never trigger a discography grab; see §3.3.)

**`TestConnection`:** `GET /api/v1/system/status` with the API key (validates URL + key); then verify `root_folder` is a known root folder and `quality_profile_id` exists, returning a friendly error **listing the valid values** when not (so the user can correct manual IDs without guessing).

**`CanDownload`:** returns `false` — Lidarr is invoked only when explicitly picked (`req.Downloader == "lidarr"`), never by the auto fallback chain or batch/playlist imports.

---

## 5. Default-downloader setting + picker UX

**Setting:** a `default_downloader` key in the existing settings store (DB-canonical
config), value = an enabled downloader name, or empty = **"Always ask"**. Surfaced in
the **admin Settings UI** as a "Default downloader" select (options: each enabled
downloader + "Always ask"). `PUT /settings` validates the value is empty or the name
of an enabled downloader.

**Picker behavior** (`DownloadAction` + `DownloadPopover`):
- **0 downloaders** → disabled (unchanged).
- **1 downloader** → click enqueues it directly (unchanged).
- **2+ downloaders:**
  - `default_downloader` set **and still enabled** → a normal click enqueues the default (no popover). A **split-button caret** next to the download control opens the popover to override the source for that one download.
  - `default_downloader` empty **or** the configured default no longer enabled → a normal click opens the popover (today's behavior; graceful fallback).
- **Lidarr album disclosure:** whenever Lidarr is the chosen downloader (default or explicitly picked), a confirmation discloses the album acquisition — "Lidarr fetches the whole album *[Album]*, not just this track. Continue?" — using the track's known album name. spotDL never shows this.

---

## 6. Data Model

### 6.1 New column (goose migration + sqlc)
```
ALTER TABLE download_jobs ADD COLUMN downloader_ref TEXT NOT NULL DEFAULT '';
```
A new sequential goose migration (latest applied is `0011` per the handoff — verify at
plan time; this is `0012`). `downloader_ref` persists the Lidarr handle so the
reconciler resumes after restart. `core.DownloadJob` gains `DownloaderRef string`;
`download_jobs.sql` queries that select/persist it (Insert + a dedicated
`UpdateDownloadJobRef`); `sqlStore` + the in-memory test store implement it.

### 6.2 Setting
`default_downloader` lives in the settings store (no schema change if settings are a
key/value table; otherwise follow the existing settings persistence). Read by the FE
via the existing settings API.

---

## 7. API Surface

**No new HTTP routes.** Lidarr is configured through the existing `/api/v1/adapters`
flow (it's a downloader adapter); the default is the existing `/api/v1/settings`;
downloads, cancel, clear, and the live WS events are the existing `/downloads` + ws
surfaces. The async reconciler publishes the **existing** `download.progress` /
`download.complete` / `download.failed` events, so the FE updates with no new wiring.
OpenAPI: document the new `default_downloader` setting field if the settings
schema is described there; otherwise no OpenAPI change.

**Backend pieces:** `internal/download/download.go` gains `AsyncDownloader` +
`AsyncStatus`; `internal/download/manager.go` gains the async enqueue branch +
reconciler + `CancelAsync` wiring + ref persistence; `internal/download/lidarr/`
(adapter + client); `cmd/reverb/main.go` registers `lidarr` + the `"async"` capability
probe; `internal/wiring` builds the Lidarr instance like spotDL; the settings
handler/validation gains `default_downloader`.

---

## 8. Frontend

- **Picker** (`DownloadAction.tsx` / `DownloadPopover.tsx`): implement the default +
  split-button override from §5; reads `default_downloader` from settings. The popover
  is reused for overrides. The "Lidarr" placeholder in the existing tests becomes real.
- **Album disclosure:** a small confirmation (reuse the existing modal/confirm idiom)
  shown before a Lidarr enqueue, naming the album.
- **Admin Settings UI:** add the "Default downloader" `Select` (enabled downloaders +
  "Always ask") wired to the settings API.
- **Admin Adapters UI:** unchanged — the Lidarr `ConfigSchema` fields render
  automatically in the generic adapter form (secret redaction for `api_key`).
- **Queue surfaces:** unchanged — Lidarr jobs are `DownloadJob`s, so they appear in the
  `/downloads` page + `DownloadTray` with live state/progress from the reconciler, and
  cancel/clear work via the existing controls. Per-row state uses the B "Queued vs
  Downloading" treatment driven by `job.progress`.
- **Design tokens only**; match existing component idioms.

---

## 9. Edge Cases
- **Lidarr unreachable on Submit** → job `failed`, friendly "couldn't reach Lidarr."
- **Album not found in Lidarr metadata** → job `failed`, "couldn't find *album* in Lidarr — add it there or refine."
- **No release found by indexers** → after search, no queue item materializes; the reconciler maps a Lidarr no-release/failed signal (or `AsyncMaxAge`) → `failed`, "Lidarr found no release."
- **Lidarr briefly unreachable during Poll** → job untouched, retried next tick (not failed on transient errors).
- **Restart mid-flight** → reconciler resumes from persisted `downloader_ref`.
- **Two tracks, same album** → both Submit; Lidarr no-ops the duplicate add; both jobs reconcile to `completed` on import.
- **Cancel an in-flight Lidarr job** → `CancelAsync` removes the queue item; job `canceled`.
- **Default downloader removed/disabled** → picker falls back to "Always ask."
- **No album on the request** (e.g. a single without album metadata) → `Submit` can't map → `failed` with a clear message (or `CanDownload`-style guard).
- **spotDL unaffected** throughout (no capability → plain sync path).

---

## 10. Testing
- **Go unit — Lidarr adapter** (fake Lidarr HTTP `Doer`): album lookup (match / no-match), artist add (new / already-exists idempotency), monitor + search command, `Poll` state mapping (searching → downloading%→ imported → completed; no-release → failed), `CancelAsync`, `TestConnection` (status ok; invalid root/profile → helpful error), `CanDownload` false.
- **Go unit — Manager async lane** (fake `AsyncDownloader`): async enqueue calls `Submit` and does **not** pin a worker (job goes `running` with a ref, queue channel untouched); reconciler advances progress, and on `completed` triggers `scheduleScan` (assert the scan/rematch path runs and `library_track_id` is set via the existing fakes); `Submit` error → `failed`; `Cancel` → `CancelAsync` + `canceled`; restart resume polls persisted refs.
- **Go unit — capability probe** detects `AsyncDownloader`; spotDL is not async.
- **Go unit — store**: `downloader_ref` round-trips; migration applies.
- **Go unit — settings**: `default_downloader` validation (empty ok; unknown/disabled name rejected).
- **FE component**: picker default → direct enqueue + split-button override opens popover; "Always ask" → popover; disabled-default fallback; **Lidarr album disclosure** shown before enqueue; Settings "Default downloader" select.
- **Gate**: repo root `go test ./... && go build ./... && go vet ./...`; `web/` `npx vitest run && npx tsc --noEmit && npm run build && npm run e2e` (**e2e stays 3/3**; no new e2e required — the existing flows must stay green).

---

## 11. Sequencing (for the plan)
1. **`AsyncDownloader` + `AsyncStatus`** in `download.go` + the `"async"` capability probe (TDD, probe test).
2. **`downloader_ref`** column (goose migration + sqlc) + `DownloadJob.DownloaderRef` + store methods (round-trip).
3. **Manager async lane**: async-aware `Enqueue` (Submit + ref + no worker), `Cancel` → `CancelAsync` (TDD with a fake `AsyncDownloader`).
4. **Manager reconciler** goroutine: poll in-flight async jobs → progress/state → `scheduleScan` on completion; restart-resume + `AsyncMaxAge` (TDD).
5. **Lidarr HTTP client** (`client.go`) against a fake `Doer` (lookup/add/monitor/search/queue/history/status).
6. **Lidarr adapter** (`adapter.go`): config/Init/TestConnection/CanDownload + Submit/Poll/CancelAsync (TDD).
7. **Wiring**: register `lidarr` at the composition root; build the instance in `internal/wiring`.
8. **`default_downloader` setting**: backend validation + persistence.
9. **FE**: picker default + split-button override + Lidarr album disclosure; Settings "Default downloader" select.
10. **e2e + edges + full gate green.**

Each step verified before the next; fresh implementer per task, review each, whole-branch review before merge to **local `main`**.
