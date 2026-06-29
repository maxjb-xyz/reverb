# Artist-Level "Request All" Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a user request an artist's whole discography in one action, fanning out into individual album requests through the existing request system.

**Architecture:** A backend `POST /requests/batch` endpoint creates one `kind:'album'` request per item (reusing the single-request create+approve/pending logic via an extracted `createOneRequest` helper, with server-side dedup). The Artist page gets a "Request all" button that sends the not-fully-owned discography entries. No new request kind/entity.

**Tech stack:** Go (request service + chi API) + React/TS (Artist page). Builds on the shipped album-request infra.

## Global Constraints

- No new request `kind` — "request all" creates `kind:'album'` requests, one per discography entry. Spec: `docs/superpowers/specs/2026-06-28-artist-request-all-design.md`.
- Server-side dedup per `(requested_by, source, external_id)` (existing `GetOpenRequestByItem`); already-open album → skipped, not re-created.
- The FE sends only NOT-fully-owned entries (`coverage.owned < coverage.total`, or unresolved → treat as not-owned).
- `POST /requests/batch` is in the `request`-capability route group (same as `POST /requests`). It's the future quota-enforcement seam (leave a TODO; quota is the NEXT feature, not this one).
- Design tokens only in FE. Every task gates green: `go test ./... && go build ./... && go vet ./...`; FE tasks also `npx vitest run && npx tsc --noEmit && npm run build`. Commit footer `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.

---

### Task 1: Backend — `createOneRequest` helper + `POST /requests/batch`

**Files:**
- Modify: `internal/api/requests.go` (extract `createOneRequest`; `handleCreateRequest` uses it; add `handleBatchCreateRequests`), `internal/api/server.go` (register the route in the `request`-gated group)
- Test: `internal/api/requests_test.go`

**Interfaces produced:**
- `(s *Server) createOneRequest(ctx context.Context, cu auth.CurrentUser, item core.RequestItem) (core.Request, bool, error)` — runs the existing single-request logic: `Create` (dedups) → if existed, return `(req, false, nil)` (created=false) → else if `cu.Has("auto_approve")`: enqueue `downloadReqFromItem(item, cu.ID)` to `s.downloads()` + `MarkApproved`, return `(req, true, nil)` → else `NotifyPending`, return `(req, true, nil)`. (Mirror the current `handleCreateRequest` body exactly; it now calls this helper for the single case.)
- `POST /api/v1/requests/batch` → `handleBatchCreateRequests`: decode `{ items []core.RequestItem }`; loop `createOneRequest` per item; tally `created` (true), `skipped` (existed=false), and log+skip per-item errors; respond `200 {created, skipped, requests: []}`.

- [ ] **Step 1 — failing tests** (`requests_test.go`): `POST /requests/batch` with 2 album items by an `auto_approve` user → both enqueued (fake manager records 2 enqueues, each `Granularity==album`), `created==2`; by a `request`-only user → 2 pending, 0 enqueues; a batch where one item duplicates an already-open album request → that one counted `skipped`, not re-enqueued; empty `items` → `{created:0,skipped:0}`. Also: the EXISTING single `POST /requests` tests still pass (the extraction didn't change behavior).
- [ ] **Step 2 — run, expect FAIL.**
- [ ] **Step 3 — implement:** extract `createOneRequest` from `handleCreateRequest` (the latter becomes: decode item → `req, _, err := s.createOneRequest(...)` → write the req/err); add `handleBatchCreateRequests`; register the route. Add a `// TODO(quota): enforce per-user request quota here (feature 2)` comment at the batch entry.
- [ ] **Step 4 — run, expect PASS;** full `go test ./...`.
- [ ] **Step 5 — gate green + commit.**

---

### Task 2: Frontend — Artist "Request all" + `postBatchRequest`

**Files:**
- Modify: `web/src/lib/requestApi.ts` (`postBatchRequest`), `web/src/routes/Artist.tsx` (the "Request all" button + disclosure)
- Test: `web/src/lib/requestApi.test.ts`, `web/src/routes/Artist.test.tsx`

**Interfaces consumed:** Task 1's `POST /requests/batch`. The Artist page has `detail.albums` (`DiscographyAlbum[]` — fields incl. `source`, the album id, `name`, `coverUrl`, `totalTracks`) and `coverage` (a map keyed by album id → `AlbumCoverage` with `owned`/`total`); the artist's name is on `detail` (read the file for the exact field, e.g. `detail.name`). Auth: `useAuthStore((s)=>s.can('request'))`.
**Interfaces produced:** `postBatchRequest(items: CreateRequestItem[]): Promise<{ created: number; skipped: number; requests: Request[] }>` (POSTs `/requests/batch` with `{items}`).

- [ ] **Step 1 — failing tests:** `requestApi.test.ts` — `postBatchRequest` POSTs `/api/v1/requests/batch` with `{items}`. `Artist.test.tsx` — a `can('request')` user sees a "Request all" control; it's hidden/disabled when every discography entry is fully owned; clicking → a disclosure ("Request all N albums by <artist> not in your library") → confirm calls `postBatchRequest` with one album item per NOT-fully-owned entry (`kind:'album'`, `source`, `externalId`=album id, `title`+`album`=name, `artist`, `coverUrl`, `trackCount`=totalTracks); a user without `request` does not see it.
- [ ] **Step 2 — run, expect FAIL.**
- [ ] **Step 3 — implement:** `postBatchRequest`; the Artist-header "Request all" button gated on `can('request')`; build the not-owned album items from `detail.albums` + `coverage` (entry not owned when `coverage[id]?.owned ?? 0` < `coverage[id]?.total ?? album.totalTracks`, or no coverage → not owned); disclosure → confirm → `postBatchRequest` → success toast ("Requested N albums" for auto_approve / "Requested N albums — pending approval" otherwise, from the response). Token-styled, mirror the Album "Request album" disclosure.
- [ ] **Step 4 — run vitest + tsc, expect PASS.**
- [ ] **Step 5 — gate green + commit.**

---

### Task 3: e2e + final gate

**Files:**
- Modify: `web/e2e/mocks.ts` (artist detail + coverage + `POST /requests/batch` mock), `web/e2e/*.spec.ts` (an artist request-all spec)
- Test: the hermetic Playwright suite

- [ ] **Step 1 — write the spec:** mock an artist with a discography (a couple of albums, at least one not-fully-owned) + the `/requests/batch` route; on the artist page, "Request all" → disclosure → confirm → assert the `POST /requests/batch` body carries album items (`kind:'album'`) for the not-owned albums → toast; (if the mock reflects it) the requested albums appear in `/requests`. Resilient selectors.
- [ ] **Step 2 — FULL gate:** `go test ./... && go build ./... && go vet ./...`; `cd web && npx vitest run && npx tsc --noEmit && npm run build && npm run e2e`. Report counts. (`playlist-sync.spec.ts` ERR_ABORTED is a known flake — re-run once if only that fails.)
- [ ] **Step 3 — commit.**

---

## Self-review notes
- **Spec coverage:** batch endpoint + createOneRequest extraction + dedup (T1), FE Request-all + not-owned filter + disclosure + toast (T2), e2e (T3). All spec sections mapped.
- **Type consistency:** `createOneRequest(ctx, cu, item) (Request, bool, error)`; `postBatchRequest(items) → {created, skipped, requests}`; album items are `kind:'album'`. Consistent.
- **Out of scope (unchanged):** request quotas (the TODO seam — feature 2); bulk-approve; an artist-request entity.
