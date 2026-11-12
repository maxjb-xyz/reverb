import { describe, it, expect, beforeEach, vi } from 'vitest'
import { AudioEngine, type AudioElement } from './audioEngine'
import { loadSnapshot, saveSnapshot, startPlayerPersistence } from './playerPersistence'
import type { Track } from './types'

function track(id: string): Track {
  return {
    id, title: 'T' + id, albumId: 'al', album: 'Album', artistId: 'ar', artist: 'Artist', coverArtId: '',
    trackNumber: 1, discNumber: 1, durationMs: 60_000, bitRate: 320, suffix: 'mp3', contentType: 'audio/mpeg',
  }
}

class FakeAudio implements AudioElement {
  src = ''
  currentTime = 0
  duration = 0
  volume = 1
  paused = true
  buffered = { length: 0, end: () => 0, start: () => 0 }
  async play() { this.paused = false }
  pause() { this.paused = true }
  load() {}
  addEventListener() {}
  removeEventListener() {}
}

const newEngine = () => new AudioEngine(() => new FakeAudio(), (t) => `/stream/${t.id}`)

describe('playerPersistence', () => {
  beforeEach(() => localStorage.clear())

  it('saves the session when the queue changes and restores it, paused, for the same user', () => {
    const first = newEngine()
    const stop = startPlayerPersistence(first, 'u1')
    first.playTrackList([track('a'), track('b')], 1)
    stop()

    const second = newEngine()
    startPlayerPersistence(second, 'u1')()
    const s = second.getState()
    expect(s.queue.map((t) => t.id)).toEqual(['a', 'b'])
    expect(s.current?.id).toBe('b')
    expect(s.playing).toBe(false)
  })

  it('keeps each user separate', () => {
    const e = newEngine()
    const stop = startPlayerPersistence(e, 'u1')
    e.playTrackList([track('a')], 0)
    stop()
    const other = newEngine()
    startPlayerPersistence(other, 'u2')()
    expect(other.getState().queue).toEqual([])
  })

  it('saves the position on pagehide', () => {
    const e = newEngine()
    const stop = startPlayerPersistence(e, 'u1')
    e.playTrackList([track('a')], 0)
    e.seekMs(12_000)
    vi.spyOn(Date, 'now').mockReturnValue(0) // defeat the periodic save
    window.dispatchEvent(new Event('pagehide'))
    expect(loadSnapshot('u1')?.positionMs).toBe(12_000)
    vi.restoreAllMocks()
    stop()
  })

  it('ignores corrupt or wrongly shaped data', () => {
    localStorage.setItem('reverb.player.v1:u1', '{not json')
    expect(loadSnapshot('u1')).toBeNull()
    localStorage.setItem('reverb.player.v1:u1', JSON.stringify({ queue: 'nope' }))
    expect(loadSnapshot('u1')).toBeNull()
  })

  it('trims very long queues around the current track', () => {
    const queue = Array.from({ length: 800 }, (_, i) => track(String(i)))
    saveSnapshot('u1', { queue, index: 700, positionMs: 0, volume: 1, shuffle: false, repeat: 'off' })
    const snap = loadSnapshot('u1')!
    expect(snap.queue.length).toBe(500)
    expect(snap.queue[snap.index].id).toBe('700')
  })

  it('forgets the session when the queue is emptied', () => {
    saveSnapshot('u1', { queue: [track('a')], index: 0, positionMs: 0, volume: 1, shuffle: false, repeat: 'off' })
    saveSnapshot('u1', { queue: [], index: -1, positionMs: 0, volume: 1, shuffle: false, repeat: 'off' })
    expect(loadSnapshot('u1')).toBeNull()
  })
})
