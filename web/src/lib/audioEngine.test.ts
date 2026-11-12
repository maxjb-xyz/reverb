import { afterEach, describe, expect, it, beforeEach, vi } from 'vitest'
import { AudioEngine, type AudioElement } from './audioEngine'
import type { Mixer } from './audioMixer'
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

// FakeMixer records what the engine asks of the Web Audio graph.
class FakeMixer implements Mixer {
  connected = new Set<AudioElement>()
  buses = new Map<AudioElement, string>()
  gains = new Map<AudioElement, number>()
  fades: Array<{ el: AudioElement; from: number; to: number; ms: number }> = []
  ducks: Array<{ level: number; ms: number }> = []
  resumed = 0
  connect(el: AudioElement, bus: 'music' | 'voice' = 'music') {
    this.connected.add(el)
    this.buses.set(el, bus)
    return true
  }
  setGain(el: AudioElement, v: number) {
    this.gains.set(el, v)
  }
  fade(el: AudioElement, from: number, to: number, ms: number) {
    this.fades.push({ el, from, to, ms })
    this.gains.set(el, to)
  }
  duckMusic(level: number, ms: number) {
    this.ducks.push({ level, ms })
  }
  resume() {
    this.resumed++
  }
}

function newEngine(mixer: Mixer | null = null) {
  const audios: FakeAudio[] = []
  const engine = new AudioEngine(
    () => {
      const a = new FakeAudio()
      audios.push(a)
      return a
    },
    (t) => `mock://${t.id}`,
    () => mixer,
  )
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

describe('AudioEngine session restore', () => {
  const snap = (over: Partial<Parameters<AudioEngine['restore']>[0]> = {}) => ({
    queue: [track('1'), track('2'), track('3')],
    index: 1,
    positionMs: 400,
    volume: 0.5,
    shuffle: false,
    repeat: 'all' as const,
    ...over,
  })

  it('restores queue, track, position and settings without playing or fetching', () => {
    const { engine, audios: els } = newEngine()
    engine.restore(snap())
    const s = engine.getState()
    expect(s.current?.id).toBe('2')
    expect(s.queue.map((t) => t.id)).toEqual(['1', '2', '3'])
    expect(s.playing).toBe(false)
    expect(s.currentTimeMs).toBe(400)
    expect(s.volume).toBe(0.5)
    expect(s.repeat).toBe('all')
    expect(els[0].src).toBe('') // nothing loaded until play
  })

  it('play after a restore loads the track and resumes from the saved position', () => {
    const { engine, audios: els } = newEngine()
    engine.restore(snap())
    engine.play()
    expect(els[0].src).toContain('2')
    expect(els[0].currentTime).toBeCloseTo(0.4)
    expect(engine.getState().playing).toBe(true)
  })

  it('skipping after a restore starts the next track from the beginning', () => {
    const { engine, audios: els } = newEngine()
    engine.restore(snap())
    engine.next()
    expect(engine.getState().current?.id).toBe('3')
    expect(engine.getState().currentTimeMs).toBe(0)
    expect(els[0].currentTime).toBe(0)
  })

  it('scrubbing before pressing play moves the resume point', () => {
    const { engine, audios: els } = newEngine()
    engine.restore(snap())
    engine.seekMs(800)
    engine.play()
    expect(els[0].currentTime).toBeCloseTo(0.8)
  })

  it('does not overwrite a queue that is already there', () => {
    const { engine } = newEngine()
    engine.playTrackList([track('9')], 0)
    engine.restore(snap())
    expect(engine.getState().current?.id).toBe('9')
  })

  it('snapshot round-trips through restore', () => {
    const { engine } = newEngine()
    engine.restore(snap())
    expect(engine.snapshot()).toEqual(snap())
  })
})


describe('AudioEngine crossfade', () => {
  let engine: AudioEngine
  let audios: FakeAudio[]
  let mixer: FakeMixer
  beforeEach(() => {
    vi.useFakeTimers()
    mixer = new FakeMixer()
    ;({ engine, audios } = newEngine(mixer))
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  // Move the playing element to `sec` into a 200 s track and tell the engine.
  function at(el: FakeAudio, sec: number) {
    el.duration = 200
    el.currentTime = sec
    el.fire('timeupdate')
  }

  it('is off by default: the track plays to its end', () => {
    engine.playTrackList(list, 0)
    at(audios[0], 199)
    expect(engine.getState().index).toBe(0)
    expect(audios[1].paused).toBe(true)
    expect(mixer.connected.size).toBe(0) // nothing is routed through Web Audio
  })

  it('clamps the crossfade to 0..12 s and reports it in state', () => {
    engine.setCrossfade(30_000)
    expect(engine.getState().crossfadeMs).toBe(12_000)
    engine.setCrossfade(-5)
    expect(engine.getState().crossfadeMs).toBe(0)
  })

  it('routes both elements through the mixer once crossfade is on and playback starts', () => {
    engine.setCrossfade(5000)
    engine.playTrackList(list, 0)
    expect(mixer.connected.has(audios[0])).toBe(true)
    expect(mixer.connected.has(audios[1])).toBe(true)
    expect(mixer.resumed).toBeGreaterThan(0)
  })

  it('starts the next track on the other element as the current one nears its end', () => {
    engine.setCrossfade(5000)
    engine.playTrackList(list, 0)
    at(audios[0], 190) // 10 s left: too early
    expect(engine.getState().index).toBe(0)
    at(audios[0], 195) // 5 s left: fade starts
    const s = engine.getState()
    expect(s.index).toBe(1)
    expect(s.current?.id).toBe('2')
    expect(audios[1].src).toBe('mock://2')
    expect(audios[1].paused).toBe(false)
    expect(audios[1].currentTime).toBe(0)
    expect(mixer.fades).toContainEqual({ el: audios[1], from: 0, to: 1, ms: 5000 })
    expect(mixer.fades).toContainEqual({ el: audios[0], from: 1, to: 0, ms: 5000 })
  })

  it('shortens the fade to the time that is actually left', () => {
    engine.setCrossfade(5000)
    engine.playTrackList(list, 0)
    at(audios[0], 197)
    expect(mixer.fades).toContainEqual({ el: audios[0], from: 1, to: 0, ms: 3000 })
  })

  it('stops the old track after the fade and preloads the one after next on it', () => {
    engine.setCrossfade(5000)
    engine.playTrackList(list, 0)
    at(audios[0], 195)
    expect(audios[0].paused).toBe(false) // still fading out
    vi.advanceTimersByTime(5000)
    expect(audios[0].paused).toBe(true)
    expect(audios[0].src).toBe('mock://3')
    expect(mixer.gains.get(audios[0])).toBe(1) // ready to be faded in next time
  })

  it('ignores the old track ending during the fade', () => {
    engine.setCrossfade(5000)
    engine.playTrackList(list, 0)
    at(audios[0], 195)
    audios[0].fire('ended')
    expect(engine.getState().index).toBe(1)
  })

  it('follows the new element for time, errors and its own end', () => {
    engine.setCrossfade(5000)
    engine.playTrackList(list, 0)
    at(audios[0], 195)
    vi.advanceTimersByTime(5000)
    at(audios[1], 42)
    expect(engine.getState().currentTimeMs).toBe(42_000)
    audios[1].fire('error')
    expect(engine.getState().index).toBe(2)
  })

  it('pausing mid-fade silences both tracks and resumes only the new one at full level', () => {
    engine.setCrossfade(5000)
    engine.playTrackList(list, 0)
    at(audios[0], 195)
    engine.pause()
    expect(audios[0].paused).toBe(true)
    expect(audios[1].paused).toBe(true)
    engine.play()
    expect(audios[1].paused).toBe(false)
    expect(audios[0].paused).toBe(true)
    expect(mixer.gains.get(audios[1])).toBe(1)
  })

  it('skipping mid-fade drops the old track at once', () => {
    engine.setCrossfade(5000)
    engine.playTrackList(list, 0)
    at(audios[0], 195)
    engine.next()
    expect(engine.getState().index).toBe(2)
    expect(audios[0].paused).toBe(true)
    expect(mixer.gains.get(audios[1])).toBe(1)
  })

  it('does not crossfade on repeat one', () => {
    engine.setCrossfade(5000)
    engine.playTrackList(list, 0)
    engine.cycleRepeat()
    engine.cycleRepeat()
    at(audios[0], 196)
    expect(engine.getState().index).toBe(0)
    expect(audios[1].paused).toBe(true)
  })

  it('does not crossfade past the end of the queue', () => {
    engine.setCrossfade(5000)
    engine.playTrackList(list, 2)
    at(audios[0], 196)
    expect(engine.getState().index).toBe(2)
    expect(audios[1].paused).toBe(true)
  })

  it('does not crossfade tracks shorter than twice the fade', () => {
    engine.setCrossfade(5000)
    engine.playTrackList(list, 0)
    audios[0].duration = 8
    audios[0].currentTime = 4
    audios[0].fire('timeupdate')
    expect(engine.getState().index).toBe(0)
  })

  it('plays normally without a crossfade when the browser has no Web Audio', () => {
    const plain = newEngine(null)
    plain.engine.setCrossfade(5000)
    plain.engine.playTrackList(list, 0)
    plain.audios[0].duration = 200
    plain.audios[0].currentTime = 196
    plain.audios[0].fire('timeupdate')
    expect(plain.engine.getState().index).toBe(0)
    plain.audios[0].fire('ended')
    expect(plain.engine.getState().index).toBe(1)
  })
})

describe('AudioEngine voice-over', () => {
  let engine: AudioEngine
  let audios: FakeAudio[]
  let mixer: FakeMixer
  beforeEach(() => {
    mixer = new FakeMixer()
    ;({ engine, audios } = newEngine(mixer))
  })

  it('plays a line on its own element, on the voice bus, with the music ducked', async () => {
    engine.playTrackList(list, 0)
    const done = engine.speak('mock://line-1')
    const voice = audios[2]
    expect(voice.src).toBe('mock://line-1')
    expect(voice.paused).toBe(false)
    expect(mixer.buses.get(voice)).toBe('voice')
    expect(mixer.connected.has(audios[0])).toBe(true) // music routed even with crossfade off
    expect(mixer.ducks.at(-1)).toEqual({ level: 0.25, ms: 400 })
    voice.fire('ended')
    await done
    expect(mixer.ducks.at(-1)).toEqual({ level: 1, ms: 800 })
    expect(engine.getState().speaking).toBe(false)
  })

  it('reports speaking in state while a line plays', () => {
    engine.playTrackList(list, 0)
    void engine.speak('mock://line-1')
    expect(engine.getState().speaking).toBe(true)
  })

  it('takes a custom duck level', () => {
    engine.playTrackList(list, 0)
    void engine.speak('mock://line-1', { duckTo: 0.5 })
    expect(mixer.ducks.at(-1)).toEqual({ level: 0.5, ms: 400 })
  })

  it('a broken line restores the music and still resolves', async () => {
    engine.playTrackList(list, 0)
    const done = engine.speak('mock://broken')
    audios[2].fire('error')
    await done
    expect(mixer.ducks.at(-1)?.level).toBe(1)
    expect(engine.getState().index).toBe(0) // never touches the queue
  })

  it('a new line cuts off the one playing', async () => {
    engine.playTrackList(list, 0)
    const first = engine.speak('mock://line-1')
    void engine.speak('mock://line-2')
    await first
    expect(audios[2].src).toBe('mock://line-2')
    expect(audios[2].paused).toBe(false)
  })

  it('pausing drops the line and brings the music back up', async () => {
    engine.playTrackList(list, 0)
    const done = engine.speak('mock://line-1')
    engine.pause()
    await done
    expect(audios[2].paused).toBe(true)
    expect(mixer.ducks.at(-1)?.level).toBe(1)
    expect(engine.getState().speaking).toBe(false)
  })

  it('without Web Audio, lowers the element volume instead and restores it', async () => {
    const plain = newEngine(null)
    plain.engine.setVolume(0.8)
    plain.engine.playTrackList(list, 0)
    const done = plain.engine.speak('mock://line-1')
    expect(plain.audios[0].volume).toBeCloseTo(0.2)
    plain.audios[2].fire('ended')
    await done
    expect(plain.audios[0].volume).toBeCloseTo(0.8)
  })
})
