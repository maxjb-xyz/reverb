# Reverb Phase 4 — Home & Browse Surfaces Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use `- [ ]`.

**Goal:** Build the content surfaces inside the shell — a new Home feed (`/`), the Library browse page, and the Album & Artist pages — as faithful Spotify pages composed from the Phase-2 primitives and shared molecules.

**Architecture:** Three shared molecules (`MediaCard`, `TrackRow`, `Carousel`) compose `Cover/Icon/IconButton/Badge` and are reused across all surfaces. Each route consumes the existing `libraryApi` hooks and `usePlayer().playTrackList` to play. Empty/loading states use `EmptyState`/`Skeleton`. No backend changes (verified §9.1).

**Tech Stack:** React 19 + TS + Tailwind, Vitest. Library hooks: `useAlbums(type)`, `useArtists()`, `usePlaylists()`, `useAlbum(id)`, `useArtist(id)`, `coverUrl(id,size)`, `streamUrl`. Player: `usePlayer().playTrackList(tracks, startIndex)`.

## Global Constraints (craft bar — every task)
- **Token classes only** (`bg-surface/raised/raised-hover`, `text-text-primary/secondary/muted`, `text-accent`, `rounded*`, spacing scale, `shadow-cover`). No raw hex/palette, no `text-white`, no arbitrary `[..px..]` duplicating a token. **Zero emoji** — `Icon`.
- **Reuse Phase-2 primitives + the Phase-4 molecules.** Don't hand-roll a card/row/cover.
- **Every interactive element**: hover, `focus-visible` ring, active, disabled, `aria-label`/accessible name.
- **Reuse `libraryApi` hooks + `usePlayer` unchanged.** Read the existing route file you replace (`web/src/routes/Library.tsx`, `Album.tsx`, `Artist.tsx`, `components/TrackRow.tsx`) to learn current usage; don't invent hook/field names.
- **Loading → `Skeleton`; empty/zero-data → `EmptyState`** (Home hides empty sections per §9.1; e.g. `recent`/`frequent` may be empty on fresh installs).
- **Visual source of truth:** `/Users/maximusjb/Repos/reverb/.superpowers/brainstorm/87868-1782028461/content/spotify-faithful.html` — `.grid8` (home shortcut grid), `.hero` (just-added hero), `.carousel`/`.acard` (carousels), `.short` rows; and `.../search-settings-v2.html` `.trow` (track rows). Match structure/spacing.
- Keep the suite green (`cd web && npx vitest run`) and `npm run build` clean before each commit. Every commit ends with: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`. Branch `feat/ui-overhaul-spotify`. Spec §9.1, §9.3, §9.4, §9.5.

## File Structure
- `web/src/components/ui/MediaCard.tsx`, `Carousel.tsx` (+ tests) — new molecules.
- `web/src/components/ui/TrackRow.tsx` (rebuild from `web/src/components/TrackRow.tsx`) (+ test).
- `web/src/routes/Home.tsx` (new) (+ test); wire `/` in `web/src/App.tsx`.
- `web/src/routes/Library.tsx` (rebuild) (+ test).
- `web/src/routes/Album.tsx`, `Artist.tsx` (rebuild) (+ tests).

---

### Task 1: Shared molecules — MediaCard, Carousel, TrackRow

**Interfaces (produce these — later tasks consume):**
- `MediaCard(props: { title: string; subtitle?: string; coverId?: string; rounded?: 'md'|'full'; onClick?: () => void; onPlay?: () => void; badge?: ReactNode })` — Spotify card: `Cover` (uses `coverUrl`), title (`text-text-primary` truncate), subtitle (`text-text-secondary` 2-line clamp), an accent reveal-on-hover round play `IconButton` (calls `onPlay`, stops propagation), whole card `onClick` navigates. `bg-raised` hover `bg-raised-hover`, radius, `shadow-cover` on the cover.
- `Carousel(props: { title: string; onShowAll?: () => void; children: ReactNode })` — section header (h2 + "Show all") + a horizontal-scroll row.
- `TrackRow(props: { track: Track; index?: number; active?: boolean; onPlay: () => void; right?: ReactNode })` — grid row (lead index/`Equalizer` when active · `Cover` · title/artist · album · `right` slot · duration formatted m:ss). Hover shows a play affordance; `active` → `text-accent`. The `right` slot is where Phase 5 injects download badges.

- [ ] **Step 1:** Read the mockup (`.acard`, `.short`, `.trow`) and the current `components/TrackRow.tsx` for the existing duration-format + play wiring.
- [ ] **Step 2 (TDD each):** write tests first — MediaCard: renders title/subtitle/cover, `onPlay` fires on the play button without firing `onClick`, `onClick` fires on the card body; Carousel: renders title + "Show all" calls `onShowAll`; TrackRow: renders track meta + formatted duration, `active` adds the accent/`Equalizer`, clicking calls `onPlay`. Fail → implement → pass. Extend the `ui` barrel.
- [ ] **Step 3:** `cd web && npx vitest run src/components/ui && npm run build` → PASS/clean.
- [ ] **Step 4:** Commit `feat(ui): MediaCard, Carousel, TrackRow molecules`.

---

### Task 2: Home feed (`/`)

**Files:** create `web/src/routes/Home.tsx` (+ test); modify `web/src/App.tsx` (add `<Route path="/" element={<Home/>} />`, keep `/search` etc.; the `*` fallback may stay → `/search` or change to `/` — keep Search reachable).

Layout (read mockup home): top `Chip` filter row (All / Music / Downloads — visual only is fine for now) → a 2-col **shortcut grid** of 8 `MediaCard`-like compact tiles (recent albums/playlists; the currently-playing tile shows `Equalizer`) → a **"Just added to your library"** hero (largest recent album: big `Cover`, "Album · Artist", title, accent play `Button`) → a **"Jump back in"** `Carousel` of `MediaCard`s → a **"Recently downloaded"** `Carousel` (from `useDownloads()` completed jobs, newest first). Data: `useAlbums('newest')`, `useAlbums('recent')`, `useAlbums('frequent')`, `usePlaylists()`, `useDownloads()`. **Hide any section whose data is empty** (don't render an empty carousel). Loading → `Skeleton` rows/cards. Clicking a card → navigate to its album/artist; play button → `playTrackList`.

- [ ] **Step 1:** Read `App.tsx` routing + `downloadStore` for completed-jobs accessor.
- [ ] **Step 2 (TDD):** `Home.test.tsx` — mock the library hooks: renders the shortcut grid + "Jump back in" when data present; **hides "Recently downloaded" when there are no completed downloads**; shows skeletons while loading. Fail → implement → pass. Wire `/` route.
- [ ] **Step 3:** `cd web && npx vitest run && npm run build` → PASS/clean.
- [ ] **Step 4:** Commit `feat(routes): Home feed`.

---

### Task 3: Library browse (`/library`)

**Files:** rebuild `web/src/routes/Library.tsx` (+ test).

A page header ("Your Library") + a `Chip` filter row (Albums / Artists / Playlists) switching a responsive `MediaCard` grid (`grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5`, artists rounded-full). Data: `useAlbums()`, `useArtists()`, `usePlaylists()`. Loading → skeleton grid; empty → `EmptyState` ("Nothing here yet" + hint). Card click → navigate to album/artist.

- [ ] **Step 1:** Read the current `Library.tsx` for existing tab/grid behavior to preserve routing targets.
- [ ] **Step 2 (TDD):** `Library.test.tsx` — chips switch albums/artists/playlists grids; empty list → EmptyState; loading → skeletons; card navigates. Fail → implement → pass.
- [ ] **Step 3:** `cd web && npx vitest run && npm run build` → PASS/clean.
- [ ] **Step 4:** Commit `feat(routes): Library browse`.

---

### Task 4: Album (`/album/:id`) + Artist (`/artist/:id`)

**Files:** rebuild `web/src/routes/Album.tsx`, `web/src/routes/Artist.tsx` (+ tests).

- **Album:** Spotify header — large `Cover`, "Album", title (display size), "Artist · year · N songs · duration", a big accent play `Button` (plays the album via `playTrackList(album.tracks, 0)`) + add `IconButton`; below, a numbered `TrackRow` list (each row plays from its index). Contextual color wash at the top (reuse the palette hook if easy, else a subtle gradient). Loading → skeleton header + rows.
- **Artist:** header (artist `Cover`/image, name display size, "In your library · N albums"), a "Popular"/top-tracks `TrackRow` list if available, and an albums `MediaCard` grid (`artist.albums`). Layout reserves room for the Phase-2 discography but builds only library data.

- [ ] **Step 1:** Read current `Album.tsx`/`Artist.tsx` + `useAlbum`/`useArtist` shapes (`album.tracks`, `artist.albums`).
- [ ] **Step 2 (TDD Album):** `Album.test.tsx` — renders header (title/meta), play button calls `playTrackList` with the album tracks, track rows render and a row click plays from its index, loading → skeleton. Fail → implement → pass.
- [ ] **Step 3 (TDD Artist):** `Artist.test.tsx` — renders name + album grid (navigates to album), loading → skeleton. Fail → implement → pass.
- [ ] **Step 4:** `cd web && npx vitest run && npm run build` → PASS/clean.
- [ ] **Step 5:** Commit `feat(routes): Album + Artist pages`.

---

## Self-Review
**Spec coverage:** §9.1 Home (shortcut grid, just-added hero, jump-back-in + recently-downloaded carousels, hide empty) → T2; §9.5 Library browse → T3; §9.3 Album → T4; §9.4 Artist (Phase-1 depth, Phase-2-ready) → T4; shared row/card vocabulary → T1. ✅
**Placeholders:** none — molecule interfaces are exact; routes name their hooks + mockup regions + concrete test assertions. Full JSX delegated to the implementer working from the validated mockup, under adversarial review.
**Type consistency:** `MediaCard`/`Carousel`/`TrackRow` (T1) consumed by T2–T4; all use `coverUrl`/`playTrackList` and the unchanged `libraryApi` hooks; `TrackRow.right` slot is the Phase-5 download-badge seam.
