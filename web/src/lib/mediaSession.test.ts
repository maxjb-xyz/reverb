import { describe, it, expect, beforeEach, vi } from 'vitest'
import { startMediaSession } from './mediaSession'
import type { PlayerState } from './audioEngine'

// jsdom has no Media Session API — install a fake before each test.
interface FakeMediaSession {
  metadata: unknown
  playbackState: string
  setActionHandler: ReturnType<typeof vi.fn>
  setPositionState: ReturnType<typeof vi.fn>
}

class FakeMediaMetadata {
  title: string
  artist: string
  album: string
  artwork: { src: string; sizes?: string }[]
  constructor(init: {
    title: string
    artist: string
    album: string
    artwork: { src: string; sizes?: string }[]
  }) {
    this.title = init.title
    this.artist = init.artist
    this.album = init.album
    this.artwork = init.artwork
  }
}

function makeTrack(id: string, title: string) {
  return {
    id,
    title,
    albumId: 'alb1',
    album: 'OK Computer',
    artistId: 'ar1',
    artist: 'Radiohead',
    coverArtId: 'cov1',
    trackNumber: 1,
    discNumber: 1,
    durationMs: 238000,
    bitRate: 320,
    suffix: 'mp3',
    contentType: 'audio/mpeg',
  }
}

function makeState(overrides: Partial<PlayerState> = {}): PlayerState {
  return {
    queue: [],
    index: -1,
    current: null,
    playing: false,
    currentTimeMs: 0,
    durationMs: 0,
    bufferedMs: 0,
    volume: 1,
    shuffle: false,
    repeat: 'off',
    ...overrides,
  }
}

function makeEngine() {
  let cb: ((s: PlayerState) => void) | null = null
  const unsub = vi.fn()
  return {
    engine: {
      subscribe: (fn: (s: PlayerState) => void) => {
        cb = fn
        return unsub
      },
      play: vi.fn(),
      pause: vi.fn(),
      next: vi.fn(),
      prev: vi.fn(),
      seekMs: vi.fn(),
    },
    emit: (s: PlayerState) => cb?.(s),
    unsub,
  }
}

describe('startMediaSession', () => {
  let ms: FakeMediaSession

  beforeEach(() => {
    ms = {
      metadata: null,
      playbackState: 'none',
      setActionHandler: vi.fn(),
      setPositionState: vi.fn(),
    }
    Object.defineProperty(navigator, 'mediaSession', {
      value: ms,
      configurable: true,
    })
    vi.stubGlobal('MediaMetadata', FakeMediaMetadata)
  })

  it('is a no-op returning a function when mediaSession is unavailable', () => {
    // @ts-expect-error - removing the fake to simulate unsupported browsers
    delete navigator.mediaSession
    const { engine } = makeEngine()
    const stop = startMediaSession(engine)
    expect(typeof stop).toBe('function')
    stop()
  })

  it('registers play/pause/previoustrack/nexttrack/seekto handlers wired to the engine', () => {
    const { engine } = makeEngine()
    startMediaSession(engine)
    const registered = new Map(
      ms.setActionHandler.mock.calls.map(([a, h]) => [a, h]),
    )
    expect([...registered.keys()].sort()).toEqual(
      ['nexttrack', 'pause', 'play', 'previoustrack', 'seekto'].sort(),
    )
    ;(registered.get('play') as () => void)()
    expect(engine.play).toHaveBeenCalled()
    ;(registered.get('pause') as () => void)()
    expect(engine.pause).toHaveBeenCalled()
    ;(registered.get('nexttrack') as () => void)()
    expect(engine.next).toHaveBeenCalled()
    ;(registered.get('previoustrack') as () => void)()
    expect(engine.prev).toHaveBeenCalled()
    ;(registered.get('seekto') as (d: { seekTime?: number }) => void)({ seekTime: 42 })
    expect(engine.seekMs).toHaveBeenCalledWith(42000)
  })

  it('sets metadata once per track change and mirrors playbackState', () => {
    const { engine, emit } = makeEngine()
    startMediaSession(engine)
    const t = makeTrack('t1', 'Karma Police')
    emit(makeState({ current: t, playing: true, durationMs: 238000 }))
    const meta1 = ms.metadata as FakeMediaMetadata
    expect(meta1.title).toBe('Karma Police')
    expect(meta1.artist).toBe('Radiohead')
    expect(meta1.album).toBe('OK Computer')
    expect(meta1.artwork[0].src).toContain('alb1')
    expect(ms.playbackState).toBe('playing')

    // same track again: metadata object must NOT be rebuilt
    emit(makeState({ current: t, playing: false, durationMs: 238000 }))
    expect(ms.metadata).toBe(meta1)
    expect(ms.playbackState).toBe('paused')

    // new track: metadata rebuilt
    emit(makeState({ current: makeTrack('t2', 'Creep'), playing: true }))
    expect((ms.metadata as FakeMediaMetadata).title).toBe('Creep')
  })

  it('reports position state when duration is known', () => {
    const { engine, emit } = makeEngine()
    startMediaSession(engine)
    emit(
      makeState({
        current: makeTrack('t1', 'Karma Police'),
        playing: true,
        durationMs: 200000,
        currentTimeMs: 50000,
      }),
    )
    expect(ms.setPositionState).toHaveBeenCalledWith({
      duration: 200,
      playbackRate: 1,
      position: 50,
    })
  })

  it('clears metadata when the queue empties, and tears down on stop()', () => {
    const { engine, emit, unsub } = makeEngine()
    const stop = startMediaSession(engine)
    emit(makeState({ current: makeTrack('t1', 'Karma Police'), playing: true }))
    expect(ms.metadata).not.toBeNull()

    emit(makeState({ current: null }))
    expect(ms.metadata).toBeNull()
    expect(ms.playbackState).toBe('none')

    stop()
    expect(unsub).toHaveBeenCalled()
    // teardown nulls every registered handler
    const nulled = ms.setActionHandler.mock.calls.filter(([, h]) => h === null)
    expect(nulled.length).toBe(5)
  })
})
