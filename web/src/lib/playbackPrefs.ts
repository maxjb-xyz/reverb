import { MAX_CROSSFADE_MS } from './audioEngine'

// Playback preferences that belong to this device rather than the account,
// the way crossfade does on most players: a laptop and a phone can differ.

const KEY = 'reverb.playback.v1'

export interface PlaybackPrefs {
  /** Crossfade length in ms; 0 = off. */
  crossfadeMs: number
}

const DEFAULTS: PlaybackPrefs = { crossfadeMs: 0 }

function storage(): Storage | null {
  try {
    return typeof localStorage === 'undefined' ? null : localStorage
  } catch {
    return null // storage blocked (private mode, embedded contexts)
  }
}

export function loadPlaybackPrefs(): PlaybackPrefs {
  const raw = storage()?.getItem(KEY)
  if (!raw) return { ...DEFAULTS }
  try {
    const v = JSON.parse(raw) as Partial<PlaybackPrefs>
    const ms = typeof v.crossfadeMs === 'number' && Number.isFinite(v.crossfadeMs) ? v.crossfadeMs : 0
    return { crossfadeMs: Math.min(MAX_CROSSFADE_MS, Math.max(0, ms)) }
  } catch {
    return { ...DEFAULTS }
  }
}

export function savePlaybackPrefs(p: PlaybackPrefs): void {
  try {
    storage()?.setItem(KEY, JSON.stringify(p))
  } catch {
    // quota or blocked storage: the setting still applies for this session
  }
}
