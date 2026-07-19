import { describe, expect, it } from 'vitest'
import { activeLyricIndex } from './useActiveLyricLine'
import type { LyricLine } from './lyricsApi'

const lines: LyricLine[] = [
  { timeMs: 1000, text: 'a' },
  { timeMs: 5000, text: 'b' },
  { timeMs: 9000, text: 'c' },
]

describe('activeLyricIndex', () => {
  it('is -1 before the first line', () => {
    expect(activeLyricIndex(lines, 0)).toBe(-1)
    expect(activeLyricIndex(lines, 999)).toBe(-1)
  })
  it('activates exactly on a boundary', () => {
    expect(activeLyricIndex(lines, 1000)).toBe(0)
    expect(activeLyricIndex(lines, 5000)).toBe(1)
  })
  it('holds between lines', () => {
    expect(activeLyricIndex(lines, 4999)).toBe(0)
    expect(activeLyricIndex(lines, 8999)).toBe(1)
  })
  it('stays on the last line after the end', () => {
    expect(activeLyricIndex(lines, 100000)).toBe(2)
  })
  it('handles empty input', () => {
    expect(activeLyricIndex([], 5000)).toBe(-1)
  })
})
