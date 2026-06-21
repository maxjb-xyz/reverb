# Reverb Phase 3 — Spotify Shell Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use `- [ ]`.

**Goal:** Rebuild the app chrome into the faithful Spotify three-pane shell — global TopBar, "Your Library" rail, center route outlet, a Now-Playing right panel (closed by default), and the full bottom PlayerBar — plus restyle Login/Setup. Presentation only; the existing stores/engine/realtime wiring is reused unchanged.

**Architecture:** `AppShell` becomes a CSS grid: `TopBar` row / `[LibraryRail | <Outlet/> | RightPanel]` / `PlayerBar`. Each panel is a `bg-surface` rounded card on the `bg-base` (#000) gap grid. Region components consume the existing `usePlayer` (transport/queue) and `useUI` (panels) stores and the new `web/src/components/ui` primitives. The right panel holds Now-Playing-or-Downloads, default closed.

**Tech Stack:** React 19 + TS + Tailwind, Vitest + @testing-library/react. Uses Phase 2 primitives (`Icon, Button, IconButton, Chip, Segmented, Toggle, Cover, Badge, Skeleton, EmptyState, ProgressRing, Equalizer`).

## Global Constraints (craft bar — every task)
- **Token classes only** (`bg-surface/raised/raised-hover/input`, `text-text-primary/secondary/muted`, `text-accent`, `border-border-subtle`, the spacing scale, `rounded`/`rounded-lg`/`rounded-full`, shadows `shadow-cover/float/pop`). No raw hex, no arbitrary px that duplicate a token. **Zero emoji** — use `Icon`.
- **Reuse the Phase 2 primitives** — do not hand-roll a button/chip/toggle/icon that a primitive already covers.
- **Every interactive element** has hover, `focus-visible` ring, active, disabled states; controls carry `aria-label`.
- **Reuse existing store APIs unchanged:** `usePlayer()` (`current`, playback state, `toggle/next/prev/seekMs/setVolume/toggleShuffle/cycleRepeat`, queue: `queue/jumpTo/removeAt/moveItem`), `useUI()` (`rightPanel`, `openPanel/closePanel/togglePanel`, mobile `nowPlayingOpen`), `useDownloads()`, `libraryApi` hooks, `coverUrl`. Read the existing component you are replacing to learn the exact store shape; do NOT change store signatures except the one `RightPanel` addition in Task 2.
- **Visual source of truth:** `/Users/maximusjb/Repos/reverb/.superpowers/brainstorm/87868-1782028461/content/spotify-faithful.html` (the entire shell: top bar, `.lib-*` rail, `.np-*` right panel, `.player` bar). Match its structure, spacing, and proportions.
- **Keep tests green.** Rebuilt components break their existing `.test.tsx` — rewrite each affected test to assert the NEW structure's real behavior (don't delete coverage, don't assert on removed emoji). Run `cd web && npx vitest run` (full) before each commit; `npm run build` must be clean.
- Every commit ends with: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`. Branch `feat/ui-overhaul-spotify`. Spec §8, §9 ([../specs/2026-06-21-reverb-ui-overhaul-design.md](../specs/2026-06-21-reverb-ui-overhaul-design.md)).

## File Structure
- `web/src/components/shell/TopBar.tsx` (new) — global top bar.
- `web/src/components/shell/LibraryRail.tsx` (new, replaces `Sidebar.tsx`) — left "Your Library".
- `web/src/components/shell/PlayerBar.tsx` (rebuild from `components/PlayerBar.tsx`) — bottom bar.
- `web/src/components/shell/NowPlayingPanel.tsx` (new) — right panel (now-playing + up-next + about-artist).
- `web/src/components/AppShell.tsx` (rebuild) — the grid assembly.
- `web/src/lib/uiStore.ts` (modify) — add `'nowplaying'` to `RightPanel`.
- `web/src/components/MiniPlayer.tsx`, `MobileTabNav.tsx`, `NowPlayingOverlay.tsx`, `DownloadTray.tsx`, `PlayQueue.tsx` (restyle to tokens; PlayQueue's up-next list folds into NowPlayingPanel).
- `web/src/routes/Login.tsx`, `web/src/routes/Setup.tsx` (restyle).
- Tests alongside each.

---

### Task 1: TopBar + LibraryRail (top & left chrome)

**Files:** create `web/src/components/shell/TopBar.tsx` (+ test); create `web/src/components/shell/LibraryRail.tsx` (+ test); delete `Sidebar.tsx` after AppShell stops importing it (Task 3 — for now leave it).

**TopBar** (read mockup `.top`): left = back/`fwd` `IconButton`s (use `window.history`); center = a round Home `IconButton` (navigates `/`) + a pill search button (`bg-input`, `Icon search` + "Search your library — or everywhere", navigates `/search`) + a `browse` icon; right = a **Downloads** `Button` (ghost, `Icon dl`, `togglePanel('downloads')`, badge = `useDownloads().active().length`) + an avatar menu button (`Icon`/initial; menu offers Logout → `POST /auth/logout` then reload). All token classes, all aria-labels.

**LibraryRail** (read mockup `.lib-*`): header row "Your Library" (`browse` icon) + an expand `IconButton`; a `Chip` filter row (Playlists / Albums / Artists) controlling which list shows; a list of items (from `usePlaylists`/`useArtists`/`useAlbums` library hooks) — each row: `Cover` (artists rounded-full), name, "Type · owner" in `text-text-secondary`; the row whose album/track is currently playing (compare to `usePlayer().current`) is highlighted `text-accent` with an `Equalizer` glyph. NavLinks for Search/Library/Settings live in the rail header or top of the list (keep navigation reachable). Loading → `Skeleton` rows; empty → `EmptyState`.

- [ ] **Step 1:** Read the mockup and the current `Sidebar.tsx`/`libraryApi` hooks for exact store/query usage.
- [ ] **Step 2 (TDD TopBar):** write `TopBar.test.tsx` — asserts: a search control navigating to `/search` (mock router), a Downloads button that calls `togglePanel('downloads')` (spy the store), the downloads badge shows when `active()` > 0, all buttons have accessible names. Fail → implement → pass.
- [ ] **Step 3 (TDD LibraryRail):** write `LibraryRail.test.tsx` — asserts: filter chips switch the list; a currently-playing item gets the accent/`Equalizer` treatment (mock `usePlayer`); loading shows skeletons. Fail → implement → pass.
- [ ] **Step 4:** `cd web && npx vitest run src/components/shell && npm run build` → PASS/clean.
- [ ] **Step 5:** Commit `feat(shell): TopBar + Your Library rail`.

---

### Task 2: PlayerBar + NowPlayingPanel + right-panel state

**Files:** modify `web/src/lib/uiStore.ts`; create `web/src/components/shell/PlayerBar.tsx` + `NowPlayingPanel.tsx` (+ tests). Read existing `components/PlayerBar.tsx` + `PlayQueue.tsx` for store usage, then supersede them.

- [ ] **Step 1 (uiStore TDD):** add `'nowplaying'` to `RightPanel` (`'nowplaying' | 'downloads' | null`, default `null` = closed). Update `uiStore` and write a test asserting `togglePanel('nowplaying')` opens then closes it and that opening `'downloads'` replaces `'nowplaying'`. Keep `nowPlayingOpen` (mobile) untouched.

- [ ] **Step 2 (PlayerBar, read mockup `.player`):** rebuild as a 3-column grid — left: `Cover` + title/artist (`text-text-primary`/`secondary`) + a heart `IconButton`; center: shuffle/`prev`/**play** (white circle, accent on hover)/`next`/repeat `IconButton`s wired to `usePlayer` actions (`toggleShuffle/prev/toggle/next/cycleRepeat`), with the active shuffle/repeat in `text-accent`, plus a scrubber (current time · seekable track · duration) driven by `usePlayer` position/duration and `seekMs`; right: lyrics(`mic`)/queue(`queue`, toggles `'nowplaying'`)/`device`/volume(`vol` + slider→`setVolume`)/`mini`/`full` `IconButton`s. Hidden `< md` (mobile uses MiniPlayer). Preserve the existing keyboard shortcuts from the old PlayerBar. Rewrite `PlayerBar.test.tsx` to assert transport buttons call the right store actions and the scrubber seeks.

- [ ] **Step 3 (NowPlayingPanel, read mockup `.np-*`):** a `bg-surface` panel: header (context name + close `IconButton` → `closePanel`), big `Cover`, title/artist + heart, a "Next in queue" card listing the upcoming `usePlayer().queue` items (click → `jumpTo`), and an "About the artist" card (image, name, "In your library · N albums" from the current track's artist via `getArtist`; Phase-1 facts only). Write `NowPlayingPanel.test.tsx`: renders current track, lists up-next, close button calls `closePanel`.

- [ ] **Step 4:** `cd web && npx vitest run && npm run build` → PASS/clean.
- [ ] **Step 5:** Commit `feat(shell): PlayerBar + Now-Playing panel + right-panel state`.

---

### Task 3: AppShell grid assembly + responsive + mobile restyle

**Files:** rebuild `web/src/components/AppShell.tsx`; restyle `MiniPlayer.tsx`, `MobileTabNav.tsx`, `NowPlayingOverlay.tsx`, `DownloadTray.tsx` to tokens; delete `Sidebar.tsx` + `PlayQueue.tsx` (folded into rail/NowPlayingPanel) and remove their imports/tests. Update `AppShell.test.tsx`.

- [ ] **Step 1:** Rebuild `AppShell` as the grid: `TopBar` (row 1) / middle `[LibraryRail | <Outlet/> (bg-surface rounded card, scrolls) | RightPanel]` / `PlayerBar` (row 3), `bg-base` gaps. The right column renders `NowPlayingPanel` when `rightPanel==='nowplaying'`, `DownloadTray` when `'downloads'`, and **nothing (collapsed) when `null` (default)**. Keep the ambient dynamic-background gradient but source the base from the token (`--bg-base`) and the wash from the palette. Keep `useRealtime()`.
- [ ] **Step 2:** Responsive: `≥1200px` all three columns; `900–1200px` right panel overlays instead of taking a column; `<md` single column — hide LibraryRail/PlayerBar/right column, show `MiniPlayer` + `MobileTabNav`, and `NowPlayingOverlay` for the mobile full-screen player. Restyle those three mobile components + `DownloadTray` to token classes + primitives (no emoji).
- [ ] **Step 3:** Update `AppShell.test.tsx` to assert: TopBar/LibraryRail/PlayerBar present on desktop; right column is absent when `rightPanel===null` and renders NowPlayingPanel when `'nowplaying'`. Rewrite the mobile components' tests for the new markup. Remove `Sidebar.test`/`PlayQueue.test` if those components are deleted (migrate any still-relevant assertions into LibraryRail/NowPlayingPanel tests).
- [ ] **Step 4:** `cd web && npx vitest run && npm run build` → PASS/clean. Grep to confirm no leftover emoji in shell/components: `grep -rnP "[\x{1F000}-\x{1FAFF}\x{2600}-\x{27BF}\x{2B00}-\x{2BFF}]" web/src/components web/src/routes` returns nothing.
- [ ] **Step 5:** Commit `feat(shell): three-pane AppShell grid + responsive + mobile restyle`.

---

### Task 4: Login + Setup restyle

**Files:** rebuild `web/src/routes/Login.tsx`, `web/src/routes/Setup.tsx` (+ keep their tests green). Read both current files first to preserve behavior (Login: `loginErrorMessage` from Phase 1; Setup: the admin-password → library → search → downloader steps via `AdapterForm`).

- [ ] **Step 1:** Login — a branded, centered card on `bg-base` with a subtle gradient backdrop: Reverb wordmark, a password field (`bg-input`, focus ring), a primary `Button` "Log in", inline error via the Phase-1 `loginErrorMessage`. Preserve the submit→reload behavior. Update `Login.test` if present for new markup; keep the error-copy assertions.
- [ ] **Step 2:** Setup — the same dark, branded shell wrapping the existing multi-step wizard; restyle each step with primitives + `AdapterForm`, a progress indicator, Test-connection buttons, and a clear final hand-off (don't dead-end on "restart then log in" — show a primary action). Keep the step logic/endpoints unchanged.
- [ ] **Step 3:** `cd web && npx vitest run && npm run build` → PASS/clean.
- [ ] **Step 4:** Commit `feat(auth): branded Login + Setup onboarding`.

---

## Self-Review
**Spec coverage (§8, §9.8):** TopBar/LibraryRail → T1; PlayerBar + Now-Playing right panel (closed by default) → T2; three-pane grid + responsive + mobile → T3; Login/Setup restyle → T4. Dynamic-background wash preserved (T3). Downloads reachable from TopBar (T1), queue via PlayerBar (T2). ✅
**Placeholders:** none — each task names exact files, the store APIs to reuse, the mockup regions, and concrete test assertions. Full code is intentionally delegated to the implementer working from the validated mockup + existing components (compositions of Phase-2 primitives), under adversarial review.
**Type consistency:** `RightPanel` extended once (T2) and consumed by AppShell (T3). All regions consume the unchanged `usePlayer`/`useUI` APIs. Primitives imported from `web/src/components/ui`.
