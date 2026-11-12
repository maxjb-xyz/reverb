import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PlayerState } from './audioEngine'
import type { EngineItem, EngineNextRequest, EngineResult, Lane } from './engineApi'
import type { DJClip, DJLine, DJSegment, DJStatus } from './djApi'
import { getPlayContext, setPlayContext, type PlayContext } from './playContext'
import type { Track } from './types'
import {
  _resetDJ, BLOCK_SIZE, CARD_MS, changeVibe, DJEmptyError, startDJ, startDJWatcher, stopDJ, TALK_WINDOW_MS,
  throwbackYears, useDJ, type DJDeps,
} from './dj'

const track = (id: string): Track => ({
  id, title: `Song ${id}`, albumId: 'al', album: 'Album', artistId: 'ar', artist: `Artist ${id}`, coverArtId: '',
  trackNumber: 1, discNumber: 1, durationMs: 200_000, bitRate: 320, suffix: 'mp3', contentType: 'audio/mpeg',
})
const item = (id: string): EngineItem => ({
  owned: true, track: track(id), year: 2011, score: 1,
  reason: { kind: 'similar_artist', artist: `Artist ${id}`, seedArtist: 'Bonobo', playCount: 3 },
})
const block = (lane: Lane, ids: string[]): EngineResult => ({
  station: { id: `lane:${lane}:taste`, kind: 'taste', lane, label: `Label ${lane}` },
  items: ids.map(item),
})

class FakeEngine {
  queue: Track[] = []
  index = -1
  playing = true
  currentTimeMs = 0
  listeners = new Set<(s: PlayerState) => void>()
  speak = vi.fn((_url: string) => Promise.resolve())
  getState(): PlayerState {
    return {
      queue: [...this.queue], index: this.index, current: this.queue[this.index] ?? null,
      playing: this.playing, currentTimeMs: this.currentTimeMs,
    } as PlayerState
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
    this.currentTimeMs = 0
    this.emit()
  }
  advance() {
    this.index++
    this.currentTimeMs = 0
    this.emit()
  }
}

// Lets every pending promise chain run.
const flush = () => new Promise((r) => setTimeout(r, 0))

function setup(opts: { voice?: boolean; lanes?: Partial<Record<Lane, string[]>> } = {}) {
  const eng = new FakeEngine()
  const lanes = opts.lanes ?? { 'on-repeat': ['a1', 'a2', 'a3', 'a4'], 'sounds-like': ['b1', 'b2', 'b3', 'b4'] }
  const next = vi.fn(async (r: EngineNextRequest) => block(r.lane as Lane, lanes[r.lane as Lane] ?? []))
  const line = vi.fn(async (s: DJSegment): Promise<DJLine> => ({ text: `Line for ${s.next[0].title}`, source: 'template' }))
  const say = vi.fn(async (text: string): Promise<DJClip> => ({ id: 'c', url: `/clip/${encodeURIComponent(text)}` }))
  const status = vi.fn(async (): Promise<DJStatus> => ({
    voice: { available: opts.voice ?? true, name: 'kokoro' },
    writer: { available: false, name: '' },
  }))
  const play = vi.fn((tracks: Track[], i: number, c?: PlayContext) => {
    setPlayContext(c ?? { kind: 'other' })
    eng.load(tracks, i)
  })
  const deps: Partial<DJDeps> & Pick<DJDeps, 'play'> = {
    next, line, say, status, play,
    listener: () => 'Max',
    now: () => new Date(2026, 10, 12, 21, 0),
    random: () => 0,
  }
  const stop = startDJWatcher(eng)
  return { eng, next, line, say, status, play, deps, stop }
}

beforeEach(() => {
  setPlayContext({ kind: 'other' })
})

afterEach(() => {
  _resetDJ()
  vi.useRealTimers()
})

describe('startDJ', () => {
  it('plays the first lane as a DJ block, each song tagged with its lane', async () => {
    const { deps, next, play, eng } = setup()
    const st = await startDJ(deps)
    expect(st.label).toBe('Label on-repeat')
    expect(next).toHaveBeenCalledWith({ lane: 'on-repeat', n: BLOCK_SIZE, exclude: [] })
    expect(play).toHaveBeenCalledWith(eng.queue, 0, { kind: 'dj', id: 'dj' })
    expect(eng.queue.map((t) => t.id)).toEqual(['a1', 'a2', 'a3', 'a4'])
    expect(getPlayContext('a2')).toEqual({ kind: 'dj', id: 'lane:on-repeat:taste' })
    expect(useDJ.getState()).toMatchObject({ active: true, label: 'Label on-repeat', voice: true, busy: false })
  })

  it('skips lanes with nothing to play', async () => {
    const { deps, next, eng } = setup({ lanes: { 'new-to-you': ['n1'] } })
    await startDJ(deps)
    // (A one-song block also starts the search for the next one straight away.)
    expect(next.mock.calls.slice(0, 3).map((c) => c[0].lane)).toEqual(['on-repeat', 'sounds-like', 'new-to-you'])
    expect(eng.queue.map((t) => t.id)).toEqual(['n1'])
  })

  it('gives up with DJEmptyError when no lane has anything, and stays off', async () => {
    const { deps, play } = setup({ lanes: {} })
    await expect(startDJ(deps)).rejects.toBeInstanceOf(DJEmptyError)
    expect(play).not.toHaveBeenCalled()
    expect(useDJ.getState().active).toBe(false)
  })

  it('asks a throwback lane for a period', async () => {
    const { deps, next } = setup({ lanes: { throwback: ['t1'] } })
    await startDJ(deps)
    const req = next.mock.calls.find((c) => c[0].lane === 'throwback')![0]
    expect(req).toMatchObject({ fromYear: 2017, toYear: 2021 })
    expect(throwbackYears(new Date(2026, 0, 1), 0.99)).toEqual({ fromYear: 2002, toYear: 2006 })
  })
})

describe('the DJ talking', () => {
  it('introduces the first block with a greeting segment and says it over the song', async () => {
    const { deps, line, say, eng } = setup()
    await startDJ(deps)
    await flush()
    const seg = line.mock.calls[0][0]
    expect(seg).toMatchObject({
      lane: 'on-repeat',
      reason: { artist: 'Artist a1', play_count: 3, seed: 'Bonobo' },
      listener: 'Max',
      hour: 21,
    })
    expect(seg.previous).toBeUndefined()
    expect(seg.next[0]).toEqual({ title: 'Song a1', artist: 'Artist a1', album: 'Album', year: 2011 })
    expect(say).toHaveBeenCalledWith('Line for Song a1')
    expect(eng.speak).toHaveBeenCalledWith('/clip/Line%20for%20Song%20a1')
    expect(useDJ.getState().card).toMatchObject({ label: 'Label on-repeat', text: 'Line for Song a1' })
  })

  it('shows the line without speaking when there is no voice', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    const { deps, say, eng } = setup({ voice: false })
    await startDJ(deps)
    await vi.advanceTimersByTimeAsync(0)
    expect(say).not.toHaveBeenCalled()
    expect(eng.speak).not.toHaveBeenCalled()
    expect(useDJ.getState().card?.text).toBe('Line for Song a1')
    await vi.advanceTimersByTimeAsync(CARD_MS)
    expect(useDJ.getState().card).toBeNull()
  })

  it('falls back to a plain line when the writer is unreachable, and to text when the voice fails', async () => {
    const { deps, line, say, eng } = setup()
    line.mockRejectedValueOnce(new Error('offline'))
    say.mockRejectedValueOnce(new Error('warming up'))
    await startDJ(deps)
    await flush()
    expect(eng.speak).not.toHaveBeenCalled()
    expect(useDJ.getState().card?.text).toBe('Label on-repeat. Starting with Song a1 by Artist a1.')
  })

  it('stays quiet when the line arrives after the listener moved on', async () => {
    const { deps, line, eng } = setup()
    let release!: (l: DJLine) => void
    line.mockImplementationOnce(() => new Promise((r) => (release = r)))
    await startDJ(deps)
    eng.currentTimeMs = TALK_WINDOW_MS + 1
    release({ text: 'Too late', source: 'llm' })
    await flush()
    expect(eng.speak).not.toHaveBeenCalled()
    expect(useDJ.getState().card).toBeNull()
  })
})

describe('the DJ keeping the music going', () => {
  it('queues the next lane near the end of a block and introduces it when it starts', async () => {
    const { deps, next, line, eng } = setup()
    await startDJ(deps)
    await flush()
    eng.advance() // a2
    eng.advance() // a3: one left, time for the next block
    await flush()
    expect(next).toHaveBeenLastCalledWith({ lane: 'sounds-like', n: BLOCK_SIZE, exclude: ['a1', 'a2', 'a3', 'a4'] })
    expect(eng.queue.map((t) => t.id)).toEqual(['a1', 'a2', 'a3', 'a4', 'b1', 'b2', 'b3', 'b4'])
    expect(getPlayContext('b1')).toEqual({ kind: 'dj', id: 'lane:sounds-like:taste' })
    // The break's line knows what played before it.
    expect(line.mock.calls[1][0].previous).toEqual({ title: 'Song a4', artist: 'Artist a4', album: 'Album' })
    eng.speak.mockClear()
    eng.advance() // a4
    expect(eng.speak).not.toHaveBeenCalled()
    eng.advance() // b1
    await flush()
    expect(eng.speak).toHaveBeenCalledWith('/clip/Line%20for%20Song%20b1')
    expect(useDJ.getState().label).toBe('Label sounds-like')
  })

  it('ends when the listener plays something else', async () => {
    const { deps, next, eng } = setup()
    await startDJ(deps)
    setPlayContext({ kind: 'album', id: 'al' })
    eng.load([track('x1')], 0)
    expect(useDJ.getState().active).toBe(false)
    next.mockClear()
    await flush()
    expect(next).not.toHaveBeenCalled()
  })

  it('changes the vibe on request: the next lane plays now', async () => {
    const { deps, play, eng, line } = setup()
    await startDJ(deps)
    await flush()
    expect(await changeVibe()).toBe(true)
    expect(play).toHaveBeenLastCalledWith(expect.anything(), 0, { kind: 'dj', id: 'dj' })
    expect(eng.queue.map((t) => t.id)).toEqual(['b1', 'b2', 'b3', 'b4'])
    expect(line.mock.calls[1][0].previous?.title).toBe('Song a1')
    await flush()
    expect(eng.speak).toHaveBeenLastCalledWith('/clip/Line%20for%20Song%20b1')
    expect(useDJ.getState()).toMatchObject({ active: true, label: 'Label sounds-like' })
  })

  it('stopping leaves the queue playing but stops refilling', async () => {
    const { deps, next, eng } = setup()
    await startDJ(deps)
    stopDJ()
    next.mockClear()
    eng.advance()
    eng.advance()
    await flush()
    expect(next).not.toHaveBeenCalled()
    expect(eng.queue).toHaveLength(4)
    expect(useDJ.getState().active).toBe(false)
  })
})
