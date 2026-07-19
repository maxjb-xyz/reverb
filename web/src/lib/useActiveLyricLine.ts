import type { LyricLine } from './lyricsApi'
import { usePlayer } from './playerStore'

// Index of the last line whose timeMs <= timeMs; -1 before the first line.
export function activeLyricIndex(lines: LyricLine[], timeMs: number): number {
  let lo = 0
  let hi = lines.length - 1
  let ans = -1
  while (lo <= hi) {
    const mid = (lo + hi) >> 1
    if (lines[mid].timeMs <= timeMs) {
      ans = mid
      lo = mid + 1
    } else {
      hi = mid - 1
    }
  }
  return ans
}

export function useActiveLyricLine(lines: LyricLine[] | undefined): number {
  const currentTimeMs = usePlayer((s) => s.currentTimeMs)
  if (!lines || lines.length === 0) return -1
  return activeLyricIndex(lines, currentTimeMs)
}
