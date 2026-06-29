# Request Quotas Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an admin cap the number of pending requests a single user can have, enforced at request creation (single + batch).

**Architecture:** A global `max_pending_requests_per_user` setting (0=unlimited). `createOneRequest` (the shared single+batch path) counts the caller's pending requests before creating a would-pend request and returns a quota sentinel when at the cap. The single handler maps it to 429; the batch tallies it as `quotaCapped`. `auto_approve` users never pend, so they're never capped.

**Tech stack:** Go (request service + settings + chi API) + React/TS (Admin settings + request toasts). Builds on the request system + the batch endpoint (feature 1).

## Global Constraints

- Quota = **max PENDING requests per user** (`status='pending'` count). Setting `max_pending_requests_per_user`, integer, **0/unset = unlimited**. Spec: `docs/superpowers/specs/2026-06-28-request-quotas-design.md`.
- `auto_approve` users are NEVER quota-checked (their requests don't pend) — the check applies only when the request will pend (caller lacks `auto_approve`).
- Enforced in the shared `createOneRequest` (so single `POST /requests` AND batch `POST /requests/batch` both respect it). DRY — one enforcement point.
- The setting is read/written via the existing `/settings` DTO, which is already `CapManageLibrary`-gated (server.go:253) — so the quota write is admin-gated for free. Don't add a new endpoint.
- Design tokens only in FE. Every task gates green: `go test ./... && go build ./... && go vet ./...`; FE tasks also `npx vitest run && npx tsc --noEmit && npm run build`. Commit footer `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.

---

### Task 1: Backend — quota setting + count + enforcement

**Files:**
- Create: query in `internal/store/queries/requests.sql` (`CountPendingRequestsByUser`) → `make generate`
- Modify: `internal/api/settings.go` (`maxPendingRequestsPerUser` in the DTO GET + PUT), `internal/api/requests.go` (`errQuotaReached` sentinel + the quota check in `createOneRequest`; 429 in `handleCreateRequest`; `quotaCapped` in `handleBatchCreateRequests`)
- Test: `internal/api/settings_test.go`, `internal/api/requests_test.go`, `internal/store` (count query test if a store test exists)

**Interfaces produced:**
- Setting key `max_pending_requests_per_user`; DTO field `maxPendingRequestsPerUser int` (0=unlimited).
- `CountPendingRequestsByUser(ctx, requestedBy) (int64, error)` — `SELECT COUNT(*) FROM requests WHERE requested_by = ? AND status = 'pending'`.
- `errQuotaReached` sentinel; `createOneRequest` returns it (without creating) when the request would pend AND the cap is exceeded.

- [ ] **Step 1 — failing tests:**
  - settings: `GET /settings` includes `maxPendingRequestsPerUser` (default 0 when unset); `PUT /settings {maxPendingRequestsPerUser: 3}` persists it (and is rejected for a non-`CapManageLibrary` user — the route is already gated; assert the value round-trips for an admin).
  - `createOneRequest`: with cap=2 and the user (no `auto_approve`) already having 2 pending → returns `errQuotaReached`, no new request created; with 1 pending → creates (pending); an `auto_approve` user with 5 pending → still creates (no quota check); cap=0 → never checked.
  - `handleCreateRequest`: at cap → HTTP 429 with the limit in the body; below → 200.
  - `handleBatchCreateRequests`: a non-`auto_approve` user with cap=2, 0 pending, batch of 4 distinct albums → 2 `created`, 2 `quotaCapped`; an `auto_approve` user batch of 4 → 4 created, 0 capped; dedup-hits don't consume quota.
- [ ] **Step 2 — run, expect FAIL.**
- [ ] **Step 3 — implement:** add the count query (`make generate`); add `maxPendingRequestsPerUser` to the settings DTO read (`handleGetSettings`) + write (`handlePutSettings`, parse non-negative int, mirror `libraryBackendMode`); add `errQuotaReached`; in `createOneRequest`, BEFORE `Create`: `if !cu.Has("auto_approve") { cap := <read max_pending_requests_per_user via GetSetting, parse int, 0 on err/empty>; if cap > 0 { n := CountPendingRequestsByUser(ctx, cu.ID); if int(n) >= cap { return core.Request{}, false, errQuotaReached } } }`. In `handleCreateRequest`, map `errQuotaReached` → `writeJSON(w, 429, {error, limit})`. In `handleBatchCreateRequests`, count `errQuotaReached` results as `quotaCapped` and add `quotaCapped` to the response struct.
- [ ] **Step 4 — run, expect PASS;** full `go test ./...`.
- [ ] **Step 5 — gate green + commit.**

---

### Task 2: Frontend — Admin quota field + quota toasts

**Files:**
- Modify: `web/src/lib/settingsApi.ts` (`maxPendingRequestsPerUser`), the Admin settings UI (a field near `web/src/components/admin/RegistrationSection.tsx` — or RegistrationSection itself), `web/src/components/download/DownloadAction.tsx` + `web/src/routes/Album.tsx` (429 quota toast), `web/src/routes/Artist.tsx` (batch `quotaCapped` in the toast), `web/src/lib/requestApi.ts` (`postBatchRequest` return type gains `quotaCapped`)
- Test: the touched units' tests

**Interfaces consumed:** Task 1's `maxPendingRequestsPerUser` setting + the 429 (with `limit`) + the batch `quotaCapped`.

- [ ] **Step 1 — failing tests:**
  - Admin: a "Max pending requests per user" number input (helper "0 = unlimited") renders in the Admin area, bound to `maxPendingRequestsPerUser`; changing it calls the settings update mutation.
  - `DownloadAction`/`Album`: a `postRequest` that rejects with a 429 (mock it carrying the server message/limit) shows that message as an error toast (not the generic "Couldn't file your request").
  - `Artist`: a `postBatchRequest` response with `quotaCapped: 2` → the toast notes it ("Requested N albums — 2 not requested (limit reached)").
- [ ] **Step 2 — run, expect FAIL.**
- [ ] **Step 3 — implement:** add `maxPendingRequestsPerUser` to `settingsApi` + the Admin field; `postBatchRequest` return type gains `quotaCapped: number`; map a 429 from `postRequest` to a toast using the server's message (read the error body's `error`/`limit`); the artist toast appends the `quotaCapped` note when > 0. Token-styled.
- [ ] **Step 4 — run vitest + tsc, expect PASS.**
- [ ] **Step 5 — gate green + commit.**

---

### Task 3: e2e + final gate

**Files:**
- Modify: `web/e2e/mocks.ts` (settings with a low quota; `/requests` + `/requests/batch` reflecting quota), a spec
- Test: the hermetic Playwright suite

- [ ] **Step 1 — write the spec:** mock a low `maxPendingRequestsPerUser` + a `request`-only user; either a single request at the cap → assert the quota error toast, OR an artist "Request all" beyond the cap → assert the toast notes some were capped (`quotaCapped`). (Pick the one that's cleanest to mock; assert the real toast.) Resilient selectors.
- [ ] **Step 2 — FULL gate:** `go test ./... && go build ./... && go vet ./...`; `cd web && npx vitest run && npx tsc --noEmit && npm run build && npm run e2e`. Report counts. (`playlist-sync.spec.ts` ERR_ABORTED is a known flake — re-run once if only that.)
- [ ] **Step 3 — commit.**

---

## Self-review notes
- **Spec coverage:** setting + count + enforcement in createOneRequest + 429 + batch quotaCapped (T1), Admin field + 429 toast + batch quotaCapped toast (T2), e2e (T3). All mapped.
- **Type consistency:** `max_pending_requests_per_user`/`maxPendingRequestsPerUser`; `CountPendingRequestsByUser`; `errQuotaReached`; batch response `{created, skipped, quotaCapped, requests}`. Consistent.
- **Out of scope (unchanged):** per-role quotas; rolling-window rate limits; capping auto_approve throughput.
