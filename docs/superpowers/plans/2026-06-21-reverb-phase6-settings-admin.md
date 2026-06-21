# Reverb Phase 6 — User Settings & Admin Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use `- [ ]`.

**Goal:** Split today's mixed `Settings.tsx` into a **User Settings** page (`/settings`: Appearance, Account) and a separate **Admin** page (`/admin`: Providers split into Library / Search / Downloaders, with the restart-to-apply banner) — both in the Spotify skin, all real/wired controls (no fake toggles).

**Architecture:** `AdapterCard` + `AdapterSection` + `RestartBanner` are the admin building blocks (reusing the existing `adaptersApi` CRUD/test + `AdapterForm`). User Settings reuses `settingsApi` (`applyAccent`, `putSettings`, `useSettings`). The adapter-management logic currently in `Settings.tsx` moves wholesale into Admin; `Settings.tsx` keeps only user-scoped preferences.

**Tech Stack:** React 19 + TS + Tailwind, Vitest. APIs: `useAdapters()`, `useAvailableAdapters()`, `usePendingRestart()`, `createAdapter/updateAdapter/deleteAdapter/testAdapter`, `AdapterInstance{type,name,enabled,priority,config}`, `SECRET_SENTINEL`; `useSettings()`, `putSettings({accentColor,dynamicBackground})`, `applyAccent(hex)`; `AdapterForm`.

## Global Constraints (craft bar — every task)
- **Token classes only** (no raw hex/palette/text-white/arbitrary-px). **Zero emoji/glyph-literals** — `Icon`. **Reuse primitives** (`Button, IconButton, Chip, Toggle, Select, Badge, Icon, EmptyState, Skeleton`) + `AdapterForm`. Every control: hover, `focus-visible` ring, active, disabled, accessible name.
- **No fake/non-functional controls.** Only render a toggle/field that is actually wired to a real setting/endpoint. If a spec'd section (e.g. Playback) has nothing real to wire yet, OMIT it (or a single honest "coming soon" line) rather than shipping dead switches.
- **Status/health uses the fixed semantic colors** (success/warning/error), never the accent. Accent swatches drive `--color-accent` live via `applyAccent` and persist via `putSettings`.
- **Reuse the existing adapter CRUD logic** from the current `Settings.tsx` (read it — `onToggle`, `onReorder`, `onRemove`, `stripIsSet`, the create/edit flow via `AdapterForm`); move it into Admin unchanged in behavior.
- **Admin gating:** MVP is single-admin — the authenticated user reaches `/admin` (already auth-gated by the App guard). Label it "Admin"; multi-user gating is future. Note it; don't build auth roles now.
- **Visual source of truth:** `/Users/maximusjb/Repos/reverb/.superpowers/brainstorm/87868-1782028461/content/search-settings-v2.html` — `.set-*` (settings rows/tabs), `.swatches`/`.sw` (accent swatches), `.toggle`, `.adapter` (AdapterCard), `.sec-title`/`.add`, `.admin-banner` (restart banner), `.status` pills.
- Suite green + `npm run build` clean before each commit. Commit trailer: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`. Branch `feat/ui-overhaul-spotify`. Spec §9.6, §9.7.

## File Structure
- `web/src/components/admin/AdapterCard.tsx`, `AdapterSection.tsx`, `RestartBanner.tsx` (+ tests).
- `web/src/routes/Settings.tsx` (rebuild → user prefs only) (+ test).
- `web/src/routes/Admin.tsx` (new) (+ test); wire `/admin` in `web/src/App.tsx`; add an Admin link in `TopBar` avatar menu (and/or LibraryRail).
- `web/src/components/AccentSwatches.tsx` (+ test) — the preset/custom accent picker.

---

### Task 1: Admin building blocks — AdapterCard, AdapterSection, RestartBanner

**Interfaces:**
- `AdapterCard(props: { instance: AdapterInstance; onTest; onEdit; onToggle; onRemove; onReorder?; order?: number })` — read mockup `.adapter`: order index, a logo chip (first letter), name + a status `Badge` (Connected/Ready/Disabled/Restart-pending — derive from `enabled` + test result + pending), a redacted config summary (use `SECRET_SENTINEL` for secrets), and Test / Edit / Remove / enable-Toggle controls.
- `AdapterSection(props: { title: string; subtitle?: string; type: 'library'|'search'|'downloader'; instances: AdapterInstance[]; available: AvailableAdapter[]; onAdd; ...cardHandlers })` — read mockup `.sec-title`: header (title + count + "Add <type>" Button), an honest empty state, and the list of `AdapterCard`s (ordered by priority for search/downloader).
- `RestartBanner(props: { show: boolean })` — read mockup `.admin-banner`: a `warning`-toned banner "Changes saved — restart Reverb to apply." Only renders when `show`.

- [ ] **Step 1:** Read the mockup `.adapter`/`.sec-title`/`.admin-banner` and the current `Settings.tsx` adapter logic + `AdapterForm` props.
- [ ] **Step 2 (TDD each):** AdapterCard — renders name + status badge + redacted secret (never the real value), Test/Edit/Remove/Toggle fire their handlers; AdapterSection — "Add" fires `onAdd`, empty list → empty state, renders one card per instance; RestartBanner — renders only when `show`, uses the warning token. Fail → implement → pass.
- [ ] **Step 3:** `cd web && npx vitest run src/components/admin && npm run build` → PASS/clean.
- [ ] **Step 4:** Commit `feat(admin): AdapterCard, AdapterSection, RestartBanner`.

---

### Task 2: User Settings (`/settings`) — Appearance + Account

**Files:** create `web/src/components/AccentSwatches.tsx` (+ test); rebuild `web/src/routes/Settings.tsx` (+ test) to USER prefs only (remove adapter management — it moves to Admin in Task 3).

- **AccentSwatches**: a row of preset swatches — red `#F0354B` (default), indigo `#7C6AF7`, green `#1ed760`, amber `#f5a623`, cyan `#26d0ce`, pink `#ff5fa2` — plus a custom-hex `Icon plus` swatch (opens a hex input). Selecting one calls `applyAccent(hex)` immediately (live) and persists via `putSettings({accentColor: hex})`; the active swatch shows a selected ring. Read the current accent from `useSettings().data.accentColor`.
- **Settings page**: header "Settings" + tabs (Chips) — **Appearance** (AccentSwatches; a `Toggle` "Dynamic album background" wired to `putSettings({dynamicBackground})` + `useSettings`; a `Select`/static "Theme: Dark" — dark-only, honest) and **Account** (the current user + a "Log out" `Button` → `POST /auth/logout` then reload). **Do not** add a Playback tab unless there's a real engine setting to wire — omit it (note "Playback settings coming soon" at most). No fake controls.
- [ ] **Step 1:** Read `settingsApi.ts` (`applyAccent`, `putSettings`, `useSettings`) and the current `Settings.tsx` appearance code.
- [ ] **Step 2 (TDD):** AccentSwatches — selecting a preset calls `applyAccent` + `putSettings` with its hex, the active one shows selected styling; custom opens a hex field. Settings — Appearance tab shows swatches + dynamic-bg toggle (toggling calls `putSettings({dynamicBackground})`); Account tab shows Log out (calls `/auth/logout`); NO adapter UI present. Fail → implement → pass.
- [ ] **Step 3:** `cd web && npx vitest run && npm run build` → PASS/clean.
- [ ] **Step 4:** Commit `feat(routes): user Settings (Appearance + Account)`.

---

### Task 3: Admin (`/admin`) — Providers split by type + restart banner

**Files:** create `web/src/routes/Admin.tsx` (+ test); wire `<Route path="/admin" element={<Admin/>} />` in `App.tsx`; add an "Admin" entry to the `TopBar` avatar menu. Move the adapter CRUD (create/edit via `AdapterForm`, toggle/reorder/remove/test) from the old Settings into Admin.

- Header "Admin" + tabs (Chips): **Providers** (default), **Server**, **Users**.
- **Providers tab**: a `RestartBanner` (driven by `usePendingRestart()`), then three `AdapterSection`s — **Library providers** (type `library`, "Add library"), **Search providers** (type `search`, priority-ordered, "Add search"), **Downloaders** (type `downloader`, fallback chain, "Add downloader") — each wired to the real CRUD (`createAdapter`/`updateAdapter`/`deleteAdapter`/`testAdapter`, invalidate the adapters + pending-restart queries on change). Editing/adding opens the existing `AdapterForm` (in a panel/modal) with Test-connection. Loading → `Skeleton`.
- **Server tab**: honest read-only info (version via the version endpoint if a hook exists, else omit) + the restart guidance. **Users tab**: an honest "Single-admin for now — multi-user coming" `EmptyState` (no fake user table).
- [ ] **Step 1:** Read the current `Settings.tsx` CRUD handlers + `AdapterForm` usage to port them verbatim; read `App.tsx` routing + `TopBar` avatar menu.
- [ ] **Step 2 (TDD):** Admin — Providers tab renders the three sections with their headings; adding fires the create flow (mock `createAdapter`); the restart banner shows when `usePendingRestart` returns true; Users tab shows the honest empty state. Wire `/admin` route + the TopBar Admin link. Fail → implement → pass.
- [ ] **Step 3:** `cd web && npx vitest run && npm run build` → PASS/clean. Confirm the old Settings adapter tests were migrated (no orphaned/abandoned coverage).
- [ ] **Step 4:** Commit `feat(routes): Admin providers (library/search/downloaders) + restart banner`.

---

## Self-Review
**Spec coverage:** §9.6 user Settings (Appearance accent swatches incl. red default + presets + custom, dynamic-bg toggle, dark theme; Account) → T2; §9.7 Admin gated separate page, Providers split into Library/Search/Downloaders sections, restart banner, Server/Users tabs → T1 + T3; AdapterCard/Section/RestartBanner building blocks → T1. ✅ Playback intentionally omitted (no real control to wire — honesty over fake toggles). Admin gating is route-separation only for single-admin MVP (noted).
**Placeholders:** none — building-block interfaces are exact; routes name their hooks, the mockup regions, and concrete test assertions; the adapter CRUD is ported from existing code, not invented.
**Type consistency:** `AdapterCard`/`AdapterSection`/`RestartBanner` (T1) consumed by Admin (T3); `AccentSwatches` (T2) uses `applyAccent`/`putSettings`; both routes reuse the unchanged `adaptersApi`/`settingsApi`. Old `Settings.tsx` adapter logic relocated to `Admin.tsx`, not duplicated.
