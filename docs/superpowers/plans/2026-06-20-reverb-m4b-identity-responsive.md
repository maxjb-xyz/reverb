# Reverb M4b — Identity + Responsive Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Each task is a self-contained unit: a fresh implementer with ZERO prior context can complete it from the file paths, interfaces, and complete code given here. Tasks are ordered pure palette/contrast helpers (TDD, no worker) → the Web Worker wrapper → an injectable palette service → the `useAlbumPalette` hook (gated on `dynamic_background`) → AppShell dynamic background → PlayerBar dominant-color tint with computed-contrast text → mobile bottom tab nav → mobile mini-player + fullscreen now-playing overlay → responsive AppShell layout (desktop unchanged, imports the mobile components built in the preceding two tasks) → right panels as full-screen sheets on mobile → final smoke (full `npm run test` + `npm run build`).

**Goal:** Give Reverb its visual signature and make it mobile-ready, FRONTEND-ONLY. (1) Extract the dominant color from the currently-playing track's already-loaded cover in a **Web Worker** (CPU off the main thread), cache it per cover URL, and expose it through a `useAlbumPalette(coverUrl)` hook. (2) When the `dynamic_background` setting is on (default), the AppShell paints a **subtle ambient background gradient** derived from that color (shifting on track change), and the **PlayerBar fills with the album's dominant color as a solid fill** with **computed-contrast text** (light/dark + optional scrim) — never blur-over-art; glassmorphism only on overlay panels. When the setting is off (or nothing is playing) the app falls back to the static dark base. (3) Make the **AppShell responsive**: desktop (≥md) keeps sidebar + bottom player + slide-over right panels exactly as today; mobile (<md) renders a **bottom tab nav** (Search / Library / Settings + Downloads), a **mini player** above the tabs that **expands to a fullscreen now-playing overlay** (iOS-Music style, transport + seek + cover + dynamic bg), and the **right panels render as full-screen sheets** instead of side slide-overs. Routes are identical; only the chrome swaps via Tailwind breakpoints + one fullscreen-open boolean. Tap targets ≥44px on mobile controls.

**Architecture — testability decision: the worker is NEVER constructed in tests.** The heavy work (pixel → dominant color) lives in **pure functions** (`dominantColorFromPixels`, `contrastTextColor`, `relativeLuminance`) in `web/src/lib/palette.ts`, unit-tested in jsdom with synthetic `Uint8ClampedArray`s — no canvas, no worker. The Web Worker (`web/src/lib/paletteWorker.ts`) is a thin wrapper that imports those pure functions, decodes the image with `OffscreenCanvas`, and posts `[r,g,b]` back. A **palette service** (`web/src/lib/paletteService.ts`) owns the single `Worker` instance, the per-cover-URL `Map` cache, and a swappable `computeFn` so tests inject a synchronous fake instead of spinning a real `Worker` (jsdom has no `Worker`/`OffscreenCanvas`). The `useAlbumPalette` hook reads from that service and is gated on the `dynamic_background` setting; component tests mock the hook module entirely. No backend changes — `dynamic_background` and `accent_color` already exist via M4a's `/settings` endpoint + `useSettings()`.

**Tech Stack:** React 19, TypeScript ~6 (strict; `verbatimModuleSyntax`/`erasableSyntaxOnly` → use `import type`, NO constructor parameter-properties, NO `class implements`), Vite 8 (worker import via `new Worker(new URL('./paletteWorker.ts', import.meta.url), { type: 'module' })`), Vitest 4 (jsdom, globals, `setupFiles: ./src/setupTests.ts`), Tailwind 3.4 (existing `accent` = `rgb(var(--color-accent) / <alpha-value>)`, `base` = `#0D0D0F`; `--color-accent` lives on `<html>`), TanStack Query 5, Zustand 4. Frontend tests stub `fetch`/mock hooks; no real network, no real Worker. All work is under `web/`.

## Global Constraints

- **FRONTEND ONLY** — no Go changes. React 19, Vite 8, Vitest 4 (jsdom), Tailwind 3.4, strict TS (`verbatimModuleSyntax`/`erasableSyntaxOnly` → `import type`, NO constructor param-properties / no class-implements pitfalls). TanStack Query 5 / Zustand 4 as already used.
- **Palette:** heavy work in a Web Worker (Vite `new Worker(new URL('./paletteWorker.ts', import.meta.url), { type: 'module' })`); PURE helpers (`dominantColorFromPixels`, `contrastTextColor`, `relativeLuminance`) unit-tested in jsdom WITHOUT a real worker/canvas. Cache per cover URL (runs once per album). Gated on `dynamic_background` (default on); off → static dark background.
- **Player bar** dominant-color fill with COMPUTED-CONTRAST text (no blur-over-art); legible on light/dark/garish covers. Background ambient gradient subtle, shifts on track change.
- **Responsive:** desktop (≥md) unchanged; mobile (<md) bottom tab nav incl Search tab + fullscreen-expandable mini player + sheet panels + ≥44px tap targets; routes identical, chrome swaps via Tailwind breakpoints. Must NOT duplicate state — the same `playerStore`/`uiStore` drive both chromes; only presentation differs (Tailwind `hidden`/`md:flex` etc. + one fullscreen-open boolean).
- **Tests:** PURE palette/contrast helpers (synthetic pixel arrays → dominant color; rgb → light/dark text + luminance); component tests for responsive chrome (mobile nav renders; mini-player expand toggle; panels-as-sheets) using jsdom (STUB the worker/palette hook — never spin a real `Worker`); `dynamic_background` off → static fallback. `npm run test` + `npm run build` green.
- **Vite worker import must be** `new Worker(new URL('./paletteWorker.ts', import.meta.url), { type: 'module' })` — the only form Vite bundles. Components MUST NOT import the worker directly; they go through the palette service / `useAlbumPalette` hook, which tests stub.
- **`contrastTextColor(rgb)`** computes WCAG relative luminance (linearize sRGB channels, `L = 0.2126R + 0.7152G + 0.0722B`) → returns light (`#FFFFFF`) or dark (`#0A0A0A`) text; signals whether a scrim is needed for mid-luminance backgrounds `[0.18, 0.70]`. Boundary cases unit-tested.
- **`dominantColorFromPixels(Uint8ClampedArray, opts?)`** is deterministic + fast (coarse color-bucket histogram, skipping near-transparent and near-white/near-black edge pixels per the spec's "ambient, not garish"). Pure + unit-tested with synthetic arrays.
- TDD always: failing test → confirm RED → minimal code → confirm GREEN → conventional-commit. Run frontend tests with `cd web && npm run test` (Vitest) and typecheck/build with `cd web && npm run build`.
- The `web` working directory is fixed; all `npm` commands run from `/Users/maximusjb/Repos/reverb/web`. Use a compound command (`cd web && npm run ...`) rather than a bare `cd` so the sandbox does not prompt.

---

## File Structure

**React (frontend) — created/modified in M4b, under `web/`:**

| Path | Responsibility |
|---|---|
| `src/lib/palette.ts` | NEW: PURE helpers — `relativeLuminance`, `contrastTextColor`, `dominantColorFromPixels`, plus shared `RGB`/`Palette`/`ContrastResult` types + `rgbToCss`. No worker, no canvas. |
| `src/lib/palette.test.ts` | NEW: unit tests for all pure helpers with synthetic `Uint8ClampedArray`s + boundary luminance/contrast cases. |
| `src/lib/paletteWorker.ts` | NEW: the Web Worker — `OffscreenCanvas` decode of a cover URL → `getImageData` → `dominantColorFromPixels` → `postMessage`. Thin wrapper around `palette.ts`. NOT imported by tests. |
| `src/lib/paletteService.ts` | NEW: owns the single `Worker` + per-cover-URL cache + swappable `computeFn`; `getPalette(coverUrl)`, `__setComputeFnForTests`, `__resetForTests`. |
| `src/lib/paletteService.test.ts` | NEW: cache-hit (compute called once per URL), injected fake `computeFn`, no real Worker constructed. |
| `src/lib/useAlbumPalette.ts` | NEW: `useAlbumPalette(coverUrl)` hook — gated on `dynamic_background` via `useSettings()`; returns `{ rgb, text, scrim } | null`. |
| `src/lib/useAlbumPalette.test.tsx` | NEW: returns null when setting off; returns palette when on (service stubbed). |
| `src/components/AppShell.tsx` | MODIFY: responsive layout (desktop sidebar + bottom player + side panels; mobile bottom tab nav + mini player + sheet panels); reads the current track palette and paints the ambient background. |
| `src/components/AppShell.test.tsx` | MODIFY/EXTEND: existing tray test stays green; add desktop-vs-mobile chrome assertions + dynamic-background-off fallback. |
| `src/components/MobileTabNav.tsx` | NEW: bottom tab nav (Search / Library / Settings / Downloads), ≥44px targets, shown only `<md`. |
| `src/components/MobileTabNav.test.tsx` | NEW: renders tabs incl. Search; Downloads tab toggles the panel. |
| `src/components/MiniPlayer.tsx` | NEW: mobile mini player above the tabs; tap expands to the fullscreen now-playing overlay. |
| `src/components/MiniPlayer.test.tsx` | NEW: shows current track; expand button opens the overlay; hidden when nothing playing. |
| `src/components/NowPlayingOverlay.tsx` | NEW: fullscreen now-playing (cover + transport + seek + dynamic bg + close). |
| `src/components/NowPlayingOverlay.test.tsx` | NEW: renders when open; close button closes; transport buttons call the engine. |
| `src/components/PlayerBar.tsx` | MODIFY: apply the dominant-color solid fill + computed-contrast text + optional scrim; desktop-only chrome (`hidden md:flex`). |
| `src/components/PlayerBar.test.tsx` | MODIFY/EXTEND: existing transport/seek/keyboard tests stay green; add a tint-style assertion (palette hook mocked). |
| `src/components/PlayQueue.tsx` | MODIFY: full-screen sheet `<md`, side slide-over `≥md` (Tailwind classes only; logic unchanged). |
| `src/components/DownloadTray.tsx` | MODIFY: same responsive sheet/slide-over treatment. |
| `src/lib/uiStore.ts` | MODIFY: add `nowPlayingOpen` boolean + `openNowPlaying`/`closeNowPlaying`/`toggleNowPlaying` (drives the mobile fullscreen overlay; single source of truth). |
| `src/lib/uiStore.test.ts` | MODIFY/EXTEND: add now-playing-open toggle tests. |

**No new dependencies.** Everything uses React 19, TanStack Query 5, Zustand 4, Tailwind 3.4 already in `web/package.json`. No `node-vibrant` — palette extraction is hand-rolled and pure so it is testable without canvas.

---

## Task 1: Pure palette + contrast helpers (`web/src/lib/palette.ts`)

**Files:**
- Create: `web/src/lib/palette.ts`
- Test: `web/src/lib/palette.test.ts`

**Interfaces:**
- Consumes: nothing (pure; only standard `Uint8ClampedArray`).
- Produces (TS):
  ```ts
  export type RGB = readonly [number, number, number]
  export interface ContrastResult {
    text: string   // "#FFFFFF" (light) or "#0A0A0A" (dark)
    scrim: boolean // true when the bg is mid-luminance and needs a subtle scrim
  }
  export interface DominantOptions {
    bucketBits?: number   // bits-per-channel for the histogram (default 4 → 16 levels/channel)
    minAlpha?: number     // skip pixels with alpha below this (default 200)
    edgeSkip?: number     // skip near-white/near-black pixels at/above this distance (default 18)
    step?: number         // sample every Nth pixel for speed (default 1)
  }
  export function relativeLuminance(rgb: RGB): number          // 0..1 (WCAG)
  export function contrastTextColor(rgb: RGB): ContrastResult
  export function dominantColorFromPixels(data: Uint8ClampedArray, opts?: DominantOptions): RGB
  export function rgbToCss(rgb: RGB, alpha?: number): string   // "rgb(240 53 75)" or "rgb(240 53 75 / 0.5)"
  ```
  - `relativeLuminance`: linearize each sRGB channel (`c/255`; if ≤0.03928 → `/12.92`, else `((c+0.055)/1.055)^2.4`), then `0.2126R + 0.7152G + 0.0722B`.
  - `contrastTextColor`: `text` = dark `#0A0A0A` when luminance > 0.5, else light `#FFFFFF`. `scrim` = true when luminance is in the murky middle band `[0.18, 0.70]` (text needs help).
  - `dominantColorFromPixels`: walk RGBA quads (4 bytes each); skip `a < minAlpha`; skip near-black (`r+g+b < edgeSkip*3`) and near-white (`r+g+b > 765 - edgeSkip*3`); bucket the rest into a coarse histogram keyed by the top `bucketBits` bits of each channel; pick the most populous bucket and return the AVERAGE color of pixels in it (so the result is a real color, not the bucket center). If every pixel is skipped, return a neutral `[64, 64, 64]`.

- [ ] **Step 1: Write the failing test**

Create `web/src/lib/palette.test.ts`:
```ts
import { describe, it, expect } from 'vitest'
import {
  relativeLuminance,
  contrastTextColor,
  dominantColorFromPixels,
  rgbToCss,
} from './palette'

// rgba builds a flat Uint8ClampedArray of `count` pixels all of color [r,g,b,a].
function rgba(r: number, g: number, b: number, a: number, count: number): Uint8ClampedArray {
  const out = new Uint8ClampedArray(count * 4)
  for (let i = 0; i < count; i++) {
    out[i * 4] = r
    out[i * 4 + 1] = g
    out[i * 4 + 2] = b
    out[i * 4 + 3] = a
  }
  return out
}

// concat joins several pixel arrays into one.
function concat(...arrs: Uint8ClampedArray[]): Uint8ClampedArray {
  const total = arrs.reduce((n, a) => n + a.length, 0)
  const out = new Uint8ClampedArray(total)
  let off = 0
  for (const a of arrs) {
    out.set(a, off)
    off += a.length
  }
  return out
}

describe('relativeLuminance', () => {
  it('is ~1 for white and ~0 for black', () => {
    expect(relativeLuminance([255, 255, 255])).toBeCloseTo(1, 2)
    expect(relativeLuminance([0, 0, 0])).toBeCloseTo(0, 2)
  })
  it('green is brighter than blue', () => {
    expect(relativeLuminance([0, 255, 0])).toBeGreaterThan(relativeLuminance([0, 0, 255]))
  })
})

describe('contrastTextColor', () => {
  it('pure white background → dark text', () => {
    expect(contrastTextColor([255, 255, 255]).text).toBe('#0A0A0A')
  })
  it('pure black background → light text', () => {
    expect(contrastTextColor([0, 0, 0]).text).toBe('#FFFFFF')
  })
  it('mid-gray flags a scrim', () => {
    expect(contrastTextColor([128, 128, 128]).scrim).toBe(true)
  })
  it('clearly dark color does not need a scrim', () => {
    expect(contrastTextColor([20, 20, 30]).scrim).toBe(false)
  })
})

describe('dominantColorFromPixels', () => {
  it('returns the single solid color of a uniform image', () => {
    const [r, g, b] = dominantColorFromPixels(rgba(200, 30, 40, 255, 64))
    expect(r).toBeGreaterThan(180)
    expect(g).toBeLessThan(60)
    expect(b).toBeLessThan(70)
  })
  it('picks the majority color, ignoring a minority color', () => {
    // 90 red pixels, 10 blue pixels → red dominates.
    const data = concat(rgba(220, 20, 20, 255, 90), rgba(20, 20, 220, 255, 10))
    const [r, , b] = dominantColorFromPixels(data)
    expect(r).toBeGreaterThan(b)
  })
  it('skips near-white and near-black edge pixels', () => {
    // mostly white + black (skipped) with a small teal core that should win.
    const data = concat(
      rgba(255, 255, 255, 255, 50),
      rgba(0, 0, 0, 255, 50),
      rgba(0, 180, 170, 255, 20),
    )
    const [r, g, b] = dominantColorFromPixels(data)
    expect(g).toBeGreaterThan(120)
    expect(b).toBeGreaterThan(120)
    expect(r).toBeLessThan(80)
  })
  it('skips near-transparent pixels', () => {
    const data = concat(rgba(220, 20, 20, 10, 80), rgba(20, 180, 20, 255, 20))
    const [r, g] = dominantColorFromPixels(data)
    expect(g).toBeGreaterThan(r) // the opaque green core wins
  })
  it('returns a neutral gray when everything is skipped', () => {
    const data = rgba(0, 0, 0, 0, 32) // all transparent
    expect(dominantColorFromPixels(data)).toEqual([64, 64, 64])
  })
})

describe('rgbToCss', () => {
  it('formats without alpha', () => {
    expect(rgbToCss([240, 53, 75])).toBe('rgb(240 53 75)')
  })
  it('formats with alpha', () => {
    expect(rgbToCss([240, 53, 75], 0.5)).toBe('rgb(240 53 75 / 0.5)')
  })
})
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && npm run test -- palette`
Expected: FAIL — `Cannot find module './palette'`.

- [ ] **Step 3: Write `palette.ts`**

Create `web/src/lib/palette.ts`:
```ts
// Pure palette + contrast math. ZERO DOM/canvas/worker dependencies so it is unit
// testable in jsdom with synthetic pixel arrays. The Web Worker (paletteWorker.ts)
// imports these functions; it never reimplements them.

export type RGB = readonly [number, number, number]

export interface ContrastResult {
  text: string // "#FFFFFF" (light) or "#0A0A0A" (dark)
  scrim: boolean // true when the bg is mid-luminance and text needs a subtle scrim
}

export interface DominantOptions {
  bucketBits?: number // bits-per-channel for the histogram (default 4 → 16 levels/channel)
  minAlpha?: number // skip pixels with alpha below this (default 200)
  edgeSkip?: number // skip near-white/near-black pixels within this margin (default 18)
  step?: number // sample every Nth pixel for speed (default 1)
}

const LIGHT_TEXT = '#FFFFFF'
const DARK_TEXT = '#0A0A0A'

function linearize(c: number): number {
  const s = c / 255
  return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4)
}

// relativeLuminance returns the WCAG relative luminance (0..1) of an sRGB color.
export function relativeLuminance(rgb: RGB): number {
  return 0.2126 * linearize(rgb[0]) + 0.7152 * linearize(rgb[1]) + 0.0722 * linearize(rgb[2])
}

// contrastTextColor samples luminance and returns the legible text color plus a
// scrim flag for murky mid-luminance backgrounds that a flat text color can't fully
// fix on its own.
export function contrastTextColor(rgb: RGB): ContrastResult {
  const l = relativeLuminance(rgb)
  const text = l > 0.5 ? DARK_TEXT : LIGHT_TEXT
  const scrim = l >= 0.18 && l <= 0.7
  return { text, scrim }
}

// dominantColorFromPixels finds an ambient dominant color via a coarse color-bucket
// histogram, skipping near-transparent and near-white/near-black edge pixels (so the
// result is the album's character color, not its border). Deterministic + fast.
export function dominantColorFromPixels(data: Uint8ClampedArray, opts: DominantOptions = {}): RGB {
  const bucketBits = opts.bucketBits ?? 4
  const minAlpha = opts.minAlpha ?? 200
  const edgeSkip = opts.edgeSkip ?? 18
  const step = Math.max(1, opts.step ?? 1)
  const shift = 8 - bucketBits

  const nearBlack = edgeSkip * 3
  const nearWhite = 765 - edgeSkip * 3

  const counts = new Map<number, number>()
  const sums = new Map<number, [number, number, number]>()

  const pixelCount = Math.floor(data.length / 4)
  for (let p = 0; p < pixelCount; p += step) {
    const i = p * 4
    const r = data[i]
    const g = data[i + 1]
    const b = data[i + 2]
    const a = data[i + 3]
    if (a < minAlpha) continue
    const sum = r + g + b
    if (sum <= nearBlack || sum >= nearWhite) continue
    const key = ((r >> shift) << (bucketBits * 2)) | ((g >> shift) << bucketBits) | (b >> shift)
    counts.set(key, (counts.get(key) ?? 0) + 1)
    const acc = sums.get(key)
    if (acc) {
      acc[0] += r
      acc[1] += g
      acc[2] += b
    } else {
      sums.set(key, [r, g, b])
    }
  }

  let bestKey = -1
  let bestCount = 0
  for (const [key, count] of counts) {
    if (count > bestCount) {
      bestCount = count
      bestKey = key
    }
  }
  if (bestKey < 0) return [64, 64, 64]
  const acc = sums.get(bestKey)!
  const n = bestCount
  return [Math.round(acc[0] / n), Math.round(acc[1] / n), Math.round(acc[2] / n)]
}

// rgbToCss renders a CSS rgb() string in the modern space-separated form (matching
// the project's --color-accent channel convention).
export function rgbToCss(rgb: RGB, alpha?: number): string {
  const base = `${rgb[0]} ${rgb[1]} ${rgb[2]}`
  return alpha === undefined ? `rgb(${base})` : `rgb(${base} / ${alpha})`
}
```

- [ ] **Step 4: Run to verify it passes + typecheck**

Run: `cd web && npm run test -- palette`
Expected: PASS (all subtests).
Run: `cd web && npm run build`
Expected: clean typecheck + build.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/palette.ts web/src/lib/palette.test.ts
git commit -m "feat(web): pure palette + WCAG contrast helpers (dominant color, luminance, text)"
```

---

## Task 2: Palette Web Worker (`web/src/lib/paletteWorker.ts`)

**Files:**
- Create: `web/src/lib/paletteWorker.ts`

**Interfaces:**
- Consumes: `dominantColorFromPixels`, `RGB` from `./palette`; browser `OffscreenCanvas`, `createImageBitmap`, `fetch` (worker scope only — NOT exercised in tests).
- Produces (worker message protocol):
  ```ts
  // To the worker:   { id: string; coverUrl: string }
  // From the worker: { id: string; rgb: [number, number, number] }  (on success)
  //                  { id: string; error: string }                  (on failure)
  ```
  - The worker fetches the cover URL, decodes it to an `ImageBitmap`, draws it onto a small `OffscreenCanvas` (e.g. 32×32 — downscaling is the speed win), reads `getImageData`, runs `dominantColorFromPixels`, and posts `{ id, rgb }`. Any failure posts `{ id, error }`. There is NO test for this file — it is a thin wrapper; all logic it relies on is covered by Task 1 and the service-level fake in Task 3.

> **Why no test:** jsdom provides neither `Worker`, `OffscreenCanvas`, nor `createImageBitmap`. The worker is intentionally a 25-line shim with no branching logic worth testing in isolation; the pure math is fully covered in Task 1, and Task 3 verifies the service contract with an injected fake `computeFn`. Keeping the worker untested is the deliberate testability boundary stated in the architecture note.

- [ ] **Step 1: Write `paletteWorker.ts`**

Create `web/src/lib/paletteWorker.ts`:
```ts
// Web Worker: extracts the dominant color of a cover image OFF the main thread, so
// the player-bar / background transition never drops a frame. It is a thin wrapper
// around the pure helpers in ./palette — no extraction math lives here.
//
// NOTE: this file is never imported by tests (jsdom has no Worker/OffscreenCanvas);
// the palette SERVICE is tested with an injected fake computeFn instead.
import { dominantColorFromPixels, type RGB } from './palette'

// SAMPLE_SIZE downscales the cover before sampling: 32×32 = 1024 pixels is plenty
// for an ambient dominant color and keeps the work sub-millisecond.
const SAMPLE_SIZE = 32

interface Request {
  id: string
  coverUrl: string
}

async function extract(coverUrl: string): Promise<RGB> {
  const res = await fetch(coverUrl, { credentials: 'include' })
  const blob = await res.blob()
  const bitmap = await createImageBitmap(blob)
  const canvas = new OffscreenCanvas(SAMPLE_SIZE, SAMPLE_SIZE)
  const ctx = canvas.getContext('2d')
  if (!ctx) throw new Error('no 2d context')
  ctx.drawImage(bitmap, 0, 0, SAMPLE_SIZE, SAMPLE_SIZE)
  bitmap.close()
  const { data } = ctx.getImageData(0, 0, SAMPLE_SIZE, SAMPLE_SIZE)
  return dominantColorFromPixels(data)
}

self.onmessage = (e: MessageEvent<Request>) => {
  const { id, coverUrl } = e.data
  extract(coverUrl)
    .then((rgb) => self.postMessage({ id, rgb }))
    .catch((err: unknown) => self.postMessage({ id, error: err instanceof Error ? err.message : 'palette failed' }))
}
```

- [ ] **Step 2: Typecheck (no runtime test)**

Run: `cd web && npm run build`
Expected: clean. (If TS complains about `OffscreenCanvas`/`createImageBitmap`/`self` types, they are part of the DOM + WebWorker lib already enabled by Vite's default `tsconfig`; do NOT add `lib` overrides — the project already typechecks DOM. If a `self` typing issue appears, prefix the handler with `// @ts-expect-error worker global` ONLY on the `self.onmessage` line is NOT needed because `self` is typed in the DOM lib; leave as-is.)

- [ ] **Step 3: Commit**

```bash
git add web/src/lib/paletteWorker.ts
git commit -m "feat(web): palette Web Worker wrapping the pure dominant-color extractor"
```

---

## Task 3: Palette service — single worker, per-URL cache, injectable computeFn

**Files:**
- Create: `web/src/lib/paletteService.ts`
- Test: `web/src/lib/paletteService.test.ts`

**Interfaces:**
- Consumes: `RGB` from `./palette`; the worker via `new Worker(new URL('./paletteWorker.ts', import.meta.url), { type: 'module' })` (lazily, only when no test fake is set).
- Produces (TS):
  ```ts
  export type ComputeFn = (coverUrl: string) => Promise<RGB>
  // getPalette resolves the dominant RGB for a cover URL, computing once and caching
  // per URL. Concurrent calls for the same URL share one in-flight promise.
  export function getPalette(coverUrl: string): Promise<RGB>
  // test seams:
  export function __setComputeFnForTests(fn: ComputeFn | null): void
  export function __resetForTests(): void   // clears cache + in-flight + fake
  ```
  - When `__setComputeFnForTests(fn)` has installed a fake, `getPalette` uses it and NEVER constructs a `Worker`. In production (no fake), it lazily creates the single worker, posts `{id, coverUrl}`, and resolves on the matching reply.
  - Cache is a `Map<string, RGB>`; in-flight is a `Map<string, Promise<RGB>>` so a URL computes exactly once even under concurrency.

- [ ] **Step 1: Write the failing test**

Create `web/src/lib/paletteService.test.ts`:
```ts
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { getPalette, __setComputeFnForTests, __resetForTests } from './paletteService'
import type { RGB } from './palette'

describe('paletteService', () => {
  beforeEach(() => {
    __resetForTests()
  })

  it('computes via the injected fn and caches per URL (one compute per URL)', async () => {
    const fake = vi.fn<(u: string) => Promise<RGB>>(async () => [10, 20, 30] as RGB)
    __setComputeFnForTests(fake)

    const a1 = await getPalette('/cover/a')
    const a2 = await getPalette('/cover/a')
    const b1 = await getPalette('/cover/b')

    expect(a1).toEqual([10, 20, 30])
    expect(a2).toEqual([10, 20, 30])
    expect(b1).toEqual([10, 20, 30])
    // /cover/a computed once (cached on the 2nd call), /cover/b once → 2 total.
    expect(fake).toHaveBeenCalledTimes(2)
  })

  it('shares one in-flight promise for concurrent identical URLs', async () => {
    let resolveFn: (v: RGB) => void = () => {}
    const fake = vi.fn<(u: string) => Promise<RGB>>(
      () => new Promise<RGB>((res) => { resolveFn = res }),
    )
    __setComputeFnForTests(fake)

    const p1 = getPalette('/cover/x')
    const p2 = getPalette('/cover/x')
    resolveFn([1, 2, 3])
    const [r1, r2] = await Promise.all([p1, p2])

    expect(r1).toEqual([1, 2, 3])
    expect(r2).toEqual([1, 2, 3])
    expect(fake).toHaveBeenCalledTimes(1) // de-duped while in flight
  })

  it('does not construct a real Worker when a test fn is set', async () => {
    // jsdom has no Worker; if the service tried to build one this would throw.
    __setComputeFnForTests(async () => [5, 5, 5] as RGB)
    await expect(getPalette('/cover/safe')).resolves.toEqual([5, 5, 5])
  })
})
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && npm run test -- paletteService`
Expected: FAIL — `Cannot find module './paletteService'`.

- [ ] **Step 3: Write `paletteService.ts`**

Create `web/src/lib/paletteService.ts`:
```ts
import type { RGB } from './palette'

export type ComputeFn = (coverUrl: string) => Promise<RGB>

const cache = new Map<string, RGB>()
const inflight = new Map<string, Promise<RGB>>()

// testComputeFn, when set, replaces the worker entirely (tests + SSR safety). It is
// null in production so the real worker is used.
let testComputeFn: ComputeFn | null = null

// worker is created lazily on first real use so importing this module never spins a
// Worker (which would break jsdom and SSR).
let worker: Worker | null = null
let nextId = 0
const pending = new Map<string, { resolve: (v: RGB) => void; reject: (e: Error) => void }>()

function ensureWorker(): Worker {
  if (worker) return worker
  worker = new Worker(new URL('./paletteWorker.ts', import.meta.url), { type: 'module' })
  worker.onmessage = (e: MessageEvent<{ id: string; rgb?: RGB; error?: string }>) => {
    const { id, rgb, error } = e.data
    const p = pending.get(id)
    if (!p) return
    pending.delete(id)
    if (rgb) p.resolve(rgb)
    else p.reject(new Error(error ?? 'palette failed'))
  }
  return worker
}

function computeViaWorker(coverUrl: string): Promise<RGB> {
  const id = String(nextId++)
  const w = ensureWorker()
  return new Promise<RGB>((resolve, reject) => {
    pending.set(id, { resolve, reject })
    w.postMessage({ id, coverUrl })
  })
}

// getPalette resolves the dominant color for a cover URL, computing it exactly once
// per URL (cache + in-flight de-dup). Uses the injected test fn when present.
export function getPalette(coverUrl: string): Promise<RGB> {
  const cached = cache.get(coverUrl)
  if (cached) return Promise.resolve(cached)
  const existing = inflight.get(coverUrl)
  if (existing) return existing

  const compute = testComputeFn ?? computeViaWorker
  const promise = compute(coverUrl)
    .then((rgb) => {
      cache.set(coverUrl, rgb)
      inflight.delete(coverUrl)
      return rgb
    })
    .catch((err) => {
      inflight.delete(coverUrl)
      throw err
    })
  inflight.set(coverUrl, promise)
  return promise
}

// __setComputeFnForTests swaps the compute path (tests inject a synchronous fake so
// no real Worker/OffscreenCanvas is needed). Pass null to restore production behavior.
export function __setComputeFnForTests(fn: ComputeFn | null): void {
  testComputeFn = fn
}

// __resetForTests clears the cache, in-flight map, and any installed fake.
export function __resetForTests(): void {
  cache.clear()
  inflight.clear()
  pending.clear()
  testComputeFn = null
}
```

- [ ] **Step 4: Run to verify it passes + typecheck**

Run: `cd web && npm run test -- paletteService`
Expected: PASS (3 subtests).
Run: `cd web && npm run build`
Expected: clean. (Vite resolves the `new URL('./paletteWorker.ts', import.meta.url)` worker import at build time; the test path never reaches it.)

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/paletteService.ts web/src/lib/paletteService.test.ts
git commit -m "feat(web): palette service with single worker, per-URL cache, injectable computeFn"
```

---

## Task 4: `useAlbumPalette` hook — gated on `dynamic_background`

**Files:**
- Create: `web/src/lib/useAlbumPalette.ts`
- Test: `web/src/lib/useAlbumPalette.test.tsx`

**Interfaces:**
- Consumes: `getPalette` (paletteService), `contrastTextColor`/`RGB` (palette), `useSettings` (settingsApi — returns `{ data?: { accentColor: string; dynamicBackground: boolean } }`).
- Produces (TS):
  ```ts
  export interface AlbumPalette {
    rgb: RGB          // dominant color
    text: string      // computed-contrast text color
    scrim: boolean    // whether a scrim is recommended
  }
  // useAlbumPalette returns the palette for a cover URL, or null when:
  //  - dynamic_background is OFF (static dark fallback), OR
  //  - coverUrl is empty, OR
  //  - extraction has not resolved / failed.
  export function useAlbumPalette(coverUrl: string | undefined): AlbumPalette | null
  ```
  - When `dynamicBackground` is false (or settings still loading → treat as default ON only once `data` exists; while undefined, return null to avoid flashing), do not call `getPalette` and return null.
  - When on and `coverUrl` is non-empty: call `getPalette(coverUrl)`, and on resolve set state to `{ rgb, ...contrastTextColor(rgb) }`. Ignore stale resolutions when `coverUrl` changes (guard with the requested URL).

- [ ] **Step 1: Write the failing test**

Create `web/src/lib/useAlbumPalette.test.tsx`:
```tsx
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { useAlbumPalette } from './useAlbumPalette'
import { __setComputeFnForTests, __resetForTests } from './paletteService'
import type { RGB } from './palette'

// Mock useSettings so the gate is controllable.
vi.mock('./settingsApi', () => ({
  useSettings: vi.fn(),
}))
import { useSettings } from './settingsApi'

function setSettings(dynamicBackground: boolean | undefined) {
  vi.mocked(useSettings).mockReturnValue({
    data: dynamicBackground === undefined ? undefined : { accentColor: '#F0354B', dynamicBackground },
  } as ReturnType<typeof useSettings>)
}

describe('useAlbumPalette', () => {
  beforeEach(() => {
    __resetForTests()
    __setComputeFnForTests(async () => [200, 30, 40] as RGB)
  })

  it('returns null while settings are still loading', () => {
    setSettings(undefined)
    const { result } = renderHook(() => useAlbumPalette('/cover/a'))
    expect(result.current).toBeNull()
  })

  it('returns null when dynamic_background is off', () => {
    setSettings(false)
    const { result } = renderHook(() => useAlbumPalette('/cover/a'))
    expect(result.current).toBeNull()
  })

  it('returns null for an empty cover URL', () => {
    setSettings(true)
    const { result } = renderHook(() => useAlbumPalette(''))
    expect(result.current).toBeNull()
  })

  it('resolves the palette with contrast text when on', async () => {
    setSettings(true)
    const { result } = renderHook(() => useAlbumPalette('/cover/a'))
    await waitFor(() => expect(result.current).not.toBeNull())
    expect(result.current?.rgb).toEqual([200, 30, 40])
    // luminance of (200,30,40) < 0.5 → light text
    expect(result.current?.text).toBe('#FFFFFF')
    expect(typeof result.current?.scrim).toBe('boolean')
  })
})
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && npm run test -- useAlbumPalette`
Expected: FAIL — `Cannot find module './useAlbumPalette'`.

- [ ] **Step 3: Write `useAlbumPalette.ts`**

Create `web/src/lib/useAlbumPalette.ts`:
```ts
import { useEffect, useState } from 'react'
import { getPalette } from './paletteService'
import { contrastTextColor, type RGB } from './palette'
import { useSettings } from './settingsApi'

export interface AlbumPalette {
  rgb: RGB
  text: string
  scrim: boolean
}

// useAlbumPalette returns the dominant-color palette for a cover URL, gated on the
// dynamic_background setting. Returns null while settings load, when the setting is
// off, when there is no cover, or before extraction resolves. Stale resolutions are
// dropped when the cover URL changes mid-flight.
export function useAlbumPalette(coverUrl: string | undefined): AlbumPalette | null {
  const settings = useSettings()
  const enabled = settings.data?.dynamicBackground === true
  const [palette, setPalette] = useState<AlbumPalette | null>(null)

  useEffect(() => {
    if (!enabled || !coverUrl) {
      setPalette(null)
      return
    }
    let active = true
    getPalette(coverUrl)
      .then((rgb) => {
        if (!active) return
        setPalette({ rgb, ...contrastTextColor(rgb) })
      })
      .catch(() => {
        if (active) setPalette(null)
      })
    return () => {
      active = false
    }
  }, [enabled, coverUrl])

  return palette
}
```

- [ ] **Step 4: Run to verify it passes + typecheck**

Run: `cd web && npm run test -- useAlbumPalette`
Expected: PASS (4 subtests).
Run: `cd web && npm run build`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/useAlbumPalette.ts web/src/lib/useAlbumPalette.test.tsx
git commit -m "feat(web): useAlbumPalette hook gated on dynamic_background with contrast text"
```

---

## Task 5: uiStore — fullscreen now-playing toggle

**Files:**
- Modify: `web/src/lib/uiStore.ts`
- Test: `web/src/lib/uiStore.test.ts` (extend)

**Interfaces:**
- Produces (added to the existing `UIStore`):
  ```ts
  nowPlayingOpen: boolean
  openNowPlaying(): void
  closeNowPlaying(): void
  toggleNowPlaying(): void
  ```
  - This is the SINGLE source of truth for the mobile fullscreen now-playing overlay (no duplicate state). The right-panel slot is unchanged.

- [ ] **Step 1: Write the failing test (extend the existing describe)**

Append to `web/src/lib/uiStore.test.ts` (after the existing `describe('uiStore right-panel slot', ...)` block — keep the existing `beforeEach` resetting `rightPanel`; add a fresh reset for the new field inside the new describe):
```ts
describe('uiStore now-playing overlay', () => {
  beforeEach(() => {
    useUI.setState({ nowPlayingOpen: false })
  })

  it('starts closed', () => {
    expect(useUI.getState().nowPlayingOpen).toBe(false)
  })

  it('openNowPlaying opens it', () => {
    useUI.getState().openNowPlaying()
    expect(useUI.getState().nowPlayingOpen).toBe(true)
  })

  it('closeNowPlaying closes it', () => {
    useUI.getState().openNowPlaying()
    useUI.getState().closeNowPlaying()
    expect(useUI.getState().nowPlayingOpen).toBe(false)
  })

  it('toggleNowPlaying flips it', () => {
    useUI.getState().toggleNowPlaying()
    expect(useUI.getState().nowPlayingOpen).toBe(true)
    useUI.getState().toggleNowPlaying()
    expect(useUI.getState().nowPlayingOpen).toBe(false)
  })
})
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && npm run test -- uiStore`
Expected: FAIL — `nowPlayingOpen` / `openNowPlaying` undefined.

- [ ] **Step 3: Extend `uiStore.ts`**

Replace `web/src/lib/uiStore.ts` with:
```ts
import { create } from 'zustand'

// RightPanel models the single right-side slot. M1 ships 'queue'. M3 adds
// 'downloads' (Download Tray) into the SAME slot — opening one closes the other.
export type RightPanel = 'queue' | 'downloads' | null

interface UIStore {
  rightPanel: RightPanel
  openPanel(p: Exclude<RightPanel, null>): void
  closePanel(): void
  togglePanel(p: Exclude<RightPanel, null>): void
  // nowPlayingOpen drives the MOBILE fullscreen now-playing overlay (M4b). It is the
  // single source of truth — the desktop player bar never reads it.
  nowPlayingOpen: boolean
  openNowPlaying(): void
  closeNowPlaying(): void
  toggleNowPlaying(): void
}

export const useUI = create<UIStore>((set, get) => ({
  rightPanel: null,
  openPanel: (p) => set({ rightPanel: p }),
  closePanel: () => set({ rightPanel: null }),
  togglePanel: (p) => set({ rightPanel: get().rightPanel === p ? null : p }),
  nowPlayingOpen: false,
  openNowPlaying: () => set({ nowPlayingOpen: true }),
  closeNowPlaying: () => set({ nowPlayingOpen: false }),
  toggleNowPlaying: () => set({ nowPlayingOpen: !get().nowPlayingOpen }),
}))
```

- [ ] **Step 4: Run + typecheck**

Run: `cd web && npm run test -- uiStore`
Expected: PASS (existing right-panel tests + new now-playing tests).
Run: `cd web && npm run build`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/uiStore.ts web/src/lib/uiStore.test.ts
git commit -m "feat(web): uiStore nowPlayingOpen toggle for the mobile fullscreen player"
```

---

## Task 6: PlayerBar dynamic tint — dominant-color fill + computed-contrast text

**Files:**
- Modify: `web/src/components/PlayerBar.tsx`
- Test: `web/src/components/PlayerBar.test.tsx` (extend)

**Interfaces:**
- Consumes: `useAlbumPalette` (Task 4), `rgbToCss` (palette), `coverUrl` (libraryApi), existing `usePlayer`/`useUI`.
- Behavior:
  - Compute `const palette = useAlbumPalette(current?.coverArtId ? coverUrl(current.coverArtId, 80) : undefined)`.
  - When `palette` is non-null: set the bar's `style.backgroundColor = rgbToCss(palette.rgb)` and `style.color = palette.text`; when `palette.scrim` is true, overlay a subtle scrim (`bg-black/20` absolute layer) BEHIND the content so text stays legible. When `palette` is null: keep the existing static look (transparent over `base`, with the `border-t` divider). NO blur, NO album art behind the bar.
  - Make the outer bar `hidden md:flex` so the desktop player bar is hidden on mobile (the MiniPlayer in Task 9 replaces it `<md`). Keep all transport/seek/volume/keyboard logic EXACTLY as-is (the keyboard `useEffect` still mounts because the component still renders — `hidden` is CSS only).
  - Add `data-testid="player-bar"` to the outer element so tests can assert the inline tint style.

- [ ] **Step 1: Write the failing test (extend the existing file)**

Add to `web/src/components/PlayerBar.test.tsx`. First, at the TOP of the file (after the imports), add a mock of `useAlbumPalette` plus a controllable return. The existing tests must keep passing, so the default mock returns `null` (static look). Insert after the existing imports:
```tsx
import { useAlbumPalette } from '../lib/useAlbumPalette'
vi.mock('../lib/useAlbumPalette', () => ({ useAlbumPalette: vi.fn(() => null) }))
```
Then add a new describe block at the end of the file (inside the top-level scope, after the existing `describe('PlayerBar', ...)`):
```tsx
describe('PlayerBar dynamic tint', () => {
  beforeEach(() => {
    act(() => {
      usePlayer.getState().playTrackList([track('1')], 0)
      useUI.getState().closePanel()
    })
    vi.mocked(useAlbumPalette).mockReset()
  })

  it('applies the dominant-color fill + contrast text when a palette is present', () => {
    vi.mocked(useAlbumPalette).mockReturnValue({ rgb: [200, 30, 40], text: '#FFFFFF', scrim: false })
    render(<PlayerBar />)
    const bar = screen.getByTestId('player-bar')
    expect(bar.style.backgroundColor).toBe('rgb(200, 30, 40)')
    expect(bar.style.color).toBe('rgb(255, 255, 255)')
  })

  it('falls back to the static look when there is no palette', () => {
    vi.mocked(useAlbumPalette).mockReturnValue(null)
    render(<PlayerBar />)
    const bar = screen.getByTestId('player-bar')
    expect(bar.style.backgroundColor).toBe('')
  })
})
```
> **Note for the implementer:** `backgroundColor` set via `style={{ backgroundColor: 'rgb(200 53 75)' }}` is normalized by jsdom to `rgb(200, 30, 40)` (comma form) when read back from `el.style.backgroundColor`. The implementation uses `rgbToCss(palette.rgb)` (space form) which jsdom accepts and re-serializes with commas — the assertion above expects the comma form. `text` likewise becomes `rgb(255, 255, 255)`.

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && npm run test -- PlayerBar`
Expected: FAIL — `data-testid="player-bar"` not present / no inline tint.

- [ ] **Step 3: Modify `PlayerBar.tsx`**

In `web/src/components/PlayerBar.tsx`:

(a) Add imports near the top (after the existing imports):
```tsx
import { useAlbumPalette } from '../lib/useAlbumPalette'
import { rgbToCss } from '../lib/palette'
```

(b) Inside `PlayerBar()`, after the existing `const rightPanel = useUI((s) => s.rightPanel)` line, add:
```tsx
  const palette = useAlbumPalette(current?.coverArtId ? coverUrl(current.coverArtId, 80) : undefined)
```

(c) Replace the entire `return (...)` of `PlayerBar`. The exact before/after is:

**BEFORE** (current root element — the single `<div>` wrapping all three sections):
```tsx
  return (
    <div className="flex h-20 items-center gap-4 border-t border-neutral-800 px-4">
      {/* LEFT: cover art + track meta */}
      <div className="flex w-48 shrink-0 items-center gap-3 overflow-hidden"> ... </div>
      {/* CENTER: transport buttons + seek bar */}
      <div className="flex flex-1 flex-col items-center gap-1"> ... </div>
      {/* RIGHT: volume + panel toggles */}
      <div className="flex w-48 shrink-0 items-center justify-end gap-2"> ... </div>
    </div>
  )
```

**AFTER** (copy-paste this; replace the `{/* LEFT */}`, `{/* CENTER */}`, `{/* RIGHT */}` comment-blocks with your actual existing JSX verbatim — do NOT rewrap or reformat the inner sections):
```tsx
  return (
    <div
      data-testid="player-bar"
      className={`relative hidden h-20 px-4 md:flex ${palette ? '' : 'border-t border-neutral-800'}`}
      style={palette ? { backgroundColor: rgbToCss(palette.rgb), color: palette.text } : undefined}
    >
      {palette?.scrim && <div className="pointer-events-none absolute inset-0 bg-black/20" />}
      <div className="relative z-10 flex w-full items-center gap-4">
        {/* LEFT: cover art + track meta — paste the existing section here verbatim */}
        <div className="flex w-48 shrink-0 items-center gap-3 overflow-hidden"> ... </div>
        {/* CENTER: transport buttons + seek bar — paste the existing section here verbatim */}
        <div className="flex flex-1 flex-col items-center gap-1"> ... </div>
        {/* RIGHT: volume + panel toggles — paste the existing section here verbatim */}
        <div className="flex w-48 shrink-0 items-center justify-end gap-2"> ... </div>
      </div>
    </div>
  )
```

The `relative z-10 flex w-full items-center gap-4` wrapper sits above the optional scrim layer. Desktop layout is preserved exactly — the `gap-4` and three-section flex structure are unchanged.

> **Contrast-aware sub-elements:** the secondary text currently uses `text-neutral-400` and transport icons use `text-neutral-300`. When a palette is active these would clash with a colored fill. Keep them but rely on the inherited `style.color` for the PRIMARY title; for secondary text and the divider lines under a tint, they remain acceptable because the scrim + computed primary color carry legibility for MVP. Do NOT add per-icon recoloring logic in M4b — the computed-contrast PRIMARY text + optional scrim satisfy the spec's legibility requirement; finer per-control theming is out of scope.

- [ ] **Step 4: Run the FULL PlayerBar suite (regression + new)**

Run: `cd web && npm run test -- PlayerBar`
Expected: PASS — all existing transport/seek/keyboard/panel tests AND the two new tint tests. (The `hidden md:flex` change is CSS only; jsdom renders the element regardless of viewport, so existing `getByRole`/`getByText` queries still find the controls.)
Run: `cd web && npm run build`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/PlayerBar.tsx web/src/components/PlayerBar.test.tsx
git commit -m "feat(web): PlayerBar dominant-color fill with computed-contrast text + scrim"
```

---

## Task 7: Mobile bottom tab nav (`web/src/components/MobileTabNav.tsx`)

**Files:**
- Create: `web/src/components/MobileTabNav.tsx`
- Test: `web/src/components/MobileTabNav.test.tsx`
- Modify: `web/src/components/Sidebar.tsx` (make it `hidden md:block` — desktop only)

**Interfaces:**
- Consumes: `NavLink` (react-router-dom), `useUI` (`togglePanel`, `rightPanel`), `useDownloads` (active count badge).
- Produces:
  ```tsx
  export function MobileTabNav(): JSX.Element
  ```
  - A fixed bottom bar, `flex md:hidden`, with `data-testid="mobile-tab-nav"`. Tabs: **Search** (`/search`), **Library** (`/library`), **Settings** (`/settings`) as `NavLink`s, plus a **Downloads** button that toggles the downloads panel (sheet on mobile). Each tab is a ≥44px tap target (`min-h-[44px] min-w-[44px]`). Active link uses the accent color. Downloads shows the active-count badge.

- [ ] **Step 1: Write the failing test**

Create `web/src/components/MobileTabNav.test.tsx`:
```tsx
import { describe, it, expect, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { MobileTabNav } from './MobileTabNav'
import { useUI } from '../lib/uiStore'
import { useDownloads } from '../lib/downloadStore'

function renderNav() {
  return render(
    <MemoryRouter initialEntries={['/search']}>
      <MobileTabNav />
    </MemoryRouter>,
  )
}

describe('MobileTabNav', () => {
  beforeEach(() => {
    useUI.setState({ rightPanel: null })
    useDownloads.setState({ jobs: {} })
  })

  it('renders Search, Library, and Settings tabs', () => {
    renderNav()
    expect(screen.getByRole('link', { name: /search/i })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /library/i })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /settings/i })).toBeInTheDocument()
  })

  it('includes a dedicated Search tab pointing at /search', () => {
    renderNav()
    expect(screen.getByRole('link', { name: /search/i })).toHaveAttribute('href', '/search')
  })

  it('the Downloads tab toggles the downloads panel', () => {
    renderNav()
    fireEvent.click(screen.getByRole('button', { name: /downloads/i }))
    expect(useUI.getState().rightPanel).toBe('downloads')
  })

  it('tap targets are at least 44px', () => {
    renderNav()
    const search = screen.getByRole('link', { name: /search/i })
    expect(search.className).toMatch(/min-h-\[44px\]/)
  })
})
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && npm run test -- MobileTabNav`
Expected: FAIL — `Cannot find module './MobileTabNav'`.

- [ ] **Step 3: Write `MobileTabNav.tsx` + make Sidebar desktop-only**

Create `web/src/components/MobileTabNav.tsx`:
```tsx
import { NavLink } from 'react-router-dom'
import { useUI } from '../lib/uiStore'
import { useDownloads } from '../lib/downloadStore'

const tabs = [
  { to: '/search', label: 'Search' },
  { to: '/library', label: 'Library' },
  { to: '/settings', label: 'Settings' },
]

const tabClass = 'flex min-h-[44px] min-w-[44px] flex-1 items-center justify-center rounded px-2 py-2 text-sm'

// MobileTabNav is the bottom tab bar shown only < md. Routes are identical to
// desktop; this is purely alternate chrome. Tap targets are ≥44px.
export function MobileTabNav() {
  const togglePanel = useUI((s) => s.togglePanel)
  const rightPanel = useUI((s) => s.rightPanel)
  const activeCount = useDownloads((s) => s.active().length)

  return (
    <nav
      data-testid="mobile-tab-nav"
      className="flex shrink-0 items-stretch gap-1 border-t border-neutral-800 bg-base/95 px-1 py-1 backdrop-blur md:hidden"
    >
      {tabs.map((t) => (
        <NavLink
          key={t.to}
          to={t.to}
          className={({ isActive }) => `${tabClass} ${isActive ? 'text-accent' : 'text-neutral-300'}`}
        >
          {t.label}
        </NavLink>
      ))}
      <button
        type="button"
        onClick={() => togglePanel('downloads')}
        className={`${tabClass} relative ${rightPanel === 'downloads' ? 'text-accent' : 'text-neutral-300'}`}
      >
        Downloads
        {activeCount > 0 && (
          <span className="absolute right-1 top-0 rounded-full bg-accent px-1.5 text-xs text-white">{activeCount}</span>
        )}
      </button>
    </nav>
  )
}
```

Make the desktop `Sidebar` hidden on mobile — edit `web/src/components/Sidebar.tsx`, changing the `<nav>` className from:
```tsx
    <nav className="w-56 shrink-0 border-r border-neutral-800 p-4 space-y-1">
```
to:
```tsx
    <nav className="hidden w-56 shrink-0 border-r border-neutral-800 p-4 space-y-1 md:block">
```

- [ ] **Step 4: Run the tests + typecheck**

Run: `cd web && npm run test -- MobileTabNav`
Expected: PASS (4 subtests).
Run: `cd web && npm run build`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/MobileTabNav.tsx web/src/components/MobileTabNav.test.tsx web/src/components/Sidebar.tsx
git commit -m "feat(web): mobile bottom tab nav (Search/Library/Settings/Downloads); sidebar desktop-only"
```

---

## Task 8: Mobile mini player + fullscreen now-playing overlay

**Files:**
- Create: `web/src/components/MiniPlayer.tsx`
- Test: `web/src/components/MiniPlayer.test.tsx`
- Create: `web/src/components/NowPlayingOverlay.tsx`
- Test: `web/src/components/NowPlayingOverlay.test.tsx`

**Interfaces:**
- Consumes: `usePlayer` (current, playing, toggle, next, prev, seekMs, currentTimeMs, durationMs), `useUI` (`openNowPlaying`, `closeNowPlaying`, `nowPlayingOpen`), `coverUrl` (libraryApi), `useAlbumPalette` + `rgbToCss` (dynamic bg in the overlay), `formatDuration` (types).
- Produces:
  ```tsx
  export function MiniPlayer(): JSX.Element | null        // null when nothing playing
  export function NowPlayingOverlay(): JSX.Element | null // null when !nowPlayingOpen
  ```
  - **MiniPlayer:** a compact bar shown only `<md` (`flex md:hidden`), above the tab nav. Shows cover + title + artist + a play/pause button (≥44px). Tapping the bar (not the play button) calls `openNowPlaying()`. Returns `null` when `current` is null. `data-testid="mini-player"`.
  - **NowPlayingOverlay:** fixed full-screen overlay (`fixed inset-0 z-40`), shown only when `nowPlayingOpen`. Large cover, title/artist, transport (prev/play-pause/next, ≥44px), a seek display (`currentTime / duration`), a Close button (`aria-label="Close now playing"`) calling `closeNowPlaying()`. Uses the dynamic background gradient when a palette exists; static base otherwise. Transport buttons call the engine via `usePlayer`. `data-testid="now-playing-overlay"`.

- [ ] **Step 1: Write the failing tests**

Create `web/src/components/MiniPlayer.test.tsx`:
```tsx
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent, act } from '@testing-library/react'
import { MiniPlayer } from './MiniPlayer'
import { usePlayer, engine } from '../lib/playerStore'
import { useUI } from '../lib/uiStore'
import type { Track } from '../lib/types'

vi.mock('../lib/useAlbumPalette', () => ({ useAlbumPalette: vi.fn(() => null) }))

function track(id: string): Track {
  return {
    id, title: 'Song ' + id, albumId: 'al', album: 'Album', artistId: 'ar', artist: 'Artist',
    coverArtId: 'co', trackNumber: 1, discNumber: 1, durationMs: 200000, bitRate: 320,
    suffix: 'mp3', contentType: 'audio/mpeg',
  }
}

describe('MiniPlayer', () => {
  beforeEach(() => {
    useUI.setState({ nowPlayingOpen: false })
  })

  it('renders null when nothing is playing', () => {
    act(() => { usePlayer.getState().playTrackList([], 0) })
    const { container } = render(<MiniPlayer />)
    expect(container.firstChild).toBeNull()
  })

  it('shows the current track and expands to the fullscreen overlay on tap', () => {
    act(() => { usePlayer.getState().playTrackList([track('1')], 0) })
    render(<MiniPlayer />)
    expect(screen.getByText('Song 1')).toBeInTheDocument()
    fireEvent.click(screen.getByTestId('mini-player-expand'))
    expect(useUI.getState().nowPlayingOpen).toBe(true)
  })

  it('the play/pause button calls toggle and does NOT expand', () => {
    act(() => { usePlayer.getState().playTrackList([track('1')], 0) })
    const spy = vi.spyOn(engine, 'toggle')
    render(<MiniPlayer />)
    fireEvent.click(screen.getByRole('button', { name: /^(play|pause)$/i }))
    expect(spy).toHaveBeenCalledTimes(1)
    expect(useUI.getState().nowPlayingOpen).toBe(false)
    spy.mockRestore()
  })
})
```

Create `web/src/components/NowPlayingOverlay.test.tsx`:
```tsx
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent, act } from '@testing-library/react'
import { NowPlayingOverlay } from './NowPlayingOverlay'
import { usePlayer, engine } from '../lib/playerStore'
import { useUI } from '../lib/uiStore'
import type { Track } from '../lib/types'

vi.mock('../lib/useAlbumPalette', () => ({ useAlbumPalette: vi.fn(() => null) }))

function track(id: string): Track {
  return {
    id, title: 'Song ' + id, albumId: 'al', album: 'Album', artistId: 'ar', artist: 'Artist',
    coverArtId: 'co', trackNumber: 1, discNumber: 1, durationMs: 200000, bitRate: 320,
    suffix: 'mp3', contentType: 'audio/mpeg',
  }
}

describe('NowPlayingOverlay', () => {
  beforeEach(() => {
    act(() => { usePlayer.getState().playTrackList([track('1'), track('2')], 0) })
  })

  it('renders nothing when closed', () => {
    act(() => { useUI.getState().closeNowPlaying() })
    const { container } = render(<NowPlayingOverlay />)
    expect(container.firstChild).toBeNull()
  })

  it('renders the current track when open', () => {
    act(() => { useUI.getState().openNowPlaying() })
    render(<NowPlayingOverlay />)
    expect(screen.getByTestId('now-playing-overlay')).toBeInTheDocument()
    expect(screen.getByText('Song 1')).toBeInTheDocument()
  })

  it('the close button closes the overlay', () => {
    act(() => { useUI.getState().openNowPlaying() })
    render(<NowPlayingOverlay />)
    fireEvent.click(screen.getByRole('button', { name: /close now playing/i }))
    expect(useUI.getState().nowPlayingOpen).toBe(false)
  })

  it('transport buttons drive the engine', () => {
    act(() => { useUI.getState().openNowPlaying() })
    const nextSpy = vi.spyOn(engine, 'next')
    render(<NowPlayingOverlay />)
    fireEvent.click(screen.getByRole('button', { name: /^next$/i }))
    expect(nextSpy).toHaveBeenCalledTimes(1)
    nextSpy.mockRestore()
  })
})
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd web && npm run test -- MiniPlayer NowPlayingOverlay`
Expected: FAIL — modules not found.

- [ ] **Step 3: Write `MiniPlayer.tsx`**

Create `web/src/components/MiniPlayer.tsx`:
```tsx
import { usePlayer } from '../lib/playerStore'
import { useUI } from '../lib/uiStore'
import { coverUrl } from '../lib/libraryApi'
import { useAlbumPalette } from '../lib/useAlbumPalette'
import { rgbToCss } from '../lib/palette'

// MiniPlayer is the mobile-only compact player (shown < md), sitting above the tab
// nav. Tapping the bar expands to the fullscreen now-playing overlay; the play/pause
// button is a separate ≥44px target that does NOT expand.
export function MiniPlayer() {
  const current = usePlayer((s) => s.current)
  const playing = usePlayer((s) => s.playing)
  const toggle = usePlayer((s) => s.toggle)
  const openNowPlaying = useUI((s) => s.openNowPlaying)
  const palette = useAlbumPalette(current?.coverArtId ? coverUrl(current.coverArtId, 80) : undefined)

  if (!current) return null

  return (
    <div
      data-testid="mini-player"
      className="flex items-center gap-3 border-t border-neutral-800 px-3 py-2 md:hidden"
      style={palette ? { backgroundColor: rgbToCss(palette.rgb), color: palette.text } : undefined}
    >
      <button
        type="button"
        data-testid="mini-player-expand"
        aria-label="Expand player"
        onClick={openNowPlaying}
        className="flex min-w-0 flex-1 items-center gap-3 text-left"
      >
        {current.coverArtId ? (
          <img src={coverUrl(current.coverArtId, 80)} alt="" className="h-10 w-10 rounded object-cover" />
        ) : (
          <div className="h-10 w-10 rounded bg-neutral-800" />
        )}
        <div className="min-w-0">
          <div className="truncate text-sm font-medium">{current.title}</div>
          <div className="truncate text-xs opacity-80">{current.artist}</div>
        </div>
      </button>
      <button
        type="button"
        aria-label={playing ? 'Pause' : 'Play'}
        onClick={toggle}
        className="flex h-11 w-11 items-center justify-center rounded-full bg-white text-black"
      >
        {playing ? '⏸' : '▶'}
      </button>
    </div>
  )
}
```

- [ ] **Step 4: Write `NowPlayingOverlay.tsx`**

Create `web/src/components/NowPlayingOverlay.tsx`:
```tsx
import { usePlayer } from '../lib/playerStore'
import { useUI } from '../lib/uiStore'
import { coverUrl } from '../lib/libraryApi'
import { formatDuration } from '../lib/types'
import { useAlbumPalette } from '../lib/useAlbumPalette'
import { rgbToCss } from '../lib/palette'

// NowPlayingOverlay is the mobile fullscreen now-playing view (iOS-Music style),
// toggled from the mini player. It reuses the SAME playerStore — no duplicate state.
export function NowPlayingOverlay() {
  const open = useUI((s) => s.nowPlayingOpen)
  const close = useUI((s) => s.closeNowPlaying)
  const current = usePlayer((s) => s.current)
  const playing = usePlayer((s) => s.playing)
  const toggle = usePlayer((s) => s.toggle)
  const next = usePlayer((s) => s.next)
  const prev = usePlayer((s) => s.prev)
  const currentTimeMs = usePlayer((s) => s.currentTimeMs)
  const durationMs = usePlayer((s) => s.durationMs)
  const palette = useAlbumPalette(current?.coverArtId ? coverUrl(current.coverArtId, 80) : undefined)

  if (!open) return null

  const ambient = palette
    ? { background: `linear-gradient(180deg, ${rgbToCss(palette.rgb, 0.45)} 0%, rgb(13 13 15) 70%)`, color: palette.text }
    : undefined

  return (
    <div
      data-testid="now-playing-overlay"
      className="fixed inset-0 z-40 flex flex-col bg-base p-6 md:hidden"
      style={ambient}
    >
      <div className="flex items-center justify-between">
        <button
          type="button"
          aria-label="Close now playing"
          onClick={close}
          className="flex h-11 w-11 items-center justify-center rounded-full text-2xl"
        >
          ⌄
        </button>
        <div className="text-xs uppercase tracking-wide opacity-70">Now Playing</div>
        <div className="h-11 w-11" />
      </div>

      <div className="flex flex-1 flex-col items-center justify-center gap-6">
        {current?.coverArtId ? (
          <img src={coverUrl(current.coverArtId, 600)} alt="" className="aspect-square w-full max-w-xs rounded-lg object-cover shadow-2xl" />
        ) : (
          <div className="aspect-square w-full max-w-xs rounded-lg bg-neutral-800" />
        )}
        <div className="w-full max-w-xs text-center">
          <div className="truncate text-xl font-bold">{current ? current.title : 'Nothing playing'}</div>
          <div className="truncate text-sm opacity-80">{current?.artist ?? ''}</div>
        </div>
      </div>

      <div className="mb-2 flex items-center justify-between text-xs tabular-nums opacity-80">
        <span>{formatDuration(currentTimeMs)}</span>
        <span>{formatDuration(durationMs)}</span>
      </div>

      <div className="mb-6 flex items-center justify-center gap-8">
        <button type="button" aria-label="Previous" onClick={prev} className="flex h-11 w-11 items-center justify-center text-2xl">
          ⏮
        </button>
        <button
          type="button"
          aria-label={playing ? 'Pause' : 'Play'}
          onClick={toggle}
          className="flex h-16 w-16 items-center justify-center rounded-full bg-white text-2xl text-black"
        >
          {playing ? '⏸' : '▶'}
        </button>
        <button type="button" aria-label="Next" onClick={next} className="flex h-11 w-11 items-center justify-center text-2xl">
          ⏭
        </button>
      </div>
    </div>
  )
}
```

> **Test naming note:** the "Next" transport test uses `name: /^next$/i`. The button's `aria-label="Next"` satisfies it; `/^next$/i` anchors so it doesn't also match the (absent) "Up Next". The play/pause aria-label toggles between "Play"/"Pause".

- [ ] **Step 5: Run the tests + typecheck**

Run: `cd web && npm run test -- MiniPlayer NowPlayingOverlay`
Expected: PASS (3 + 4 subtests).
Run: `cd web && npm run build`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/MiniPlayer.tsx web/src/components/MiniPlayer.test.tsx web/src/components/NowPlayingOverlay.tsx web/src/components/NowPlayingOverlay.test.tsx
git commit -m "feat(web): mobile mini player + fullscreen now-playing overlay (shared playerStore)"
```

---

## Task 9: Responsive AppShell — desktop unchanged + ambient dynamic background

**Files:**
- Modify: `web/src/components/AppShell.tsx`
- Test: `web/src/components/AppShell.test.tsx` (extend; keep the existing tray test green)

**Interfaces:**
- Consumes: `useAlbumPalette` (Task 4), `rgbToCss` (palette), `usePlayer` (for the current track's cover), existing `Sidebar`/`PlayerBar`/`PlayQueue`/`DownloadTray`/`useRealtime`. NEW children (Tasks 7–8): `MobileTabNav`, `MiniPlayer`, `NowPlayingOverlay`.
- Behavior:
  - Desktop (≥md) layout is IDENTICAL to today: `Sidebar` (hidden `<md`), main `Outlet`, side `PlayQueue`/`DownloadTray` slide-overs, bottom `PlayerBar` (now `hidden md:flex` from Task 6). No visual change on desktop.
  - The ROOT element gets an ambient background: when a palette is present, set `style.background` to a subtle radial/linear gradient from the dominant color into `base`; when null, leave the default `base` (from `body`). Add `data-testid="app-shell-root"`.
  - Mobile chrome (`MobileTabNav` + `MiniPlayer` + `NowPlayingOverlay`) was built in Tasks 7–8 and is imported here. This task imports `MobileTabNav`/`MiniPlayer`/`NowPlayingOverlay`, which already exist (Tasks 7 and 8 run before this task).

- [ ] **Step 1: Write the failing/extended test**

Replace `web/src/components/AppShell.test.tsx` with (keeps the existing tray assertion, adds desktop-structure + background assertions; mocks the palette hook and the not-yet-relevant child internals are real):
```tsx
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AppShell } from './AppShell'
import { useUI } from '../lib/uiStore'
import { useDownloads } from '../lib/downloadStore'
import { usePlayer } from '../lib/playerStore'
import type { Track } from '../lib/types'

vi.mock('../lib/realtimeWiring', () => ({ useRealtime: () => {} }))
import { useAlbumPalette } from '../lib/useAlbumPalette'
vi.mock('../lib/useAlbumPalette', () => ({ useAlbumPalette: vi.fn(() => null) }))

function track(id: string): Track {
  return {
    id, title: 'Song ' + id, albumId: 'al', album: 'Album', artistId: 'ar', artist: 'Artist',
    coverArtId: 'co', trackNumber: 1, discNumber: 1, durationMs: 200000, bitRate: 320,
    suffix: 'mp3', contentType: 'audio/mpeg',
  }
}

function renderShell() {
  const qc = new QueryClient()
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <AppShell />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('AppShell', () => {
  beforeEach(() => {
    useDownloads.setState({ jobs: {} })
    useUI.setState({ rightPanel: 'downloads', nowPlayingOpen: false })
    vi.mocked(useAlbumPalette).mockReset()
    vi.mocked(useAlbumPalette).mockReturnValue(null)
  })

  it('mounts the Download Tray when the right panel is downloads', () => {
    renderShell()
    expect(screen.getByText('Download Tray')).toBeInTheDocument()
  })

  it('renders the desktop sidebar and the mobile tab nav (chrome swaps via CSS)', () => {
    renderShell()
    // Both chromes are in the DOM; Tailwind hidden/md: classes decide visibility.
    expect(screen.getByTestId('app-shell-root')).toBeInTheDocument()
    expect(screen.getByTestId('mobile-tab-nav')).toBeInTheDocument()
  })

  it('paints an ambient background when a palette is present', () => {
    vi.mocked(useAlbumPalette).mockReturnValue({ rgb: [200, 30, 40], text: '#FFFFFF', scrim: false })
    useUI.setState({ rightPanel: null })
    act(() => { usePlayer.getState().playTrackList([track('1')], 0) })
    renderShell()
    const root = screen.getByTestId('app-shell-root')
    expect(root.style.background).not.toBe('')
  })

  it('uses the static background when dynamic_background is off (no palette)', () => {
    vi.mocked(useAlbumPalette).mockReturnValue(null)
    renderShell()
    const root = screen.getByTestId('app-shell-root')
    expect(root.style.background).toBe('')
  })
})
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && npm run test -- AppShell`
Expected: FAIL — `app-shell-root` / `mobile-tab-nav` not present.

- [ ] **Step 3: Rewrite `AppShell.tsx`** (Tasks 7 and 8 must already be complete before this step)

Replace `web/src/components/AppShell.tsx` with:
```tsx
import { Outlet } from 'react-router-dom'
import { Sidebar } from './Sidebar'
import { PlayerBar } from './PlayerBar'
import { PlayQueue } from './PlayQueue'
import { DownloadTray } from './DownloadTray'
import { MobileTabNav } from './MobileTabNav'
import { MiniPlayer } from './MiniPlayer'
import { NowPlayingOverlay } from './NowPlayingOverlay'
import { useRealtime } from '../lib/realtimeWiring'
import { usePlayer } from '../lib/playerStore'
import { useAlbumPalette } from '../lib/useAlbumPalette'
import { coverUrl } from '../lib/libraryApi'
import { rgbToCss } from '../lib/palette'

export function AppShell() {
  // One app-wide realtime WS (distinct from the SSE search stream): drives the
  // download store, TanStack invalidation, and play-when-ready auto-play.
  useRealtime()

  const current = usePlayer((s) => s.current)
  const palette = useAlbumPalette(current?.coverArtId ? coverUrl(current.coverArtId, 80) : undefined)

  // Ambient dynamic background: a subtle gradient from the dominant color into the
  // static base. When no palette (setting off / nothing playing / not yet resolved),
  // leave the body's static dark base. NOT blur-over-art.
  const ambient = palette
    ? {
        background: `radial-gradient(120% 120% at 50% 0%, ${rgbToCss(palette.rgb, 0.22)} 0%, rgb(13 13 15) 60%)`,
      }
    : undefined

  return (
    <div data-testid="app-shell-root" className="flex h-full flex-col" style={ambient}>
      <div className="relative flex min-h-0 flex-1">
        {/* Desktop sidebar — hidden on mobile (the bottom tab nav replaces it). */}
        <Sidebar />
        <main className="flex-1 overflow-auto p-6 pb-24 md:pb-6">
          <Outlet />
        </main>
        {/* Single right-panel slot: side slide-over on desktop, full-screen sheet
            on mobile (the components apply the responsive classes themselves). */}
        <PlayQueue />
        <DownloadTray />
      </div>

      {/* Desktop bottom player bar (hidden < md from PlayerBar's own classes). */}
      <PlayerBar />

      {/* Mobile chrome: mini player + bottom tab nav, both hidden ≥ md. The
          fullscreen now-playing overlay is portal-free and self-gates on open. */}
      <MiniPlayer />
      <MobileTabNav />
      <NowPlayingOverlay />
    </div>
  )
}
```

> The `Sidebar` is made desktop-only in Task 7 Step 3 by adding `hidden md:block` to its root `<nav>`. Do that there; here we just render it.

- [ ] **Step 4: Run the tests + typecheck**

Run: `cd web && npm run test -- AppShell`
Expected: PASS (tray + chrome + background-on + background-off).
Run: `cd web && npm run build`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/AppShell.tsx web/src/components/AppShell.test.tsx
git commit -m "feat(web): responsive AppShell with ambient dynamic background; desktop unchanged"
```

---

## Task 10: Right panels as full-screen sheets on mobile

**Files:**
- Modify: `web/src/components/PlayQueue.tsx`
- Modify: `web/src/components/DownloadTray.tsx`
- Test: `web/src/components/PlayQueue.test.tsx` (extend with a responsive-class assertion), `web/src/components/DownloadTray.test.tsx` (extend)

**Interfaces:**
- No prop/logic changes. Only the `<aside>` className changes: side slide-over `≥md`, full-screen sheet `<md`. The same `useUI.rightPanel` gating drives both. Glassmorphism (`backdrop-blur`) stays — these are overlay panels over content (allowed by the spec), never over album art.

Current desktop class on BOTH `<aside>`s:
```tsx
className="absolute right-0 top-0 z-20 flex h-full w-80 flex-col border-l border-neutral-800 bg-neutral-950/95 backdrop-blur"
```
New responsive class (apply to BOTH `PlayQueue` and `DownloadTray`):
```tsx
className="absolute inset-0 z-30 flex h-full w-full flex-col border-neutral-800 bg-neutral-950/95 backdrop-blur md:inset-y-0 md:left-auto md:right-0 md:z-20 md:w-80 md:border-l"
```
- `<md`: `inset-0 w-full` → full-screen sheet, `z-30` so it covers the mini player/tab nav.
- `≥md`: `md:left-auto md:right-0 md:w-80 md:border-l md:z-20` → restores the exact desktop slide-over.

- [ ] **Step 1: Write the failing assertions (extend both test files)**

Add to `web/src/components/PlayQueue.test.tsx` (inside the existing `describe('PlayQueue', ...)`):
```tsx
  it('is a full-screen sheet on mobile and a side panel on desktop (responsive classes)', () => {
    render(<PlayQueue />)
    const aside = screen.getByRole('complementary')
    expect(aside.className).toMatch(/inset-0/)   // mobile full-screen
    expect(aside.className).toMatch(/md:w-80/)   // desktop side panel
  })
```
> `getByRole('complementary')` selects the `<aside>`. If the panel is closed in a given test it returns null; this test runs with the panel open (the existing `beforeEach` opens `'queue'`).

Add to `web/src/components/DownloadTray.test.tsx`. First check the existing file's setup; it likely opens the downloads panel via `useUI.setState({ rightPanel: 'downloads' })` in a `beforeEach`. Add inside its top describe:
```tsx
  it('is a full-screen sheet on mobile and a side panel on desktop (responsive classes)', () => {
    render(<DownloadTray />)
    const aside = screen.getByRole('complementary')
    expect(aside.className).toMatch(/inset-0/)
    expect(aside.className).toMatch(/md:w-80/)
  })
```
> If `DownloadTray.test.tsx` does not already open the panel in a `beforeEach`, set `useUI.setState({ rightPanel: 'downloads' })` at the start of this test (import `useUI`).

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && npm run test -- PlayQueue DownloadTray`
Expected: FAIL — classes don't yet include `inset-0`/`md:w-80`.

- [ ] **Step 3: Apply the responsive classes**

In `web/src/components/PlayQueue.tsx`, replace the `<aside>` className with the new responsive class above.
In `web/src/components/DownloadTray.tsx`, replace the `<aside>` className with the same new responsive class.

- [ ] **Step 4: Run the tests + typecheck**

Run: `cd web && npm run test -- PlayQueue DownloadTray`
Expected: PASS (existing behavior tests + the new responsive-class tests).
Run: `cd web && npm run build`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/PlayQueue.tsx web/src/components/PlayQueue.test.tsx web/src/components/DownloadTray.tsx web/src/components/DownloadTray.test.tsx
git commit -m "feat(web): right panels render as full-screen sheets on mobile, side panels on desktop"
```

---

## Task 11: Full-suite smoke + final verification

**Files:** none new — run the entire frontend suite and build to confirm M4b is green end-to-end and desktop behavior is intact.

- [ ] **Step 0: Stub `useAlbumPalette` in `App.test.tsx`**

In `web/src/App.test.tsx`, add the following mock alongside the existing `vi.mock('./lib/session')` (or whatever session mock is already there), so that AppShell's render does not fire an un-stubbed `/api/v1/settings` fetch through the real `useAlbumPalette` → `useSettings` path:
```ts
vi.mock('./lib/useAlbumPalette', () => ({ useAlbumPalette: () => null }))
```
(If the hook file lives at a different path — e.g. `'../lib/useAlbumPalette'` — use the path relative to `App.test.tsx`. Check the actual import in `AppShell.tsx` to confirm the module path.)

- [ ] **Step 1: Run the WHOLE frontend test suite**

Run: `cd web && npm run test`
Expected: ALL test files PASS — the new M4b files (palette, paletteService, useAlbumPalette, uiStore, PlayerBar, AppShell, MobileTabNav, MiniPlayer, NowPlayingOverlay, PlayQueue, DownloadTray) AND every pre-existing M0–M4a test (App, Search, Settings, Setup, AdapterForm, adaptersApi, settingsApi, audioEngine, downloadStore, everywhereStore, libraryApi, playerStore, realtime, realtimeWiring, searchStream, ExternalRow, TrackRow, etc.).

> If any PRE-EXISTING test fails, it is because the responsive refactor changed the DOM (e.g. the desktop `PlayerBar` is now `hidden md:flex`, or `Sidebar` is `hidden md:block`). jsdom ignores CSS visibility, so `getByText('Reverb')` / `getByRole` queries still resolve — these should NOT break. If one does break, FIX THE TEST to remain meaningful (assert the element exists in the DOM, not that it is visually shown), per the milestone constraint "update any that assert desktop-only structure if the responsive refactor changes the DOM — keep them meaningful." Do NOT delete coverage.

- [ ] **Step 2: Typecheck + production build**

Run: `cd web && npm run build`
Expected: `tsc -b` passes (strict; `import type` everywhere needed) AND `vite build` succeeds, including bundling the `paletteWorker.ts` worker chunk (Vite emits a separate worker asset). No errors, no warnings about the worker import.

- [ ] **Step 3: Lint (optional but recommended)**

Run: `cd web && npm run lint`
Expected: clean (or pre-existing warnings only — do not introduce new lint errors).

- [ ] **Step 4: Commit (if any test-fix touch-ups were needed)**

```bash
git add -A web/src
git commit -m "test(web): keep M0–M4a suites meaningful after the responsive refactor"
```
(Skip this commit if Step 1 was already fully green with no edits.)

---

## Definition of Done (M4b)

- **Palette extraction is real and off-main-thread:** `paletteWorker.ts` runs `dominantColorFromPixels` on an `OffscreenCanvas`-decoded cover and posts `[r,g,b]`; the `paletteService` caches per cover URL (computed once per album) and de-dups concurrent requests; the worker is constructed via the Vite form `new Worker(new URL('./paletteWorker.ts', import.meta.url), { type: 'module' })`.
- **Pure + testable:** `dominantColorFromPixels`, `contrastTextColor`, `relativeLuminance`, `rgbToCss` are pure and unit-tested with synthetic `Uint8ClampedArray`s and boundary luminance cases — NO real worker/canvas in any test. The service is tested with an injected `computeFn`; the hook + components mock the palette layer.
- **Dynamic background + player tint, gated:** when `dynamic_background` is ON (default), the AppShell paints a subtle ambient gradient from the current track's dominant color (shifting on track change) and the desktop PlayerBar fills with that color using computed-contrast text (+ scrim when mid-luminance). When OFF (or nothing playing / not yet resolved), the static dark base is used. No blur-over-art; glassmorphism only on the overlay panels.
- **Responsive shell:** desktop (≥md) is visually unchanged (sidebar + bottom player + side slide-over panels). Mobile (<md) shows a bottom tab nav INCLUDING a Search tab (→ `/search`), a mini player above the tabs that expands to a fullscreen now-playing overlay (cover + transport + seek + dynamic bg + close), and the Play Queue / Download Tray render as full-screen sheets. Tap targets ≥44px. Routes are identical; only chrome swaps via Tailwind `md:` classes + the single `nowPlayingOpen` boolean.
- **No duplicated state:** the same `playerStore`/`uiStore` drive both chromes; the only new state is `uiStore.nowPlayingOpen`.
- **Green:** `cd web && npm run test` passes (all new + all pre-existing, kept meaningful) and `cd web && npm run build` typechecks + builds (worker chunk emitted). Strict TS respected (`import type`, no class-implements/param-property pitfalls). No backend/Go changes.

## Self-Review

- **Coverage vs scope:** (1) palette in a Web Worker + pure helpers + per-URL cache + `useAlbumPalette` gated on `dynamic_background` → Tasks 1–4. (2) dynamic background + player tint with computed-contrast text → Tasks 6 + 9. (3) responsive shell (bottom tab nav incl Search, expandable mini player, sheet panels, ≥44px) → Tasks 5, 7, 8, 9, 10. (4) desktop intact + existing tests pass → Tasks 6–11 (CSS-only desktop changes; Task 11 smoke). All four scope items covered.
- **No placeholders:** every task has complete runnable code (full files for new modules; exact className/style edits for modifications), exact commands, and expected output. The only intentional "stub-free" deviation is the Task 7/8/9 ordering note (build 8 → 9 → 7) — called out explicitly so a linear executor doesn't import a not-yet-created component.
- **Type consistency:** `RGB = readonly [number, number, number]` used uniformly across `palette.ts`, `paletteWorker.ts`, `paletteService.ts`, `useAlbumPalette.ts`. `useSettings()` shape (`{ data?: { accentColor; dynamicBackground } }`) matches the as-built M4a `settingsApi.ts`. `usePlayer` fields (`current.coverArtId/title/artist`, `currentTimeMs`, `durationMs`, `toggle/next/prev/seekMs`) match `audioEngine.ts`/`playerStore.ts`. `coverUrl(id, size)` and `formatDuration(ms)` signatures match `libraryApi.ts`/`types.ts`. `import type` used for all type-only imports (strict `verbatimModuleSyntax`). No classes added → no class-implements / param-property pitfalls.
- **Worker testability (the key risk) is concrete:** the worker is never imported by tests; `paletteService.__setComputeFnForTests` swaps the compute path so `getPalette` resolves synchronously via a fake — verified by a test asserting "does not construct a real Worker"; the hook + every component test mocks `../lib/useAlbumPalette` directly. jsdom's lack of `Worker`/`OffscreenCanvas`/`createImageBitmap` therefore never bites. `vi.mock('../lib/useAlbumPalette', () => ({ useAlbumPalette: vi.fn(() => null) }))` is the default-null stub reused in PlayerBar/AppShell/MiniPlayer/NowPlayingOverlay tests.
- **Contrast decision is concrete:** WCAG luminance with explicit linearization; light/dark threshold at 0.5; scrim band `[0.18, 0.70]`; boundary tests for pure white (→ dark), pure black (→ light), mid-gray (→ scrim). Returns `#FFFFFF` / `#0A0A0A`.
- **Responsive decision is concrete:** Tailwind `hidden md:flex` / `flex md:hidden` / `md:` overrides on the same DOM; no separate route tree; `nowPlayingOpen` is the only added boolean; jsdom renders both chromes so existing `getByRole`/`getByText` queries keep working (the milestone's "keep tests meaningful" clause covers the rare case a desktop-structure assertion must be softened to "exists in DOM"). Tap targets enforced via `min-h-[44px] min-w-[44px]` / `h-11 w-11` (44px) and asserted in the MobileTabNav test.
- **Backend untouched:** all paths under `web/`; `dynamic_background`/`accent_color` consumed via the existing `/settings` endpoint + `useSettings()`. Verified against the as-built `settingsApi.ts`.
- **Possible nit fixed inline:** the PlayerBar tint test asserts the jsdom-normalized comma form `rgb(200, 30, 40)`; the note in Task 6 Step 1 explains why the space-form `rgbToCss` input reads back comma-form — preventing a false RED.
