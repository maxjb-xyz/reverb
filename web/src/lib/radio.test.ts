import { afterEach, describe, expect, it, vi } from 'vitest'
import type { PlayerState } from './audioEngine'
import type { EngineItem, EngineNextRequest, EngineResult } from './engineApi'
import type { PlayContext } from './playContext'
import type { Track } from './types'
import { _resetRadio, currentStation, EmptyStationError, reasonFor, startRadio, startRadioRefill, REFILL_AT } from './radio'

const track = (id: string): Track => ({
  id, title: `Song ${id}`, albumId: 'al', album: 'Album', artistId: 'ar', artist: 'Artist', coverArtId: '',
  trackNumber: 1, discNumber: 1, durationMs: 200_000, bitRate: 320, suffix: 'mp3', contentType: 'audio/mpeg',
})
const item = (id: string): EngineItem => ({ owned: true, track: track(id), score: 1, reason: { kind: 'similar_artist', artist: 'X', seedArtist: 'Bonobo' } })
const result = (ids: string[], stationId = 'artist:bonobo'): EngineResult => ({
  station: { id: stationId, kind: 'artist', label: 'Bonobo radio' },
  items: ids.map(item),
})

class FakeEngine {
  queue: Track[] = []
  index = -1
  listeners = new Set<(s: PlayerState) => void>()
  getState(): PlayerState {
    return { queue: [...this.queue], index: this.index, current: this.queue[this.index] ?? null } as PlayerState
  }
  subscribe(cb: (s: PlayerState) => void) {
    this.listeners.add(cb)
    cb(this.getState())
    return () => this.listeners.delete(cb)
  }
  emit() {
    for (const l of this.listeners) l(this.getState())
  }
  enqueue(t: Track) {
    this.queue.push(t)
    this.emit()
  }
  load(tracks: Track[], i: number) {
    this.queue = tracks.slice()
    this.index = i
    this.emit()
  }
}

afterEach(() => {
  _resetRadio()
  vi.useRealTimers()
})

function setup(ctx: PlayContext = { kind: 'radio', id: 'artist:bonobo' }) {
  const eng = new FakeEngine()
  let context = ctx
  const fetchFn = vi.fn<(r: EngineNextRequest) => Promise<EngineResult>>()
  const stop = startRadioRefill(eng, fetchFn, () => context)
  const play = vi.fn((tracks: Track[], i: number, c?: PlayContext) => {
    context = c ?? { kind: 'other' }
    eng.load(tracks, i)
  })
  return { eng, fetchFn, play, stop, setContext: (c: PlayContext) => (context = c) }
}

const flush = () => new Promise((r) => setTimeout(r, 0))

describe('radio', () => {
  it('startRadio plays the first batch under a radio context and remembers why each song was picked', async () => {
    const { fetchFn, play } = setup()
    fetchFn.mockResolvedValueOnce(result(['a', 'b', 'c', 'd', 'e', 'f']))
    const st = await startRadio({ kind: 'artist', artist: 'Bonobo' }, play, {}, fetchFn)
    expect(fetchFn).toHaveBeenCalledWith({ seed: { kind: 'artist', artist: 'Bonobo' }, n: 20 })
    expect(play).toHaveBeenCalledWith(['a', 'b', 'c', 'd', 'e', 'f'].map(track), 0, { kind: 'radio', id: 'artist:bonobo' })
    expect(st.label).toBe('Bonobo radio')
    expect(currentStation()?.id).toBe('artist:bonobo')
    expect(reasonFor('b')).toEqual({ kind: 'similar_artist', artist: 'X', seedArtist: 'Bonobo' })
  })

  it('skips songs that are not in the library and fails on an empty station', async () => {
    const { fetchFn, play } = setup()
    fetchFn.mockResolvedValueOnce({ station: { id: 's', kind: 'artist', label: 'x' }, items: [{ owned: false, score: 1, reason: { kind: 'discovery' } }] })
    await expect(startRadio({ kind: 'artist', artist: 'Nobody' }, play, {}, fetchFn)).rejects.toBeInstanceOf(EmptyStationError)
    expect(play).not.toHaveBeenCalled()
    expect(currentStation()).toBeNull()
  })

  it('refills when the queue runs low, excluding what is queued, and only once at a time', async () => {
    const { eng, fetchFn, play } = setup()
    const first = ['a', 'b', 'c', 'd', 'e', 'f']
    fetchFn.mockResolvedValueOnce(result(first))
    await startRadio({ kind: 'artist', artist: 'Bonobo' }, play, {}, fetchFn)
    fetchFn.mockClear()
    // 5 songs left after the current one: no refill yet.
    eng.index = 0
    eng.emit()
    expect(fetchFn).not.toHaveBeenCalled()
    let resolve!: (r: EngineResult) => void
    fetchFn.mockReturnValueOnce(new Promise((r) => (resolve = r)))
    eng.index = first.length - 1 - REFILL_AT
    eng.emit()
    eng.emit() // a second state change while fetching doesn't fire another request
    expect(fetchFn).toHaveBeenCalledTimes(1)
    expect(fetchFn.mock.calls[0][0]).toEqual({ seed: { kind: 'artist', artist: 'Bonobo' }, exclude: first, n: 15 })
    resolve(result(['g', 'a', 'h'])) // 'a' is already queued
    await flush()
    expect(eng.queue.map((t) => t.id)).toEqual([...first, 'g', 'h'])
  })

  it('stops when the user plays something else', async () => {
    const { eng, fetchFn, play, setContext } = setup()
    fetchFn.mockResolvedValueOnce(result(['a', 'b', 'c', 'd', 'e', 'f']))
    await startRadio({ kind: 'artist', artist: 'Bonobo' }, play, {}, fetchFn)
    fetchFn.mockClear()
    setContext({ kind: 'album', id: 'al-1' })
    eng.index = 5
    eng.emit()
    expect(fetchFn).not.toHaveBeenCalled()
    expect(currentStation()).toBeNull()
  })

  it('stops asking once the engine has nothing more', async () => {
    const { eng, fetchFn, play } = setup()
    fetchFn.mockResolvedValueOnce(result(['a', 'b', 'c', 'd', 'e', 'f']))
    await startRadio({ kind: 'genre', id: 'jazz' }, play, {}, fetchFn)
    fetchFn.mockClear()
    fetchFn.mockResolvedValue(result([]))
    eng.index = 5
    eng.emit()
    await flush()
    eng.emit()
    await flush()
    expect(fetchFn).toHaveBeenCalledTimes(1)
  })

  it('waits before retrying after a failed refill', async () => {
    vi.useFakeTimers()
    const { eng, fetchFn, play } = setup()
    fetchFn.mockResolvedValueOnce(result(['a', 'b', 'c', 'd', 'e', 'f']))
    await startRadio({ kind: 'artist', artist: 'Bonobo' }, play, {}, fetchFn)
    fetchFn.mockClear()
    fetchFn.mockRejectedValueOnce(new Error('offline'))
    eng.index = 5
    eng.emit()
    await vi.advanceTimersByTimeAsync(0)
    eng.emit()
    expect(fetchFn).toHaveBeenCalledTimes(1)
    fetchFn.mockResolvedValueOnce(result(['c']))
    await vi.advanceTimersByTimeAsync(10_000)
    eng.emit()
    expect(fetchFn).toHaveBeenCalledTimes(2)
  })
})
