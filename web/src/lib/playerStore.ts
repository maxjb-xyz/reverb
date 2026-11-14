import { create } from 'zustand'
import { AudioEngine, type PlayerState } from './audioEngine'
import { loadPlaybackPrefs, savePlaybackPrefs } from './playbackPrefs'
import type { Track } from './types'
import { contextFromPath, setPlayContext, type PlayContext } from './playContext'
import {
  attachEngine,
  isRemote,
  remotePlayerState,
  reportNow,
  sendCommand,
  useSession,
  type Command,
} from './playbackSession'

// Single imperative engine instance, living OUTSIDE React.
export const engine = new AudioEngine()
// Device-level playback preferences. Nothing is routed through Web Audio here:
// that waits for the first play, which is a user gesture.
engine.setCrossfade(loadPlaybackPrefs().crossfadeMs)
// This tab is a device in the account's listening session.
attachEngine(engine)

interface PlayerActions {
  /**
   * Replace the queue and start playing. `context` says where the tracks came
   * from (radio, a DJ lane, a mix...); when omitted it is inferred from the
   * page the user pressed play on. Listens are stamped with it.
   */
  playTrackList(tracks: Track[], startIndex: number, context?: PlayContext): void
  enqueue(t: Track): void
  removeAt(i: number): void
  moveItem(from: number, to: number): void
  jumpTo(index: number): void
  play(): void
  pause(): void
  toggle(): void
  next(): void
  prev(): void
  seekMs(ms: number): void
  setVolume(v: number): void
  toggleShuffle(): void
  cycleRepeat(): void
  /** Crossfade length in ms (0 = off); remembered on this device. */
  setCrossfade(ms: number): void
}

export type PlayerStore = PlayerState & PlayerActions

// While another device is active, the player shows its state and every action
// becomes a command for it. Otherwise the local engine plays as always.
function viewState(): PlayerState {
  const local = engine.getState()
  return isRemote() ? remotePlayerState(local) : local
}

// local runs `fn` on this device, or sends `cmd` to the active device.
function route(cmd: Command, fn: () => void) {
  if (isRemote()) sendCommand(cmd)
  else fn()
}

export const usePlayer = create<PlayerStore>((set) => {
  // Mirror the engine into the store, unless a remote device is shown.
  engine.subscribe((s) => {
    if (!isRemote()) set(s)
  })
  // Follow the session: switch views and keep the remote position ticking.
  let ticker: ReturnType<typeof setInterval> | null = null
  const syncTicker = () => {
    const want = isRemote() && !!useSession.getState().remote?.playing
    if (want && !ticker) {
      ticker = setInterval(() => set(viewState()), 500)
    } else if (!want && ticker) {
      clearInterval(ticker)
      ticker = null
    }
  }
  useSession.subscribe(() => {
    set(viewState())
    syncTicker()
  })
  return {
    ...engine.getState(),
    playTrackList: (tracks, startIndex, context) => {
      const ctx = context ?? contextFromPath(typeof window === 'undefined' ? '/' : window.location.pathname)
      if (isRemote()) {
        sendCommand({ action: 'playTracks', tracks, index: startIndex, context: ctx })
        return
      }
      setPlayContext(ctx)
      engine.playTrackList(tracks, startIndex)
    },
    enqueue: (t) => route({ action: 'enqueue', track: t }, () => engine.enqueue(t)),
    removeAt: (i) => route({ action: 'removeAt', index: i }, () => engine.removeAt(i)),
    moveItem: (from, to) => route({ action: 'moveItem', from, to }, () => engine.moveItem(from, to)),
    jumpTo: (index) => route({ action: 'jumpTo', index }, () => engine.playAt(index)),
    play: () => route({ action: 'play' }, () => engine.play()),
    pause: () => route({ action: 'pause' }, () => engine.pause()),
    toggle: () => route({ action: 'toggle' }, () => engine.toggle()),
    next: () => route({ action: 'next' }, () => engine.next()),
    prev: () => route({ action: 'prev' }, () => engine.prev()),
    seekMs: (ms) =>
      route({ action: 'seek', positionMs: Math.max(0, Math.round(ms)) }, () => {
        engine.seekMs(ms)
        reportNow()
      }),
    setVolume: (v) => route({ action: 'setVolume', volume: Math.min(1, Math.max(0, v)) }, () => engine.setVolume(v)),
    toggleShuffle: () => route({ action: 'toggleShuffle' }, () => engine.toggleShuffle()),
    cycleRepeat: () => route({ action: 'cycleRepeat' }, () => engine.cycleRepeat()),
    setCrossfade: (ms) => {
      engine.setCrossfade(ms)
      savePlaybackPrefs({ ...loadPlaybackPrefs(), crossfadeMs: engine.getState().crossfadeMs })
    },
  }
})
