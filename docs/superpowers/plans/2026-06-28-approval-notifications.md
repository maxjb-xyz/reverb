# Approval Notifications Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An in-app notification center — managers get a live, persistent, reviewable alert when a request needs approval; requesters get notified on approve/deny/fulfill — driven by the existing request lifecycle events over the existing WS.

**Architecture:** A `notifications` table + `notification.Service`; a `Notifier` (subscribes to `request.created`/`request.updated`, mirrors `request.Tracker`) that creates notification rows + publishes per-user `notification` WS events; a user-scoped notifications API; the WS gains a per-user `notification` topic; the FE gets a notification store (hydrated + live) + a TopBar bell + dropdown center.

**Tech stack:** Go (events + notifications store/service + Notifier + chi API + WS) + React/TS (store + realtimeWiring + nav bell). Builds on the request system. NO new dependencies.

## Global Constraints

- **In-app notification center ONLY.** Web Push (service worker / VAPID / OS push when app-closed) is OUT — a documented future layer. Spec: `docs/superpowers/specs/2026-06-28-approval-notifications-design.md`.
- Notify: `request.created`→ every manager (`manage_requests`); `request.updated`→approved/denied/fulfilled → the requester. On a request leaving `pending` → mark its `request_pending` notifications read for all recipients.
- The `Notifier` is a side-effect consumer (like `Tracker`): a notification failure is logged, NEVER breaks the request flow.
- Per-user WS routing reuses `wsShouldForward` (ws.go:38) — the `notification` event carries the recipient id; only that user's socket receives it.
- Design tokens only; Spotify-faithful craft. Every task gates green: `go test ./... && go build ./... && go vet ./...`; FE tasks also `npx vitest run && npx tsc --noEmit && npm run build`. Commit footer `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.

---

### Task 1: Store layer — model, queries, `notification.Service`

**Files:**
- Create: `internal/store/migrations/0018_notifications.sql`, `internal/store/queries/notifications.sql`, `internal/notification/service.go`, `internal/notification/service_test.go`
- Modify: `internal/core/` (add `notification.go` with the types)
- Test: `internal/notification/service_test.go` (+ a store-level test if the pattern exists)

**Interfaces produced:**
- `core.Notification{ ID, UserID, Type, Title, Body, RequestID string; Read bool; CreatedAt int64 }` (json camelCase; `RequestID` omitempty). Consts: `NotifyRequestPending="request_pending"`, `NotifyRequestApproved="request_approved"`, `NotifyRequestDenied="request_denied"`, `NotifyRequestFulfilled="request_fulfilled"`.
- `core.NotificationEvent{ TargetUserID string; Notification core.Notification }` (the WS payload).
- Migration `0018`: `notifications(id TEXT PK, user_id TEXT NOT NULL, type TEXT NOT NULL, title TEXT NOT NULL, body TEXT NOT NULL, request_id TEXT, read INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL)` + index `(user_id, read)` + `(request_id)`; goose down drops it.
- Queries → `make generate`: `CreateNotification`, `ListNotificationsForUser(user_id, limit)` (ORDER BY created_at DESC), `CountUnreadForUser(user_id)`, `MarkNotificationsRead(user_id, ids)` (only the caller's rows), `MarkAllReadForUser(user_id)`, `MarkPendingResolvedForRequest(request_id)` (set read=1 WHERE request_id=? AND type='request_pending').
- `notification.Service` (`NewService(q, now)`): `Create(ctx, core.Notification) (core.Notification, error)` (fills id/createdAt), `ListForUser(ctx, userID, limit) ([]core.Notification, error)`, `CountUnread(ctx, userID) (int, error)`, `MarkRead(ctx, userID, ids []string) error`, `MarkAllRead(ctx, userID) error`, `ResolvePendingForRequest(ctx, requestID) error`.

- [ ] **Step 1 — failing tests:** Create→ListForUser round-trips (newest first); `CountUnread` counts only that user's `read=0`; `MarkRead(user, [id])` flips one (and won't touch another user's row); `MarkAllRead` zeroes the user's unread; `ResolvePendingForRequest(reqID)` marks ONLY `request_pending` rows for that request read (leaves a `request_approved` row for the same request untouched).
- [ ] **Step 2 — run, expect FAIL.**
- [ ] **Step 3 — implement** the migration (+`make generate`), the core types, the service.
- [ ] **Step 4 — run, expect PASS;** full `go test ./...`.
- [ ] **Step 5 — gate green + commit.**

---

### Task 2: `Notifier` — subscribe to request events, fan out, publish

**Files:**
- Create: `internal/notification/notifier.go`, `internal/notification/notifier_test.go`
- Modify: `internal/wiring/` (wire the Notifier where the request `Tracker` is wired — search `NewTracker`)
- Test: `internal/notification/notifier_test.go`

**Interfaces consumed:** `events.Bus` (`Subscribe(topic) (<-chan Event, func())`); `request.TopicCreated`/`request.TopicUpdated`; the `request.updated`/`created` payload is `core.RequestEvent` (read its shape — it carries `TargetUserID` + the request incl. `Status`, `ID`, `RequestedBy`, and item fields for the title/body); `notification.Service` (Task 1); `auth.Service.ListUsers(ctx)` (UserViews with caps) to enumerate managers; `core.NotificationEvent` (Task 1).
**Interfaces produced:** `notification.Notifier` with `NewNotifier(bus, svc, auth, pub)` + a `Run`/start (mirror `request.Tracker`'s subscribe-and-loop). Subscribes to `request.created` + `request.updated`.

- [ ] **Step 1 — failing tests** (`notifier_test.go`, with a fake bus + fake auth (2 managers + 1 non-manager) + a real/fake notification service + a capture of published events):
  - a `request.created` event → a `request_pending` `core.Notification` created for EACH of the 2 managers (not the non-manager) + a `notification` event published per manager (payload `core.NotificationEvent` with that manager's `TargetUserID`).
  - a `request.updated` with status `approved` → a `request_approved` notification for the requester (`RequestedBy`) + a `notification` event to the requester; AND `ResolvePendingForRequest(reqID)` invoked.
  - `request.updated` status `fulfilled` → `request_fulfilled` for the requester; `denied` → `request_denied` + resolve-pending.
  - a `request.updated` with a NON-terminal status (e.g. still `pending`/`approved`-already-handled noise) → no new notification. (Define terminal set: approved, denied, fulfilled; the Notifier acts once per transition into those — keep it simple: act on these statuses.)
- [ ] **Step 2 — run, expect FAIL.**
- [ ] **Step 3 — implement** the Notifier (build title/body from the request — e.g. pending: "<requester> requested <title>"; approved/denied/fulfilled: "Your request for <title> was <status>"); fan out to managers via `auth.ListUsers` filtered to `CapManageRequests`; on terminal statuses notify the requester + `ResolvePendingForRequest`; publish `notification` events; log+swallow errors. Wire it in `internal/wiring` next to the Tracker (start its loop for the process lifetime).
- [ ] **Step 4 — run, expect PASS;** full `go test ./...`.
- [ ] **Step 5 — gate green + commit.**

---

### Task 3: Notifications API + WS `notification` topic

**Files:**
- Create: `internal/api/notifications.go` (handlers), `internal/api/notifications_test.go`
- Modify: `internal/api/server.go` (routes in the authenticated group), `internal/api/ws.go` (add `"notification"` to `wsTopics` + a `wsShouldForward` case), `internal/api/server.go` deps (the notification service handle if needed)
- Test: `internal/api/notifications_test.go`, `internal/api/ws_test.go`

**Interfaces consumed:** `notification.Service`; `core.NotificationEvent`. 
**Interfaces produced:**
- `GET /api/v1/notifications` → `{ "notifications": [...recent (limit 50)], "unread": int }` (the caller's own).
- `POST /api/v1/notifications/read` → body `{ "ids"?: []string }` (empty/omitted = mark ALL read); responds `{ "unread": int }`.
- `wsShouldForward` gains: `case "notification": ne, ok := ev.Payload.(core.NotificationEvent); return ok && cu.ID == ne.TargetUserID`; `"notification"` added to `wsTopics`.

- [ ] **Step 1 — failing tests:** `GET /notifications` returns the caller's recent + unread count (seed a couple via the service); `POST /notifications/read {ids:[id]}` marks that one (unread drops); `POST {}` marks all; user A cannot mark/read user B's rows (scoped by caller id). `ws_test.go`: `wsShouldForward(cuA, notificationEvent(target=A))` → true; `(cuB, …target=A)` → false; a malformed payload → false.
- [ ] **Step 2 — run, expect FAIL.**
- [ ] **Step 3 — implement** the handlers (scope every op by `currentUser(r).ID`), register the routes in the authenticated group, add the `wsShouldForward` case + the topic.
- [ ] **Step 4 — run, expect PASS;** full `go test ./...`.
- [ ] **Step 5 — gate green + commit.**

---

### Task 4: FE — notification store + realtime + hydration

**Files:**
- Create: `web/src/lib/notificationApi.ts` (store + api), `web/src/lib/notificationApi.test.ts`
- Modify: `web/src/lib/realtimeWiring.ts` (handle the `notification` WS event + hydrate on open), `web/src/lib/realtimeWiring.test.ts`
- Test: as above

**Interfaces consumed:** Task 3's `GET /notifications`, `POST /notifications/read`, the `notification` WS event (envelope `{type:'notification', payload: core.NotificationEvent}`).
**Interfaces produced:** `useNotificationStore` (Zustand): state `{ byId, unread }` + selectors `items()` (newest-first array), `unread`; actions `setAll(notifications, unread)`, `add(n)` (prepend + unread++ if unread), `markRead(ids)`, `markAllRead()`. `getNotifications(): Promise<{notifications, unread}>`, `postMarkRead(ids?): Promise<{unread}>`. Types: `Notification` (mirror core camelCase).

- [ ] **Step 1 — failing tests:** `notificationApi.test.ts` — `getNotifications` GETs `/notifications`; `postMarkRead([id])` POSTs `/notifications/read {ids}`; the store `add` prepends + bumps unread, `markRead`/`markAllRead` update unread. `realtimeWiring.test.ts` — on WS open, `getNotifications` is fetched → `setAll` (mirror the request-store hydration test); a `notification` WS event → `add` (item present + unread bumped).
- [ ] **Step 2 — run, expect FAIL.**
- [ ] **Step 3 — implement** the store + api; in `realtimeWiring`: `onOpen` also hydrates notifications (`getNotifications().then(setAll).catch(()=>{})`); the `onEvent` handler routes `type==='notification'` → `useNotificationStore.getState().add(payload.notification)`.
- [ ] **Step 4 — run vitest + tsc, expect PASS.**
- [ ] **Step 5 — gate green + commit.**

---

### Task 5: FE — TopBar bell + notification center

**Files:**
- Create: `web/src/components/NotificationBell.tsx` (the bell + dropdown center), `web/src/components/NotificationBell.test.tsx`
- Modify: the top nav / app shell (where the desktop nav lives — find it, e.g. `AppShell.tsx`; place the bell near the existing controls), possibly `MobileTabNav.tsx` if appropriate
- Test: `NotificationBell.test.tsx`

**Interfaces consumed:** `useNotificationStore` (Task 4); `useNavigate` (react-router); `postMarkRead`.

- [ ] **Step 1 — failing tests:** the bell shows the unread count badge (hidden when `unread===0`); clicking opens a dropdown listing notifications newest-first (title, body, relative time, unread dot); clicking a notification navigates to `/requests` and marks it read (`postMarkRead([id])` + store `markRead`); a "Mark all read" control zeroes the badge (`postMarkRead()` + `markAllRead`).
- [ ] **Step 2 — run, expect FAIL.**
- [ ] **Step 3 — implement** the bell + dropdown (Spotify-faithful, tokens only; reuse existing dropdown/menu primitives); wire it into the top nav. Mark-read on click + mark-all-read. Accessible (aria-label, the live region already exists).
- [ ] **Step 4 — run FULL vitest + tsc + build, expect PASS** (the nav is widely used).
- [ ] **Step 5 — gate green + commit.**

---

### Task 6: e2e + final gate

**Files:**
- Modify: `web/e2e/mocks.ts` (`GET /notifications` with a couple + unread; `POST /notifications/read`), a spec
- Test: the hermetic Playwright suite

- [ ] **Step 1 — write the spec:** mock notifications + unread for the user; the bell shows the badge; opening the center lists them newest-first; clicking "Mark all read" clears the badge (assert the POST + the badge gone); clicking a notification navigates to `/requests`. (If the harness can push a WS frame, also assert a live `notification` arrives; otherwise hydration + mark-read suffices.) Resilient selectors.
- [ ] **Step 2 — FULL gate:** `go test ./... && go build ./... && go vet ./...`; `cd web && npx vitest run && npx tsc --noEmit && npm run build && npm run e2e`. Report counts. (`playlist-sync.spec.ts` ERR_ABORTED is a known flake — re-run once if only that.)
- [ ] **Step 3 — commit.**

---

## Self-review notes
- **Spec coverage:** model+service (T1), Notifier+fan-out+resolve (T2), API+WS topic (T3), FE store+realtime+hydration (T4), bell+center (T5), e2e (T6). All spec sections mapped.
- **Type consistency:** `core.Notification` (camelCase json) + `core.NotificationEvent{TargetUserID, Notification}`; the WS case keys on `NotificationEvent.TargetUserID` (matches `wsShouldForward`'s `RequestEvent.TargetUserID` pattern); `notification.Service` methods; `useNotificationStore` setAll/add/markRead/markAllRead; `getNotifications`/`postMarkRead`. Consistent across tasks.
- **Out of scope (unchanged):** Web Push / OS push (documented future layer); email; per-type prefs; digests.
