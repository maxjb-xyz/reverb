import { describe, it, expect, vi, beforeEach, type Mock } from 'vitest'
import { startPlayTracker } from './playTracker'
import type { PlayerState } from './audioEngine'
import type { Track } from './types'
import type { PlayInput } from './playApi'

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

// playFromTo drives REALISTIC gradual playback: emit incremental progress states
// from `fromMs` to `toMs` in <5000ms steps (the real engine fires timeupdate ~4×/s,
// so each delta is small). This is how genuine listening accrues msPlayed — distinct
// from a single big jump, which the tracker treats as a SEEK (skipped time).
function playFromTo(
  eng: ReturnType<typeof fakeEngine>,
  track: Track,
  durationMs: number,
  fromMs: number,
  toMs: number,
  stepMs = 1_000,
) {
  for (let t = fromMs + stepMs; t < toMs; t += stepMs) {
    eng.emit({ ...baseState(), current: track, durationMs, playing: true, currentTimeMs: t })
  }
  // Always land exactly on toMs.
  eng.emit({ ...baseState(), current: track, durationMs, playing: true, currentTimeMs: toMs })
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('playTracker', () => {
  let recordFn: Mock<(input: PlayInput) => Promise<void>>

  beforeEach(() => {
    recordFn = vi.fn<(input: PlayInput) => Promise<void>>().mockResolvedValue(undefined)
  })

  // ── Test 1 ────────────────────────────────────────────────────────────────
  it('qualifies at >=50% of a >30 s track and calls recordFn exactly once', () => {
    const eng = fakeEngine()
    const stop = startPlayTracker(eng as any, recordFn)

    const t = mkTrack('t1', 60_000)

    // Track begins, then GRADUAL playback to 31 s (just past 50% of 60 s).
    eng.emit({ ...baseState(), current: t, durationMs: 60_000, playing: true, currentTimeMs: 0 })
    playFromTo(eng, t, 60_000, 0, 31_000)

    expect(recordFn).toHaveBeenCalledTimes(1)
    const call = recordFn.mock.calls[0][0]
    expect(call.libraryTrackId).toBe('t1')
    expect(call.msPlayed).toBeGreaterThanOrEqual(30_000)
    expect(call.completed).toBe(false) // 31 s is not near the end of a 60 s track

    stop()
  })

  // ── Test 1b: forward seek does NOT count skipped time (the C1 fix) ─────────
  it('a forward seek does NOT accrue skipped time and does NOT qualify', () => {
    const eng = fakeEngine()
    const stop = startPlayTracker(eng as any, recordFn)

    const t = mkTrack('t1b', 60_000)

    // Start at 0, then a SINGLE jump to 31 s — a seek, no intermediate progress.
    // The user heard 0 seconds; the skipped 31 s must NOT count toward the threshold.
    eng.emit({ ...baseState(), current: t, durationMs: 60_000, playing: true, currentTimeMs: 0 })
    eng.emit({ ...baseState(), current: t, durationMs: 60_000, playing: true, currentTimeMs: 31_000 })

    expect(recordFn).not.toHaveBeenCalled()
    stop()
  })

  // ── Test 2 ────────────────────────────────────────────────────────────────
  it('never qualifies a track with durationMs <= 30 000 ms', () => {
    const eng = fakeEngine()
    const stop = startPlayTracker(eng as any, recordFn)

    const t = mkTrack('short', 30_000) // exactly 30 s — not > 30 s

    eng.emit({ ...baseState(), current: t, durationMs: 30_000, playing: true, currentTimeMs: 0 })
    // Genuine gradual playback all the way through.
    playFromTo(eng, t, 30_000, 0, 30_000)

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
    // Genuine play to 40 s (accrues ~40 000 ms — not yet qualifying).
    playFromTo(eng, t, 100_000, 0, 40_000)
    expect(recordFn).not.toHaveBeenCalled()

    // Seek BACK to 10 s (backward jump → reset lastTimeMs, accrue NOTHING).
    eng.emit({ ...baseState(), current: t, durationMs: 100_000, playing: true, currentTimeMs: 10_000 })
    // Play forward to 40 s again (accrues ~30 000 ms; total = 40 000 + 30 000 = 70 000 ≥ 50 000).
    playFromTo(eng, t, 100_000, 10_000, 40_000)

    // Fires exactly once; msPlayed is the SUM of forward play time only (≤ 70 000),
    // NOT 80 000 (which a naive 0→40 + 10→40 with the replayed seconds would give).
    expect(recordFn).toHaveBeenCalledTimes(1)
    const call = recordFn.mock.calls[0][0]
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

    eng.emit({ ...baseState(), current: t, durationMs: 200_000, playing: true, currentTimeMs: 0 })
    // Play to 40 s
    playFromTo(eng, t, 200_000, 0, 40_000)
    // Seek back to 5 s
    eng.emit({ ...baseState(), current: t, durationMs: 200_000, playing: true, currentTimeMs: 5_000 })
    // Play to 40 s again (30 s of legitimate time — total = 70 s, still < 100 s threshold)
    playFromTo(eng, t, 200_000, 5_000, 40_000)

    // Still hasn't qualified
    expect(recordFn).not.toHaveBeenCalled()
    stop()
  })

  // ── Test 4 ────────────────────────────────────────────────────────────────
  it('repeat-one re-loop fires a SECOND qualified play after the first', () => {
    const eng = fakeEngine()
    const stop = startPlayTracker(eng as any, recordFn)

    const t = mkTrack('t4', 60_000)

    // First play — gradual to 31 s
    eng.emit({ ...baseState(), current: t, durationMs: 60_000, playing: true, currentTimeMs: 0 })
    playFromTo(eng, t, 60_000, 0, 31_000)
    expect(recordFn).toHaveBeenCalledTimes(1)

    // Repeat-one: backward jump to near-zero AFTER fired → reset for new play
    eng.emit({ ...baseState(), current: t, durationMs: 60_000, playing: true, currentTimeMs: 0 })
    // Gradual play to 31 s again
    playFromTo(eng, t, 60_000, 0, 31_000)

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
    playFromTo(eng, t1, 120_000, 0, 30_000)

    // Track changes to t2 — t1 had NOT qualified; no record for t1
    eng.emit({ ...baseState(), current: t2, durationMs: 120_000, playing: true, currentTimeMs: 0 })
    // Play t2 to 31 s — only 31 s of LEGITIMATE t2 time; must qualify on t2's own merits
    playFromTo(eng, t2, 120_000, 0, 31_000)

    // t1 never qualified (only 30 s < 60 s threshold)
    // t2 has 31 s which is < 60 s threshold — should NOT have qualified yet
    expect(recordFn).not.toHaveBeenCalled()

    // Continue t2 to 62 s → qualifies
    playFromTo(eng, t2, 120_000, 31_000, 62_000)
    expect(recordFn).toHaveBeenCalledTimes(1)
    expect(recordFn.mock.calls[0][0].libraryTrackId).toBe('t5b')
    // msPlayed should reflect ONLY t2's time (not contaminated by t1's 30 s).
    // t2 legitimate time totals ~62 000 ms.
    expect(recordFn.mock.calls[0][0].msPlayed).toBeLessThanOrEqual(62_000)

    stop()
  })

  // ── Test 5b: outgoing track ALREADY fired, then id flips → no re-fire ──────
  it('does not re-fire an already-qualified outgoing track on track change, and the new track starts clean', () => {
    const eng = fakeEngine()
    const stop = startPlayTracker(eng as any, recordFn)

    const t1 = mkTrack('t5c', 60_000)
    const t2 = mkTrack('t5d', 60_000)

    // t1 qualifies and fires (gradual to 31 s of a 60 s track).
    eng.emit({ ...baseState(), current: t1, durationMs: 60_000, playing: true, currentTimeMs: 0 })
    playFromTo(eng, t1, 60_000, 0, 31_000)
    expect(recordFn).toHaveBeenCalledTimes(1)
    expect(recordFn.mock.calls[0][0].libraryTrackId).toBe('t5c')

    // Continue t1 to its end, THEN flip to t2. The track change must NOT re-fire t1.
    playFromTo(eng, t1, 60_000, 31_000, 59_000)
    expect(recordFn).toHaveBeenCalledTimes(1) // still 1 — no re-fire

    // Track changes to t2 at 0.
    eng.emit({ ...baseState(), current: t2, durationMs: 60_000, playing: true, currentTimeMs: 0 })
    // Only a little of t2 (20 s) — must NOT qualify on t2's own (no contamination
    // from t1's large msPlayed; t2 starts clean at 0).
    playFromTo(eng, t2, 60_000, 0, 20_000)
    expect(recordFn).toHaveBeenCalledTimes(1) // t2 hasn't qualified yet

    // Push t2 over its threshold (31 s) → now it fires for the FIRST time.
    playFromTo(eng, t2, 60_000, 20_000, 31_000)
    expect(recordFn).toHaveBeenCalledTimes(2)
    expect(recordFn.mock.calls[1][0].libraryTrackId).toBe('t5d')
    expect(recordFn.mock.calls[1][0].msPlayed).toBeLessThanOrEqual(31_000)

    stop()
  })

  // ── Test 6: completed flag ────────────────────────────────────────────────
  it('sets completed=true when the play qualifies with currentTimeMs within 1500 ms of durationMs', () => {
    const eng = fakeEngine()
    const stop = startPlayTracker(eng as any, recordFn)

    const t = mkTrack('t6', 60_000)

    // User seeks forward to ~29 s — that skipped time accrues NOTHING (forward
    // seek). Then plays GRADUALLY from there to the end. The ~30.5 s of genuine
    // second-half listening crosses 50% of 60 s only at ~59.5 s — within 1500 ms
    // of the end → completed=true at fire time.
    eng.emit({ ...baseState(), current: t, durationMs: 60_000, playing: true, currentTimeMs: 0 })
    // Forward seek to 29 s (single jump, no intermediate progress).
    eng.emit({ ...baseState(), current: t, durationMs: 60_000, playing: true, currentTimeMs: 29_000 })
    // Genuine gradual second-half playback to 59.5 s (≈30.5 s of accrual ≥ 30 s).
    playFromTo(eng, t, 60_000, 29_000, 59_500)

    expect(recordFn).toHaveBeenCalledTimes(1)
    expect(recordFn.mock.calls[0][0].completed).toBe(true)
    stop()
  })

  // ── Test 7: durationMs init from current.durationMs (m2 robustness) ───────
  it('qualifies even when engine state durationMs is 0 but the Track carries durationMs', () => {
    const eng = fakeEngine()
    const stop = startPlayTracker(eng as any, recordFn)

    const t = mkTrack('t7', 60_000)

    // Engine state durationMs stays 0 (not yet settled), but the Track metadata
    // carries 60 000. The tracker should seed durationMs from current.durationMs
    // so qualification still works without waiting for durationchange.
    eng.emit({ ...baseState(), current: t, durationMs: 0, playing: true, currentTimeMs: 0 })
    playFromTo(eng, t, 0, 0, 31_000)

    expect(recordFn).toHaveBeenCalledTimes(1)
    expect(recordFn.mock.calls[0][0].durationMs).toBe(60_000)
    stop()
  })
})
