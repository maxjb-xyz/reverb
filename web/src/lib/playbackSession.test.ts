import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { AudioEngine, type AudioElement } from './audioEngine'
import {
  attachEngine,
  describeBrowser,
  handlePlaybackFrame,
  isRemote,
  remotePlayerState,
  remotePositionMs,
  resetPlaybackSession,
  sendCommand,
  setPlaybackTransport,
  transferTo,
  useSession,
  type RemoteState,
} from './playbackSession'
import { getPlayContext } from './playContext'
import type { Track } from './types'

function track(id: string, durationMs = 200_000): Track {
  return {
    id, title: 'T' + id, albumId: 'al', album: 'Album', artistId: 'ar', artist: 'Artist',
    coverArtId: 'co', trackNumber: 1, discNumber: 1, durationMs, bitRate: 320,
    suffix: 'mp3', contentType: 'audio/mpeg',
  }
}

class FakeAudio implements AudioElement {
  src = ''
  currentTime = 0
  duration = 0
  volume = 1
  paused = true
  buffered = { length: 0, end: () => 0, start: () => 0 }
  async play() {
    this.paused = false
  }
  pause() {
    this.paused = true
  }
  load() {}
  addEventListener() {}
  removeEventListener() {}
}

type Frame = { type: string; payload: unknown }

let sent: Frame[]
let engine: AudioEngine
let detach: () => void

function newEngine() {
  return new AudioEngine(() => new FakeAudio(), (t) => `mock://${t.id}`, () => null)
}

function connect() {
  setPlaybackTransport((f) => sent.push(f))
}

function lastOf(type: string): Frame | undefined {
  return [...sent].reverse().find((f) => f.type === type)
}

function session(over: Partial<{ devices: unknown[]; activeDeviceId: string; state: RemoteState | null; serverTime: number }> = {}) {
  const me = useSession.getState().deviceId
  handlePlaybackFrame('playback.session', {
    devices: [{ id: me, name: 'Me', kind: 'web' }, { id: 'phone', name: 'Pixel', kind: 'phone' }],
    activeDeviceId: '',
    state: null,
    serverTime: Date.now(),
    ...over,
  })
}

function remote(over: Partial<RemoteState> = {}): RemoteState {
  return {
    queue: [track('a'), track('b'), track('c')],
    index: 1,
    positionMs: 30_000,
    playing: true,
    shuffle: false,
    repeat: 'off',
    volume: 0.6,
    updatedAt: Date.now(),
    ...over,
  }
}

beforeEach(() => {
  resetPlaybackSession()
  sent = []
  engine = newEngine()
  detach = attachEngine(engine)
})

afterEach(() => detach())

describe('playback session', () => {
  it('says hello as a device when the connection opens', () => {
    connect()
    const hello = lastOf('playback.hello')
    expect(hello?.payload).toMatchObject({ id: useSession.getState().deviceId, kind: expect.any(String) })
    expect(useSession.getState().connected).toBe(true)
  })

  it('claims the session when music starts here', () => {
    connect()
    session()
    engine.playTrackList([track('1'), track('2')], 1)
    const claim = lastOf('playback.claim')
    expect(claim).toBeDefined()
    expect(claim?.payload).toMatchObject({ index: 1, playing: true })
    expect(useSession.getState().activeDeviceId).toBe(useSession.getState().deviceId)
  })

  it('does nothing over the wire while disconnected', () => {
    engine.playTrackList([track('1')], 0)
    expect(sent).toEqual([])
    expect(isRemote()).toBe(false)
  })

  it('reports structural changes while active, not every tick', () => {
    connect()
    session()
    engine.playTrackList([track('1'), track('2')], 0)
    sent = []
    engine.next()
    expect(lastOf('playback.state')?.payload).toMatchObject({ index: 1 })
    sent = []
    engine.setVolume(engine.getState().volume) // no change
    expect(lastOf('playback.state')).toBeUndefined()
  })

  it('becomes a remote while another device plays', () => {
    connect()
    session({ activeDeviceId: 'phone', state: remote() })
    expect(isRemote()).toBe(true)
    const view = remotePlayerState(engine.getState())
    expect(view.current?.id).toBe('b')
    expect(view.playing).toBe(true)
    expect(view.volume).toBe(0.6)
  })

  it('extrapolates the remote position, capped at the track length', () => {
    connect()
    const now = 1_000_000
    session({ activeDeviceId: 'phone', state: remote({ updatedAt: now - 5000 }), serverTime: Date.now() })
    expect(remotePositionMs(useSession.getState(), now - useSession.getState().clockOffsetMs)).toBe(35_000)
    session({ activeDeviceId: 'phone', state: remote({ updatedAt: now - 10_000_000 }), serverTime: Date.now() })
    expect(remotePositionMs(useSession.getState(), now - useSession.getState().clockOffsetMs)).toBe(200_000)
  })

  it('picks the account session up paused when nothing is active', () => {
    connect()
    session({ state: remote({ playing: false }) })
    const s = engine.getState()
    expect(s.current?.id).toBe('b')
    expect(s.playing).toBe(false)
    expect(s.currentTimeMs).toBe(30_000)
  })

  it('keeps its own music if it was playing when the connection came back', () => {
    engine.playTrackList([track('9')], 0)
    connect()
    session({ activeDeviceId: '', state: remote({ playing: false }) })
    expect(lastOf('playback.claim')).toBeDefined()
    expect(engine.getState().current?.id).toBe('9')
  })

  it('takes over on activate and reports at once', () => {
    connect()
    session({ activeDeviceId: 'phone', state: remote() })
    handlePlaybackFrame('playback.activate', { state: remote(), play: true })
    expect(engine.getState().current?.id).toBe('b')
    expect(engine.getState().playing).toBe(true)
    expect(isRemote()).toBe(false)
    expect(lastOf('playback.state')?.payload).toMatchObject({ index: 1, playing: true })
  })

  it('stops on deactivate', () => {
    connect()
    session()
    engine.playTrackList([track('1')], 0)
    handlePlaybackFrame('playback.deactivate', {})
    expect(engine.getState().playing).toBe(false)
  })

  it('runs commands only while active', () => {
    connect()
    session()
    engine.playTrackList([track('1'), track('2'), track('3')], 0)
    handlePlaybackFrame('playback.command', { action: 'jumpTo', index: 2 })
    expect(engine.getState().index).toBe(2)
    handlePlaybackFrame('playback.command', {
      action: 'playTracks',
      tracks: [track('x'), track('y')],
      index: 1,
      context: { kind: 'album', id: 'al9' },
    })
    expect(engine.getState().current?.id).toBe('y')
    expect(getPlayContext()).toEqual({ kind: 'album', id: 'al9' })

    handlePlaybackFrame('playback.deactivate', {})
    handlePlaybackFrame('playback.command', { action: 'play' })
    expect(engine.getState().playing).toBe(false)
  })

  it('reports a window of a long queue and maps command indices back', () => {
    connect()
    session()
    const q = Array.from({ length: 800 }, (_, i) => track('t' + i))
    engine.playTrackList(q, 200)
    const claim = lastOf('playback.claim')?.payload as { queue: Track[]; index: number; queueOffset: number }
    expect(claim.queue.length).toBe(500)
    expect(claim.queue[claim.index].id).toBe('t200')
    handlePlaybackFrame('playback.command', { action: 'jumpTo', index: claim.index + 1, base: claim.queueOffset })
    expect(engine.getState().current?.id).toBe('t201')
    // The window moved on since (t201 is now current): an index into the old
    // window is dropped rather than landing on the wrong track.
    handlePlaybackFrame('playback.command', { action: 'removeAt', index: 0, base: claim.queueOffset })
    expect(engine.getState().queue.length).toBe(800)
  })

  it('a frame from before the server saw our claim does not stop our music', () => {
    connect()
    session()
    engine.playTrackList([track('1')], 0)
    session() // broadcast in flight: nobody active yet
    expect(engine.getState().playing).toBe(true)
    expect(isRemote()).toBe(false)
    expect(useSession.getState().activeDeviceId).toBe(useSession.getState().deviceId)
  })

  it('stops instead of grabbing the session back when another device took over during an outage', () => {
    engine.playTrackList([track('9')], 0)
    connect()
    session({ activeDeviceId: 'phone', state: remote() })
    expect(lastOf('playback.claim')).toBeUndefined()
    expect(engine.getState().playing).toBe(false)
    expect(isRemote()).toBe(true)
  })

  it('shows the account queue when the device it was controlling leaves', () => {
    engine.setQueue([track('old')], 0)
    connect()
    session({ activeDeviceId: 'phone', state: remote() })
    session({ activeDeviceId: '', state: remote({ playing: false, positionMs: 40_000 }) })
    expect(engine.getState().current?.id).toBe('b')
    expect(engine.getState().currentTimeMs).toBe(40_000)
  })

  it('caps a huge playTracks around the start and stamps index commands with the window', () => {
    connect()
    session({ activeDeviceId: 'phone', state: remote({ queueOffset: 200 }) })
    const many = Array.from({ length: 1500 }, (_, i) => track('m' + i))
    sendCommand({ action: 'playTracks', tracks: many, index: 1200 })
    const c = lastOf('playback.command')?.payload as { tracks: Track[]; index: number }
    expect(c.tracks.length).toBe(1000)
    expect(c.tracks[c.index].id).toBe('m1200')
    sendCommand({ action: 'jumpTo', index: 3 })
    expect(lastOf('playback.command')?.payload).toEqual({ action: 'jumpTo', index: 3, base: 200 })
  })

  it('sends commands and transfers', () => {
    connect()
    sendCommand({ action: 'next' })
    transferTo('phone', true)
    expect(lastOf('playback.command')?.payload).toEqual({ action: 'next' })
    expect(lastOf('playback.transfer')?.payload).toEqual({ deviceId: 'phone', play: true })
  })

  it('falls back to local play when the connection drops', () => {
    connect()
    session({ activeDeviceId: 'phone', state: remote() })
    setPlaybackTransport(null)
    expect(isRemote()).toBe(false)
    expect(useSession.getState().devices).toEqual([])
  })

  it('names the browser', () => {
    expect(describeBrowser('Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15')).toBe('Safari on macOS')
    expect(describeBrowser('Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36')).toBe('Chrome on Windows')
    expect(describeBrowser('Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Mobile Safari/537.36')).toBe('Chrome on Android')
    expect(describeBrowser('')).toBe('Browser')
  })
})
