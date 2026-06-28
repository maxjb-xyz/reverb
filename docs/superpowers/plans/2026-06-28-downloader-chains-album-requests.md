# Granularity-Aware Downloader Chains + Album Requests — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the per-add downloader picker with admin-ordered, granularity-aware downloader chains (song-level tried in order with failure-fallback; album-level separate), and extend the request system to support album requests.

**Architecture:** Add a `track`|`album` granularity to each downloader; `pick()` scopes selection to the chain matching the request's granularity; the worker falls back to the next in-chain downloader on failure. Delete the picker. Add a `kind` to requests so "Request album" flows through the existing request/approval/fulfillment machinery (one request ↔ one album job).

**Tech stack:** Go (chi, modernc sqlite, goose, sqlc, registry+EventBus) + React 19/TS (Vite, Zustand, TanStack Query, Tailwind tokens, Playwright). Builds on SP2 (request system).

## Global Constraints

- Granularity values are exactly `"track"` and `"album"` (a `core.DownloadGranularity` string type). spotDL = `track`, Lidarr = `album`. `core.DownloadRequest.Granularity` defaults to `track`.
- A `track` request must NEVER select an `album` downloader (the core guarantee — tested directly).
- `CanDownload` is a per-request filter applied *within* a chain, not a chain-membership gate (granularity decides the chain). Lidarr's old `CanDownload=false` exclusion hack is removed.
- The manual-`ManualURL` last-resort stays gated behind all-providers-exhausted, unchanged.
- Request `kind` values are exactly `"track"` and `"album"`; the `requests.kind` column defaults to `'track'`. Album requests reuse the existing request item fields (album id in `external_id`, album name in `title` AND `album`, artist, cover); `isrc`/`duration_ms` null. Dedup is per `(requested_by, source, external_id)`, unchanged.
- One album request ↔ exactly one album-chain job; the existing `request.Tracker` flips it on that job's `download.complete`/`download.failed`. No new fulfillment path.
- Capability gating unchanged: `request` to file/see a request, `auto_approve` to skip the Approval queue, `manage_requests` to approve.
- Design tokens only in FE (no raw hex / `text-black` / `text-white`). Every task gates green: `go test ./... && go build ./... && go vet ./...`; FE tasks also `npx vitest run && npx tsc --noEmit && npm run build`. Commit footer `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.

---

### Task 1: Downloader granularity foundation

**Files:**
- Modify: `internal/core/download.go` (add `DownloadGranularity` type + `Granularity` field on `DownloadRequest`)
- Modify: `internal/download/download.go` (add `Granularity()` to the `Downloader` interface)
- Modify: `internal/download/spotdl/adapter.go` (return `track`), `internal/download/lidarr/adapter.go` (return `album`; flip `CanDownload`→true)
- Modify: `internal/download/conformance.go` (assert every downloader declares a valid granularity)
- Test: `internal/download/conformance.go` runs for both adapters; `internal/download/lidarr/adapter_test.go`, `internal/download/spotdl/adapter_test.go`

**Interfaces produced:**
- `core.DownloadGranularity` (string); consts `core.GranularityTrack = "track"`, `core.GranularityAlbum = "album"`.
- `core.DownloadRequest.Granularity core.DownloadGranularity` (json `"granularity,omitempty"`; empty == track).
- `download.Downloader` gains `Granularity() core.DownloadGranularity`.

- [ ] **Step 1 — failing test:** in `spotdl/adapter_test.go` assert `a.Granularity() == core.GranularityTrack`; in `lidarr/adapter_test.go` assert `a.Granularity() == core.GranularityAlbum` AND that `CanDownload` now returns `(true, nil)` for an album-shaped req.
- [ ] **Step 2 — run, expect FAIL** (method undefined).
- [ ] **Step 3 — implement:** add the type + consts to `core/download.go`; add `Granularity()` to the interface; spotDL returns track, Lidarr returns album; change Lidarr `CanDownload` to `return true, nil` (granularity now keeps it out of the song chain). Add `Granularity` field to `DownloadRequest`.
- [ ] **Step 4 — conformance:** in `conformance.go` add a subtest asserting `d.Granularity()` is one of the two consts (so any future downloader must declare one).
- [ ] **Step 5 — gate green + commit.**

---

### Task 2: Granularity-scoped `pick()`

**Files:**
- Modify: `internal/download/manager.go:425` (`pick`), remove the `req.Downloader` explicit-name branch
- Test: `internal/download/manager_test.go`

**Interfaces consumed:** Task 1's `Granularity()` + `DownloadRequest.Granularity`.
**Interfaces produced:** `pick(ctx, req)` now returns the first downloader whose `Granularity() == req.Granularity` (defaulting empty→track) AND `CanDownload` is true, in slice order; error `"no <granularity> downloader can fetch ..."` when none.

- [ ] **Step 1 — failing tests** (`manager_test.go`): with a fake `track` downloader + a fake `album` downloader registered: a req with `Granularity: track` (or empty) selects the track one; `Granularity: album` selects the album one; order among two track downloaders is preserved; no downloader of that granularity → error.
- [ ] **Step 2 — run, expect FAIL.**
- [ ] **Step 3 — implement:** in `pick`, normalize `g := req.Granularity; if g == "" { g = core.GranularityTrack }`; delete the `if req.Downloader != ""` block; in the loop, `if d.Granularity() != g { continue }` before the `CanDownload` check.
- [ ] **Step 4 — run, expect PASS.**
- [ ] **Step 5 — commit.**

---

### Task 3: Failure-fallback in the worker (build the `FUTURE:` stub)

**Files:**
- Modify: `internal/download/manager.go` (the sync worker failure branch ~line 701, and add a `pickAfter` helper near `pick`)
- Test: `internal/download/manager_test.go`

**Interfaces produced:** `pickAfter(ctx, req, afterName string) (Downloader, error)` — the next downloader in the same granularity chain *after* `afterName` whose `CanDownload` accepts the req; error when exhausted.

- [ ] **Step 1 — failing test:** register two fake `track` downloaders `d1`(fails) `d2`(succeeds) in priority order; enqueue a track job; assert the job COMPLETES via `d2` (e.g. `d2` recorded the call / the job's final `DownloaderName == "d2"`). Second test: both fail → job ends `DownloadFailed`. Third: a single failing downloader → `DownloadFailed` and the `ManualURL` last-resort path is still reachable (unchanged).
- [ ] **Step 2 — run, expect FAIL** (single downloader fails straight to failed today).
- [ ] **Step 3 — implement:** add `pickAfter`. In the sync worker failure branch (the `default:` at ~701, before setting `DownloadFailed`): `next, err := m.pickAfter(ctx, req, cur.DownloaderName)`; if `next != nil`: set `cur.DownloaderName = next.Name()`, persist, and re-run the download with `next` (loop back into the Start attempt) rather than failing; only when `pickAfter` is exhausted fall through to `DownloadFailed` (then the existing `ManualURL` last-resort condition, untouched). Log each attempt. Keep async (Lidarr) on its reconciler lane — fallback applies to the sync (song-chain) lane only; note this in a comment.
- [ ] **Step 4 — run, expect PASS** (all three).
- [ ] **Step 5 — gate green + commit.**

---

### Task 4: Remove `default_downloader` + `req.Downloader` (backend)

**Files:**
- Modify: `internal/api/settings.go` (or wherever settings are read/written — remove `default_downloader`/`defaultDownloader`), `internal/core/download.go` (remove `Downloader` field), and any setter (`internal/api/requests.go`/`downloads.go` that set `req.Downloader`)
- Test: existing settings + downloads tests; add a grep-style guard test

**Interfaces consumed:** Task 2 (pick no longer reads `req.Downloader`).

- [ ] **Step 1 — failing test:** a test asserting the settings response no longer contains a `defaultDownloader` key (or that `GET /settings` omits it); and that enqueuing a track request with no downloader specified still routes through the song chain (already true post-Task-2, lock it in).
- [ ] **Step 2 — run, expect FAIL** (key still present).
- [ ] **Step 3 — implement:** remove the `default_downloader` setting read/write + its struct field; remove `core.DownloadRequest.Downloader` and any code that set it; run `make generate` if a sqlc settings query changes (likely not — settings are key/value).
- [ ] **Step 4 — `grep -rn "default_downloader\|\.Downloader " internal/ | grep -v _test` is empty** (except `DownloaderName` on the job, which stays). Full `go build ./...` clean.
- [ ] **Step 5 — gate green + commit.**

---

### Task 5: Remove the picker (frontend)

**Files:**
- Delete: `web/src/components/download/DownloadPopover.tsx` + `DownloadPopover.test.tsx`
- Modify: `web/src/components/download/DownloadAction.tsx` (remove the split-button/popover; a track add is a single button), `web/src/routes/Settings.tsx:69-82` (remove the "Default downloader" row + `defaultDownloader` from `settingsApi`), `web/src/components/download/parts.tsx` if it carries picker bits
- Test: `DownloadAction.test.tsx`, `Settings.test.tsx`

**Interfaces consumed:** Task 4 (no `defaultDownloader` setting, no `downloader` field on the add body).

- [ ] **Step 1 — failing/adjusted tests:** `DownloadAction.test.tsx` — an `auto_approve` user sees a SINGLE add/download button, no popover/split control; clicking it calls the add path with no `downloader` field. `Settings.test.tsx` — no "Default downloader" control rendered.
- [ ] **Step 2 — run, expect FAIL.**
- [ ] **Step 3 — implement:** delete `DownloadPopover`; simplify `DownloadAction` to a single button; drop `downloader`/`defaultDownloader` from the request/settings clients; remove the Settings row.
- [ ] **Step 4 — run vitest, expect PASS;** `npx tsc --noEmit` clean (no dangling imports).
- [ ] **Step 5 — gate green (vitest+tsc+build) + commit.**

---

### Task 6: Admin downloader ordering + granularity display

**Files:**
- Modify: `internal/registry/registry.go` consumers — register a capability probe so granularity is describable; `internal/download/*` registration; `internal/api/adapters.go` (granularity surfaces via `DescribeCapabilities`)
- Modify: `web/src/routes/Settings.tsx` (downloaders list: up/down reorder writing `priority`; show granularity read-only), `web/src/lib/adaptersApi.ts`
- Test: `internal/api/adapters_test.go`, `web/src/routes/Settings.test.tsx`

**Interfaces produced:** downloader adapter instances expose granularity to the FE (a capability string `"grain:album"` present iff the downloader is album-granularity; absence ⇒ track).

- [ ] **Step 1 — failing tests:** backend — `DescribeCapabilities` for the Lidarr plugin includes `"grain:album"` and spotDL does not (register a probe `RegisterCapability("grain:album", func(p) bool { d, ok := p.(download.Downloader); return ok && d.Granularity() == core.GranularityAlbum })`). FE — `Settings.test.tsx`: the downloaders list shows each downloader with a granularity label ("Album"/"Track") and up/down controls; clicking "up" on the second downloader PATCHes its `priority` below the first's.
- [ ] **Step 2 — run, expect FAIL.**
- [ ] **Step 3 — implement:** register the capability probe at downloader-registry setup; the FE reads `capabilities.includes('grain:album')` → "Album" else "Track"; render the priority-sorted downloader list with up/down buttons that swap priorities via the existing adapter-update mutation. (Reuse the existing adapters list/mutation in `Settings.tsx`; do NOT add drag-and-drop — up/down is sufficient, YAGNI.)
- [ ] **Step 4 — run both gates, expect PASS.**
- [ ] **Step 5 — gate green + commit.**

---

### Task 7: Request `kind` (schema + model + plumbing)

**Files:**
- Create: `internal/store/migrations/0016_requests_kind.sql`
- Modify: `internal/store/queries/requests.sql` (add `kind` to `CreateRequest` + all SELECTs), then `make generate`
- Modify: `internal/core/request.go` (add `Kind` to `RequestItem` + `Request`), `internal/request/service.go` (store + map `kind`), `internal/api/requests.go:171` `downloadReqFromItem` (set `Granularity` from `kind`)
- Test: `internal/request/service_test.go`, `internal/api/requests_test.go`

**Interfaces produced:** `core.RequestItem.Kind` + `core.Request.Kind` (string `"track"`|`"album"`, json `"kind,omitempty"`, default track). `downloadReqFromItem` maps `kind=="album"` → `DownloadRequest.Granularity = album` (with `Album` + `Title` set to the album name so Lidarr's `Submit` resolves it).

- [ ] **Step 1 — failing tests:** `service_test.go` — Create→Get round-trips `Kind: "album"`. `requests_test.go` — `downloadReqFromItem` of an album item yields `Granularity == core.GranularityAlbum` and a non-empty `Album`.
- [ ] **Step 2 — run, expect FAIL.**
- [ ] **Step 3 — implement** (mirror the existing `cover_url` plumbing added in `0015`): migration `ADD COLUMN kind TEXT NOT NULL DEFAULT 'track'` (+ goose down DROP); add `kind` to CreateRequest INSERT + every SELECT; `make generate`; add `Kind` to the structs; map it in `Create` (`Kind: item.Kind` defaulting empty→"track") and the row→core mapping; in `downloadReqFromItem`, if `item.Kind == "album"` set `Granularity: album` and ensure `Album`/`Title` carry the album name.
- [ ] **Step 4 — run, expect PASS;** `make generate` clean.
- [ ] **Step 5 — gate green + commit.**

---

### Task 8: Album request creation + fulfillment (backend wiring)

**Files:**
- Modify: `internal/api/requests.go` (`handleCreateRequest` — the album branch: `auto_approve` enqueues with `Granularity=album`; `request`-only → pending — both already flow through the existing branch once `downloadReqFromItem` sets granularity, so this is mostly verifying + a focused test), `internal/download/lidarr/adapter.go` (`Submit`/`CanDownload` confirmed to accept an album-granularity req — `Submit` already resolves by `req.Artist`+`req.Album`, so verify, don't rewrite)
- Test: `internal/api/requests_test.go`, `internal/request/tracker_test.go`

**Interfaces consumed:** Tasks 1, 2, 7.

- [ ] **Step 1 — failing tests:** `requests_test.go` — POST `/requests {kind:"album", source, externalId:<albumId>, title:<albumName>, artist}` by an `auto_approve` user enqueues a job whose `DownloaderName` is the album downloader (fake album downloader in the test) and `Granularity == album`; by a `request`-only user creates a `pending` request (no enqueue); dedup: a second identical album request returns the existing one. `tracker_test.go` — an album request linked to a job → `download.complete` flips it `fulfilled` (the tracker is kind-agnostic; assert it works for an album request).
- [ ] **Step 2 — run, expect FAIL.**
- [ ] **Step 3 — implement:** ensure `handleCreateRequest` passes the item through unchanged (granularity is carried by `downloadReqFromItem`); confirm the `auto_approve` enqueue path uses the album chain for `kind=album`; if Lidarr's `Submit` needs the album id, thread `ExternalID` (album id) but keep the existing artist+album-name lookup as the resolver. Add the focused tests.
- [ ] **Step 4 — run, expect PASS.**
- [ ] **Step 5 — gate green + commit.**

---

### Task 9: Frontend — `kind` + "Request album" action

**Files:**
- Modify: `web/src/lib/requestApi.ts` (add `kind?` to the post-item + `Request` types), `web/src/routes/Album.tsx` (a "Request album" button → `postRequest({kind:'album', source, externalId, title, artist, coverUrl})` with a "this fetches the whole album" disclosure), reuse the disclosure copy from the old per-track Lidarr modal
- Test: `web/src/routes/Album.test.tsx`, `web/src/lib/requestApi.test.ts`

**Interfaces consumed:** Task 7 (`kind` on the request), Task 8 (the album branch).

- [ ] **Step 1 — failing tests:** `Album.test.tsx` — for a user with `request`, a "Request album" control renders; clicking it shows the whole-album disclosure and, on confirm, calls `postRequest` with `kind:'album'` and the album's source/id/title/artist/cover. `requestApi.test.ts` — `postRequest` includes `kind` in the body when set.
- [ ] **Step 2 — run, expect FAIL.**
- [ ] **Step 3 — implement:** add `kind` to the FE types; add the album action + disclosure (token-styled, matching existing modals) to `Album.tsx`; gate it on `can('request')`.
- [ ] **Step 4 — run vitest + tsc, expect PASS.**
- [ ] **Step 5 — gate green + commit.**

---

### Task 10: Frontend — render album requests

**Files:**
- Modify: `web/src/routes/Requests.tsx` (My Requests rows + Approval rows show an "Album · ~N tracks" cue when `req.kind === 'album'`; N from the album track count if present, else just "Album")
- Test: `web/src/routes/Requests.test.tsx`

**Interfaces consumed:** Task 7 (`req.kind`).

- [ ] **Step 1 — failing tests:** `Requests.test.tsx` — a request with `kind:'album'` renders an "Album" cue in both the My-Requests row and the Approval-queue row; a `track` request does not.
- [ ] **Step 2 — run, expect FAIL.**
- [ ] **Step 3 — implement:** add the cue (token-styled badge/subtext) keyed on `req.kind`.
- [ ] **Step 4 — run vitest, expect PASS.**
- [ ] **Step 5 — gate green + commit.**

---

### Task 11: e2e + final gate

**Files:**
- Create/modify: `web/e2e/requests.spec.ts` (or a sibling) + `web/e2e/mocks.ts`
- Test: the hermetic Playwright suite

- [ ] **Step 1 — write the spec:** mock an `auto_approve` user; on an album page (mock the album endpoints), "Request album" → mock `POST /requests {kind:album}` → it appears in My Requests as an Album request; a mocked `request.updated` (fulfilled) flips it to Added. Second flow: a `manage_requests` user sees an album request in the Approval queue with the "Album" cue and Approve calls the approve endpoint. Also assert a normal track add still works (single button, no picker).
- [ ] **Step 2 — run the FULL gate:** `go test ./... && go build ./... && go vet ./...`; `cd web && npx vitest run && npx tsc --noEmit && npm run build && npm run e2e`. Report exact counts. (`playlist-sync.spec.ts` has a known intermittent flake — re-run once if only that fails.)
- [ ] **Step 3 — commit.**

---

## Self-review notes
- **Spec coverage:** granularity (T1), two chains + scoped pick (T2), fallback (T3), picker removal (T4 BE/T5 FE), admin order+granularity (T6), request kind (T7), album request create+fulfillment (T8), FE action (T9), FE render (T10), e2e (T11). All spec sections mapped.
- **Type consistency:** `core.DownloadGranularity` + consts `GranularityTrack/GranularityAlbum`; `DownloadRequest.Granularity`; `Downloader.Granularity()`; `RequestItem.Kind`/`Request.Kind` ("track"/"album"); `pickAfter`; capability `"grain:album"` — used identically across tasks.
- **Out of scope (unchanged from spec):** push-notifications for approvals, artist-level request-all, track→album auto-escalation, partial-ownership optimization, shipping a second real track downloader.
