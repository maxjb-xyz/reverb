# Download Queue Panel — Design Spec

> Phase 2 / sub-project B. Deliver **two surfaces** over the existing live
> download backend: a polished compact **pane** (`DownloadTray`, the glance) and a
> new dedicated **full page** (`/downloads`, the control center). The pane links
> into the page. New backend is deliberately small — **global pause/resume** and
> **clear/dismiss (hard delete)** — everything else is frontend on the live store
> + WebSocket that already exist.

- **Status:** Approved design (brainstormed 2026-06-24), ready for implementation planning.
- **Author:** Reverb maintainer + Claude.
- **Builds on:** the M3 download manager (`internal/download/manager.go`: worker
  pool, dedup-join, cancel/retry, EventBus publication), the realtime WS
  (`internal/api/ws.go` + `web/src/lib/realtimeWiring.ts`), the Zustand
  `downloadStore`, and today's `DownloadTray` + `DownloadAction`.

---

## 1. Goals & Non-Goals

### Goals
1. **Pane (improve `DownloadTray`)** — a tighter at-a-glance right-panel tray:
   grouped collapsing sections, honest live state, a prominent **See all →
   /downloads** link, calmer empty/active/failed states, auto-tidy when idle.
2. **Page (new `/downloads` route)** — the power-user control center: every job
   grouped + **filterable/searchable**, per-item **and bulk** cancel/retry/clear,
   a **Clear finished** action, and a **Pause/Resume** queue toggle.
3. **Pause/resume the queue** — stop dispatching new downloads while letting
   in-flight ones finish; resume drains the backlog.
4. **Clear/dismiss** — permanently remove finished jobs (single, selected, or all
   finished) so the history doesn't grow forever.
5. **Honest per-row state** — a freshly-clicked/queued job reads **"Queued"**, not
   a fake "Downloading" progress; only a `running` job shows the progress bar.

### Non-Goals (this sub-project)
- **Per-item reorder / prioritize.** A 2-worker FIFO queue drains fast; reorder is
  niche. The `priority` column already exists in the DB (unused) if we ever want it.
- **Changing worker count / making it configurable.** Stays at the current default
  of 2. (Worker count is *why* the queue is usually latent; out of scope to touch.)
- **A separate "history" view.** The page **is** the history — completed/failed
  rows stay until cleared; the Completed/Failed filter chips scope it.
- **Pausing an in-flight download mid-stream.** A spotDL subprocess can't be cleanly
  suspended without losing progress; pause only gates *new* dispatch.
- **Persisting pause across restart.** Pause is in-memory; a restart comes up running.
- **A new left-sidebar nav item.** v1 entry is TopBar counter → pane → "See all" →
  page (the page is also directly reachable by URL). *Open call — revisit on review.*

---

## 2. Locked Product Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Backend ambition | **Lean + pause/resume** | Pause is cheap + high-value for batch imports; reorder deferred. |
| Page layout | **Grouped sections (A)** with first-class **checkboxes + bulk bar** | Spotify-faithful, live work on top; selection grafted in for bulk ops. |
| Clear semantics | **Hard delete** terminal jobs (no migration) | Job's purpose ends once the file is in the library; dedup only reads active jobs, so deletion is safe. |
| Pause semantics | **Stop dispatch, in-flight finishes, in-memory, resets on restart** | Least surprising, cheapest, matches typical download managers. |
| Bulk cancel/retry | **Client-side fan-out** over existing per-item endpoints | No new backend; only clear needs an atomic server path. |
| Pane tidy | **Tidy when idle** — completed clear ~5s after the queue goes idle; failed sticky; active always shown | Shows while it's happening, tidies itself after; failures stay until acted on. |
| Pane scope | **No pause, no bulk-select** in the pane | Pane = glance + link; those are the page's job. |

---

## 3. Architecture

### 3.1 What already exists (extend, don't rebuild)
- **Backend:** `Manager` (2-worker pool over a 256-slot FIFO channel; jobs created
  `queued`, flipped to `running` only when a worker pulls them), `Enqueue` /
  `List` / `Cancel` / `Retry`, dedup-join, scan-debounce + post-download rematch,
  EventBus topics `download.{queued,progress,complete,failed}` + `library.updated`,
  job store (`internal/download/sqlstore.go` + `internal/store/queries/download_jobs.sql`).
- **Realtime:** `internal/api/ws.go` fans 5 topics to the FE; `realtimeWiring.ts`
  applies them to `downloadStore` and resyncs the full list on (re)connect.
- **Frontend:** `downloadApi.ts`, `downloadStore.ts` (Zustand, keyed by job id),
  `DownloadTray.tsx` (pane), `DownloadAction.tsx` (per-row state machine),
  `DownloadPopover.tsx`. React Router 6 routes hang off `AppShell`.

### 3.2 New backend: pause/resume (the gate)
Add to `Manager`: `Pause()`, `Resume()`, `IsPaused() bool`. Pause is a **dispatch
gate**, implemented with the closed-channel-as-open-gate idiom:

- A `mu`-guarded `paused bool` + a `resumeCh chan struct{}`. **Running:** `resumeCh`
  is a *closed* channel, so receiving returns immediately. **Paused:** `Pause()`
  swaps in a fresh *open* channel; `Resume()` closes it (and flips `paused`).
- The worker loop waits on the gate **before** pulling a job:
  ```
  for {
      select { case <-m.stopCh: return; default: }
      select {                       // block here while paused
      case <-m.stopCh: return
      case <-m.gate():               // returns instantly unless paused
      }
      select {
      case <-m.stopCh: return
      case id := <-m.queue: m.process(id)
      }
  }
  ```
- **In-flight jobs are untouched** — they were already pulled, so they run to
  completion. **Queued jobs stay `queued`** (their status never changes; they're
  simply not dispatched). `Enqueue` still persists + buffers them while paused.
- Pause/resume publish a **`download.queue`** event (`{paused}`) so every client
  reflects the state live. `Stop()` must also unblock gated workers (close `stopCh`
  is already awaited in the gate `select`).

### 3.3 New backend: clear (hard delete)
Add to `Manager`: `Clear(ctx, id)` and `ClearFinished(ctx)`.
- `Clear`: load the job; if status is **active** (`queued`/`running`) → return an
  error (the API maps it to `422` — you cancel an active job, you don't clear it);
  else `store.Delete(id)`, drop any `reqs[id]`, publish **`download.removed`**.
- `ClearFinished`: `store.DeleteFinished()` (status ∈ {completed, failed, canceled})
  → returns the deleted **ids** (sqlite `DELETE … RETURNING id`) → publish one
  `download.removed` carrying the list.
- **Bulk clear of an explicit selection** runs the same `Clear` over each id
  server-side (skipping any active id), returning the removed ids in one
  `download.removed`.

### 3.4 New events
| Topic | Payload | When |
|---|---|---|
| `download.queue` | `QueueStateEvent{ Paused bool }` | pause/resume |
| `download.removed` | `DownloadRemovedEvent{ JobIDs []string }` | clear (single/selected/finished) |

Both are added to the `ws.go` subscription set and the merged frame writer.

### 3.5 Reuse ledger
- **Reused as-is:** the worker pool / queue / dedup, `Enqueue`/`List`/`Cancel`/`Retry`,
  the EventBus + WS transport, the resync-on-connect path, `Cover`, `Button`/`IconButton`,
  design tokens.
- **New backend:** the pause gate (`Pause`/`Resume`/`IsPaused`), `Clear`/`ClearFinished`,
  store `Delete`/`DeleteFinished` + SQL, the two events, the new routes.
- **New / reworked frontend:** the `/downloads` page; the `DownloadTray` redesign;
  the `DownloadAction` queued-vs-running fix; store additions (`paused`, `remove`,
  selectors); `downloadApi` additions; shared row sub-components.

---

## 4. Data Model & Store

### 4.1 No migration
Clear is a **delete**; pause is **in-memory**. The `download_jobs` table is unchanged.

### 4.2 New SQL queries (`internal/store/queries/download_jobs.sql` → `make gen`)
- `DeleteDownloadJob` — `DELETE FROM download_jobs WHERE id = ?`.
- `DeleteFinishedDownloadJobs` — `DELETE FROM download_jobs WHERE status IN
  ('completed','failed','canceled') RETURNING id`.

`JobStore` gains `Delete(ctx, id) error` and `DeleteFinished(ctx) ([]string, error)`;
the in-memory test store implements both.

### 4.3 Core types (`internal/core/download.go`)
```
QueueStateEvent     { Paused bool }
DownloadRemovedEvent{ JobIDs []string }
```

### 4.4 Frontend store (`downloadStore.ts`)
- **State:** add `paused: boolean`.
- **Actions:** `setPaused(b)`, `remove(ids: string[])` (delete from the `jobs` map).
- **Selectors:** `queued()`, `running()`, `completed()`, `failed()`, and `counts()`
  (`{downloading, queued, finished}`); keep `active()` = queued+running.
- `applyEvent` is unchanged for the existing topics; the two new events are routed
  in `realtimeWiring` to `setPaused` / `remove` (their payloads aren't `DownloadEvent`s).

---

## 5. API Surface (protected `/api/v1` group)

| Method + path | Body | Returns |
|---|---|---|
| `POST /downloads/pause` | — | `{paused:true}` |
| `POST /downloads/resume` | — | `{paused:false}` |
| `GET /downloads/queue` | — | `{paused:boolean}` |
| `POST /downloads/{id}/clear` | — | `{ok:true}` · `422` if the job is active |
| `POST /downloads/clear` | `{ids?:string[]}` (omitted = all finished) | `{removed:number}` |

- `GET /downloads` (list) stays a bare `core.DownloadJob[]` — **non-breaking**;
  pause state is read separately via `GET /downloads/queue` (and pushed live).
- Existing `POST /downloads`, `/batch`, `/{id}/cancel`, `/{id}/retry` unchanged.
- Handlers `503` when the downloader is unconfigured, mirroring the existing list
  handler. OpenAPI documents the five new routes.
- **Bulk cancel/retry have no new route** — the FE fans out `cancelDownload` /
  `retryDownload` over the selection with `Promise.allSettled`.

**Backend pieces:** `Manager` gains the gate + clear methods; `download.go` gains
`TopicQueueState`/`TopicRemoved`; `internal/api/downloads.go` gains the handlers;
`ws.go` subscribes the two new topics; wiring unchanged (no new deps).

---

## 6. Frontend

**Routing:** add `<Route path="/downloads" element={<Downloads/>} />` under
`AppShell`, alongside the other route-level page components.

**Data layer (`downloadApi.ts`):** add `pauseQueue()`, `resumeQueue()`,
`getQueueState()`, `clearDownload(id)`, `clearDownloads(ids?)`. New TS types
mirroring §4.3.

**Realtime (`realtimeWiring.ts`):** handle `download.queue` → `setPaused`;
`download.removed` → `remove(jobIds)`. On (re)connect, also `getQueueState()` →
`setPaused` (alongside the existing full-list resync). Keep the active-poll fallback.

### 6.1 Full page — `/downloads` (layout A + selection)
- **Header:** "Downloads" + a summary line (`2 downloading · 4 queued · 18 finished`),
  a **Pause queue / Resume** toggle (bound to `paused`), and **Clear finished**.
- **Toolbar:** filter chips **All / Downloading / Queued / Completed / Failed**
  (refines the mockup's ambiguous "Active" to the explicit `running` state) + a text
  **search** (matches title/artist).
- **Grouped sections** Downloading → Queued → Finished (completed + failed +
  canceled), each **hidden when empty**. Per-section convenience actions (Cancel all
  / Clear all). `canceled` is terminal: it lives in Finished, is clearable, and
  matches the "All" chip (no dedicated chip — it's a rare explicit-cancel state).
- **Selection:** first-class checkboxes; a **bulk-action bar** appears when ≥1 row
  is selected — Cancel / Retry / Clear over the selection (each enabled only for
  rows it applies to), plus Deselect all.
- **Per-row:** cancel (active), retry (failed), clear (terminal). Reuses the row
  sub-components below.

### 6.2 Pane — `DownloadTray` redesign
- **Header:** "Downloads" + close; a **See all → /downloads** link with a live count
  of total tracked jobs (navigates to the page **and** closes the pane).
- **Grouped collapsing sections:** Downloading (live bar) / Queued ("Queued", no
  bar) / Done / Failed — each shown only when non-empty.
- **Empty state:** calm "Nothing downloading — search a track and hit download."
- **Failure:** friendly copy + one-tap **Retry** (the manual-URL fallback stays on
  the row's expanded state / the page; not surfaced in the pane).
- **Auto-tidy (component-local, view-only — never deletes):** active rows always
  shown; **completed** rows shown while `active().length > 0`, then hidden ~5s after
  the queue goes idle (a `useEffect` timer cancelled if a new download starts);
  **failed** rows always shown (sticky) until the user retries/dismisses.
- **No pause, no bulk-select.**

### 6.3 Per-row `DownloadAction` fix
Split the conflated active branch: `running` → progress bar/ring; **`queued`
(and the optimistic post-click state, since new jobs start `queued`) → a "Queued"
badge with no progress.** In-library / completed / failed / no-downloader /
available branches unchanged.

### 6.4 Shared row sub-components (`components/download/`)
Extract small presentational pieces consumed by both pane and page: `StatusPill`
(queued/downloading/done/failed), `DownloadProgress` (bar + %), `FailureNote`
(friendly copy + raw-error disclosure), reusing `Cover`. Keeps the pane and page
rows thin and consistent. Design tokens only — no raw hex, `text-on-accent` on
accent surfaces.

---

## 7. Edge Cases
- **Pause with jobs in-flight** → the ≤2 running finish; queued stay `queued`;
  resume drains in FIFO order.
- **Restart while paused** → comes up **running** (in-memory flag); FE reads
  `paused:false` from `GET /downloads/queue`.
- **Clear an active job** → `422` (cancel first); the row's Clear button only renders
  on terminal states, and the backend rejects defensively.
- **Clear finished while others download** → only terminal rows are deleted; active
  rows are skipped; one `download.removed` lists the removed ids.
- **Bulk cancel/retry partial failure** → `Promise.allSettled`; failures are
  surfaced, the rest still apply.
- **Very large batch enqueued while paused** → buffered up to 256; beyond that
  `Enqueue` blocks until resume (acceptable; documented).
- **Multi-tab** → clear/pause propagate via `download.removed` / `download.queue`.
- **Optimistic click** → row shows **"Queued"** immediately; flips to the progress
  bar when the `download.progress`/running event arrives, then Done/Failed.
- **Pane idle-tidy race** → completed hidden after 5s idle, but a download starting
  inside the window cancels the timer and keeps them visible.

---

## 8. Testing
- **Go unit** (in-memory store + fake clock, as today): pause gates dispatch
  (enqueue-while-paused stays `queued`, no worker picks it up; resume drains it);
  in-flight job completes despite pause; `IsPaused`/events emitted. `Clear` deletes a
  terminal job + publishes `download.removed`; `Clear` on an active job errors;
  `ClearFinished` deletes only terminal rows and returns their ids; store
  `Delete`/`DeleteFinished` round-trip.
- **FE component (vitest):** store (`paused` set/clear, `remove`, selectors,
  queued-vs-running counts); `DownloadAction` shows "Queued" for a queued job and
  a progress bar for a running one; `/downloads` page (filter chips, search, group
  collapse-when-empty, selection + bulk bar, pause toggle, clear); `DownloadTray`
  (See-all link + count, collapsing sections, empty state, **auto-tidy timer** via
  fake timers, failed stickiness).
- **Hermetic e2e (stays 3/3; extend only if low-risk):** the existing flows must
  stay green; optionally add a queue-page smoke (open `/downloads`, see a seeded
  job, clear it).
- **Gate before merge:** repo root `go test ./... && go build ./... && go vet ./...`;
  `web/` `npx vitest run && npx tsc --noEmit && npm run build && npm run e2e`.

---

## 9. Sequencing (for the plan)
1. **Store deletes** — `DeleteDownloadJob` + `DeleteFinishedDownloadJobs` SQL +
   `make gen`; `JobStore.Delete`/`DeleteFinished` + in-memory impl (TDD).
2. **Manager clear** — `Clear` / `ClearFinished`, `download.removed` event +
   `TopicRemoved`, `DownloadRemovedEvent` core type (TDD).
3. **Manager pause** — the gate (`Pause`/`Resume`/`IsPaused`), `download.queue`
   event + `TopicQueueState`, `QueueStateEvent`; `Stop()` unblocks gated workers (TDD).
4. **API + WS** — `pause`/`resume`/`queue`/`clear` routes + handlers; subscribe the
   two new topics in `ws.go`; OpenAPI.
5. **FE data layer** — `downloadApi` additions; store `paused`/`remove`/selectors;
   `realtimeWiring` for the two events + queue-state fetch on connect.
6. **Shared row sub-components** + the `DownloadAction` queued-vs-running fix.
7. **`DownloadTray` redesign** (grouping, See-all, empty/failure states, auto-tidy).
8. **`/downloads` page** (header + pause + clear, chips + search, grouped sections,
   selection + bulk bar) + the route.
9. **Wire entry points** — TopBar counter → pane; pane "See all" → page (+ close).
10. **e2e + edges + full gate green.**

Each step verified before the next; fresh implementer per task, review each, and a
whole-branch review before fast-forward merge to local `main`.
