import { describe, it, expect, vi, beforeEach, type Mock } from 'vitest'
import { startNowPlaying } from './nowPlaying'
import type { PlayerState } from './audioEngine'
import type { Track } from './types'

// ---------------------------------------------------------------------------
// Helpers (mirror playTracker.test.ts harness)
// ---------------------------------------------------------------------------

function mkTrack(id: string, durationMs = 60_000): Track {
  return {
    id,
    title: `Title-${id}`,
    artist: `Artist-${id}`,
    album: `Album-${id}`,
    albumId: 'al1',
    artistId: 'ar1',
    coverArtId: 'co1',
    trackNumber: 1,
    discNumber: 1,
    durationMs,
    bitRate: 320,
    suffix: 'mp3',
    contentType: 'audio/mpeg',
  }
}

function baseState(): PlayerState {
  return {
    queue: [],
    index: 0,
    current: null,
    playing: false,
    currentTimeMs: 0,
    durationMs: 0,
    bufferedMs: 0,
    volume: 1,
    shuffle: false,
    repeat: 'off',
  }
}

function fakeEngine() {
  let subscriber: ((s: PlayerState) => void) | null = null
  return {
    subscribe(cb: (s: PlayerState) => void) {
      subscriber = cb
      cb(baseState())
      return () => {
        subscriber = null
      }
    },
    emit(s: PlayerState) {
      subscriber?.(s)
    },
  }
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('startNowPlaying', () => {
  let nowPlayingFn: Mock<(t: { title: string; artist: string; album: string; durationMs: number }) => Promise<void>>

  beforeEach(() => {
    nowPlayingFn = vi
      .fn<(t: { title: string; artist: string; album: string; durationMs: number }) => Promise<void>>()
      .mockResolvedValue(undefined)
  })

  // ── Test 1: fires once on track change ──────────────────────────────────────
  it('calls nowPlayingFn once with the new track when the track id changes', () => {
    const eng = fakeEngine()
    const stop = startNowPlaying(eng as any, nowPlayingFn)

    const t = mkTrack('t1', 60_000)
    eng.emit({ ...baseState(), current: t, playing: true, currentTimeMs: 0 })

    expect(nowPlayingFn).toHaveBeenCalledTimes(1)
    expect(nowPlayingFn).toHaveBeenCalledWith({
      title: t.title,
      artist: t.artist,
      album: t.album,
      durationMs: 60_000,
    })

    stop()
  })

  // ── Test 2: NOT called again on pause/seek/progress for same track id ───────
  it('does NOT call nowPlayingFn again on subsequent pause/seek/progress events for the same track id', () => {
    const eng = fakeEngine()
    const stop = startNowPlaying(eng as any, nowPlayingFn)

    const t = mkTrack('t2', 60_000)

    // Initial track load → fires
    eng.emit({ ...baseState(), current: t, playing: true, currentTimeMs: 0 })
    expect(nowPlayingFn).toHaveBeenCalledTimes(1)

    // Progress (same id, different time)
    eng.emit({ ...baseState(), current: t, playing: true, currentTimeMs: 5_000 })
    eng.emit({ ...baseState(), current: t, playing: true, currentTimeMs: 10_000 })
    // Pause (same id, playing=false)
    eng.emit({ ...baseState(), current: t, playing: false, currentTimeMs: 10_000 })
    // Seek (same id, time jumps)
    eng.emit({ ...baseState(), current: t, playing: false, currentTimeMs: 30_000 })
    // Resume
    eng.emit({ ...baseState(), current: t, playing: true, currentTimeMs: 30_000 })

    // Still only the initial fire
    expect(nowPlayingFn).toHaveBeenCalledTimes(1)

    stop()
  })

  // ── Test 3: fires again when track id changes to a new track ────────────────
  it('calls nowPlayingFn again when the track id changes to a new track', () => {
    const eng = fakeEngine()
    const stop = startNowPlaying(eng as any, nowPlayingFn)

    const t1 = mkTrack('t3a', 60_000)
    const t2 = mkTrack('t3b', 90_000)

    eng.emit({ ...baseState(), current: t1, playing: true, currentTimeMs: 0 })
    expect(nowPlayingFn).toHaveBeenCalledTimes(1)

    eng.emit({ ...baseState(), current: t2, playing: true, currentTimeMs: 0 })
    expect(nowPlayingFn).toHaveBeenCalledTimes(2)
    expect(nowPlayingFn).toHaveBeenLastCalledWith({
      title: t2.title,
      artist: t2.artist,
      album: t2.album,
      durationMs: 90_000,
    })

    stop()
  })

  // ── Test 4: fires again when returning to a previous track after null ────────
  it('fires again when the same track id reappears after current went null', () => {
    const eng = fakeEngine()
    const stop = startNowPlaying(eng as any, nowPlayingFn)

    const t = mkTrack('t4', 60_000)

    // First play
    eng.emit({ ...baseState(), current: t, playing: true, currentTimeMs: 0 })
    expect(nowPlayingFn).toHaveBeenCalledTimes(1)

    // Track goes away (queue cleared)
    eng.emit({ ...baseState(), current: null })
    expect(nowPlayingFn).toHaveBeenCalledTimes(1)

    // Same track plays again
    eng.emit({ ...baseState(), current: t, playing: true, currentTimeMs: 0 })
    expect(nowPlayingFn).toHaveBeenCalledTimes(2)

    stop()
  })

  // ── Test 5: a rejecting nowPlayingFn does NOT throw ─────────────────────────
  it('swallows errors — a rejecting nowPlayingFn does not throw', async () => {
    const eng = fakeEngine()
    const rejectFn = vi
      .fn<(t: { title: string; artist: string; album: string; durationMs: number }) => Promise<void>>()
      .mockRejectedValue(new Error('network error'))

    const stop = startNowPlaying(eng as any, rejectFn)

    const t = mkTrack('t5', 60_000)

    // Must not throw synchronously
    expect(() => {
      eng.emit({ ...baseState(), current: t, playing: true, currentTimeMs: 0 })
    }).not.toThrow()

    // Allow any microtasks to settle — should also not cause unhandled rejection
    await Promise.resolve()

    stop()
  })

  // ── Test 6: durationMs fallback to state.durationMs when current.durationMs is 0 ──
  it('uses state.durationMs as fallback when current.durationMs is 0', () => {
    const eng = fakeEngine()
    const stop = startNowPlaying(eng as any, nowPlayingFn)

    const t = mkTrack('t6', 0) // current.durationMs = 0

    eng.emit({ ...baseState(), current: t, durationMs: 45_000, playing: true, currentTimeMs: 0 })

    expect(nowPlayingFn).toHaveBeenCalledTimes(1)
    expect(nowPlayingFn).toHaveBeenCalledWith(
      expect.objectContaining({ durationMs: 45_000 }),
    )

    stop()
  })
})
