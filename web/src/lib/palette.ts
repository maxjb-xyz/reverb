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
