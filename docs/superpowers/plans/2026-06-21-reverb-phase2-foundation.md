# Reverb Phase 2 — Design-System Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use `- [ ]`.

**Goal:** Build the design-token layer, self-hosted Figtree typography, a custom SVG icon set (zero emoji), and the reusable UI primitives that every later phase composes — the anti-slop foundation.

**Architecture:** CSS custom properties in `web/src/index.css` are the single source of truth; `tailwind.config.js` maps semantic names to them. Components live in `web/src/components/ui/`, reference only token classes (never raw hex/px), and ship with all interaction states. Accent stays the existing configurable `--color-accent` (default red `#F0354B`).

**Tech Stack:** React 19 + TypeScript, Tailwind CSS 3, Vitest + @testing-library/react, `@fontsource-variable/figtree` for self-hosted fonts.

## Global Constraints (the craft bar — every task is held to this)

- **Tokens only.** No raw hex or arbitrary px in components — use the semantic Tailwind classes (`bg-surface`, `text-text-secondary`, `text-accent`, `rounded-full`, spacing scale). The token values are defined ONCE in Task 1.
- **Zero emoji.** All glyphs are SVG via the `Icon` component (Task 3).
- **Every interactive primitive defines all states:** default, hover, `focus-visible` (visible ring), active, disabled (and selected/loading where applicable).
- **Restrained motion:** 120–180ms transitions; honor `prefers-reduced-motion` (global rule in Task 1).
- **Accent = content-state color** (buttons, selected, In-Library); **semantic status colors are fixed** (success `#1ed760`, warning `#f5c518`, error `#ff6b6b`) and never the accent.
- **Visual source of truth:** the validated mockups. Implementers MUST read the relevant mockup for exact spacing/sizing/color before building:
  - `/Users/maximusjb/Repos/reverb/.superpowers/brainstorm/87868-1782028461/content/spotify-faithful.html` (shell, icons, buttons, chips, rows, player)
  - `/Users/maximusjb/Repos/reverb/.superpowers/brainstorm/87868-1782028461/content/search-settings-v2.html` (badges, toggles, swatches, adapter cards)
- Tests verify behavior/structure (variant class applied, disabled blocks onClick, aria/role present, focus-visible class present) — not pixels. Run `cd web && npx vitest run <file>`; full build `cd web && npm run build`.
- Every commit ends with: `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
- Branch `feat/ui-overhaul-spotify`. Spec: [../specs/2026-06-21-reverb-ui-overhaul-design.md](../specs/2026-06-21-reverb-ui-overhaul-design.md) §6, §7.

## File Structure
- `web/src/index.css` — full token `:root` + reduced-motion rule (modify).
- `web/tailwind.config.js` — semantic colors, fontFamily, boxShadow (modify).
- `web/src/main.tsx` — import the Figtree fontsource CSS (modify).
- `web/package.json` — add `@fontsource-variable/figtree` (modify).
- `web/src/components/ui/Icon.tsx` (+ `Icon.test.tsx`) — SVG icon set.
- `web/src/components/ui/Button.tsx`, `IconButton.tsx`, `Chip.tsx`, `Segmented.tsx`, `Toggle.tsx`, `Select.tsx` (+ tests) — interactive primitives.
- `web/src/components/ui/Cover.tsx`, `Badge.tsx`, `Skeleton.tsx`, `EmptyState.tsx`, `ProgressRing.tsx`, `Equalizer.tsx` (+ tests) — display primitives.
- `web/src/components/ui/index.ts` — barrel export.

---

### Task 1: Design tokens + Tailwind mapping + reduced motion

**Files:** Modify `web/src/index.css`, `web/tailwind.config.js`. Test: `web/src/tokens.test.ts` (create).

- [ ] **Step 1: Write the failing test** — `web/src/tokens.test.ts`:

```ts
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const css = readFileSync(resolve(__dirname, './index.css'), 'utf8')

describe('design tokens', () => {
  it('keeps the configurable accent defaulting to red #F0354B channels', () => {
    expect(css).toMatch(/--color-accent:\s*240 53 75/)
  })
  it('defines the core surface, text and status tokens', () => {
    for (const t of ['--bg-base', '--bg-surface', '--bg-raised', '--bg-input',
      '--text-primary', '--text-secondary', '--text-muted',
      '--status-success', '--status-warning', '--status-error']) {
      expect(css, t).toContain(t)
    }
  })
  it('disables motion under prefers-reduced-motion', () => {
    expect(css).toMatch(/prefers-reduced-motion:\s*reduce/)
  })
})
```

- [ ] **Step 2: Run → fail.** `cd web && npx vitest run src/tokens.test.ts` → FAIL.

- [ ] **Step 3: Implement `web/src/index.css`:**

```css
@tailwind base;
@tailwind components;
@tailwind utilities;

:root {
  /* accent — configurable; default red #F0354B as space-separated RGB channels */
  --color-accent: 240 53 75;

  /* surfaces (fixed) */
  --bg-base: #000000;        /* gaps between panels */
  --bg-surface: #121212;     /* panels */
  --bg-raised: #181818;      /* cards */
  --bg-raised-hover: #1f1f1f;
  --bg-input: #1f1f1f;
  --border-subtle: #242424;

  /* text (fixed) */
  --text-primary: #ffffff;
  --text-secondary: #b3b3b3;
  --text-muted: #8a8a8a;

  /* semantic status (fixed — never the accent) */
  --status-success: #1ed760;
  --status-warning: #f5c518;
  --status-error: #ff6b6b;

  /* dynamic album wash — set live by the palette service; defaults to surface (invisible) */
  --album-wash: 18 18 18;
}

html, body, #root { height: 100%; }
body { @apply bg-base text-text-secondary font-sans; margin: 0; -webkit-font-smoothing: antialiased; }

@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after {
    animation-duration: .001ms !important;
    animation-iteration-count: 1 !important;
    transition-duration: .001ms !important;
    scroll-behavior: auto !important;
  }
}
```

- [ ] **Step 4: Implement `web/tailwind.config.js`:**

```js
/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        accent: 'rgb(var(--color-accent) / <alpha-value>)',
        base: 'var(--bg-base)',
        surface: 'var(--bg-surface)',
        raised: 'var(--bg-raised)',
        'raised-hover': 'var(--bg-raised-hover)',
        input: 'var(--bg-input)',
        'border-subtle': 'var(--border-subtle)',
        'text-primary': 'var(--text-primary)',
        'text-secondary': 'var(--text-secondary)',
        'text-muted': 'var(--text-muted)',
        success: 'var(--status-success)',
        warning: 'var(--status-warning)',
        error: 'var(--status-error)',
      },
      fontFamily: {
        sans: ['"Figtree Variable"', 'Figtree', 'system-ui', '-apple-system', 'sans-serif'],
      },
      boxShadow: {
        cover: '0 8px 18px -8px rgba(0,0,0,.6)',
        float: '0 8px 16px rgba(0,0,0,.35)',
        pop: '0 24px 60px rgba(0,0,0,.6)',
      },
    },
  },
  plugins: [],
}
```

- [ ] **Step 5: Run tests + build.** `cd web && npx vitest run src/tokens.test.ts && npm run build` → PASS + build clean. (Existing `settingsApi.test.ts` still passes — `--color-accent` unchanged.)

- [ ] **Step 6: Commit** `feat(web): full design-token layer + reduced-motion`.

---

### Task 2: Self-hosted Figtree typography

**Files:** Modify `web/package.json` (dep), `web/src/main.tsx` (import). Test: `web/src/font.test.ts` (create).

- [ ] **Step 1: Add the dependency.** `cd web && npm install @fontsource-variable/figtree` (self-hosts the variable font; bundled into the build, no runtime CDN fetch).

- [ ] **Step 2: Write the failing test** — `web/src/font.test.ts`:

```ts
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
const main = readFileSync(resolve(__dirname, './main.tsx'), 'utf8')
describe('typography', () => {
  it('imports the self-hosted Figtree variable font', () => {
    expect(main).toMatch(/@fontsource-variable\/figtree/)
  })
})
```

- [ ] **Step 3: Run → fail.** `cd web && npx vitest run src/font.test.ts` → FAIL.

- [ ] **Step 4: Implement** — add to the TOP of `web/src/main.tsx` (before other imports):

```ts
import '@fontsource-variable/figtree'
```

(The Tailwind `font-sans` set in Task 1 already targets `"Figtree Variable"`; `body` applies `font-sans`.)

- [ ] **Step 5: Run + build.** `cd web && npx vitest run src/font.test.ts && npm run build` → PASS + build bundles the font.

- [ ] **Step 6: Commit** `feat(web): self-host Figtree variable font`.

---

### Task 3: Custom SVG icon system (zero emoji)

**Files:** `web/src/components/ui/Icon.tsx`, `web/src/components/ui/Icon.test.tsx` (create).

**Interfaces:** Produces `type IconName` (union) and `function Icon(props: { name: IconName; className?: string; 'aria-label'?: string })` rendering an `<svg>` (stroke `currentColor`, width/height `1em` scalable via `className`).

- [ ] **Step 1: Read the mockup sprite.** Read `spotify-faithful.html` and copy the exact `<symbol>` path data for each icon. Required names (at minimum): `home, search, browse, back, fwd, dl, plus, play, pause, prev, next, shuffle, repeat, heart, queue, mic, device, vol, mini, full, sort, expand, bell, check, x, warn, retry`.

- [ ] **Step 2: Write the failing test** — `web/src/components/ui/Icon.test.tsx`:

```tsx
import { describe, it, expect } from 'vitest'
import { render } from '@testing-library/react'
import { Icon } from './Icon'

describe('Icon', () => {
  it('renders an svg using currentColor (themable) for a known name', () => {
    const { container } = render(<Icon name="play" />)
    const svg = container.querySelector('svg')
    expect(svg).toBeTruthy()
    expect(svg!.getAttribute('viewBox')).toBe('0 0 24 24')
  })
  it('is aria-hidden by default and labelled when a label is given', () => {
    const { container, rerender } = render(<Icon name="search" />)
    expect(container.querySelector('svg')!.getAttribute('aria-hidden')).toBe('true')
    rerender(<Icon name="search" aria-label="Search" />)
    const svg = container.querySelector('svg')!
    expect(svg.getAttribute('aria-hidden')).toBeNull()
    expect(svg.getAttribute('aria-label')).toBe('Search')
    expect(svg.getAttribute('role')).toBe('img')
  })
})
```

- [ ] **Step 3: Run → fail.** `cd web && npx vitest run src/components/ui/Icon.test.tsx` → FAIL.

- [ ] **Step 4: Implement `Icon.tsx`** — a `Record<IconName, ReactNode>` of inline `<path>`s (copied from the mockup), and an `Icon` component that wraps the paths in an `<svg viewBox="0 0 24 24" width="1em" height="1em" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" className={...}>`. Filled glyphs (`play`, `pause`, `heart` when filled) use `fill="currentColor" stroke="none"` — model this with a per-icon `filled?: boolean` in the path map or a separate `fill` variant. When `aria-label` is absent, set `aria-hidden="true"`; when present, set `role="img"` and the label and omit `aria-hidden`. Size via `className` (e.g. `className="w-5 h-5"`), since width/height default to `1em`.

- [ ] **Step 5: Run.** `cd web && npx vitest run src/components/ui/Icon.test.tsx` → PASS.

- [ ] **Step 6: Commit** `feat(ui): custom SVG icon set`.

---

### Task 4: Interactive primitives — Button, IconButton, Chip, Segmented, Toggle, Select

**Files:** create `Button.tsx`, `IconButton.tsx`, `Chip.tsx`, `Segmented.tsx`, `Toggle.tsx`, `Select.tsx` and matching `.test.tsx` under `web/src/components/ui/`; create `web/src/components/ui/index.ts` barrel.

**Interfaces (produce these exact signatures — later phases consume them):**
- `Button(props: { variant?: 'primary' | 'secondary' | 'ghost'; size?: 'sm' | 'md'; disabled?; onClick?; type?; children })` — primary = `bg-accent text-black` pill; secondary = bordered; ghost = text-only. Default focus-visible ring (`focus-visible:ring-2 focus-visible:ring-accent`).
- `IconButton(props: { name: IconName; label: string; active?: boolean; disabled?; onClick?; size? })` — round, `aria-label={label}`, active → `text-accent`.
- `Chip(props: { selected?: boolean; onClick?; children })` — pill; selected = `bg-text-primary text-base`, else `bg-raised text-text-primary`.
- `Segmented<T>(props: { options: {value:T;label:string}[]; value:T; onChange:(v:T)=>void })` — the My-Library/Everywhere control; selected segment = `bg-accent text-black`. `role="tablist"`, each `role="tab"` with `aria-selected`.
- `Toggle(props: { checked: boolean; onChange:(v:boolean)=>void; label: string })` — switch; on = `bg-accent`. `role="switch"`, `aria-checked`, `aria-label`.
- `Select(props: { value:string; options:{value:string;label:string}[]; onChange; label:string })` — styled native `<select>` wrapper (`bg-input`), accessible.

- [ ] **Step 1: Read the mockup** for exact chip/segment/button/toggle styling (`spotify-faithful.html` `.chip/.seg/.pl-ctrls`, `search-settings-v2.html` `.toggle/.seg`).

- [ ] **Step 2: TDD each primitive.** For each component write its `.test.tsx` first (assert: variant/selected class applied; `disabled` blocks `onClick`; aria role/attr present; `focus-visible` ring class present), run to fail, implement to token classes only, run to pass. Example for Button (`Button.test.tsx`):

```tsx
import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Button } from './Button'

describe('Button', () => {
  it('applies the primary accent style', () => {
    render(<Button variant="primary">Go</Button>)
    expect(screen.getByRole('button', { name: 'Go' }).className).toMatch(/bg-accent/)
  })
  it('does not fire onClick when disabled', async () => {
    const onClick = vi.fn()
    render(<Button disabled onClick={onClick}>Go</Button>)
    await userEvent.click(screen.getByRole('button'))
    expect(onClick).not.toHaveBeenCalled()
  })
  it('exposes a visible focus ring', () => {
    render(<Button>Go</Button>)
    expect(screen.getByRole('button').className).toMatch(/focus-visible:ring/)
  })
})
```

Write equivalent focused tests for IconButton (aria-label, active class), Chip (selected class, onClick), Segmented (aria-selected on the active tab, onChange fires), Toggle (role=switch, aria-checked flips via onChange), Select (label association, onChange). If `@testing-library/user-event` is not already a dependency, use `fireEvent` from `@testing-library/react` instead (check `package.json` first).

- [ ] **Step 3: Barrel** — `web/src/components/ui/index.ts` re-exports all primitives + `Icon`.

- [ ] **Step 4: Run all + build.** `cd web && npx vitest run src/components/ui && npm run build` → PASS + clean.

- [ ] **Step 5: Commit** `feat(ui): interactive primitives (Button, IconButton, Chip, Segmented, Toggle, Select)`.

---

### Task 5: Display primitives — Cover, Badge, Skeleton, EmptyState, ProgressRing, Equalizer

**Files:** create `Cover.tsx`, `Badge.tsx`, `Skeleton.tsx`, `EmptyState.tsx`, `ProgressRing.tsx`, `Equalizer.tsx` (+ `.test.tsx`) under `web/src/components/ui/`; extend the barrel.

**Interfaces (produce these — later phases consume them):**
- `Cover(props: { src?: string; alt: string; size?: number | 'full'; rounded?: 'md' | 'full'; className? })` — square art; `loading="lazy"`; shows a `Skeleton` until loaded; falls back to a neutral `bg-raised` placeholder with a muted music `Icon` when `src` is missing or errors.
- `Badge(props: { kind: 'in-library' | 'available' | 'downloading' | 'downloaded' | 'disabled' | 'status'; tone?: 'success' | 'warning' | 'error'; children })` — the §9.2 search badges + admin status pills. `in-library`/`downloaded` use accent/`text-accent`; `status` uses the `tone` semantic color with a dot.
- `Skeleton(props: { className?: string; rounded? })` — pulse placeholder (`bg-raised` + subtle pulse animation that respects reduced-motion).
- `EmptyState(props: { icon: IconName; title: string; hint?: string; action?: ReactNode })` — designed empty/zero-data state.
- `ProgressRing(props: { value: number /*0-100*/; size?: number })` — the download progress ring (SVG `stroke-dasharray`), stroke = accent.
- `Equalizer(props: { className? })` — the small now-playing animated bars (accent), paused under reduced-motion.

- [ ] **Step 1: Read the mockup** for the ring math (`search-settings-v2.html` `.ring` circle: r=15, dasharray≈94.2), badge styles, equalizer keyframes (`spotify-faithful.html` `.eq`).

- [ ] **Step 2: TDD each.** Focused tests: Cover renders `<img loading="lazy" alt>` when `src` given and a placeholder Icon when not; Badge applies accent class for `in-library` and the tone color for `status`; ProgressRing sets `stroke-dashoffset` proportional to `value` (e.g. value=0 → offset==circumference, value=100 → offset≈0); EmptyState renders title + action; Equalizer renders N bars. Write test → fail → implement → pass for each.

- [ ] **Step 3: Extend the barrel** to export the new primitives.

- [ ] **Step 4: Run all + build.** `cd web && npx vitest run src/components/ui && npm run build` → PASS + clean.

- [ ] **Step 5: Commit** `feat(ui): display primitives (Cover, Badge, Skeleton, EmptyState, ProgressRing, Equalizer)`.

---

## Self-Review

**Spec coverage (§6, §7):** tokens/palette/spacing/radii/shadows → Task 1; typography (Figtree) → Task 2; iconography (no emoji) → Task 3; primitives Button/IconButton/Chip/Segmented/Toggle/Select → Task 4; Cover/Badge/Skeleton/EmptyState/ProgressRing/Equalizer → Task 5. Motion + reduced-motion → Task 1. Dynamic-album-wash token declared in Task 1 (consumed by the shell in Phase 3). Composite primitives (TrackRow, MediaCard, Carousel, Popover, Toast) are intentionally deferred to the phases that first need them. ✅

**Placeholders:** none — token/font/test code is complete; component tasks give exact interfaces, the mockup as visual source, and concrete test assertions.

**Type consistency:** `IconName` (Task 3) is consumed by `IconButton`, `EmptyState`, `Cover` fallback (Tasks 4–5). All primitives exported from `web/src/components/ui/index.ts`. Badge `kind` values match the §9.2 search states used in Phase 5.
