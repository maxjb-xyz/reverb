import type { AudioEngine, PlayerSnapshot, PlayerState } from './audioEngine'

// Remembers the queue, the current track and the position in localStorage so a
// reload (or reopening the tab) puts the player back where it was, paused.
// Keyed per user: two people sharing a browser never see each other's queue.

const KEY_PREFIX = 'reverb.player.v1:'
/** Queues beyond this are trimmed around the current track before saving. */
const MAX_QUEUE = 500
/** Position is saved at most this often while a track plays. */
const POSITION_INTERVAL_MS = 5000

function storage(): Storage | null {
  try {
    return typeof localStorage === 'undefined' ? null : localStorage
  } catch {
    return null // storage blocked (private mode, embedded contexts)
  }
}

function trim(snap: PlayerSnapshot): PlayerSnapshot {
  if (snap.queue.length <= MAX_QUEUE) return snap
  const start = Math.max(0, Math.min(snap.index - 50, snap.queue.length - MAX_QUEUE))
  return { ...snap, queue: snap.queue.slice(start, start + MAX_QUEUE), index: snap.index - start }
}

function isSnapshot(v: unknown): v is PlayerSnapshot {
  if (!v || typeof v !== 'object') return false
  const s = v as Partial<PlayerSnapshot>
  return (
    Array.isArray(s.queue) &&
    s.queue.every((t) => t && typeof t.id === 'string' && typeof t.title === 'string') &&
    typeof s.index === 'number' &&
    typeof s.positionMs === 'number' &&
    typeof s.volume === 'number' &&
    typeof s.shuffle === 'boolean' &&
    (s.repeat === 'off' || s.repeat === 'all' || s.repeat === 'one')
  )
}

export function loadSnapshot(userId: string): PlayerSnapshot | null {
  const raw = storage()?.getItem(KEY_PREFIX + userId)
  if (!raw) return null
  try {
    const parsed: unknown = JSON.parse(raw)
    return isSnapshot(parsed) && parsed.queue.length > 0 ? parsed : null
  } catch {
    return null
  }
}

export function saveSnapshot(userId: string, snap: PlayerSnapshot): void {
  const store = storage()
  if (!store) return
  try {
    if (snap.queue.length === 0) store.removeItem(KEY_PREFIX + userId)
    else store.setItem(KEY_PREFIX + userId, JSON.stringify(trim(snap)))
  } catch {
    // Quota exceeded or storage disabled: remembering the queue is a nicety.
  }
}

/**
 * Restore the user's last session into the engine and keep saving it from then
 * on. Returns a function that stops saving (call on sign-out / user change).
 */
export function startPlayerPersistence(engine: AudioEngine, userId: string): () => void {
  const saved = loadSnapshot(userId)
  if (saved) engine.restore(saved)

  let lastKey = ''
  let lastSavedAt = 0
  const save = () => {
    lastSavedAt = Date.now()
    saveSnapshot(userId, engine.snapshot())
  }
  const unsubscribe = engine.subscribe((s: PlayerState) => {
    // Structural changes save at once; the ticking position only every few seconds.
    const key = `${s.current?.id ?? ''}|${s.index}|${s.queue.length}|${s.playing}|${s.shuffle}|${s.repeat}|${s.volume}`
    if (key !== lastKey) {
      lastKey = key
      save()
    } else if (Date.now() - lastSavedAt >= POSITION_INTERVAL_MS) {
      save()
    }
  })
  // The last few seconds before the tab closes.
  const onHide = () => save()
  window.addEventListener('pagehide', onHide)
  return () => {
    unsubscribe()
    window.removeEventListener('pagehide', onHide)
  }
}
