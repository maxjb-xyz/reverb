import { describe, expect, it, beforeEach } from 'vitest'
import { AudioEngine, type AudioElement } from './audioEngine'
import type { Track } from './types'

function track(id: string): Track {
  return {
    id,
    title: 'T' + id,
    albumId: 'al',
    album: 'Album',
    artistId: 'ar',
    artist: 'Artist',
    coverArtId: 'co',
    trackNumber: 1,
    discNumber: 1,
    durationMs: 1000,
    bitRate: 320,
    suffix: 'mp3',
    contentType: 'audio/mpeg',
  }
}

// fakeAudio is a minimal AudioElement stub: records play/pause, fires ended on demand.
class FakeAudio implements AudioElement {
  src = ''
  currentTime = 0
  duration = 0
  volume = 1
  paused = true
  private listeners: Record<string, Array<() => void>> = {}
  buffered = { length: 0, end: () => 0, start: () => 0 }
  async play() {
    this.paused = false
  }
  pause() {
    this.paused = true
  }
  load() {}
  addEventListener(type: string, cb: () => void) {
    ;(this.listeners[type] ||= []).push(cb)
  }
  removeEventListener(type: string, cb: () => void) {
    this.listeners[type] = (this.listeners[type] || []).filter((f) => f !== cb)
  }
  fire(type: string) {
    ;(this.listeners[type] || []).forEach((cb) => cb())
  }
}

function newEngine() {
  const audios: FakeAudio[] = []
  const engine = new AudioEngine(() => {
    const a = new FakeAudio()
    audios.push(a)
    return a
  }, (t) => `mock://${t.id}`)
  return { engine, audios }
}

const list = [track('1'), track('2'), track('3')]

describe('AudioEngine queue + transport', () => {
  let engine: AudioEngine
  let audios: FakeAudio[]
  beforeEach(() => {
    ;({ engine, audios } = newEngine())
  })

  it('plays a track list from an index', () => {
    engine.playTrackList(list, 1)
    const s = engine.getState()
    expect(s.index).toBe(1)
    expect(s.current?.id).toBe('2')
    expect(s.playing).toBe(true)
  })

  it('next advances and wraps only with repeat all', () => {
    engine.playTrackList(list, 2)
    engine.next() // at last track, repeat off → NO-OP (playing stays true, index unchanged)
    expect(engine.getState().playing).toBe(true)
    expect(engine.getState().index).toBe(2)

    engine.cycleRepeat() // off -> all
    engine.playTrackList(list, 2)
    engine.next()
    expect(engine.getState().index).toBe(0) // wrapped
  })

  it('prev goes back, clamps at start', () => {
    engine.playTrackList(list, 1)
    engine.prev()
    expect(engine.getState().index).toBe(0)
    engine.prev()
    expect(engine.getState().index).toBe(0)
  })

  it('prev restarts current track when >3s in', () => {
    engine.playTrackList(list, 1)
    audios[0].currentTime = 5 // active element; >3s in
    audios[0].fire('timeupdate')
    expect(engine.getState().currentTimeMs).toBeGreaterThan(3000)
    engine.prev()
    const s = engine.getState()
    expect(s.index).toBe(1) // unchanged
    expect(s.currentTimeMs).toBe(0) // restarted
  })

  it('repeat one replays same index on track end', () => {
    engine.playTrackList(list, 0)
    engine.cycleRepeat() // off -> all
    engine.cycleRepeat() // all -> one
    expect(engine.getState().repeat).toBe('one')
    audios[0].fire('ended')
    expect(engine.getState().index).toBe(0)
    expect(engine.getState().playing).toBe(true)
  })

  it('ended advances to next track when repeat off', () => {
    engine.playTrackList(list, 0)
    audios[0].fire('ended')
    expect(engine.getState().index).toBe(1)
  })

  it('shuffle produces a permutation covering all tracks', () => {
    engine.playTrackList(list, 0)
    engine.toggleShuffle()
    const seen = new Set<string>()
    seen.add(engine.getState().current!.id)
    engine.next()
    seen.add(engine.getState().current!.id)
    engine.next()
    seen.add(engine.getState().current!.id)
    expect(seen.size).toBe(3) // all three visited, no repeats within a cycle
  })

  it('enqueue and removeAt mutate the queue', () => {
    engine.setQueue(list, 0)
    engine.enqueue(track('4'))
    expect(engine.getState().queue.length).toBe(4)
    engine.removeAt(3)
    expect(engine.getState().queue.length).toBe(3)
  })

  it('moveItem reorders and keeps current track index correct', () => {
    engine.playTrackList(list, 0) // current = '1'
    engine.moveItem(0, 2) // move current to the end
    const s = engine.getState()
    expect(s.current?.id).toBe('1')
    expect(s.index).toBe(2)
    expect(s.queue.map((t) => t.id)).toEqual(['2', '3', '1'])
  })

  it('playAt jumps to the given index and plays', () => {
    engine.playTrackList(list, 0)
    engine.playAt(2)
    const s = engine.getState()
    expect(s.index).toBe(2)
    expect(s.current?.id).toBe('3')
    expect(s.playing).toBe(true)
  })

  it('playAt is a no-op for out-of-range indices', () => {
    engine.playTrackList(list, 0)
    engine.playAt(99)
    expect(engine.getState().index).toBe(0) // unchanged
    engine.playAt(-1)
    expect(engine.getState().index).toBe(0) // unchanged
  })

  it('playAt aligns shufflePos so next stays coherent', () => {
    engine.playTrackList(list, 0)
    engine.toggleShuffle()
    engine.playAt(2)
    expect(engine.getState().index).toBe(2)
    expect(engine.getState().current?.id).toBe('3')
  })

  it('setVolume clamps 0..1 and notifies subscribers', () => {
    let notified = 0
    engine.subscribe(() => notified++)
    engine.setVolume(2)
    expect(engine.getState().volume).toBe(1)
    engine.setVolume(-1)
    expect(engine.getState().volume).toBe(0)
    expect(notified).toBeGreaterThan(0)
  })
})

describe('AudioEngine stream-error recovery', () => {
  let engine: AudioEngine
  let audios: FakeAudio[]
  beforeEach(() => {
    ;({ engine, audios } = newEngine())
  })

  it('repeat=one + active error: stays on same track, does not infinite-loop', () => {
    engine.playTrackList(list, 1)
    engine.cycleRepeat() // off -> all
    engine.cycleRepeat() // all -> one
    expect(engine.getState().repeat).toBe('one')
    const indexBefore = engine.getState().index

    audios[0].fire('error')

    // index must NOT advance
    expect(engine.getState().index).toBe(indexBefore)
    // second error (simulating reload also failed) should stop, not loop
    audios[0].fire('error')
    expect(engine.getState().playing).toBe(false)
    expect(engine.getState().index).toBe(indexBefore)
  })

  it('repeat!==one + active error: skips to next track', () => {
    engine.playTrackList(list, 0)
    expect(engine.getState().repeat).toBe('off')

    audios[0].fire('error')

    expect(engine.getState().index).toBe(1)
  })

  it('three consecutive active errors (no successful play between): engine stops instead of skipping indefinitely', () => {
    engine.playTrackList(list, 0)

    audios[0].fire('error') // consecutiveErrors=1, skips to index 1
    audios[0].fire('error') // consecutiveErrors=2, skips to index 2
    audios[0].fire('error') // consecutiveErrors=3, should STOP

    expect(engine.getState().playing).toBe(false)
  })

  it('successful play resets counter: 5-track queue — error, error, play, error → skip not stop', () => {
    const longList = [track('a'), track('b'), track('c'), track('d'), track('e')]
    engine.playTrackList(longList, 0)

    audios[0].fire('error') // consecutiveErrors=1, skips → index 1
    audios[0].fire('error') // consecutiveErrors=2, skips → index 2

    // successful play resets counter
    audios[0].paused = false
    audios[0].fire('play')

    // one more error → consecutiveErrors=1, should skip not stop
    audios[0].fire('error')

    // should have advanced (not stopped) since counter reset
    expect(engine.getState().index).toBe(3)
    expect(engine.getState().playing).toBe(true) // still playing (skipped to next)
  })
})
