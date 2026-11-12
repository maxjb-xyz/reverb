import { describe, it, expect, vi } from 'vitest'
import { startListenTracker } from './listenTracker'
import type { PlayerState } from './audioEngine'
import type { Track } from './types'
import type { ListenInput } from './tasteApi'
import type { PlayContext } from './playContext'

function mkTrack(id: string, durationMs = 200_000): Track {
  return {
    id, title: `T-${id}`, artist: 'Artist', album: 'Album', albumId: 'al', artistId: 'ar',
    coverArtId: '', trackNumber: 1, discNumber: 1, durationMs, bitRate: 320, suffix: 'mp3', contentType: 'audio/mpeg',
  }
}

function st(current: Track | null, currentTimeMs: number, playing = true, crossfadeMs = 0): PlayerState {
  return {
    queue: current ? [current] : [], index: 0, current, playing, currentTimeMs,
    durationMs: current?.durationMs ?? 0, bufferedMs: 0, volume: 1, shuffle: false, repeat: 'off', crossfadeMs,
  }
}

function setup(ctx: PlayContext = { kind: 'album', id: 'alb-1' }) {
  let sub: ((s: PlayerState) => void) | null = null
  const engine = { subscribe(cb: (s: PlayerState) => void) { sub = cb; cb(st(null, 0, false)); return () => { sub = null } } }
  const calls: { input: ListenInput; opts?: { keepalive?: boolean } }[] = []
  const record = vi.fn(async (input: ListenInput, opts?: { keepalive?: boolean }) => { calls.push({ input, opts }) })
  const listeners: Record<string, () => void> = {}
  const win = {
    addEventListener: (t: string, cb: () => void) => { listeners[t] = cb },
    removeEventListener: (t: string) => { delete listeners[t] },
  } as unknown as Pick<Window, 'addEventListener' | 'removeEventListener'>
  let context = ctx
  let clock = 1_800_000_000_000
  const stop = startListenTracker(engine, record, () => context, win, () => clock)
  const emit = (s: PlayerState) => sub?.(s)
  // Gradual playback from..to in 250 ms ticks; the wall clock keeps pace.
  const play = (t: Track, from: number, to: number, tickWallMs = 250) => {
    for (let ms = from; ms <= to; ms += 250) {
      clock += tickWallMs
      emit(st(t, ms))
    }
  }
  const tick = (ms: number) => { clock += ms }
  return { emit, play, tick, calls, stop, listeners, setContext: (c: PlayContext) => { context = c } }
}

describe('listenTracker', () => {
  it('reports a track that ran to its end as completed, with its context', () => {
    const h = setup()
    const a = mkTrack('a', 60_000)
    h.play(a, 0, 59_000)
    h.emit(st(mkTrack('b'), 0))
    expect(h.calls).toHaveLength(1)
    const c = h.calls[0].input
    expect(c.outcome).toBe('completed')
    expect(c.libraryTrackId).toBe('a')
    expect(c.contextKind).toBe('album')
    expect(c.contextId).toBe('alb-1')
    expect(c.msPlayed).toBeGreaterThan(58_000)
  })

  it('counts a crossfaded hand-over as completed, not a late skip', () => {
    const h = setup()
    const a = mkTrack('a', 180_000)
    for (let ms = 0; ms <= 172_000; ms += 250) {
      h.tick(250)
      h.emit(st(a, ms, true, 8_000))
    }
    h.emit(st(mkTrack('b'), 0, true, 8_000)) // the mixer handed over 8 s early
    expect(h.calls[0].input.outcome).toBe('completed')
  })

  it('reports an early skip when the user moves on within 30 s', () => {
    const h = setup()
    const a = mkTrack('a')
    h.play(a, 0, 5_000)
    h.emit(st(mkTrack('b'), 0))
    expect(h.calls[0].input.outcome).toBe('skip_early')
  })

  it('reports a late skip after 30 s but before the end', () => {
    const h = setup()
    const a = mkTrack('a')
    h.play(a, 0, 90_000)
    h.emit(st(mkTrack('b'), 0))
    expect(h.calls[0].input.outcome).toBe('skip_late')
  })

  it('does not count a forward seek as listening', () => {
    const h = setup()
    const a = mkTrack('a')
    h.play(a, 0, 2_000)
    h.emit(st(a, 150_000)) // seek
    h.play(a, 150_250, 152_000)
    h.emit(st(mkTrack('b'), 0))
    expect(h.calls[0].input.msPlayed).toBeLessThan(10_000)
    expect(h.calls[0].input.outcome).toBe('skip_early')
  })

  it('keeps listening that arrives in throttled ticks (background tab)', () => {
    const h = setup()
    const a = mkTrack('a')
    h.emit(st(a, 0))
    for (let ms = 10_000; ms <= 100_000; ms += 10_000) {
      h.tick(10_000) // one timeupdate every 10 s of real time
      h.emit(st(a, ms))
    }
    h.emit(st(mkTrack('b'), 0))
    expect(h.calls[0].input.msPlayed).toBe(100_000)
    expect(h.calls[0].input.outcome).toBe('skip_late')
  })

  it('ignores a restored track that was never actually played', () => {
    const h = setup()
    h.emit(st(mkTrack('restored'), 40_000, false))
    h.emit(st(mkTrack('b'), 0))
    expect(h.calls).toHaveLength(0)
  })

  it('stamps each listen with the context that was current when it started', () => {
    const h = setup({ kind: 'radio', id: 'st-1' })
    const a = mkTrack('a')
    h.play(a, 0, 1_000)
    h.setContext({ kind: 'album', id: 'x' }) // user starts an album
    h.emit(st(mkTrack('b'), 0))
    expect(h.calls[0].input.contextKind).toBe('radio')
    expect(h.calls[0].input.contextId).toBe('st-1')
  })

  it('reports a repeat-one loop as a completion and keeps tracking the next loop', () => {
    const h = setup()
    const a = mkTrack('a', 40_000)
    h.play(a, 0, 39_500)
    h.emit(st(a, 0)) // looped
    expect(h.calls).toHaveLength(1)
    expect(h.calls[0].input.outcome).toBe('completed')
    h.play(a, 250, 3_000)
    h.emit(st(mkTrack('b'), 0))
    expect(h.calls).toHaveLength(2)
    expect(h.calls[1].input.outcome).toBe('skip_early')
  })

  it('reports stopped with keepalive when the page closes mid-song', () => {
    const h = setup()
    const a = mkTrack('a')
    h.play(a, 0, 60_000)
    h.listeners.pagehide()
    expect(h.calls).toHaveLength(1)
    expect(h.calls[0].input.outcome).toBe('stopped')
    expect(h.calls[0].opts?.keepalive).toBe(true)
  })

  it('reports the last track when the queue empties', () => {
    const h = setup()
    const a = mkTrack('a', 40_000)
    h.play(a, 0, 39_000)
    h.emit(st(null, 0, false))
    expect(h.calls[0].input.outcome).toBe('completed')
  })

  it('stop() unsubscribes and removes the pagehide listener', () => {
    const h = setup()
    h.stop()
    expect(h.listeners.pagehide).toBeUndefined()
  })
})
