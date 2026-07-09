import type { PlayerState } from './audioEngine'
import { trackCoverUrl } from './libraryApi'

// Minimal engine interface — the real AudioEngine satisfies this; tests supply a fake.
// Same pattern as nowPlaying.ts / playTracker.ts.
interface Enginelike {
  subscribe(cb: (s: PlayerState) => void): () => void
  play(): void
  pause(): void
  next(): void
  prev(): void
  seekMs(ms: number): void
}

const ACTIONS: MediaSessionAction[] = [
  'play',
  'pause',
  'previoustrack',
  'nexttrack',
  'seekto',
]

/**
 * startMediaSession mirrors playback state into navigator.mediaSession so OS
 * media keys, lock screens, and headphone buttons control Reverb and show
 * track metadata + artwork. No-op where the API is unavailable.
 * Returns a teardown function (unsubscribe + clear handlers/metadata).
 */
export function startMediaSession(engine: Enginelike): () => void {
  if (typeof navigator === 'undefined' || !('mediaSession' in navigator)) {
    return () => {}
  }
  const ms = navigator.mediaSession

  const handlers: Partial<Record<MediaSessionAction, MediaSessionActionHandler>> = {
    play: () => engine.play(),
    pause: () => engine.pause(),
    previoustrack: () => engine.prev(),
    nexttrack: () => engine.next(),
    seekto: (d) => {
      if (typeof d.seekTime === 'number') engine.seekMs(d.seekTime * 1000)
    },
  }
  for (const action of ACTIONS) {
    try {
      ms.setActionHandler(action, handlers[action] ?? null)
    } catch {
      // action not supported by this browser — fine, skip it
    }
  }

  let lastId = ''
  const unsub = engine.subscribe((s) => {
    if (!s.current) {
      lastId = ''
      ms.metadata = null
      ms.playbackState = 'none'
      return
    }
    if (s.current.id !== lastId) {
      lastId = s.current.id
      const artwork = trackCoverUrl(s.current, 512)
      ms.metadata = new MediaMetadata({
        title: s.current.title,
        artist: s.current.artist,
        album: s.current.album,
        artwork: artwork ? [{ src: artwork, sizes: '512x512' }] : [],
      })
    }
    ms.playbackState = s.playing ? 'playing' : 'paused'
    if (s.durationMs > 0) {
      try {
        ms.setPositionState({
          duration: s.durationMs / 1000,
          playbackRate: 1,
          position: Math.min(s.currentTimeMs, s.durationMs) / 1000,
        })
      } catch {
        // browsers throw on transiently inconsistent position state — ignore
      }
    }
  })

  return () => {
    unsub()
    for (const action of ACTIONS) {
      try {
        ms.setActionHandler(action, null)
      } catch {
        // ignore — teardown is best-effort
      }
    }
    ms.metadata = null
  }
}
