# Deferred Items Cleanup — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Clear the accumulated deferred backlog from the request-system + downloader-granularity features: move downloader ordering to Admin, fix album-job Retry, hydrate the request store on load, show the album track-count, and sweep the minor code/test nits.

**Architecture:** Independent fixes across existing files — no new subsystems. Each task is a focused, independently-testable change. These are understood fixes flagged during prior reviews (not new features), so the plan is the design.

**Tech stack:** Go (download manager, sqlc, chi) + React 19/TS (Zustand, TanStack Query, Playwright). Builds on the shipped request-system + granularity features.

## Global Constraints

- Downloader chain ordering is ADMIN/global config (like search-provider ordering, already in Admin) — it belongs in Admin → Providers, NOT personal Settings.
- The track-never-album guarantee, the granularity model (`SupportedGranularities`, `DownloaderEntry.Order`, `ResolveGranularityOrder`), and the request `kind`/`granularities` contract are all SHIPPED and must stay intact — these fixes don't alter them.
- Design tokens only in FE (no raw hex / `text-black` / `text-white`). Every task gates green: `go test ./... && go build ./... && go vet ./...`; FE tasks also `npx vitest run && npx tsc --noEmit && npm run build`. Commit footer `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.

---

### Task 1: Move downloader ordering from Settings → Admin

**Files:**
- Modify: `web/src/routes/Settings.tsx` (REMOVE the two-column Downloaders section), `web/src/routes/Admin.tsx` + `web/src/components/admin/AdapterSection.tsx` (ADD the two-column ordering to the Admin Downloaders section, drop the "Order in Settings → Downloaders" hint), `web/e2e/settings-downloaders.spec.ts` + `web/e2e/mocks.ts` (retarget the e2e to Admin)
- Test: `web/src/routes/Settings.test.tsx`, `web/src/components/admin/AdapterSection.test.tsx`, the e2e

**Interfaces:** consumes the adapter DTO's `granularities: Record<string,number>` (already present). The two-column reorder writes `config.granularities` via the existing `updateAdapter` mutation (same logic currently in Settings).

- [ ] **Step 1 — move the markup + logic:** lift the two-column Song|Album block + its independent per-chain reorder (the `moveInColumn` handler that swaps only the `[g]` order value and writes `config.granularities`) OUT of `Settings.tsx` and INTO the Admin Downloaders section (`Admin.tsx`/`AdapterSection.tsx`), placed below the downloader management list. Remove the now-moot "Order in Settings → Downloaders" hint there.
- [ ] **Step 2 — Settings tests:** assert `Settings.tsx` no longer renders a Downloaders/Song/Album section (it's gone). Admin tests: the Admin Downloaders section renders the Song + Album columns from `granularities`, each independently reorderable (mirror the assertions that were in `Settings.test.tsx`), and the "Order in Settings" hint is gone.
- [ ] **Step 3 — e2e:** retarget `settings-downloaders.spec.ts` to navigate to Admin → Providers (rename to `admin-downloaders.spec.ts` if clearer) and assert the two columns + reorder + the toggle-off case there. Keep the stateful adapter mock.
- [ ] **Step 4 — run FULL vitest + tsc + build + the e2e**, all green.
- [ ] **Step 5 — commit.**

---

### Task 2: Hydrate the request store on (re)connect

**Files:**
- Modify: `web/src/lib/realtimeWiring.ts` (the `onOpen()` resync at ~line 124)
- Test: `web/src/lib/realtimeWiring.test.ts`

**Interfaces consumed:** `getMyRequests()`, `getAllRequests('pending')` (`requestApi.ts`); `useRequestStore.getState().setMine/setQueue`; the auth store predicate (`useAuthStore.getState().can('request')` / `can('manage_requests')`).

- [ ] **Step 1 — failing test:** in `realtimeWiring.test.ts`, simulate a WS open with a user who `can('request')` → `getMyRequests` is fetched and its results land in the request store (`mine(userId)` non-empty); a user who also `can('manage_requests')` → `getAllRequests('pending')` is also fetched and `pending()` is populated; a user with neither → no request fetches. (Mock the api fns + the auth store; mirror the existing download-resync test.)
- [ ] **Step 2 — run, expect FAIL.**
- [ ] **Step 3 — implement:** in `onOpen()`, after the existing download-job resync, if `can('request')` call `getMyRequests().then(r => useRequestStore.getState().setMine(r))`; if `can('manage_requests')` call `getAllRequests('pending').then(r => useRequestStore.getState().setQueue(r))`. Swallow/log fetch errors (don't break the connection). This fixes the manager pending-badge + a track's "Requested" state being empty until `/requests` is visited.
- [ ] **Step 4 — run, expect PASS;** FULL vitest + tsc.
- [ ] **Step 5 — commit.**

---

### Task 3: "Album · N tracks" count on requests

**Files:**
- Create: `internal/store/migrations/0017_requests_track_count.sql`
- Modify: `internal/store/queries/requests.sql` (+ `make generate`), `internal/core/request.go` (`RequestItem` + `Request` gain `TrackCount`), `internal/request/service.go` (store + map), `web/src/lib/requestApi.ts` (`trackCount?`), `web/src/routes/Album.tsx` (post `trackCount` from `album.totalCount`), `web/src/routes/Requests.tsx` (render `Album · N tracks`)
- Test: `internal/request/service_test.go`, `web/src/routes/Requests.test.tsx`, `web/src/routes/Album.test.tsx`

**Interfaces produced:** `core.RequestItem.TrackCount int` + `core.Request.TrackCount int` (json `"trackCount,omitempty"`). Mirror the existing `cover_url`/`kind` plumbing exactly (migration `ADD COLUMN track_count INTEGER NOT NULL DEFAULT 0` + goose down; in CreateRequest + all SELECTs; service maps it).

- [ ] **Step 1 — failing tests:** backend Create→Get round-trips `TrackCount: 12`. FE: `Album.tsx` "Request album" posts `trackCount: album.totalCount`; a `/requests` row with `kind:'album'` + `trackCount: 12` renders "Album · 12 tracks", and one with `trackCount: 0`/absent renders just "Album".
- [ ] **Step 2 — run, expect FAIL.**
- [ ] **Step 3 — implement** the thread-through (mirror `cover_url` from migration 0015 / `kind` from 0016); `make generate`; the `/requests` cue shows the count when `kind==='album' && trackCount>0`.
- [ ] **Step 4 — run, expect PASS;** go gate + FE gate.
- [ ] **Step 5 — commit.**

---

### Task 4: Fix album-job Retry + cross-restart granularity

**Files:**
- Modify: `internal/download/manager.go` (`Retry`, and the `process()` `!haveReq` fallback)
- Test: `internal/download/manager_test.go`

**Interfaces consumed:** `m.asyncFor(name)` (returns the `AsyncDownloader` or nil); the reconciler lane; `core.DownloadRequest.Granularity`.

- [ ] **Step 1 — failing tests:**
  (a) `Retry` of a failed job whose downloader is ASYNC (e.g. a fake album `AsyncDownloader`) does NOT route it to the sync worker (which would call `Start` and re-fail); instead it re-submits via the async lane (`Submit` is called / the job goes `Running` with a ref and the reconciler picks it up). Assert via the fake async downloader's `Submit` being invoked (a counter) and the job not hitting `Start`.
  (b) The `process()` rehydration fallback (when the request isn't in `m.reqs`) preserves `Granularity` — seed a completed/queued album job with no `m.reqs` entry but `request_json` carrying `granularity:"album"`, and assert the rehydrated `core.DownloadRequest.Granularity == GranularityAlbum` (so the album timeout + `/album/` URL are used, not the track defaults).
- [ ] **Step 2 — run, expect FAIL.**
- [ ] **Step 3 — implement:** in `Retry`, branch on `m.asyncFor(job.DownloaderName) != nil` → dispatch the retried job to the async submit/reconciler path (mirror how `Enqueue`/the manager starts an async job) instead of the sync worker channel; sync jobs retry as today. In the `process()` `!haveReq` fallback, populate `Granularity` from the persisted `request_json` (it already carries it) rather than defaulting to track. Update the stale comments to match. Keep timeout/cancel/fallback logic otherwise unchanged.
- [ ] **Step 4 — run, expect PASS;** `go test ./... -race ./internal/download/...` clean (concurrency-sensitive); full `go test ./...`.
- [ ] **Step 5 — commit.**

---

### Task 5: Minor cleanups sweep

**Files (each a small, isolated fix):**
- `internal/store/queries/requests.sql` — `GetOpenRequestByItem` add `ORDER BY created_at DESC` before `LIMIT 1` (dedup determinism); `make generate`.
- `internal/download/manager.go` — `Stop()` guard: if the manager was never `Start()`ed (no workers/cancel), `Stop()` must not deadlock draining an unbuffered channel — add an `if` guard so `Stop()` without `Start()` is a safe no-op.
- `web/src/components/ui/Toaster.tsx` — render the `aria-live` region ALWAYS (even when empty) so the first toast is announced to screen readers (move the empty-check to conditionally render the toast children, not the live-region container).
- `web/src/components/download/DownloadAction.tsx` + `web/src/routes/Album.tsx` — on `postRequest` failure, surface an error toast (`useToastStore.getState().push("Couldn't file your request", "error")`) instead of a silent `console.error`.
- Test: the touched units' tests.

- [ ] **Step 1 — failing tests (where testable):** a request-store dedup test asserting `GetOpenRequestByItem` returns the NEWEST open request deterministically; a `Stop()`-without-`Start()` test that returns promptly (no deadlock); a Toaster test asserting the `aria-live` region is present when there are zero toasts; a DownloadAction/Album test asserting a rejected `postRequest` pushes an error toast.
- [ ] **Step 2 — run, expect FAIL.**
- [ ] **Step 3 — implement** each fix.
- [ ] **Step 4 — run go gate + FE gate, all green.**
- [ ] **Step 5 — commit.**

**Explicitly NOT in this sweep (consciously closed, not silently dropped):** `nullInt` storing `durationMs=0` as NULL (lossy only for a genuinely-0ms track — never real; changing `nullInt` broadly is riskier than the non-issue it fixes); tightening raw `err.Error()` in 500 JSON bodies repo-wide (a pre-existing codebase-wide pattern behind auth, not introduced by these features — out of scope for a targeted sweep).

---

## Self-review notes
- **Coverage:** ordering-move (T1), boot-hydration (T2), album count (T3), Retry-of-album + cross-restart granularity (T4), the real minor cleanups (T5, with two conscious skips). All the in-scope deferred items mapped.
- **Type consistency:** `core.RequestItem.TrackCount`/`core.Request.TrackCount` (json `trackCount`); FE `trackCount?: number`; the Retry async branch keys on `m.asyncFor(name) != nil`. Consistent.
- **The big future features (push-notifications for approvals, artist-level request-all) are NOT in scope** — they need their own brainstorm.
