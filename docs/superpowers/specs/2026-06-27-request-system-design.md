# Request System — Design Spec (Social / Multi-User, Sub-project 2)

> **The headline social feature.** A user with the `request` capability but **not** `auto_approve` can ask to add music to the shared library; the request lands in an **admin approval queue**; an approver accepts it; it flows into the existing download manager and appears in the library; the requester is **notified**. Builds directly on the SP1 identity foundation and the SP1.5 Seerr-style capability refinement (`request` / `auto_approve`). **Unified model:** *every* "add music" is a request — an `auto_approve` user's request is approved + enqueued instantly (one click, unchanged), a `request`-only user's request waits for an approver. One request trail for who-asked-for-what.

- **Status:** Approved design (brainstormed 2026-06-27), ready for implementation planning.
- **Author:** Reverb maintainer + Claude.
- **Part of epic:** *Social / Multi-User*, Sub-project 2 (SP2). SP1 (Accounts & Identity Foundation) + SP1.5 (Permissions refinement) are done and merged. SP3 (Listening History & Stats) and SP4 (Richer Social) follow.
- **Builds on:** the capability registry + roles (`internal/auth/capabilities.go`, editable roles, `EnsureSeed` idempotent remap), the download manager + queue (`internal/download/manager.go`, `core.DownloadRequest` with `InitiatedBy`), the EventBus (`internal/events`) + the broadcast WebSocket (`internal/api/ws.go`), and the SP1.5 capability semantics (`request` = "ask to add music; instant if Auto-approve, else waits for an administrator"; `auto_approve` = "fulfilled immediately").

---

## 1. Goals & Non-Goals

### Goals
1. **Turn `request` into a real workflow.** A `request`-only user can submit a request to add a track; it appears in an approval queue; an approver accepts/denies; on acceptance it downloads and lands in the library; the requester sees the outcome.
2. **Unified, request-first add path.** The user-facing "add music" always creates a request. `auto_approve` → approved + enqueued immediately (one click, identical to today). `request`-only → pending. Neither → 403. Exactly one path into the download manager (approval).
3. **Delegable approval.** A new `manage_requests` capability gates the approval queue; seeded on the Admin role, grantable to any role via the existing role editor.
4. **Requester visibility + live notifications.** A "My Requests" surface shows the caller's requests with live status; a per-user-filtered WebSocket pushes status changes (toast + row update). The request rows are the persistent record — no separate notifications store.
5. **Non-breaking + reuse.** No changes to the download manager or queue internals; the request layer sits *in front* of `Enqueue`. Existing `auto_approve` users' experience is unchanged. Existing installs' admins gain `manage_requests` automatically via the idempotent seed.

### Non-Goals — see §8 for the prioritized follow-up queue
- **Album/artist "request all"** — v1 is **track-level** (one request = one track).
- **Request quotas / limits** (Seerr-style per-role weekly caps, auto-approve thresholds).
- **Auto-expiry** of stale pending requests; a **general notifications inbox** (bell/unread); **email/push** notifications; **editing** a submitted request (cancel + resubmit covers it).

---

## 2. Locked Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Request model | **Unified — every add is a request** | Matches the shipped capability copy ("Auto-approve = your requests are fulfilled immediately"); one audit trail; one path into `Enqueue`. |
| Add path | **User-facing add → `POST /requests`** (gated on `request`); `auto_approve`→approve+enqueue, else pending | Request layer in front of the download manager; manager/queue untouched. |
| Approver gate | **New `manage_requests` capability** (seeded on `role-admin`, delegable) | Consistent with the editable-roles model; lets an admin delegate approval without full admin. |
| Requester UX | **"My Requests" view + per-user-filtered WS toasts**; no notifications table | Request rows are the persistent record; YAGNI. |
| Surface | **One `/requests` page** — My Requests (all) + Approval tab (managers) | Mirrors `/downloads`; keeps requester + approver concerns in one bounded surface. |
| Fulfillment | **Event-driven bridge** — a tracker subscribes to the download manager's existing complete/failed events, flips the linked request, publishes a targeted update | No download-manager changes. |
| Granularity | **Track-level (v1)** | Bounds v1; album "request all" is the top follow-up (§8). |
| Dedup | **Idempotent per (user, source, externalId) while open** | No duplicate pending requests; the add affordance never shows for in-library tracks. |
| Download attribution | **`initiated_by` = the requester** | The download is attributed to who wanted it. |

---

## 3. Architecture

```
FE "Add"/"Request" ──POST /requests──▶ Request service ──┐
                                          │ has auto_approve? ──yes──▶ approve+enqueue ─┐
                                          │ else (request)   ──────▶ status=pending     │
                                          ▼                                              ▼
                                    requests table                            download manager (unchanged)
   admin ──POST /requests/{id}/approve──▶ approve+enqueue ──────────────────────────────┘
                                                                                         │
                       request-tracker  ◀── EventBus download.complete/failed ◀──────────┘
                            │ flip request → fulfilled/failed; publish request.updated (target=requested_by)
                            ▼
                 broadcast WS ── per-user filter (handler knows connected user+caps) ──▶ requester toast / manager badge
```

**New units (each focused, testable):**
- `internal/core/request.go` — the `Request` type + `RequestStatus` constants.
- `internal/request/` — the Request **service** (`Create`, `ListForOwner`, `ListAll`, `Approve`, `Deny`, `Cancel`) over a `requests` **store** (sqlc), plus the **tracker** (event listener that closes the fulfillment loop). Structured like `internal/download/`.
- `internal/api/requests.go` — the HTTP handlers.
- `internal/store/queries/requests.sql` + a new goose migration for the `requests` table.
- `manage_requests` added to `internal/auth/capabilities.go`; `EnsureSeed` grants it to `role-admin`.
- WS: a per-user/per-capability filter in `internal/api/ws.go` for the request topics.
- FE: a `Request` branch in `DownloadAction`, a `/requests` route (My Requests + Approval tab), a requests API client + store, and WS wiring for toasts/badge.

---

## 4. Data model

One additive goose migration, e.g. `0014_requests.sql`:

```sql
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
  status          TEXT NOT NULL,            -- pending | approved | denied | fulfilled | failed
  created_at      INTEGER NOT NULL DEFAULT (unixepoch()),
  decided_by      TEXT REFERENCES users(id),
  decided_at      INTEGER,
  download_job_id TEXT,                     -- set on approval (links to the enqueued job)
  deny_reason     TEXT
);
CREATE INDEX idx_requests_status ON requests(status);
CREATE INDEX idx_requests_requested_by ON requests(requested_by);
```

(`requests.download_job_id` is a soft link to `download_jobs.id` — no FK, since the job store is a separate concern and jobs may be cleared.)

**Lifecycle:**
```
pending ──approve (admin OR auto_approve)──▶ approved ──linked download completes+lands──▶ fulfilled
   │                                            └────────linked download fails────────────▶ failed
   └──deny {reason?}──▶ denied
   └──cancel (requester, own pending)──▶ (row deleted, or status=cancelled)   [v1: delete the pending row]
```

**Status semantics for display:** the FE "My Requests" maps `status` directly to the labels *Pending · Approved · Downloading · Added · Denied · Failed*; while `approved`, the live download progress (if the job is still active) may show "Downloading" — derived from the linked job's live state on the existing download WS, falling back to the request `status`.

---

## 5. Request service & fulfillment bridge

`internal/request.Service`:
- `Create(ctx, requestedByUser CurrentUser, item RequestItem) (Request, error)` — dedup: if the user has an **open** (pending/approved) request for `(source, external_id)`, return it. Otherwise insert. If `requestedByUser.Has(auto_approve)` → immediately call the internal approve path (enqueue via the injected download manager with `InitiatedBy = requestedByUser.ID`, set `download_job_id`, status `approved`); **no manager event** (it needs no approval). If `request`-only → status `pending` and publish a manager-targeted `request.created` (the "new pending request" badge/queue signal). Returns the request either way.
- `ListForOwner(ctx, userID)` / `ListAll(ctx, statusFilter)`.
- `Approve(ctx, id, approver CurrentUser) (Request, error)` — `pending` only → enqueue (`InitiatedBy` = the original `requested_by`), set `download_job_id` + `decided_by`/`decided_at`, status `approved`; publish a targeted `request.updated` to the requester. Idempotent/guarded (already-decided → 409).
- `Deny(ctx, id, approver, reason)` — `pending` only → status `denied`, record reason/decider; publish `request.updated` to the requester.
- `Cancel(ctx, id, requester)` — own + `pending` only → delete the row (403 otherwise).

**Tracker** (`internal/request`): subscribes to the download manager's existing `complete`/`failed` EventBus topics. On an event whose `jobID` matches a request's `download_job_id`, it flips that request to `fulfilled`/`failed`, sets nothing else, and publishes a targeted `request.updated`. This is the *only* coupling to downloads, and it's read-only (a listener) — the download manager is unchanged.

**Enqueue seam:** the Request service depends on the same `DownloadManager` interface the API already holds (the live, reload-swappable one). Approval calls `Enqueue(ctx, core.DownloadRequest{… InitiatedBy: requestedBy})` exactly as `handleCreateDownload` does today.

---

## 6. API surface

All under `/api/v1`, in the protected group:

| Method | Path | Cap | Notes |
|---|---|---|---|
| POST | `/requests` | `request` | `{source, externalId, title, artist, album?, isrc?, durationMs?, coverArtId?}`. auto_approve→approved+enqueued, else pending. Idempotent dedup. → the `Request`. |
| GET | `/requests/mine` | `request` | the caller's own requests (My Requests), newest first. |
| GET | `/requests` | `manage_requests` | all requests for the queue; `?status=pending` (default view) / history. |
| POST | `/requests/{id}/approve` | `manage_requests` | pending→approved+enqueue. 409 if already decided. |
| POST | `/requests/{id}/deny` | `manage_requests` | `{reason?}`. pending→denied. 409 if already decided. |
| POST | `/requests/{id}/cancel` | `request` | own + pending only → withdraw. 403/404 otherwise. |

`POST /downloads*` is unchanged and retained for the download **queue** (list/cancel/retry/pause/clear) and system/batch contexts (playlist "download missing"); the **user-facing single add** moves to `POST /requests`.

---

## 7. Frontend surfaces

Design tokens only; match existing component density.

- **The "Request" affordance** (`DownloadAction.tsx`): today the add control is hidden for users without `auto_approve`. Add a branch: a user with `request` (not `auto_approve`) sees a **"Request"** button → `POST /requests` → flips to **"Requested"** (pending, disabled). `auto_approve` users see the instant add exactly as today (now routed through `/requests`). The in-library/play states are unchanged. A user with neither sees no add control.
- **`/requests` page** (nav entry shown to anyone with `request`):
  - **My Requests** (default): the caller's requests with live status chips (Pending/Approved/Downloading/Added/Denied/Failed), item art, when. Live WS updates the rows + a toast on status change.
  - **Approval** tab (only with `manage_requests`): pending requests (requester, item, when) with **Approve** / **Deny** (deny → optional reason); a decided-history list. A **pending-count badge** on the nav entry (driven by the manager-filtered WS `request.created`).
- **Live wiring:** a small requests store (Zustand) hydrated from `/requests/mine` (+ `/requests` for managers), updated by the filtered WS events; toasts via the existing toast mechanism.

---

## 8. Follow-ups (the deferred work, prioritized — do directly after SP2 core)

Each gets its own short spec/plan/build when picked up; the SP2 foundation (the `requests` table, the service, the approval queue, the fulfillment tracker, the WS filter) is built to extend cleanly.

**Do-next (build directly on this):**
1. **Album / artist "request all".** A request `kind` (`track`|`album`) + album fulfillment: on approval, enqueue the existing album/batch add; track partial fulfillment (N of M tracks landed) on the request. Foundation hooks: the `requests` row already has the item descriptor; add `kind` + a fulfillment-count, and reuse `/downloads/batch`. The approval queue groups by album.
2. **Request quotas / limits.** Per-role caps (e.g. "≤ 10 open requests" / "≤ N per week") enforced at `POST /requests`; surfaced to the requester. Stored as role-level settings; checked in `Create`. Natural extension of the role model.

**Maybe-later (only if wanted):**
3. **Auto-expiry** of stale pending requests (a sweep that denies/closes requests older than X).
4. **General notifications inbox** (bell + unread + a `notifications` table) — only if more notification types appear; the My Requests view covers requests alone.

**Not planned / already covered:**
5. **Email / push notifications** — needs separate SMTP/push infra; out of the in-app model.
6. **Editing a submitted request** — cancel + resubmit covers it.

---

## 9. Testing

Gate (must stay green): root `go test ./... && go build ./... && go vet ./...`; `web/` `npx vitest run && npx tsc --noEmit && npm run build && npm run e2e`.

- **Service:** create → auto_approve branch (approved+enqueued, `initiated_by`=requester, `download_job_id` set) vs request-only branch (pending); dedup returns the existing open request; approve (pending→approved+enqueue; already-decided→409); deny (+reason); cancel (own pending only; others→403/404).
- **Fulfillment tracker:** a `download.complete` for a request's job flips it to `fulfilled` + publishes a `request.updated` targeted at the requester; `download.failed` → `failed`.
- **Capability gates:** `POST /requests` requires `request` (else 403); the queue + approve/deny require `manage_requests` (a plain `request` user → 403); cancel-other's-request → 403.
- **Migration/seed:** the `requests` table applies; `EnsureSeed` grants `manage_requests` to `role-admin` idempotently; an existing admin gains it without losing other caps.
- **WS filter:** a `request.updated` targeted at user A reaches A's connection only (not B's); `request.created` reaches only `manage_requests` connections.
- **FE:** the Request button (request-only user) posts + flips to Requested; auto_approve user's add unchanged; My Requests renders statuses + updates on a WS event + toast; the Approval tab (manager) approves/denies and is hidden for non-managers; e2e: a request-only user requests a track → (mocked) approval → status reflects.

---

## 10. Rollout

- Branch `feat/request-system` (off `main`) → green gate → fast-forward to local `main`; push + rebuild to verify on `soulkiller` (a request-only user requesting, an admin approving, the track landing + the requester seeing "Added"). No `git push` without explicit go-ahead.
- After SP2 core lands + is verified, pick up the §8 do-next follow-ups (album "request all", then quotas).

---

*Reverb — own your music, again. Now your friends can ask for it.*
