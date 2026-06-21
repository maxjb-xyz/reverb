# Reverb Phase 7 — Polish, Token & A11y Sweep Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use `- [ ]`.

**Goal:** Sweep the accumulated polish debt from Phases 2–6 — close the last token-discipline gaps (the systemic `text-black`-on-accent), replace glyph-literal arrows with real `Icon`s, add the missing primitive variants (`ProgressRing` indeterminate, `IconButton` className), and fix the remaining a11y/motion/state gaps — so the whole app passes the Spotify squint-test and the anti-slop bar end-to-end.

**Architecture:** Mostly mechanical cleanup against the existing token system + primitives, verified by repo-wide greps. No new surfaces. Two tasks: (T1) token + icon sweep, (T2) a11y + states + motion sweep.

**Tech Stack:** React 19 + TS + Tailwind, Vitest.

## Global Constraints (craft bar)
- Token classes only; **zero emoji and zero rendered glyph-literals** (no `✓ ✗ → ↑ ↓ …` or their HTML entities in rendered output — comment separators are fine). Every interactive element: hover, `focus-visible` ring, active, disabled, accessible name. Honor `prefers-reduced-motion`.
- Keep the full suite green (`cd web && npx vitest run`) + `npm run build` clean before each commit; update tests touched by these changes. Commit trailer: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`. Branch `feat/ui-overhaul-spotify`. The worklist below is drawn from the per-phase reviews (see `.superpowers/sdd/progress.md`).

## File Structure
- `web/src/index.css` + `web/tailwind.config.js` — add the `on-accent` color token.
- `web/src/components/ui/Icon.tsx` — add `up`, `down`, `chevron-down` glyphs.
- `web/src/components/ui/IconButton.tsx` — add a `className` passthrough.
- `web/src/components/ui/ProgressRing.tsx` — add an `indeterminate` variant.
- `web/src/components/ui/EmptyState.tsx` consumers — pick fitting icons.
- Sweep edits across: `shell/TopBar`, `shell/PlayerBar`, `MobileTabNav`, `MediaCard`, `Chip`, `routes/Settings`, `admin/AdapterCard`, `AdapterForm`, `NowPlayingOverlay`, `download/DownloadAction`, `DownloadTray`, `AccentSwatches`, `lib/settingsApi`.

---

### Task 1: Token + icon sweep (kill the last raw palette & glyph-literals)

- [ ] **Step 1 — add the `on-accent` token.** In `web/src/index.css` add `--on-accent: 0 0 0;` (black channels — text/icons sitting ON the accent fill). In `tailwind.config.js` add `'on-accent': 'rgb(var(--on-accent) / <alpha-value>)'` to `extend.colors`. (Do NOT use `text-base` for this — `base` collides with Tailwind's `text-base` font-size utility.)
- [ ] **Step 2 — replace every `text-black`-on-accent** with `text-on-accent` across: `shell/TopBar.tsx` (avatar, downloads badge), `MobileTabNav.tsx` (badge), `components/ui/MediaCard.tsx` (play icon), `components/ui/Chip.tsx` (selected — confirm: selected chip is `bg-text-primary` so it wants `text-base`/`text-surface`; if it's on `bg-accent` use `text-on-accent`, else leave), `routes/Settings.tsx:90` (avatar), `admin/AdapterCard.tsx` (logo chip if on accent). Verify each is actually on an accent background before swapping.
- [ ] **Step 3 — add icons + kill glyph-literals.** Add `up`, `down`, and `chevron-down` paths to `components/ui/Icon.tsx` (clean 24×24 paths) and extend `IconName`. Replace: `admin/AdapterCard.tsx` `&#8593;`/`&#8595;` reorder arrows → `<Icon name="up"/>`/`<Icon name="down"/>`; `NowPlayingOverlay.tsx` close (`fwd` + `rotate-90`) → `<Icon name="chevron-down"/>`; fix the `sort` icon chevron alignment if trivial. Update `EmptyState` icon props that are semantically wrong (e.g. Users tab / generic empties using `"search"`) to a fitting icon.
- [ ] **Step 4 — AdapterForm cleanup.** In `components/AdapterForm.tsx` replace `text-green-400` (raw palette) with `text-success`, and the `✓`/`✗` test-result glyph-literals with `<Icon name="check"/>` / `<Icon name="x"/>`.
- [ ] **Step 5 — IconButton className passthrough.** Add an optional `className?: string` to `components/ui/IconButton.tsx`, merged after its base classes, so callers can theme it (keeps its focus-ring + a11y). (Optional: migrate `MediaCard`/`DownloadAction` raw `<button>`s to `IconButton` now that it's themable — only if low-risk; otherwise leave for safety.)
- [ ] **Step 6 — verify + commit.** `cd web && npx vitest run && npm run build` (green/clean). Repo-wide proof (must be clean):
  `grep -rnP "\btext-black\b|text-white|text-(neutral|gray|zinc|slate)-[0-9]|(from|to)-[a-z]+-[0-9]" web/src/components web/src/routes` → only intentional non-accent uses, ideally empty;
  `grep -rnP "[\x{2190}-\x{21FF}\x{2300}-\x{27BF}\x{2B00}-\x{2BFF}\x{2026}]|&#8[0-9]{3};" web/src/components web/src/routes | grep -vP "^\S+:\d+:\s*(//|/\*|\*)" ` → no rendered glyph-literals (comment lines excluded).
  Commit `polish: text-on-accent token + real icons (kill raw palette & glyph-literals)`.

---

### Task 2: A11y, states & motion sweep

- [ ] **Step 1 — ProgressRing indeterminate.** Add an `indeterminate?: boolean` prop to `components/ui/ProgressRing.tsx` that renders a spinning partial arc (a rotating stroke that respects `prefers-reduced-motion` — when reduced, show a static partial arc). Use it in `download/DownloadAction.tsx` for `queued` / `progress < 0` instead of a 0% ring. Add/adjust tests.
- [ ] **Step 2 — focus rings.** Add `focus-visible:ring-2 focus-visible:ring-accent` (and avoid bare `outline-none` without a ring) to: `shell/PlayerBar.tsx` volume `<input type="range">` and any other native `<input>`/`<button>` missing it (DownloadTray "Show details", AccentSwatches custom input). Quick audit: `grep -rn "outline-none" web/src/components web/src/routes` and ensure each pairs with a visible `focus-visible:ring`.
- [ ] **Step 3 — settings invalidation + swatch init.** In `lib/settingsApi.ts` (or the callers), invalidate `['settings']` after `putSettings` so the cache reflects saved values (restore the behavior the old Settings had). In `components/AccentSwatches.tsx`, initialize the custom-hex input to the current accent when it isn't a preset (not `#000000`).
- [ ] **Step 4 — DownloadTray positioning.** Confirm `components/DownloadTray.tsx` has no dead self-gate / stale `absolute inset-0 z-30` left from the Phase-3 restyle (it lives inside the right column now). Remove any that remain.
- [ ] **Step 5 — reduced-motion + states audit.** Verify the global `prefers-reduced-motion` rule (Phase 2 `index.css`) actually neutralizes the `Equalizer` bars, `Skeleton` pulse, and hover transforms (they should already inherit it; add a targeted guard only if one escapes). Spot-check that data surfaces (Home, Library, Album, Artist, Search, Admin) each render a `Skeleton` loading state and an `EmptyState`/empty branch — note any missing in the report (fix only quick ones).
- [ ] **Step 6 — verify + commit.** `cd web && npx vitest run && npm run build` (green/clean). Commit `polish: indeterminate ring, focus rings, settings invalidation, reduced-motion`.

---

## Self-Review
**Spec coverage (§3 craft bar, §6.6 motion, accessibility):** token discipline closed (on-accent token, no raw palette) → T1; glyph-literals → Icon → T1; primitive gaps (ProgressRing indeterminate, IconButton className) → T1/T2; a11y focus rings + reduced-motion + states audit → T2; settings invalidation regression → T2. The squint-test/final visual pass is the whole-branch review that follows. ✅
**Placeholders:** none — every item names the exact file(s) and the concrete change, with grep-based verification proving the sweep is complete.
**Type consistency:** `on-accent` token added once; `up`/`down`/`chevron-down` added to `IconName`; `ProgressRing.indeterminate` + `IconButton.className` are additive optional props (no breaking changes to existing call sites).
