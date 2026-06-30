import { describe, it, expect, vi, beforeEach } from 'vitest'
import { startPlayTracker } from './playTracker'
import type { PlayerState } from './audioEngine'
import type { Track } from './types'

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function mkTrack(id: string, durationMs: number): Track {
  return {
    id,
    title: `Title-${id}`,
    artist: 'Artist',
    album: 'Album',
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

// A fake engine that captures the subscriber so tests can push states manually.
function fakeEngine() {
  let subscriber: ((s: PlayerState) => void) | null = null
  return {
    subscribe(cb: (s: PlayerState) => void) {
      subscriber = cb
      // Emit the initial idle state on subscribe (mirrors AudioEngine.subscribe behaviour).
      cb(baseState())
      return () => { subscriber = null }
    },
    emit(s: PlayerState) {
      subscriber?.(s)
    },
  }
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('playTracker', () => {
  let recordFn: ReturnType<typeof vi.fn>

  beforeEach(() => {
    recordFn = vi.fn().mockResolvedValue(undefined)
  })

  // ── Test 1 ────────────────────────────────────────────────────────────────
  it('qualifies at >=50% of a >30 s track and calls recordFn exactly once', async () => {
    const eng = fakeEngine()
    const stop = startPlayTracker(eng as any, recordFn)

    const t = mkTrack('t1', 60_000)

    // Track begins – playing starts at 0 ms
    eng.emit({ ...baseState(), current: t, durationMs: 60_000, playing: true, currentTimeMs: 0 })
    // Advance to 31 s (just past 50% of 60 s)
    eng.emit({ ...baseState(), current: t, durationMs: 60_000, playing: true, currentTimeMs: 31_000 })

    expect(recordFn).toHaveBeenCalledTimes(1)
    const call = recordFn.mock.calls[0][0]
    expect(call.libraryTrackId).toBe('t1')
    expect(call.msPlayed).toBeGreaterThanOrEqual(30_000)
    expect(call.completed).toBe(false) // 31 s is not near the end of a 60 s track

    stop()
  })

  // ── Test 2 ────────────────────────────────────────────────────────────────
  it('never qualifies a track with durationMs <= 30 000 ms', () => {
    const eng = fakeEngine()
    const stop = startPlayTracker(eng as any, recordFn)

    const t = mkTrack('short', 30_000) // exactly 30 s — not > 30 s

    eng.emit({ ...baseState(), current: t, durationMs: 30_000, playing: true, currentTimeMs: 0 })
    eng.emit({ ...baseState(), current: t, durationMs: 30_000, playing: true, currentTimeMs: 20_000 })
    // Even "completing" it
    eng.emit({ ...baseState(), current: t, durationMs: 30_000, playing: true, currentTimeMs: 30_000 })

    expect(recordFn).not.toHaveBeenCalled()
    stop()
  })

  // ── Test 3 ────────────────────────────────────────────────────────────────
  it('does not double-count a seek backward: only legitimate play time accrues', () => {
    const eng = fakeEngine()
    const stop = startPlayTracker(eng as any, recordFn)

    // 100 s track; threshold = 50 s (50%) or 240 s; use 50 s.
    const t = mkTrack('t3', 100_000)

    eng.emit({ ...baseState(), current: t, durationMs: 100_000, playing: true, currentTimeMs: 0 })
    // Play to 40 s (accrues 40 000 ms — not yet qualifying)
    eng.emit({ ...baseState(), current: t, durationMs: 100_000, playing: true, currentTimeMs: 40_000 })
    expect(recordFn).not.toHaveBeenCalled()

    // Seek BACK to 10 s (backward jump → reset lastTimeMs, accrue NOTHING)
    eng.emit({ ...baseState(), current: t, durationMs: 100_000, playing: true, currentTimeMs: 10_000 })
    // Play forward to 40 s again (accrues 30 000 ms, total legit = 40 000 + 30 000 = 70 000 — WAIT)
    // Actually: after seek reset, lastTimeMs = 10 000.  Next delta = 40 000 - 10 000 = +30 000.
    // Total msPlayed = 40 000 (before seek) + 30 000 = 70 000 ≥ 50 000 → qualifies now.
    //
    // But the test intent is: a naive implementation that just uses currentTimeMs directly
    // would see "at 40 s twice" and might fire too early or double-count.  What we verify
    // is that it fires only ONCE, not zero times, and the msPlayed is the sum of forward
    // deltas only.
    eng.emit({ ...baseState(), current: t, durationMs: 100_000, playing: true, currentTimeMs: 40_000 })

    // After the seek-back + replay to 40 s, total legitimate ms = 40 000 + 30 000 = 70 000 ≥ 50 000.
    expect(recordFn).toHaveBeenCalledTimes(1)
    const call = recordFn.mock.calls[0][0]
    // msPlayed must NOT be 80 000 (which a naive delta from 0→40, 40→10(ignored), 10→40 would give).
    // It should be ≥ 50 000 and ≤ 70 000 (the correct accumulated forward time).
    expect(call.msPlayed).toBeGreaterThanOrEqual(50_000)
    expect(call.msPlayed).toBeLessThanOrEqual(70_000)

    stop()
  })

  // ── Test 3b ───────────────────────────────────────────────────────────────
  it('seek backward from before threshold does NOT allow qualifying on replayed seconds alone', () => {
    const eng = fakeEngine()
    const stop = startPlayTracker(eng as any, recordFn)

    // 200 s track; 50% threshold = 100 s.
    const t = mkTrack('t3b', 200_000)

    // Play to 40 s
    eng.emit({ ...baseState(), current: t, durationMs: 200_000, playing: true, currentTimeMs: 0 })
    eng.emit({ ...baseState(), current: t, durationMs: 200_000, playing: true, currentTimeMs: 40_000 })
    // Seek back to 5 s
    eng.emit({ ...baseState(), current: t, durationMs: 200_000, playing: true, currentTimeMs: 5_000 })
    // Play to 40 s again (30 s of legitimate time — total = 70 s, still < 100 s threshold)
    eng.emit({ ...baseState(), current: t, durationMs: 200_000, playing: true, currentTimeMs: 40_000 })

    // Still hasn't qualified
    expect(recordFn).not.toHaveBeenCalled()
    stop()
  })

  // ── Test 4 ────────────────────────────────────────────────────────────────
  it('repeat-one re-loop fires a SECOND qualified play after the first', async () => {
    const eng = fakeEngine()
    const stop = startPlayTracker(eng as any, recordFn)

    const t = mkTrack('t4', 60_000)

    // First play
    eng.emit({ ...baseState(), current: t, durationMs: 60_000, playing: true, currentTimeMs: 0 })
    eng.emit({ ...baseState(), current: t, durationMs: 60_000, playing: true, currentTimeMs: 31_000 })
    expect(recordFn).toHaveBeenCalledTimes(1)

    // Repeat-one: backward jump to near-zero AFTER fired → reset for new play
    eng.emit({ ...baseState(), current: t, durationMs: 60_000, playing: true, currentTimeMs: 0 })
    // Play to 31 s again
    eng.emit({ ...baseState(), current: t, durationMs: 60_000, playing: true, currentTimeMs: 31_000 })

    expect(recordFn).toHaveBeenCalledTimes(2)
    stop()
  })

  // ── Test 5 ────────────────────────────────────────────────────────────────
  it('a track change resets state so cross-track msPlayed does not contaminate the next track', () => {
    const eng = fakeEngine()
    const stop = startPlayTracker(eng as any, recordFn)

    const t1 = mkTrack('t5a', 120_000) // 2 min; threshold = 60 s
    const t2 = mkTrack('t5b', 120_000)

    // Play t1 up to 30 s (not yet qualifying)
    eng.emit({ ...baseState(), current: t1, durationMs: 120_000, playing: true, currentTimeMs: 0 })
    eng.emit({ ...baseState(), current: t1, durationMs: 120_000, playing: true, currentTimeMs: 30_000 })

    // Track changes to t2 — t1 had NOT qualified; no record for t1
    eng.emit({ ...baseState(), current: t2, durationMs: 120_000, playing: true, currentTimeMs: 0 })
    // Play t2 to 31 s — only 31 s of LEGITIMATE t2 time; must qualify on t2's own merits
    eng.emit({ ...baseState(), current: t2, durationMs: 120_000, playing: true, currentTimeMs: 31_000 })

    // t1 never qualified (only 30 s < 60 s threshold)
    // t2 has 31 s which is < 60 s threshold — should NOT have qualified yet
    expect(recordFn).not.toHaveBeenCalled()

    // Continue t2 to 62 s → qualifies
    eng.emit({ ...baseState(), current: t2, durationMs: 120_000, playing: true, currentTimeMs: 62_000 })
    expect(recordFn).toHaveBeenCalledTimes(1)
    expect(recordFn.mock.calls[0][0].libraryTrackId).toBe('t5b')
    // msPlayed should reflect ONLY t2's time (not contaminated by t1's 30 s)
    // t2 legitimate time: 0→31 000 = 31 000, then 31 000→62 000 = 31 000 → total 62 000 ms
    expect(recordFn.mock.calls[0][0].msPlayed).toBeLessThanOrEqual(62_000)

    stop()
  })

  // ── Test 6: completed flag ────────────────────────────────────────────────
  it('sets completed=true when currentTimeMs is within 1500 ms of durationMs at fire time', () => {
    const eng = fakeEngine()
    const stop = startPlayTracker(eng as any, recordFn)

    const t = mkTrack('t6', 60_000)

    eng.emit({ ...baseState(), current: t, durationMs: 60_000, playing: true, currentTimeMs: 0 })
    // Fire at 59 500 ms — within 1500 ms of 60 000
    eng.emit({ ...baseState(), current: t, durationMs: 60_000, playing: true, currentTimeMs: 59_500 })

    expect(recordFn).toHaveBeenCalledTimes(1)
    expect(recordFn.mock.calls[0][0].completed).toBe(true)
    stop()
  })
})
