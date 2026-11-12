import { create } from 'zustand'
import type { AudioEngine, PlayerSnapshot, PlayerState, RepeatMode } from './audioEngine'
import type { Track } from './types'
import { setPlayContext, type PlayContext } from './playContext'

// The account-level listening session (Spotify Connect style). This browser
// tab is one device. At most one device on the account is active: it plays the
// audio, owns the queue and reports its state to the server. While another
// device is active this tab is a remote: the player shows that device's state
// and every player action is sent to it as a command.
//
// Without a live connection (no WebSocket, or a server without the feature)
// nothing here applies: the tab plays locally exactly as before.

export type DeviceKind = 'web' | 'desktop' | 'phone' | 'discord'

export interface Device {
  id: string
  name: string
  kind: DeviceKind
}

/** What the active device last reported (server times in unix ms). */
export interface RemoteState {
  queue: Track[]
  index: number
  positionMs: number
  playing: boolean
  shuffle: boolean
  repeat: RepeatMode
  volume: number
  updatedAt: number
  /** Where `queue` starts in the active device's longer queue. */
  queueOffset?: number
}

interface SessionFrame {
  devices: Device[]
  activeDeviceId: string
  state: RemoteState | null
  serverTime: number
}

export interface Command {
  action:
    | 'play'
    | 'pause'
    | 'toggle'
    | 'next'
    | 'prev'
    | 'seek'
    | 'setVolume'
    | 'toggleShuffle'
    | 'cycleRepeat'
    | 'jumpTo'
    | 'playTracks'
    | 'enqueue'
    | 'removeAt'
    | 'moveItem'
  tracks?: Track[]
  context?: PlayContext
  index?: number
  from?: number
  to?: number
  positionMs?: number
  volume?: number
  track?: Track
  /** The queueOffset of the state the indices refer to. */
  base?: number
}

export interface SessionStore {
  /** A live connection to the server's session (hello sent). */
  connected: boolean
  /** This tab's device id. */
  deviceId: string
  devices: Device[]
  /** '' when nothing is active. */
  activeDeviceId: string
  /** The active device's last report (also kept while nothing is active). */
  remote: RemoteState | null
  /** serverTime minus local time, to extrapolate the remote position. */
  clockOffsetMs: number
}

function newDeviceId(): string {
  try {
    if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID()
  } catch {
    // fall through
  }
  return `d-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`
}

/** A readable name for this browser: "Chrome on macOS". */
export function describeBrowser(ua: string = typeof navigator === 'undefined' ? '' : navigator.userAgent): string {
  const browser = /Edg\//.test(ua)
    ? 'Edge'
    : /OPR\//.test(ua)
      ? 'Opera'
      : /Firefox\//.test(ua)
        ? 'Firefox'
        : /Chrome\//.test(ua)
          ? 'Chrome'
          : /Safari\//.test(ua)
            ? 'Safari'
            : 'Browser'
  const os = /iPhone|iPad|iPod/.test(ua)
    ? 'iOS'
    : /Android/.test(ua)
      ? 'Android'
      : /Mac OS X|Macintosh/.test(ua)
        ? 'macOS'
        : /Windows/.test(ua)
          ? 'Windows'
          : /CrOS/.test(ua)
            ? 'ChromeOS'
            : /Linux/.test(ua)
              ? 'Linux'
              : ''
  return os ? `${browser} on ${os}` : browser
}

function isPhone(ua: string = typeof navigator === 'undefined' ? '' : navigator.userAgent): boolean {
  return /iPhone|iPod|Android.+Mobile/.test(ua)
}

export const useSession = create<SessionStore>(() => ({
  connected: false,
  // A fresh id per page load: a reloaded tab is a new device, and a
  // duplicated tab (which copies sessionStorage) never shares an id.
  deviceId: newDeviceId(),
  devices: [],
  activeDeviceId: '',
  remote: null,
  clockOffsetMs: 0,
}))

/** True while another device on the account is the one playing. */
export function isRemote(s: SessionStore = useSession.getState()): boolean {
  return s.connected && s.activeDeviceId !== '' && s.activeDeviceId !== s.deviceId
}

/** The name of the device playing when it is not this one, else null. */
export function useRemoteDeviceName(): string | null {
  return useSession((s) => {
    if (!isRemote(s)) return null
    return s.devices.find((d) => d.id === s.activeDeviceId)?.name ?? 'another device'
  })
}

/** The remote position now, moved on by the time since it was reported. */
export function remotePositionMs(s: SessionStore = useSession.getState(), now = Date.now()): number {
  const r = s.remote
  if (!r) return 0
  let pos = r.positionMs
  if (r.playing) pos += Math.max(0, now + s.clockOffsetMs - r.updatedAt)
  const cur = r.queue[r.index]
  if (cur && cur.durationMs > 0) pos = Math.min(pos, cur.durationMs)
  return Math.max(0, pos)
}

/** The player state to show for the remote device. */
export function remotePlayerState(local: PlayerState, s: SessionStore = useSession.getState(), now = Date.now()): PlayerState {
  const r = s.remote
  const queue = r?.queue ?? []
  const index = r && r.index >= 0 && r.index < queue.length ? r.index : -1
  const current = index >= 0 ? queue[index] : null
  return {
    queue,
    index,
    current,
    playing: !!r?.playing,
    currentTimeMs: remotePositionMs(s, now),
    durationMs: current?.durationMs ?? 0,
    bufferedMs: 0,
    volume: r?.volume ?? local.volume,
    shuffle: !!r?.shuffle,
    repeat: r?.repeat ?? 'off',
    crossfadeMs: local.crossfadeMs,
    speaking: false,
  }
}

// ---- the controller --------------------------------------------------------

type Send = (frame: { type: string; payload: unknown }) => void

/** Queues past this are sent as a window around the current track. */
const MAX_REPORTED_QUEUE = 500
/** The most tracks a playTracks command may carry (the server's queue cap). */
const MAX_COMMAND_TRACKS = 1000
/** How often the active device re-reports its position while playing. */
const POSITION_REPORT_MS = 15_000

let send: Send | null = null
let engineRef: AudioEngine | null = null
let unsubEngine: (() => void) | null = null
let firstSession = true
let lastKey = ''
let lastReportAt = 0
let wasPlaying = false
// A claim sent and not yet confirmed by a session naming this device. Frames
// broadcast before the server saw it still say nobody is active.
let pendingClaim = false
// Where the reported window starts in the local queue; remote commands carry
// indices into the window.
let windowStart = 0

function frame(type: string, payload: unknown) {
  send?.({ type, payload })
}

function isActive(): boolean {
  const s = useSession.getState()
  return s.connected && s.activeDeviceId === s.deviceId
}

function report(st: PlayerState) {
  let queue = st.queue
  let index = st.index
  windowStart = 0
  if (queue.length > MAX_REPORTED_QUEUE) {
    windowStart = Math.max(0, Math.min(index - 50, queue.length - MAX_REPORTED_QUEUE))
    queue = queue.slice(windowStart, windowStart + MAX_REPORTED_QUEUE)
    index -= windowStart
  }
  if (queue.length === 0) index = -1
  return {
    queue,
    index,
    queueOffset: windowStart,
    positionMs: Math.max(0, Math.round(st.currentTimeMs)),
    playing: st.playing,
    shuffle: st.shuffle,
    repeat: st.repeat,
    volume: st.volume,
  }
}

function structuralKey(st: PlayerState): string {
  return `${st.queue.length}|${st.queue.map((t) => t.id).join(',')}|${st.index}|${st.playing}|${st.shuffle}|${st.repeat}|${st.volume}`
}

function onEngineState(st: PlayerState) {
  if (!send) return
  const s = useSession.getState()
  const startedPlaying = st.playing && !wasPlaying
  wasPlaying = st.playing
  if (!isActive()) {
    // The listener started music on this device itself: take the session.
    if (startedPlaying && s.connected) {
      frame('playback.claim', report(st))
      pendingClaim = true
      useSession.setState({ activeDeviceId: s.deviceId })
      lastKey = structuralKey(st)
      lastReportAt = Date.now()
    }
    return
  }
  const key = structuralKey(st)
  const now = Date.now()
  if (key !== lastKey || (st.playing && now - lastReportAt >= POSITION_REPORT_MS)) {
    lastKey = key
    lastReportAt = now
    frame('playback.state', report(st))
  }
}

function forceReport() {
  if (!engineRef) return
  const st = engineRef.getState()
  lastKey = structuralKey(st)
  lastReportAt = Date.now()
  frame('playback.state', report(st))
}

function snapshotOf(r: RemoteState, volume: number): PlayerSnapshot {
  return {
    queue: r.queue,
    index: r.queue.length ? Math.min(Math.max(r.index, 0), r.queue.length - 1) : -1,
    positionMs: r.positionMs,
    volume,
    shuffle: r.shuffle,
    repeat: r.repeat,
  }
}

function applyCommand(cmd: Command) {
  const e = engineRef
  if (!e) return
  // Indices count from the window the sender saw. Map them through that
  // window's offset; drop index commands for a window we no longer report
  // (the sender saw a queue that has moved on).
  const base = cmd.base ?? 0
  const at = (i: number | undefined) => (i ?? 0) + base
  const indexed = cmd.action === 'jumpTo' || cmd.action === 'removeAt' || cmd.action === 'moveItem'
  if (indexed && base !== windowStart) return
  switch (cmd.action) {
    case 'play':
      return e.play()
    case 'pause':
      return e.pause()
    case 'toggle':
      return e.toggle()
    case 'next':
      return e.next()
    case 'prev':
      return e.prev()
    case 'seek':
      e.seekMs(cmd.positionMs ?? 0)
      return forceReport() // a seek is not structural; tell the others now
    case 'setVolume':
      return e.setVolume(cmd.volume ?? e.getState().volume)
    case 'toggleShuffle':
      return e.toggleShuffle()
    case 'cycleRepeat':
      return e.cycleRepeat()
    case 'jumpTo':
      return e.playAt(at(cmd.index))
    case 'removeAt':
      return e.removeAt(at(cmd.index))
    case 'moveItem':
      return e.moveItem(at(cmd.from), at(cmd.to))
    case 'enqueue':
      if (cmd.track) e.enqueue(cmd.track)
      return
    case 'playTracks':
      if (!cmd.tracks?.length) return
      setPlayContext(cmd.context ?? { kind: 'other' })
      return e.playTrackList(cmd.tracks, cmd.index ?? 0)
  }
}

/** Handle a playback.* frame from the server. Returns false for other frames. */
export function handlePlaybackFrame(type: string, payload: unknown): boolean {
  const e = engineRef
  switch (type) {
    case 'playback.session': {
      const f = payload as SessionFrame
      const s = useSession.getState()
      const prevActive = s.activeDeviceId
      let active = f.activeDeviceId || ''
      if (pendingClaim) {
        if (active === '') active = s.deviceId // the server has not seen our claim yet
        else pendingClaim = false
      }
      useSession.setState({
        devices: Array.isArray(f.devices) ? f.devices : [],
        activeDeviceId: active,
        remote: f.state ?? null,
        clockOffsetMs: typeof f.serverTime === 'number' ? f.serverTime - Date.now() : 0,
      })
      if (!e) return true
      const local = e.getState()
      const adoptAccount = () => {
        if (f.state && f.state.queue.length > 0) {
          e.adopt(snapshotOf(f.state, local.volume), false)
          wasPlaying = false
        }
      }
      if (firstSession) {
        firstSession = false
        if (local.playing) {
          if (active === '') {
            // Back after a dropped connection, still playing: keep the session.
            frame('playback.claim', report(local))
            pendingClaim = true
            useSession.setState({ activeDeviceId: s.deviceId })
          } else if (active !== s.deviceId) {
            // Another device took over while we were cut off.
            wasPlaying = false
            e.pause()
          }
        } else if (active === '') {
          // Pick up where the account left off, paused.
          adoptAccount()
        }
      } else if (active !== '' && active !== s.deviceId && prevActive === s.deviceId && local.playing) {
        // Someone else took over without a deactivate reaching us first.
        wasPlaying = false
        e.pause()
      } else if (active === '' && prevActive !== '' && prevActive !== s.deviceId && !local.playing) {
        // The device we were controlling went away: show (and be ready to
        // resume) the account's queue, not this tab's old one.
        adoptAccount()
      }
      return true
    }
    case 'playback.activate': {
      const a = payload as { state: RemoteState | null; play: boolean }
      const s = useSession.getState()
      pendingClaim = false
      useSession.setState({ activeDeviceId: s.deviceId })
      if (e) {
        if (a.state) {
          e.adopt(snapshotOf(a.state, e.getState().volume), a.play)
        } else if (a.play) {
          e.play()
        }
        wasPlaying = e.getState().playing
        forceReport()
      }
      return true
    }
    case 'playback.deactivate': {
      pendingClaim = false
      if (e && e.getState().playing) {
        wasPlaying = false
        e.pause()
      }
      const s = useSession.getState()
      if (s.activeDeviceId === s.deviceId) useSession.setState({ activeDeviceId: '' })
      return true
    }
    case 'playback.command': {
      if (isActive()) applyCommand(payload as Command)
      return true
    }
    default:
      return false
  }
}

/** Plug the local engine in. Returns a function that unplugs it. */
export function attachEngine(engine: AudioEngine): () => void {
  engineRef = engine
  wasPlaying = engine.getState().playing
  unsubEngine?.()
  unsubEngine = engine.subscribe(onEngineState)
  return () => {
    unsubEngine?.()
    unsubEngine = null
    engineRef = null
  }
}

/**
 * The realtime connection opened (`fn`) or closed (null). On open this tab
 * says hello as a device; on close it falls back to playing locally.
 */
export function setPlaybackTransport(fn: Send | null) {
  send = fn
  firstSession = true
  pendingClaim = false
  if (!fn) {
    useSession.setState({ connected: false, devices: [], activeDeviceId: '' })
    return
  }
  const s = useSession.getState()
  useSession.setState({ connected: true })
  fn({
    type: 'playback.hello',
    payload: { id: s.deviceId, name: describeBrowser(), kind: isPhone() ? 'phone' : 'web' },
  })
}

/** The active device moved its own position (a scrub): tell the others now. */
export function reportNow() {
  if (isActive()) forceReport()
}

/** Send a remote control action to the active device. */
export function sendCommand(cmd: Command) {
  let c = cmd
  if (c.action === 'playTracks' && c.tracks && c.tracks.length > MAX_COMMAND_TRACKS) {
    // The server takes at most 1000 tracks: send a window around the start.
    const idx = c.index ?? 0
    const start = Math.max(0, Math.min(idx - 100, c.tracks.length - MAX_COMMAND_TRACKS))
    c = { ...c, tracks: c.tracks.slice(start, start + MAX_COMMAND_TRACKS), index: idx - start }
  }
  if (c.action === 'jumpTo' || c.action === 'removeAt' || c.action === 'moveItem') {
    c = { ...c, base: useSession.getState().remote?.queueOffset ?? 0 }
  }
  frame('playback.command', c)
}

/** Move playback to `deviceId` (this tab's own id to play here). */
export function transferTo(deviceId: string, play = true) {
  frame('playback.transfer', { deviceId, play })
}

/** Test hook: forget everything. */
export function resetPlaybackSession() {
  send = null
  firstSession = true
  lastKey = ''
  lastReportAt = 0
  wasPlaying = false
  pendingClaim = false
  windowStart = 0
  useSession.setState({ connected: false, devices: [], activeDeviceId: '', remote: null, clockOffsetMs: 0 })
}
