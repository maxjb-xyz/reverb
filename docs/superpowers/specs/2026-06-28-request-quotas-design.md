# Request Quotas — Design

**Status:** Approved by "just do everything" (2026-06-28) — design by Claude, documented here. Feature 2 of 3 closing the SP2 backlog (after artist-request-all; before push-notifications-for-approvals).

**Goal:** Let an admin cap how many **pending** requests a single user can have at once, so a shared instance's approval queue can't be flooded by one user (incl. via "request all").

**Tech stack:** Go (request service + settings + chi API) + React/TS (Admin settings + request error toasts). Builds on the request system + the batch endpoint.

---

## Decisions (made, not deferred)

1. **Quota = max PENDING requests per user.** A user can have at most N requests in `status='pending'` at once. When a manager approves/denies one (or the user cancels), it leaves `pending` and frees quota. This directly bounds the unreviewed approval-queue backlog per user — the actual problem.
2. **Global admin setting** `max_pending_requests_per_user` (integer; **0 or unset = unlimited**). One setting for the instance (not per-role — a per-role cap is a possible future, not now).
3. **`auto_approve` users are unaffected** by construction: their requests approve immediately and never sit in `pending`, so the pending-count quota never constrains them. (No explicit exemption needed — they just don't accumulate pending. They're trusted; limiting their throughput is out of scope.)
4. **Enforced at request creation**, in the shared `createOneRequest` path (so both the single `POST /requests` and the batch `POST /requests/batch` respect it — this is the seam left in feature 1).

---

## Architecture

### Backend

- **Setting:** `max_pending_requests_per_user` via the existing `GetSetting`/`UpsertSetting` key-value store. Read in the settings DTO (`handleGetSettings` → `maxPendingRequestsPerUser int`); written in `handlePutSettings` (mirror `libraryBackendMode`). **The write MUST be admin-gated** — verify the settings PUT path is already restricted to an admin capability (it carries `library_backend_mode`, which is admin config); if it isn't, gate the quota write on the admin capability (`is_admin`/`can_manage_users`). Parse/validate: non-negative int; 0 = unlimited.
- **Count query:** new sqlc `CountPendingRequestsByUser` — `SELECT COUNT(*) FROM requests WHERE requested_by = ? AND status = 'pending'`.
- **Sentinel + enforcement:** add `request.ErrQuotaReached` (or an api-level sentinel). In `createOneRequest` (`internal/api/requests.go`): BEFORE creating, if the request will PEND (the caller is NOT `auto_approve`) AND the cap > 0, count the user's pending requests; if `count >= cap`, return `ErrQuotaReached` WITHOUT creating. The cap is read once per call (single) — for the batch, see below.
- **Single handler** `handleCreateRequest`: `ErrQuotaReached` → HTTP **429** with `{"error": "...", "limit": N}` and a clear message ("You've reached your limit of N pending requests — wait for some to be reviewed.").
- **Batch handler** `handleBatchCreateRequests`: read the cap + the user's current pending count ONCE; as it creates would-pend items, stop creating once `currentPending + createdThisBatch >= cap`, counting the remainder as `quotaCapped`. Response gains `quotaCapped: int` → `{created, skipped, quotaCapped, requests}`. (Dedup-hits and auto_approve don't consume pending quota.)

### Frontend

- **Admin setting field:** a "Max pending requests per user" number input in the Admin area near `RegistrationSection` (user-governance), bound to `maxPendingRequestsPerUser` via the settings client; helper text "0 = unlimited". Admin-only (the section already lives in Admin).
- **Single-request 429:** the Request / Request-album affordances (`DownloadAction`, `Album`) — on a 429 from `postRequest`, show the server's quota message as an error toast (instead of the generic error).
- **Batch quotaCapped:** the artist "Request all" toast notes when some were capped ("Requested N albums — M not requested (limit reached)") using the response `quotaCapped`.

### Data flow

Create (single or batch) → `createOneRequest` checks the pending-quota for would-pend requests → over cap → `ErrQuotaReached` (single: 429; batch: counted `quotaCapped`, stop creating) → FE surfaces the limit. Admin sets the cap in Admin settings; 0 = unlimited (no check).

## Error handling

- Cap unset/0 → no check (unlimited) — the common default; zero overhead.
- The count query runs only when the cap > 0 AND the request would pend (skip for auto_approve / unlimited).
- 429 carries the limit so the FE message is specific.

## Out of scope (explicit)

- **Per-role quotas** — one global cap for now; per-role is a future refinement.
- **Rate limiting (requests per time window)** — the quota is a concurrent-pending cap, not a rolling-window rate limit.
- **Quotas on auto_approve throughput / active downloads** — auto_approve users are unbounded here (trusted).

## Testing

- `CountPendingRequestsByUser` counts only that user's `pending` (not approved/denied/fulfilled/failed, not other users').
- Setting round-trips (GET/PUT); the write is admin-gated (a non-admin PUT of the quota is rejected); 0/unset = unlimited.
- `createOneRequest`: a `request`-only user at the cap → `ErrQuotaReached`, no request created; below the cap → created (pending); an `auto_approve` user is never quota-checked (creates regardless of pending count); cap=0 → never checked.
- Single handler: at-cap → 429 + limit; below → 200.
- Batch: a `request`-only user with cap N and a batch of M>N → exactly the remaining-quota count created, the rest `quotaCapped`; auto_approve in a batch → all created (no cap); dedup-hits don't consume quota.
- FE: the Admin field round-trips the setting; a 429 from a single request → quota error toast; the artist "Request all" toast reflects `quotaCapped`.
- e2e (hermetic): set a low cap; a request-only user's "Request all" beyond the cap → toast notes capped; a single request at cap → quota toast.
