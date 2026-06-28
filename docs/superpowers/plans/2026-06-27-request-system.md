# Request System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the `request` capability into a real workflow — a request-only user asks to add a track, it lands in a `manage_requests` approval queue, an approver accepts it, it enqueues on the existing download manager and appears in the library, and the requester is notified live.

**Architecture:** A request layer sits *in front* of the download manager. Every user-facing add → `POST /requests`; an `auto_approve` user's request is approved + enqueued instantly, a `request`-only user's stays pending. A focused `internal/request` service manages request rows and publishes targeted events; a tracker listens to the download manager's existing complete/failed events to close the fulfillment loop. The API handlers (which hold the live, reload-swapped download manager) orchestrate enqueue; the service stays manager-agnostic. The broadcast WebSocket gains a per-user/per-capability filter for the two new request topics.

**Tech Stack:** Go (chi, modernc sqlite, goose, sqlc, EventBus, coder/websocket), React 19 + TS (Zustand, TanStack Query, Tailwind tokens, Playwright).

## Global Constraints

- **New capability key:** `manage_requests` (exact). Label "Approve requests", description "Review and approve or deny other users' requests to add music." Seeded on `role-admin`. The other capability keys are unchanged: `is_admin`, `can_manage_users`, `can_manage_library`, `request`, `auto_approve`, `can_create_playlists`.
- **Request statuses (exact):** `pending`, `approved`, `denied`, `fulfilled`, `failed`.
- **EventBus topics (exact, new):** `request.created` (manager-targeted: a new pending request), `request.updated` (requester-targeted: status changed). Payloads carry the routing target (see Task 3/6).
- **Unified flow:** user-facing add → `POST /requests` (gated `request`). `auto_approve` → approve+enqueue immediately (self-approved, no manager event); `request`-only → pending + `request.created`; neither → 403. The existing `POST /downloads*` is unchanged (queue ops + batch/system contexts).
- **Download attribution on approval:** `core.DownloadRequest.InitiatedBy` = the request's `requested_by` (the original requester), NOT the approver.
- **Dedup:** `POST /requests` is idempotent per `(requested_by, source, external_id)` while a request is open (pending or approved) — return the existing one.
- **Track-level only (v1):** one request = one track. No album "request all" (a §8 follow-up).
- **Generated code:** sqlc owns `internal/store/db/*.sql.go` — edit `.sql` + `make gen`, never hand-edit. New goose migration `0014_requests.sql`; never edit `0001`–`0013`.
- **No download-manager changes:** the tracker is a read-only listener on existing events; the manager/queue internals are untouched.
- **Design tokens only** (FE): no raw hex / `text-black`/`text-white`; `text-error`/`text-success`.
- **Commit footer:** end every commit with `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.
- **Gate:** root `go test ./... && go build ./... && go vet ./...`; `web/` `npx vitest run && npx tsc --noEmit && npm run build && npm run e2e`.

---

## Phase 1 — Backend

### Task 1: `manage_requests` capability

**Files:** Modify `internal/auth/capabilities.go`, `internal/auth/capabilities_test.go`, `internal/auth/auth.go` (EnsureSeed), `internal/auth/seed_test.go`

**Interfaces:**
- Produces: const `CapManageRequests = "manage_requests"`; it appears in `AllCapabilities()` (now 7) with the exact label/description; `role-admin`'s seed includes it; `EnsureSeed` backfills it onto an existing `role-admin`.

- [ ] **Step 1: Update the registry test** — in `capabilities_test.go`, change the count to **7** and add `CapManageRequests` to the `want` map; assert `IsCapability("manage_requests")`.
- [ ] **Step 2: Run → FAIL** (`go test ./internal/auth/ -run TestAllCapabilities -v`).
- [ ] **Step 3: Implement** in `capabilities.go`:
  - add `CapManageRequests = "manage_requests"` to the const block;
  - add to `AllCapabilities()` (place it right after `CapAutoApprove`, before `CapCreatePlaylists`): `{CapManageRequests, "Approve requests", "Review and approve or deny other users' requests to add music."}`;
  - add `CapManageRequests` to the `role-admin` entry in `DefaultSystemRoles()` (Admin = all 7).
- [ ] **Step 4: EnsureSeed backfill** — in `auth.go` `EnsureSeed`'s remap loop, extend the role-specific backfill: for `r.ID == "role-admin"`, if its caps lack `CapManageRequests`, append it + mark changed (mirror the existing `role-requester` `CapCreatePlaylists` backfill).
- [ ] **Step 5: Seed test** — in `seed_test.go`, add a test: an existing `role-admin` seeded WITHOUT `manage_requests` gains it after `EnsureSeed` (and is idempotent on a 2nd run).
- [ ] **Step 6: Run + commit** — `go test ./internal/auth/... && go build ./...`.
```bash
git add internal/auth/
git commit -m "feat(auth): add manage_requests capability, seeded on Admin"
```

---

### Task 2: requests schema + queries

**Files:** Create `internal/store/migrations/0014_requests.sql`, `internal/store/queries/requests.sql`; regen `internal/store/db/*`; Test: append to `internal/store/store_test.go`

**Interfaces:**
- Produces (sqlc): `CreateRequest`, `GetRequest`, `GetOpenRequestByItem` (by requested_by+source+external_id where status in pending/approved), `GetRequestByDownloadJob`, `ListRequestsForOwner`, `ListRequests` (optional status filter — use two queries: `ListRequests` all + `ListRequestsByStatus`), `UpdateRequestStatus` (status, decided_by, decided_at, download_job_id, deny_reason), `DeleteRequest`.

- [ ] **Step 1: Migration** — `internal/store/migrations/0014_requests.sql`:
```sql
-- +goose Up
CREATE TABLE requests (
    id              TEXT PRIMARY KEY,
    requested_by    TEXT NOT NULL REFERENCES users(id),
    source          TEXT NOT NULL,
    external_id     TEXT NOT NULL,
    title           TEXT NOT NULL,
    artist          TEXT NOT NULL,
    album           TEXT,
    isrc            TEXT,
    duration_ms     INTEGER,
    cover_art_id    TEXT,
    status          TEXT NOT NULL,
    created_at      INTEGER NOT NULL DEFAULT (unixepoch()),
    decided_by      TEXT REFERENCES users(id),
    decided_at      INTEGER,
    download_job_id TEXT,
    deny_reason     TEXT
);
CREATE INDEX idx_requests_status ON requests(status);
CREATE INDEX idx_requests_requested_by ON requests(requested_by);
-- +goose Down
DROP TABLE requests;
```
- [ ] **Step 2: Queries** — `internal/store/queries/requests.sql` with the named queries above. Key ones:
```sql
-- name: CreateRequest :exec
INSERT INTO requests (id, requested_by, source, external_id, title, artist, album, isrc, duration_ms, cover_art_id, status)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetRequest :one
SELECT * FROM requests WHERE id = ?;

-- name: GetOpenRequestByItem :one
SELECT * FROM requests WHERE requested_by = ? AND source = ? AND external_id = ? AND status IN ('pending','approved') LIMIT 1;

-- name: GetRequestByDownloadJob :one
SELECT * FROM requests WHERE download_job_id = ?;

-- name: ListRequestsForOwner :many
SELECT * FROM requests WHERE requested_by = ? ORDER BY created_at DESC;

-- name: ListRequests :many
SELECT * FROM requests ORDER BY created_at DESC;

-- name: ListRequestsByStatus :many
SELECT * FROM requests WHERE status = ? ORDER BY created_at DESC;

-- name: UpdateRequestStatus :exec
UPDATE requests SET status = ?, decided_by = ?, decided_at = ?, download_job_id = ?, deny_reason = ? WHERE id = ?;

-- name: DeleteRequest :exec
DELETE FROM requests WHERE id = ?;
```
- [ ] **Step 3: `make gen && go build ./...`** (regenerates `internal/store/db/requests.sql.go` + models).
- [ ] **Step 4: Round-trip test** (`store_test.go`): create a user + role (reuse the existing helper that seeds), insert a request, `GetOpenRequestByItem` finds it, `ListRequestsForOwner` returns it.
- [ ] **Step 5: Run + commit** — `go test ./internal/store/ -run Request`.
```bash
git add internal/store/
git commit -m "feat(store): requests table + queries"
```

---

### Task 3: core types + Request service

**Files:** Create `internal/core/request.go`, `internal/request/service.go`, `internal/request/service_test.go`

**Interfaces:**
- Consumes: Task 2 queries (a `Querier` interface), the EventBus (`events.Publisher`-like — define a minimal `Publisher interface { Publish(events.Event) }`).
- Produces:
  - `core.Request` struct (json-tagged: id, requestedBy, source, externalId, title, artist, album, isrc, durationMs, coverArtId, status, createdAt, decidedBy, decidedAt, downloadJobId, denyReason) + `core.RequestItem` (the add descriptor: source, externalId, title, artist, album, isrc, durationMs, coverArtId) + status consts `RequestPending/Approved/Denied/Fulfilled/Failed = "pending"/...`.
  - `core.RequestEvent { Request core.Request; TargetUserID string; ForManagers bool }` (the WS payload + routing).
  - `request.TopicCreated = "request.created"`, `request.TopicUpdated = "request.updated"`.
  - `request.Service` with:
    - `Create(ctx, requestedBy string, item core.RequestItem) (req core.Request, existed bool, err error)` — dedup via `GetOpenRequestByItem`; else insert `pending`. Does NOT publish (the handler decides next).
    - `NotifyPending(ctx, req core.Request)` — publish `request.created` with `ForManagers:true`.
    - `MarkApproved(ctx, id, approverID, downloadJobID string) (core.Request, error)` — require current status `pending` (else `ErrNotPending`); set status `approved`, decided_by/at, download_job_id; publish `request.updated` targeted at `requested_by`. Returns updated.
    - `Deny(ctx, id, approverID, reason string) (core.Request, error)` — require pending; status `denied`; publish updated.
    - `Cancel(ctx, id, requesterID string) error` — require own + pending (else `ErrForbidden`); `DeleteRequest`.
    - `MarkFulfilled(ctx, id string)` / `MarkFailed(ctx, id, errMsg string)` — used by the tracker; set status + publish updated.
    - `Get(ctx, id) (core.Request, error)`, `ListForOwner(ctx, userID)`, `ListAll(ctx, statusFilter string)` (empty → all), `GetByDownloadJob(ctx, jobID) (core.Request, error)`.
  - Errors: `ErrNotPending`, `ErrForbidden`, `ErrNotFound`.

- [ ] **Step 1: Write failing tests** (`service_test.go`) using a migrated store + seeded user (mirror `internal/auth` test harness — open store, Migrate, seed a user via `db.Queries`). Cover: Create inserts pending; Create again for same item returns `existed=true` + same id; MarkApproved (pending→approved, sets job id, publishes a `request.updated` whose payload `RequestEvent.TargetUserID == requestedBy`); MarkApproved on a non-pending → `ErrNotPending`; Deny; Cancel own-pending deletes; Cancel other's → `ErrForbidden`; MarkFulfilled flips + publishes. Use a fake `Publisher` capturing events.
- [ ] **Step 2: Run → FAIL.**
- [ ] **Step 3: Implement** `core/request.go` (types) + `request/service.go` (the service over the Querier + Publisher). Map DB rows ↔ `core.Request` (nullable cols → pointers/empty). Publish helper builds `events.Event{Topic: TopicUpdated, Payload: core.RequestEvent{Request: r, TargetUserID: r.RequestedBy}}`.
- [ ] **Step 4: Run → PASS** (`go test ./internal/request/ -v`).
- [ ] **Step 5: Commit.**
```bash
git add internal/core/request.go internal/request/
git commit -m "feat(request): core types + request service (create/approve/deny/cancel/fulfill)"
```

---

### Task 4: fulfillment tracker

**Files:** Create `internal/request/tracker.go`, `internal/request/tracker_test.go`

**Interfaces:**
- Consumes: the Service (`GetByDownloadJob`, `MarkFulfilled`, `MarkFailed`), the EventBus `Subscribe(topic) (<-chan events.Event, func())`, `download.TopicComplete`/`download.TopicFailed`, `core.DownloadEvent{JobID,...}`.
- Produces: `request.Tracker` with `Start(ctx)` (subscribes to the two topics, runs a goroutine: on a `download.complete` event whose `DownloadEvent.JobID` matches a request's `download_job_id` → `MarkFulfilled`; on `download.failed` → `MarkFailed`; ignore jobs with no matching request) and a `Stop()`/ctx-cancel.

- [ ] **Step 1: Failing test** — construct a Service with a request in `approved` state linked to job "j1"; build a real `events.Bus`; start the Tracker; `bus.Publish(events.Event{Topic: download.TopicComplete, Payload: core.DownloadEvent{JobID:"j1", Status: core.StatusComplete}})`; assert (poll briefly) the request flips to `fulfilled`. A `download.failed` for "j1" → `failed`. An event for an unknown job → no change, no panic.
- [ ] **Step 2: Run → FAIL.**
- [ ] **Step 3: Implement** the tracker. Subscribe to both topics, merge, and in a goroutine handle each event: type-assert `core.DownloadEvent`; `GetByDownloadJob(JobID)` (ignore `ErrNotFound`); call MarkFulfilled/MarkFailed. Respect `ctx.Done()` + unsubscribe.
- [ ] **Step 4: Run → PASS** (`go test ./internal/request/ -run Tracker -v`; consider `-race`).
- [ ] **Step 5: Commit.**
```bash
git add internal/request/tracker.go internal/request/tracker_test.go
git commit -m "feat(request): fulfillment tracker (download complete/failed → request status)"
```

---

### Task 5: API handlers + routes + wiring

**Files:** Create `internal/api/requests.go`, `internal/api/requests_test.go`; Modify `internal/api/server.go` (Deps + routes), `cmd/reverb/main.go` (construct service + tracker, wire Deps, start tracker)

**Interfaces:**
- Consumes: Task 3 service, Task 1 caps, `s.downloads()` (live manager), `currentUser(r)`.
- Produces: `Deps.Requests *request.Service` (+ a started tracker in main); handlers + routes per the spec §6.

- [ ] **Step 1: Failing tests** (`requests_test.go`, using the existing api test harness + a fake/real download manager like `downloads_test.go`):
  - request-only user (role `role-requester`, has `request` not `auto_approve`) `POST /requests` → 200, request `status:"pending"`, and the queue (`GET /requests` as admin) shows it; the download manager was NOT called.
  - owner (auto_approve) `POST /requests` → request `status:"approved"` and the download manager's `Enqueue` WAS called with `InitiatedBy == owner.ID`.
  - dedup: same user posts the same item twice → second returns the same id, only one row.
  - `GET /requests/mine` returns the caller's own.
  - admin `POST /requests/{id}/approve` on a pending → `approved`, Enqueue called with `InitiatedBy == requester.ID`; on an already-approved → 409.
  - `POST /requests/{id}/deny` → `denied`.
  - `POST /requests/{id}/cancel` by the owner of a pending → 200 + gone; by another user → 403.
  - capability gates: a plain `request` user → 403 on `GET /requests` and approve/deny.
- [ ] **Step 2: Run → FAIL.**
- [ ] **Step 3: Implement handlers** (`requests.go`):
  - `handleCreateRequest`: decode `{source,externalId,title,artist,album,isrc,durationMs,coverArtId}`; `cu := currentUser(r)`; `req, existed, err := s.deps.Requests.Create(ctx, cu.ID, item)`; if `existed` → return it. Else if `cu.Has(auth.CapAutoApprove)` → `dl := s.downloads()` (503 if nil), `job, err := dl.Enqueue(ctx, downloadReqFromItem(item, cu.ID))`, `req, err = s.deps.Requests.MarkApproved(ctx, req.ID, cu.ID, job.ID)`. Else → `s.deps.Requests.NotifyPending(ctx, req)`. Return `req` (200).
  - `handleListMyRequests` → `ListForOwner(cu.ID)`.
  - `handleListRequests` → `ListAll(r.URL.Query().Get("status"))`.
  - `handleApproveRequest`: `req, err := s.deps.Requests.Get(id)` (404); `job, err := s.downloads().Enqueue(ctx, downloadReqFromRequest(req, req.RequestedBy))`; `req, err = s.deps.Requests.MarkApproved(ctx, id, cu.ID, job.ID)` (map `ErrNotPending`→409); return.
  - `handleDenyRequest`: decode `{reason?}`; `Deny` (map `ErrNotPending`→409).
  - `handleCancelRequest`: `Cancel(id, cu.ID)` (map `ErrForbidden`→403, `ErrNotFound`→404).
  - Helper `downloadReqFromItem`/`downloadReqFromRequest` builds `core.DownloadRequest{Source,ExternalID,Title,Artist,Album,ISRC,DurationMs,InitiatedBy}`.
- [ ] **Step 4: Routes** (`server.go`, protected group):
```go
pr.Group(func(rr chi.Router) {
    rr.Use(s.requireCapability(auth.CapRequest))
    rr.Post("/requests", s.handleCreateRequest)
    rr.Get("/requests/mine", s.handleListMyRequests)
    rr.Post("/requests/{id}/cancel", s.handleCancelRequest)
})
pr.Group(func(mr chi.Router) {
    mr.Use(s.requireCapability(auth.CapManageRequests))
    mr.Get("/requests", s.handleListRequests)
    mr.Post("/requests/{id}/approve", s.handleApproveRequest)
    mr.Post("/requests/{id}/deny", s.handleDenyRequest)
})
```
  Add `Requests *request.Service` to `Deps`.
- [ ] **Step 5: Wiring** (`cmd/reverb/main.go`): after `bus` + the store, `reqSvc := request.NewService(st.Q(), bus, time.Now)`; `tracker := request.NewTracker(reqSvc, bus)`; `tracker.Start(ctx)` (+ `defer tracker.Stop()`); `deps.Requests = reqSvc`. (The tracker uses the stable `bus`; it survives manager reloads.)
- [ ] **Step 6: Run + full gate + commit** — `go test ./... && go build ./... && go vet ./...`.
```bash
git add internal/api/requests.go internal/api/requests_test.go internal/api/server.go cmd/reverb/main.go
git commit -m "feat(api): request endpoints + wiring (create/list/approve/deny/cancel)"
```

---

### Task 6: per-user WebSocket filter

**Files:** Modify `internal/api/ws.go`, `internal/api/ws_test.go`

**Interfaces:**
- Consumes: `currentUser(r)` (the connected user + caps), `request.TopicCreated/TopicUpdated`, `core.RequestEvent`.
- Produces: the WS forwards `request.updated` only to the connection whose `cu.ID == ev payload TargetUserID`, and `request.created` only to connections where `cu.Has(manage_requests)`. Non-request topics forward unchanged.

- [ ] **Step 1: Failing test** — a focused unit test of the filter decision: a helper `wsShouldForward(cu auth.CurrentUser, ev events.Event) bool` returns false for a `request.updated` whose `RequestEvent.TargetUserID != cu.ID`, true when equal; false for `request.created` when `!cu.Has(manage_requests)`, true when has it; true for any non-request topic. (Test the helper directly — it's pure.)
- [ ] **Step 2: Run → FAIL.**
- [ ] **Step 3: Implement** — add `request.TopicCreated`, `request.TopicUpdated` to `wsTopics`; extract `wsShouldForward(cu, ev)` (pure function); in `handleWS`, capture `cu, _ := currentUser(r)` at the top, and in the write loop skip events where `!wsShouldForward(cu, ev)`.
- [ ] **Step 4: Run → PASS + commit.**
```bash
git add internal/api/ws.go internal/api/ws_test.go
git commit -m "feat(api): per-user/per-capability WS filter for request events"
```

---

## Phase 2 — Frontend

> All FE tasks: design tokens only; run `npx vitest run` (touched) + `npx tsc --noEmit`; commit per task. The e2e is restored/extended in Task 11.

### Task 7: request API client + store

**Files:** Create `web/src/lib/requestApi.ts`, `web/src/lib/requestApi.test.ts`

**Interfaces:**
- Produces: `RequestStatus` type; `Request` type (mirrors `core.Request` JSON); async `postRequest(item)`, `getMyRequests()`, `getAllRequests(status?)`, `approveRequest(id)`, `denyRequest(id, reason?)`, `cancelRequest(id)` (via the `api` wrapper); a Zustand `useRequestStore` keyed by request id with `upsert(req)`, `setMine(reqs)`, `setQueue(reqs)`, and selectors `mine()`, `pending()`. `applyRequestEvent(payload)` upserts from a `request.updated`/`request.created` WS payload.

- [ ] **Step 1: Failing test** — `postRequest` POSTs `/api/v1/requests` with the item body; `useRequestStore.applyRequestEvent({request})` upserts; `mine()`/`pending()` selectors filter. (Mirror `downloadApi.test.ts`/`downloadStore.test.ts` patterns.)
- [ ] **Step 2–4: Implement + pass.** Match `downloadApi.ts` + `downloadStore.ts` style.
- [ ] **Step 5: Commit** — `feat(web): request api client + store`.

---

### Task 8: "Request" affordance in DownloadAction

**Files:** Modify `web/src/components/download/DownloadAction.tsx`, `web/src/components/download/DownloadAction.test.tsx`

**Interfaces:** Consumes Task 7 (`postRequest`, `useRequestStore`), `useAuthStore.can`.

- [ ] **Step 1: Failing tests** — a user with `request` but not `auto_approve`, on a not-in-library result, renders a **"Request"** button (not the download control); clicking it calls `postRequest` and the control shows **"Requested"** (disabled). A user with `auto_approve` still renders the instant-add control (unchanged). A user with neither renders no add control. In-library → Play (unchanged).
- [ ] **Step 2: Run → FAIL.**
- [ ] **Step 3: Implement** — in `DownloadAction.tsx`, the current gate hides the add control when `!can('auto_approve')`. Replace with: if `can('auto_approve')` → existing instant-add control; else if `can('request')` → a Request button whose state reflects the matching request from `useRequestStore` (none → "Request"; pending/approved → "Requested"; fulfilled → falls to the in-library/normal state); else → null. Keep the in-library Play path first.
- [ ] **Step 4: Run → PASS + commit** — `feat(web): Request button for request-only users in DownloadAction`.

---

### Task 9: `/requests` page (My Requests + Approval tab)

**Files:** Create `web/src/routes/Requests.tsx`, `web/src/routes/Requests.test.tsx`; Modify `web/src/App.tsx` (route), the nav (`web/src/components/shell/TopBar.tsx` account menu or `LibraryRail` — match where `/downloads` is linked), with a manager pending-count badge.

**Interfaces:** Consumes Task 7 (store + api), `useAuthStore.can`.

- [ ] **Step 1: Failing test** — render `Requests` for a `request` user → "My Requests" list (mocked store) with status chips; render for a `manage_requests` user → an "Approval" tab listing pending with Approve/Deny that call `approveRequest`/`denyRequest`; the Approval tab is absent for a non-manager. Nav entry shown for anyone with `request`.
- [ ] **Step 2–4: Implement** — a tabbed page (My Requests default; Approval tab gated on `can('manage_requests')`), hydrated from `getMyRequests()` (+ `getAllRequests('pending')` for managers) via TanStack Query, reading live updates from `useRequestStore`. Status chips use tokens (`text-success`/`text-error`/muted). Add the `/requests` route inside the authenticated AppShell group, a nav entry (gated on `can('request')`), and a pending-count badge (manager-only). Match `/downloads` page density.
- [ ] **Step 5: Commit** — `feat(web): /requests page (my requests + approval queue)`.

---

### Task 10: WS wiring + toasts

**Files:** Modify `web/src/lib/realtimeWiring.ts`, `web/src/lib/realtimeWiring.test.ts` (+ the toast util the app already uses)

**Interfaces:** Consumes Task 7 (`useRequestStore.applyRequestEvent`), the existing WS envelope (`{type, payload}`).

- [ ] **Step 1: Failing test** — a WS frame `{type:'request.updated', payload:{request:{…status:'fulfilled'…}}}` calls `useRequestStore.applyRequestEvent` and triggers a toast ("Your request for <title> was added"); `request.created` updates the manager queue/badge. (Mirror the existing download WS wiring test.)
- [ ] **Step 2–4: Implement** — in `realtimeWiring.ts`, handle the two new envelope types: upsert into `useRequestStore` and, for `request.updated`, show a status toast (added/denied/failed copy). Reuse the existing toast mechanism.
- [ ] **Step 5: Commit** — `feat(web): live request updates + toasts over WS`.

---

### Task 11: end-to-end + full gate

**Files:** Create `web/e2e/requests.spec.ts`; Modify `web/e2e/mocks.ts` (mock the request endpoints + a `request.updated` WS frame)

- [ ] **Step 1: Write the e2e** (hermetic, mock-driven like the others): a `request`-only user (mock `/me` caps `['request']`) sees a **Request** button on a not-in-library result, clicks it (mock `POST /requests` → pending), and the `/requests` "My Requests" view shows it Pending; a mocked `request.updated` WS frame flips it to "Added" with a toast. A `manage_requests` user sees the Approval tab with a pending item and an Approve button. Keep assertions resilient (roles/test-ids).
- [ ] **Step 2: Run the full gate** — root `go test ./... && go build ./... && go vet ./...`; web `npx vitest run && npx tsc --noEmit && npm run build && npm run e2e`. All green; e2e count = prior + the new spec.
- [ ] **Step 3: Commit** — `test(e2e): request → approve → fulfilled flow`.
- [ ] **Step 4: Whole-branch review + merge** — run `superpowers:requesting-code-review` over the branch; fix findings; fast-forward `feat/request-system` → local `main`; tell the user to push + rebuild to verify on `soulkiller`.

---

## Self-Review

**Spec coverage (spec §→task):**
- §2 unified flow / capability / approver gate → Tasks 1, 5 ✓
- §3 architecture (service manager-agnostic, handler orchestrates enqueue, tracker listens) → Tasks 3, 4, 5 ✓
- §4 data model + lifecycle → Tasks 2, 3 ✓
- §5 service + fulfillment bridge → Tasks 3, 4 ✓
- §6 API surface (6 routes, caps) → Task 5 ✓
- §7 FE (Request affordance, /requests page, live) → Tasks 8, 9, 10 ✓
- §9 testing (service branches, dedup, approve attribution, tracker, gates, WS filter, FE, e2e) → distributed; e2e in 11 ✓
- §8 follow-ups — intentionally NOT built (album request-all, quotas) ✓
- Per-user WS filter (§3/§6) → Task 6 ✓

**Placeholder scan:** none — each step names exact files/symbols/SQL; handler bodies specify the exact branch + error mappings.

**Type consistency:** `core.Request`/`core.RequestItem`/`core.RequestEvent{Request,TargetUserID,ForManagers}` defined in Task 3 and consumed identically in Tasks 4/5/6/7; `request.TopicCreated/TopicUpdated` consistent across 3/6; statuses `pending/approved/denied/fulfilled/failed` verbatim throughout; `CapManageRequests="manage_requests"` consistent (Tasks 1/5/6/9); `InitiatedBy = requested_by` on approval stated in Global Constraints + Tasks 5.

**Build-coupling note:** all tasks are additive (new package, new table, new routes, new Deps field, new capability) — no existing API is removed — so each task builds and gates green independently. The only behavior change to an existing surface is `DownloadAction` (Task 8) routing the add through `/requests`, which is self-contained.
