# Reverb Phase 5 — Search & the Download Loop Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use `- [ ]`.

**Goal:** Build Reverb's core interaction — unified `My Library / Everywhere` search with the **optimistic download model** (§9.2): two real row states (In Library → play, Download → enqueue), nuance moved to a click-time downloader popover and to descriptive failures in the download tray.

**Architecture:** A `DownloadAction` component maps `(match, existing job, downloaders)` → the correct affordance and handles enqueue/popover. The Search route renders library + everywhere results through `TrackRow` (with `DownloadAction` in its `right` slot) and album/artist grids. The download tray surfaces honest, actionable failures. All wiring (`useEverywhere` SSE, `useLibrarySearch`, `useDownloads`, `downloadApi`) is reused unchanged.

**Tech Stack:** React 19 + TS + Tailwind, Vitest. Data: `useEverywhere(q,type,enabled)` → `{tracks,albums,artists,sources,status}`; `useLibrarySearch(q)`; `useDownloads().byExternal(source,externalId)` / `.active()`; `postDownload(req)`, `cancelDownload(id)`, `retryDownload(id)`; downloader adapters via `adaptersApi`. Types: `ExternalResult` (`.type`, `.match: {status, libraryTrackId}`, `.source`, `.externalId`, `.title/artist/album`), `DownloadJob` (`.status: 'queued'|'running'|'completed'|'failed'|'canceled'`, `.progress`, `.error`).

## Global Constraints (craft bar — every task)
- **Token classes only** (no raw hex/palette, no `text-white`, no arbitrary `[..px..]` duplicating a token). **Zero emoji** — `Icon`. **Reuse primitives/molecules** (`Badge, ProgressRing, TrackRow, MediaCard, Button, IconButton, Segmented, Cover, Skeleton, EmptyState, Icon`) — don't hand-roll or inline SVGs.
- **Every interactive element**: hover, `focus-visible` ring, active, disabled, accessible name.
- **Optimistic model is law (§9.2):** the row shows exactly — `In Library` (accent, plays) · `Download` (one clean action; "available" and "uncertain" merged — no "try download") · `Downloading` (ProgressRing %) · `Downloaded` (transient accent check) · `No downloader` (disabled, only when zero downloaders configured). Choice of downloader appears in a popover ONLY when >1 downloader is configured. Failures live in the tray, never as scary row copy.
- **Reuse the existing SSE/match/download wiring unchanged.** Read `web/src/routes/Search.tsx` + `web/src/components/ExternalRow.tsx` for the current match/enqueue logic before replacing.
- **Visual source of truth:** `/Users/maximusjb/Repos/reverb/.superpowers/brainstorm/87868-1782028461/content/search-settings-v2.html` (`.seg`, `.srcchip`, `.trow` + badges, `.alrow`/`.alcard`) and `/Users/maximusjb/Repos/reverb/.superpowers/brainstorm/87868-1782028461/content/uncertain-rows.html` (the optimistic states, the popover, the queue-failure card).
- Suite green (`cd web && npx vitest run`) + `npm run build` clean before each commit. Commit trailer: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`. Branch `feat/ui-overhaul-spotify`. Spec §9.2, §9.9.

## File Structure
- `web/src/components/download/DownloadAction.tsx` (+ test) — state→affordance + enqueue.
- `web/src/components/download/DownloadPopover.tsx` (+ test) — choose-downloader popover.
- `web/src/routes/Search.tsx` (rebuild) (+ test).
- `web/src/components/DownloadTray.tsx` (enhance) (+ test) — descriptive failures + actions.
- `web/src/components/shell/TopBar.tsx` (small edit) — search navigates to `/search?q=` (optional polish).

---

### Task 1: DownloadAction + DownloadPopover (the optimistic model)

**Interfaces:**
- `useDownloaders(): { id, name }[]` — a small hook in `adaptersApi` (or local) returning enabled downloader adapters (read `adaptersApi.ts`; if a list hook exists, reuse it; else add one filtering `/adapters` by `type === 'downloader' && enabled`).
- `DownloadAction(props: { result: ExternalResult; onPlay?: (libraryTrackId: string) => void })` — derives state from `result.match` + `useDownloads().byExternal(result.source, result.externalId)` + the downloaders list, and renders:
  - match `in_library` → `Badge kind="in-library"` (accent); clicking the row plays (`onPlay(libraryTrackId)`).
  - job `running`/`queued` → `ProgressRing` (job.progress, or indeterminate when -1) + "Downloading"/`Badge kind="downloading"`.
  - job `completed` → `Badge kind="downloaded"` (accent check).
  - job `failed` → a small "Failed · Retry" affordance (full detail in the tray).
  - no job, ≥1 downloader → `Badge kind="available"` Download button: 1 downloader → `postDownload({source,externalId,artist,title,album,isrc})` immediately; >1 → open `DownloadPopover`.
  - no job, 0 downloaders → `Badge kind="disabled"` "No downloader" (disabled).
- `DownloadPopover(props: { downloaders: {id,name}[]; onPick: (name: string) => void; onClose: () => void })` — lists downloaders (first = "Recommended"), each `onPick(name)` enqueues with that `downloader`; "we'll fetch the closest match" caption; token-styled, `shadow-pop`, focus-trapped, Esc closes.

- [ ] **Step 1:** Read `uncertain-rows.html` + `search-settings-v2.html` for the badge/popover visuals; read `ExternalRow.tsx` for the existing enqueue/match logic and `adaptersApi.ts`/`downloadApi.ts`/`downloadStore.ts`.
- [ ] **Step 2 (TDD DownloadAction):** `DownloadAction.test.tsx` — table-drive the state mapping: in_library→in-library badge + onPlay called with libraryTrackId; running job→ProgressRing with the % ; completed→downloaded badge; no-job+1-downloader→Download click calls postDownload (spy) immediately; no-job+2-downloaders→Download click opens the popover (no immediate post); no-job+0-downloaders→disabled "No downloader". Fail → implement → pass.
- [ ] **Step 3 (TDD DownloadPopover):** picking a downloader calls `onPick(name)`; Esc/backdrop calls `onClose`.
- [ ] **Step 4:** `cd web && npx vitest run src/components/download && npm run build` → PASS/clean.
- [ ] **Step 5:** Commit `feat(download): optimistic DownloadAction + downloader popover`.

---

### Task 2: Search route rebuild (`/search`)

**Files:** rebuild `web/src/routes/Search.tsx` (+ test); optionally edit `TopBar.tsx` so its search navigates to `/search?q=<typed>` and Search reads `?q`.

Layout (read mockup): a search input + a `Segmented` `My Library | Everywhere` toggle. **My Library** mode → `useLibrarySearch(q)`; **Everywhere** mode → `useEverywhere(q,'track',enabled)` (+ album/artist as needed) and a row of **source chips** from `state.sources` (`Badge kind="status"` tone by `EnvelopeStatus`: ok→success, error→error, timeout→warning). Sectioned results: **Songs** as `TrackRow`s with `<DownloadAction result={r}/>` in the `right` slot (library tracks play directly; external tracks show the optimistic affordance); **Albums** as `MediaCard`s with an "In Library / N of M / Download all" `Badge` + a Download-all affordance (enqueue each missing track — or, if that's heavy, enqueue the album via the same popover); **Artists** as `MediaCard`s. Empty query → a designed prompt/EmptyState; streaming → show partial results + a subtle loading hint (don't reflow sections as envelopes arrive — `useEverywhere` already keeps stable sections).

- [ ] **Step 1:** Read current `Search.tsx` (SSE wiring, mode toggle, play wiring) to preserve behavior.
- [ ] **Step 2 (TDD):** `Search.test.tsx` — mode toggle switches library vs everywhere; source chips render per `sources` with the right tone; an external "not in library" track renders a Download affordance; an in-library result plays; empty query shows the prompt. Fail → implement → pass.
- [ ] **Step 3:** `cd web && npx vitest run && npm run build` → PASS/clean.
- [ ] **Step 4:** Commit `feat(routes): unified Search with optimistic downloads`.

---

### Task 3: Download tray — descriptive failures + actions (§9.9)

**Files:** enhance `web/src/components/DownloadTray.tsx` (+ test).

The tray lists active/queued/completed/failed jobs (`useDownloads().jobs`). For each: `Cover`/title/artist, a `ProgressRing` or status `Badge`, and actions. **Failed jobs must show a descriptive, human reason** derived from `job.error` framed with track + downloader context — e.g. `No matching source found for "<title>" on <downloaderName>`, `<downloaderName> exited with an error`, `Timed out` — never a bare "Failed"/"Error" (map known error substrings to friendly copy; fall back to "Couldn't download <title> on <downloaderName>" + the raw error available on expand). Actions: **Retry** (`retryDownload(id)`), **Cancel** (`cancelDownload(id)` for active), and when >1 downloader, **"Try <next downloader>"** (enqueue the same request with the next downloader in the chain). Strip any dead self-gate/`absolute` positioning left from the Phase-3 restyle so it sits cleanly in the right column.

- [ ] **Step 1:** Read current `DownloadTray.tsx` + `downloadApi` (retry/cancel) + `DownloadJob` error field.
- [ ] **Step 2 (TDD):** `DownloadTray.test.tsx` — a failed job renders a descriptive reason (not the bare word "Failed") that includes the track title; Retry calls `retryDownload`; Cancel (on an active job) calls `cancelDownload`; a generic/empty error still renders contextual copy (track + downloader), never "Error". Fail → implement → pass.
- [ ] **Step 3:** `cd web && npx vitest run && npm run build` → PASS/clean.
- [ ] **Step 4:** Commit `feat(download): descriptive, actionable failures in the tray`.

---

## Self-Review
**Spec coverage:** §9.2 optimistic two-state model + popover + no-downloader → T1; §9.2 unified library/everywhere search, source chips, sectioned results, album download-all → T2; §9.9 descriptive failures + retry/cancel/fallback → T3. ✅
**Placeholders:** none — DownloadAction's state table is enumerated, the route names its hooks + mockup regions, the tray's failure-copy rule is concrete. Full JSX delegated to implementers working from the validated mockups + existing SSE/download wiring, under adversarial review.
**Type consistency:** `DownloadAction`/`DownloadPopover` (T1) consumed by Search (T2); both use `useDownloads.byExternal` + `postDownload`. The `TrackRow.right` slot (Phase 4) carries `DownloadAction`. Tray reuses `retryDownload`/`cancelDownload`.
