# Reverb UI Overhaul — Spotify-Faithful Redesign (MVP Phase 1)

**Status:** Design — validated via interactive mockups, pending spec review
**Date:** 2026-06-21
**Owner:** Maximus

---

## 1. Goal

Reverb's backend works (Subsonic library, Spotify search, spotDL downloads, WebSocket/SSE realtime), and a functional React skeleton exists — but the visual layer is thin, identity-less, and you "can't get past the login page." The MVP is functionally Phase-1-complete but does not *look or feel* like a product.

This overhaul rebuilds the **presentation layer** of the web app to be **as close to Spotify as possible** — the dark three-pane shell, the typography, the spacing, the motion, the component vocabulary — while wiring in the few things that make it Reverb (unified library+internet search, one-click download, self-hosted admin). The existing data/state/transport layer is kept; only the UI is reimagined.

This is a redesign of MVP Phase 1 surfaces. It deliberately lays the structural groundwork for Phase 2 (artist pages, playlist sync, Lidarr) without building those features.

## 2. Non-Goals

- No new backend features or API changes (consume the existing API as mapped in §10).
- No Phase 2 features (full discography artist pages, playlist sync, Lidarr two-way sync). The shell must *accommodate* them, not implement them.
- No light theme (dark-first; light is roadmap).
- No standalone music server, mobile native apps, or social features.

## 3. The Craft Bar (read this first)

The single most important constraint: **vehemently avoid the "vibe-coded slop" look.** "As close to Spotify as possible" is impossible if the execution reads as generic AI frontend. Every implementation task is held to this bar.

**Banned (the tells of slop):**
- Emoji as icons (current code uses ▶ ⏸ ↓ 🔁). Replaced entirely by a custom SVG icon set.
- Arbitrary gradients/glows sprinkled for decoration; `rounded-2xl` + `shadow-lg` reflexively on every box.
- Arbitrary one-off spacing/size values. Everything comes from the scale (§6.3).
- Centered-everything layouts; missing visual hierarchy.
- Interactive elements missing states. Every button/row/control defines hover, focus-visible, active, disabled, and where relevant loading/selected.
- Loading = the word "Loading…"; empty = blank. Use skeletons and designed empty states.
- Fake data that doesn't line up (mismatched durations, lorem). Mock data must be plausible.
- Optical misalignment — things that "almost" line up.

**Required:**
- A real design-token layer (§6) is the source of truth; components never hardcode hex/px.
- A small set of disciplined, reused primitives (§7) — fewer, polished components over many rough ones.
- Pixel-precise alignment and proportions matched to actual Spotify (panel gaps, row heights, control sizes).
- Restrained, purposeful motion (§6.6); honor `prefers-reduced-motion`.
- Accessibility: visible `:focus-visible` rings, keyboard operability, sufficient contrast, ARIA on controls.

**Definition of done for any UI task:** matches the tokens, has all interaction states, has skeleton + empty + error states where data loads, is keyboard-accessible, and survives a side-by-side squint test against Spotify without looking "off."

## 4. Validated Design Decisions

These were confirmed interactively before this spec:

1. **Faithful Spotify layout**, not a creative reinterpretation. Three-pane shell: global top bar, "Your Library" left rail, center feed, right Now-Playing panel, bottom player bar.
2. **Accent is a configurable setting**, not a brand lock. The whole UI tints from one CSS variable (`--color-accent`). Default moves from today's red to the plan's **indigo-violet `#7C6AF7`**; red and a curated preset set remain one click away in User Settings. *(This default is the one item to confirm in review — see §12.)*
3. **User Settings and Admin are separate pages** (`/settings` vs `/admin`). Admin is gated.
4. **Admin providers split by type** into three sections: Library providers (single), Search providers (priority-ordered), Downloaders (fallback chain).
5. **Optimistic download model** for search results (§9.2). The plan's three-tier `✓ / ↓ / no-icon` collapses to two real row states because pre-flight downloadability is information we don't have; nuance moves to the click (popover) and the queue (failure handling).

## 5. Information Architecture

```
Unauthenticated
  /login            Password login (branded, dark)
  /setup            First-run wizard (admin password → library → search → downloader)

Authenticated (inside the shell)
  /                 Home feed (NEW — today the app defaults to /search)
  /search           Unified search: My Library / Everywhere
  /library          Browse: Albums / Artists / Playlists (filter chips)
  /album/:id        Album detail
  /artist/:id       Artist detail (Phase-1 depth; Phase-2-ready layout)
  /settings         User Settings: Appearance / Playback / Account
  /admin            Admin (gated): Providers / Server / Users
```

The persistent shell (top bar, library rail, right panel, player bar) wraps all authenticated routes. `/login` and `/setup` are standalone full-screen surfaces using the same tokens.

## 6. Visual Identity & Design Tokens

Tokens live in `web/src/index.css` as CSS variables and are surfaced to Tailwind via `tailwind.config.js`. Components reference semantic token classes, never raw values.

### 6.1 Color — surfaces & text (fixed)
| Token | Value | Use |
|---|---|---|
| `--bg-base` | `#000000` | App background, the gaps between panels |
| `--bg-surface` | `#121212` | Panels (rail, center, right, player) |
| `--bg-raised` | `#181818` | Cards |
| `--bg-raised-hover` | `#1f1f1f` / `#282828` | Card/row hover |
| `--bg-input` | `#1f1f1f` | Search bars, selects; hover `#2a2a2a` |
| `--border-subtle` | `#1e1e1e`–`#242424` | Panel/card borders, dividers |
| `--text-primary` | `#ffffff` | Titles, primary labels |
| `--text-secondary` | `#b3b3b3` | Subtitles, metadata |
| `--text-muted` | `#6a6a6a`–`#8a8a8a` | Tertiary, disabled |

### 6.2 Color — accent & semantics
- `--color-accent` — **configurable**, default `#7C6AF7`. Drives: primary buttons, In-Library/Downloaded content states, active nav, progress fill on hover, toggles, focus rings, selection. Stored space-separated for Tailwind alpha (`124 106 247`). `--color-accent-press` = a darkened derivative.
- **Status colors are fixed and semantic** (never the accent), reserved for system health so they stay legible regardless of the user's accent: success/connected `#1ed760`, warning/restart-pending `#f5c518`, error/failed `#ff6b6b`.
- **Rule:** content-state = accent; system-health = semantic. (Avoids the accent and a status green fighting.)

### 6.3 Spacing — 4px base scale
`4, 8, 12, 16, 20, 24, 32, 40, 48, 64`. Panel gap = `8`. Panel inner padding = `16–24`. No values off-scale.

### 6.4 Radii
`4` (inputs, small chips) · `6–8` (cards, panels, covers) · `999` (pills, buttons, search bar) · `50%` (avatars, play buttons, progress dots).

### 6.5 Typography
- **Family:** a Circular-like geometric sans as the Spotify-Circular stand-in. Recommendation: **Figtree** (used in mockups; close to Circular's friendly geometric feel, open-licensed, self-hostable). One family across the app — *not* the plan's Inter+Bricolage pairing, because faithful Spotify uses a single type family. *(Confirmable in review — §12.)* Self-host the font files (no FOUT, no third-party fetch at runtime).
- **Weights:** 400 / 500 / 600 / 700 / 800 / 900.
- **Scale:** display 48/900 (-0.02em) · h1 30/900 (-0.02em) · h2 24/800 (-0.01em) · h3 19/800 · body 14–15 (600 for labels, 400 for prose) · small 12.5–13 · micro 11 (700, uppercase, 0.1em tracking for section eyebrows). Tight tracking on large headings is part of the Spotify feel.

### 6.6 Motion
- Hover/press feedback: 120–180ms ease-out. Play-button hover scale ≤ 1.05.
- Panels/popovers/sheets: 200–280ms ease.
- Reveal-on-hover (play overlays, "Download all"): translateY 6–8px + fade, ≤180ms.
- No looping/ambient animation except the small now-playing equalizer bars and active-download progress.
- `@media (prefers-reduced-motion: reduce)` disables transforms/transitions.

### 6.7 Iconography
A single custom SVG icon set (stroke 2, 24-viewbox, `currentColor`), shipped as an inline sprite or per-icon React components. Covers: home, search, browse, back/forward, download, plus, play, pause, prev, next, shuffle, repeat, heart, queue, lyrics(mic), devices, volume, mini-player, fullscreen, sort, expand, bell, check, x, warn, retry. **Zero emoji** anywhere in the product.

### 6.8 Elevation
A short, fixed shadow set: `0` (flat panels) · cover/card `0 8px 18px -8px rgba(0,0,0,.6)` · floating play `0 8px 16px rgba(0,0,0,.35)` · popover/sheet `0 24px 60px rgba(0,0,0,.6)`. No reflexive `shadow-lg`.

## 7. Component System (primitives)

Build these once, reuse everywhere. Each ships with all states from §3.

- **Button** (variants: primary [accent fill, black text], secondary/ghost [outline], pill, icon-only) + **IconButton**.
- **Chip** (filter pill; selected = white fill / black text) and **Segmented** (My Library / Everywhere).
- **Toggle** (switch) and **Select**.
- **Cover** (square art, lazy-loaded, radius, optional hover play overlay, skeleton).
- **TrackRow** (grid: lead-state · cover · title/artist · album · state · duration; hover bg; the search/badge variant in §9).
- **MediaCard** (carousel/grid card: cover + title + subtitle + reveal play) and **ShortcutTile** (home 2-col compact).
- **Carousel** (horizontal scroll row with "Show all").
- **Badge** (In Library / Available / Downloading / Downloaded / disabled / status dots).
- **AdapterCard** (admin: order, logo, name + status pill, config summary, Test/Edit/Remove).
- **Popover** (download chooser), **Toast** (transient confirmations), **Skeleton** (cover/row/card), **EmptyState**, **ProgressRing**, **Equalizer** (now-playing bars).

Primitives live in `web/src/components/ui/`; surface-specific compositions stay in `components/` and `routes/`.

## 8. Layout & Shell

```
┌───────────────────────── Top Bar (global) ─────────────────────────┐
│ back/fwd   |   Home · [ Search … ] · Browse   |  Downloads · avatar │
├──────────────┬───────────────────────────────┬─────────────────────┤
│ Your Library │        Center (route)         │   Now Playing /      │
│  rail        │                               │   Queue panel        │
│ (chips +     │   home / search / album /     │  (collapsible)       │
│  item list)  │   artist / library / settings │                      │
├──────────────┴───────────────────────────────┴─────────────────────┤
│ ░░ cover · title/artist · ♥ │ shuffle prev ▶ next repeat · scrub │ … │
└─────────────────────────────────────────────────────────────────────┘
```

- Three `#121212` rounded panels float on `#000` with `8px` gaps; each scrolls independently.
- **Top bar:** back/forward; centered Home button + pill search ("Search your library — or everywhere") + Browse; right side **Downloads** activity (badge count) + avatar menu.
- **Left rail ("Your Library"):** header + filter chips (Playlists / Albums / Artists) + search-within + sort; scrollable item list with cover, name, "Type · owner", and the currently-playing item highlighted in accent with an equalizer glyph.
- **Right panel:** Now Playing (cover, title/artist, like) + "Next in queue" + an "About the artist" block (Phase-1: library facts; Phase-2-ready for full bios). Collapsible; on narrow widths it becomes an overlay. The download tray is reachable from the top-bar Downloads control.
- **Player bar:** left (cover/title/artist/like) · center (shuffle, prev, play, next, repeat + scrubber with times) · right (lyrics, queue, devices, volume, mini-player, fullscreen).

### 8.1 Responsive
- `≥1200px`: all three panels.
- `~900–1200px`: right panel collapses to a toggle/overlay.
- `<900px` (mobile): single column; bottom tab nav; mini-player that expands to a full-screen Now Playing; library rail and right panel become sheets. Reuse the existing mobile component roles (MiniPlayer, NowPlayingOverlay, MobileTabNav) rebuilt to the new tokens.

## 9. Core Surfaces

### 9.1 Home (`/`) — new
Spotify-faithful feed: top filter chips (All / Music / Downloads) → shortcut grid (8 compact tiles, currently-playing tile shows equalizer) → a "Just added to your library" hero → "Jump back in" carousel → **"Recently downloaded"** carousel (Reverb's identity, surfaced Spotify-style). Sourced from library browse endpoints (`newest`, `recent`, `frequent`) and the download history.

### 9.2 Search (`/search`) — the core loop
- Search bar + `My Library` / `Everywhere` segmented toggle.
- **Source chips** showing live aggregator status (Your Library ✓, Spotify ✓, MusicBrainz off) driven by SSE envelopes.
- Sectioned results (Songs, Albums, Artists), stable sections that don't reflow as SSE frames arrive.
- **Row state model (optimistic):**
  - **In Library** — accent check; plays on click.
  - **Download** — single clean action for everything we can attempt (the plan's "available" and "uncertain" merged; no confidence theater, no "try download").
  - **Downloading** — live progress ring + %.
  - **Downloaded** — transient accent check (content-state, per §6.2), then settles to In Library.
  - **No downloader** — the only non-actionable case: disabled, "No downloader," with a pointer to Admin → Downloaders.
- **On click:** one downloader → queues immediately; multiple → the plan's **download popover** (choose source, recommended pre-selected, "we'll fetch the closest match").
- **On failure:** surfaced in the **download tray** (not the search list) with an honest message + Retry / fall through to the next downloader.
- Albums show In Library / "7 / 12" / "Download all" affordances (Phase-2-ready).

### 9.3 Album (`/album/:id`)
Spotify album header (large cover, title, artist, year · track count · duration, big accent play + add) over a contextual color wash; numbered track list using TrackRow with per-track download/in-library state.

### 9.4 Artist (`/artist/:id`)
Phase-1 depth: header (image, name, library facts), top tracks, albums-in-library grid. Layout reserves space for the Phase-2 "full discography with download affordances" without building it now.

### 9.5 Library browse (`/library`)
Filter chips (Albums / Artists / Playlists); responsive cover grids using MediaCard; designed empty state when the library is unconfigured/empty.

### 9.6 User Settings (`/settings`)
Tabs: **Appearance** (accent swatches incl. red[default-today]/indigo[new default]/presets + custom hex; dynamic-album-background toggle; theme=Dark) · **Playback** (gapless/crossfade, stream quality, normalization — wired to what the engine supports) · **Account** (change password, logout). Available to any signed-in user.

### 9.7 Admin (`/admin`) — gated
Tabs: **Providers** / **Server** / **Users**. Providers split into three sections, each with header, count, and "Add":
- **Library providers** (single) — Navidrome/Subsonic; Test/Edit.
- **Search providers** (priority-ordered, drag to reorder) — Spotify, etc.; Test/Edit.
- **Downloaders** (fallback chain) — spotDL ready, Lidarr as a settable #2; Edit/Remove.
- AdapterCards show status pills (Connected/Ready/Restart-pending/Not configured) using semantic colors; a **restart-to-apply banner** when `config/pending-restart` is true; redacted secrets shown as set/last-4.

### 9.8 Setup wizard (`/setup`) & Login (`/login`)
Branded, dark, token-driven. **Login must be verified working end-to-end** (the reported blocker). Setup is the same multi-step flow (admin password → library → search → downloader) restyled, with Test-connection per step and a clear path into the app.

### 9.9 Download tray
Slide-over/overlay listing active/queued/completed/failed jobs with progress, Retry, Cancel, and the failure→fallback action from §9.2. Reachable from the top-bar Downloads control (badge = active count).

## 10. API Mapping (no backend changes)

| Surface | Endpoints |
|---|---|
| Auth/session | `POST /auth/login`, `/auth/logout`, `GET /me`, `GET /setup/status`, `POST /setup/admin` |
| Home/Library | `GET /library/albums?type=`, `/library/artists`, `/library/playlists`, `/library/album/{id}`, `/library/artist/{id}` |
| Search | `GET /library/search`, SSE `GET /search/everywhere` (Envelopes with pre-applied MatchResult) |
| Play | `GET /stream/{id}` (range), `GET /cover/{id}?size=` |
| Downloads | `POST /downloads`, `GET /downloads`, `POST /downloads/{id}/cancel|retry` |
| Realtime | WS `/ws` → `download.*`, `library.updated` |
| Settings | `GET/PUT /settings` (accentColor, dynamicBackground) |
| Admin | `GET /adapters`, `/adapters/available`, `POST/PUT/DELETE /adapters`, `POST /adapters/test`, `GET /config/pending-restart` |

Match states map from `MatchResult.status` (`in_library` → In Library; `not_in_library` → Download; `unknown` → Download). "No downloader" is derived from configured downloaders, not from match status.

## 11. Current → Target & Implementation Approach

Keep `web/src/lib/*` (stores, `api.ts`, `audioEngine.ts`, `realtime.ts`, `searchStream.ts`, palette services) — the data/state/transport layer is sound. Rebuild the presentation layer against it.

| Today | Target |
|---|---|
| `tailwind.config.js` + `index.css` (accent + base only) | Full token layer (§6); accent default → indigo |
| Emoji icons | Custom SVG icon set (§6.7) |
| `Sidebar.tsx` | "Your Library" rail (chips, item list, playing highlight) |
| No global top bar | New `TopBar` (home/search/browse/downloads/avatar) |
| `PlayerBar.tsx` (fake waveform) | Rebuilt player bar w/ full secondary controls + real scrubber |
| `PlayQueue` + `DownloadTray` slide-overs | Right Now-Playing/Queue panel + Downloads tray from top bar |
| default route `/search` | New `/` Home feed; search stays at `/search` |
| `Settings.tsx` (appearance + adapters mixed) | Split: `/settings` (user) + `/admin` (gated, providers by type) |
| `ExternalRow`/`TrackRow` ad hoc | Unified `TrackRow` + badge model (§9.2) |
| Text "Loading…" | Skeletons, empty states, toasts |

**Suggested phasing** (sequenced fully in the implementation plan via writing-plans):
1. **Foundation** — tokens, fonts, icon set, UI primitives (§7), accent plumbing. (Unblocks everything; visible nowhere yet.)
2. **Shell** — TopBar, Library rail, right panel, Player bar, responsive. Fix & verify **login/setup** here.
3. **Home + Library browse + Album/Artist** — feed and browse surfaces.
4. **Search + download loop** — unified results, badge model, popover, tray, failure handling.
5. **Settings + Admin** — user vs admin split, provider sections, restart banner.
6. **Polish pass** — states audit, motion, a11y, squint-test against Spotify.

## 12. Open Questions / Decisions to Confirm

1. **Default accent:** ship default = indigo `#7C6AF7` (recommended, per plan), with red + presets available? Or keep red as default and add indigo as a preset?
2. **Type family:** single Circular-like family (Figtree, recommended) for full Spotify fidelity, vs. the plan's Inter + Bricolage Grotesque pairing?
3. **Login blocker:** is "couldn't get past login" a setup-flow confusion (needs first-run setup) or a real auth bug? Phase 2 of implementation will reproduce and fix; flag if you already know the cause.
4. **Right panel default:** open by default (Spotify-like) or collapsed to maximize center on first run?

## 13. Risks

- **Slop creep** under time pressure — mitigated by §3 being a per-task definition of done.
- **Spotify proportions** are easy to approximate and hard to nail — mitigate with a measured reference and the squint test.
- **Scope is large** — mitigated by strict phasing; each phase is independently shippable behind the existing app.
