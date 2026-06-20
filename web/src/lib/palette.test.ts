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
