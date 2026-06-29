# Approval Notifications — Design

**Status:** Approved by "just do everything" (2026-06-28) — design by Claude, documented here. Feature 3 of 3 closing the SP2 backlog (after artist-request-all + request-quotas).

**Goal:** Give managers a persistent, live, reviewable alert when a request needs approval, and notify requesters when their request is approved / denied / fulfilled — an in-app **notification center** (bell + unread badge + dropdown), driven by the existing request lifecycle events over the existing WS.

**Tech stack:** Go (events + a new `Notifier` + notifications store/service + chi API + WS) + React/TS (notification store + TopBar bell + center). Mirrors the existing `request.Tracker` (subscribe-to-events-and-act) + the per-user WS filter.

---

## Scope decision (important)

**In scope:** the **in-app notification center** — persistent notification rows, an unread badge, a dropdown center, live delivery over the existing WebSocket, hydrate-on-load. This fully delivers "you're notified about approvals" while the app is open (and reviewable later).

**Explicitly OUT of scope (documented future):** true **Web Push** (browser/OS notifications when the app is closed). That is a separate, large subsystem — a service worker, VAPID key generation + storage, the Push API subscription lifecycle (per browser/device), and a web-push send path from Go (a new dependency) — and it layers cleanly ON TOP of this notification model later (the `Notifier` would gain a second delivery channel). Building it hastily would violate the minimal-deps + craft bar. It gets its own brainstorm when wanted. **This feature builds the foundation + the in-app channel; the OS-push channel is a future follow-up.**

---

## Decisions (made, not deferred)

1. **A `Notifier` component** (analogous to `request.Tracker`): subscribes to the request lifecycle events and creates notification rows + publishes a per-user `notification` WS event. No new event plumbing — reuse `events.Bus`.
2. **Who gets notified:**
   - `request.created` (a new PENDING request) → a `request_pending` notification to EVERY manager (users with `manage_requests`, via `auth.ListUsers` filtered by capability).
   - `request.updated` to `approved` / `denied` → an `request_approved` / `request_denied` notification to the **requester** (`requestedBy`).
   - request `fulfilled` (the `Tracker` flips it on download complete) → a `request_fulfilled` notification to the requester. (Fulfilled/failed arrive as `request.updated` with the terminal status — the Notifier keys off the status.)
3. **Auto-resolve manager alerts:** when a request leaves `pending` (approved/denied/canceled), mark the `request_pending` notifications FOR THAT REQUEST read for all recipients — so a manager's unread badge reflects *actionable* items, not handled ones.
4. **User-scoped, persistent:** notifications live in a `notifications` table (per recipient), survive reloads, and are hydrated on load (like the request store). Unread count drives the badge.

---

## Architecture

### Data — `notifications` table (migration)

`id TEXT PK, user_id TEXT NOT NULL, type TEXT NOT NULL, title TEXT NOT NULL, body TEXT NOT NULL, request_id TEXT, read INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL`. Indexed on `(user_id, read)` and `(request_id)`. `core.Notification` mirrors it (json camelCase).

Store queries (sqlc): `CreateNotification`, `ListNotificationsForUser(user_id, limit)` (recent first), `CountUnreadForUser(user_id)`, `MarkNotificationsRead(user_id, ids...)` / `MarkAllReadForUser(user_id)`, `MarkPendingResolvedForRequest(request_id)` (mark `type='request_pending'` rows for a request read).

### `notification.Service`

`Create(ctx, n) `, `ListForUser(ctx, userID, limit)`, `CountUnread(ctx, userID)`, `MarkRead(ctx, userID, ids)` / `MarkAllRead(ctx, userID)`, `ResolvePendingForRequest(ctx, requestID)`. Owns the table. (Small, focused — one responsibility.)

### `Notifier` (subscribes + acts; mirrors `Tracker`)

Subscribes to `request.created` + `request.updated`. On each event: builds the notification(s), calls `notification.Service.Create`, and publishes a `notification` event per recipient on the bus (so the WS pushes it live). On a request leaving pending, calls `ResolvePendingForRequest`. Manager fan-out via `auth.ListUsers` filtered to `manage_requests`. Wired in `internal/wiring` alongside the Tracker; runs for its lifetime.

### API (user-scoped — the caller's own notifications)

- `GET /api/v1/notifications` → `{ notifications: [...recent], unread: int }` (a sensible recent limit, e.g. 50).
- `POST /api/v1/notifications/read` → body `{ ids?: []string }` (omit/empty = mark ALL read); returns the new unread count.
- Both in the authenticated route group (any logged-in user; they only ever see/affect their own rows — the handler scopes by the caller's id).

### WS — `notification` topic, per-user

Add `notification` to `wsTopics`. The `notification` event carries the recipient `userID`; reuse the existing per-user WS filter (the same mechanism that scopes request events to the right user) so each client only receives its own notifications. On receipt, the FE prepends it + bumps the unread badge.

### Frontend

- **Notification store** (Zustand, `notificationStore.ts`): `items`, `unread`, `setAll`, `add`, `markRead(ids)`, `markAllRead`. Hydrated on WS open (`realtimeWiring.onOpen` → `GET /notifications` → `setAll`, like the request store). The `notification` WS event → `add` (prepend + unread++).
- **Bell** in the `TopBar`: a bell icon with an unread-count badge (hidden at 0). Click → a dropdown **notification center**: recent notifications (title, body, relative time, unread dot), newest first; clicking one navigates to `/requests` (or the relevant item) and marks it read; a "Mark all read" action. Opening the center marks visible items read (or an explicit control — pick the cleaner UX). Spotify-faithful, tokens only.

### Data flow

`request.created` → Notifier → a `request_pending` row per manager + a per-user `notification` WS event → managers' bells light up live; the row persists + hydrates on reload. Manager approves → `request.updated(approved)` → Notifier notifies the requester (`request_approved`) + resolves the managers' `request_pending` rows for that request (their badges drop). Download completes → Tracker flips to `fulfilled` → `request.updated(fulfilled)` → Notifier notifies the requester (`request_fulfilled`).

## Error handling

- A notification create/publish failure is logged, never breaks the request flow (the Notifier is a side-effect consumer, like the Tracker).
- Manager fan-out with zero managers → no notifications (no error).
- Hydration / WS fetch errors swallowed (don't break the connection), like the request-store hydration.

## Testing

- Store: each query round-trips; `CountUnreadForUser` counts only that user's unread; `MarkPendingResolvedForRequest` marks only `request_pending` rows for that request.
- `notification.Service`: create/list/count/markRead/resolvePending behave; user-scoped.
- `Notifier`: a `request.created` event → a `request_pending` notification for each manager (fake auth with 2 managers + 1 non-manager → 2 notifications) + a `notification` event published per manager; `request.updated(approved)` → a `request_approved` for the requester + the request's `request_pending` rows resolved; `request.updated(fulfilled)` → `request_fulfilled` for the requester; a non-terminal update → no notification.
- API: `GET /notifications` returns the caller's recent + unread; `POST /notifications/read` with ids marks those (and `{}` marks all); a user can't read/affect another user's rows.
- WS: a `notification` event for user A is delivered only to A's socket (per-user filter), not B's.
- FE: the store hydrates on WS open; a `notification` event adds + bumps the badge; the bell shows the unread count (hidden at 0); the center lists newest-first, click navigates + marks read, "mark all read" zeroes the badge.
- e2e (hermetic): mock notifications + unread; the bell shows the badge; opening the center lists them; mark-all-read clears the badge; a pushed `notification` (via the mock WS, if the harness supports it) appears — else assert hydration + mark-read.

## Out of scope (explicit)

- **Web Push / OS notifications when app-closed** (service worker + VAPID + subscriptions) — the documented future layer (see Scope decision).
- **Email notifications** — not now.
- **Per-type notification preferences / mute** — v1 notifies on the lifecycle events above; granular prefs are a future refinement.
- **Digest/batching** — each event is its own notification.
