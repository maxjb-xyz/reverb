import type { PlayerState } from './audioEngine'
import { getPlayContext, type PlayContext } from './playContext'
import { recordListen, type ListenInput, type ListenOutcome } from './tasteApi'

// Minimal interface — the real AudioEngine satisfies this; tests supply a fake.
interface Enginelike {
  subscribe(cb: (s: PlayerState) => void): () => void
}

type RecordFn = (input: ListenInput, opts?: { keepalive?: boolean }) => Promise<void>

interface Current {
  id: string
  title: string
  artist: string
  album: string
  isrc?: string
  durationMs: number
  startedAt: number
  ctx: PlayContext
  lastTimeMs: number
  lastWallMs: number
  msPlayed: number
  heard: boolean // playback actually ran (a restored-but-never-played track is not a listen)
}

export const EARLY_SKIP_MS = 30_000
// Ending within this much of the track counts as finishing it.
const END_WITHIN_MS = 3_000
// A media-time advance counts as listening only if real time kept pace with it
// (with slack for timer jitter and up to 2x playback). Comparing against the
// wall clock rather than a fixed cap keeps listening that happens while the
// browser throttles timeupdate (background tab, phone screen off), while a
// seek — media jumping ahead faster than time passed — still accrues nothing.
const WALL_SLACK_MS = 1_500
const MAX_RATE = 2
const RELOOP_NEAR_ZERO_MS = 3_000

function outcomeFor(c: Current): ListenOutcome {
  if (c.durationMs > 0 && c.lastTimeMs >= c.durationMs - END_WITHIN_MS) return 'completed'
  return c.msPlayed < EARLY_SKIP_MS ? 'skip_early' : 'skip_late'
}

/**
 * startListenTracker reports how every started track ended — finished, skipped
 * early (<30 s), skipped later, or stopped (the tab closed mid-song) — with the
 * context it was played from. These are taste signals; scrobble-qualified plays
 * are still reported separately by playTracker.
 *
 * Returns a function that unsubscribes and removes the page-hide listener.
 */
export function startListenTracker(
  engine: Enginelike,
  recordFn: RecordFn = recordListen,
  getContext: () => PlayContext = getPlayContext,
  win: Pick<Window, 'addEventListener' | 'removeEventListener'> | null = typeof window === 'undefined' ? null : window,
  now: () => number = Date.now,
): () => void {
  let cur: Current | null = null

  function emit(c: Current, outcome: ListenOutcome, keepalive = false) {
    if (!c.heard) return
    void recordFn(
      {
        libraryTrackId: c.id,
        title: c.title,
        artist: c.artist,
        album: c.album,
        ...(c.isrc ? { isrc: c.isrc } : {}),
        durationMs: c.durationMs,
        msPlayed: Math.round(c.msPlayed),
        outcome,
        contextKind: c.ctx.kind,
        ...(c.ctx.id ? { contextId: c.ctx.id } : {}),
        startedAt: c.startedAt,
      },
      keepalive ? { keepalive: true } : undefined,
    ).catch(() => {
      /* taste signals are best-effort */
    })
  }

  function begin(s: PlayerState): Current | null {
    const t = s.current
    if (!t) return null
    return {
      id: t.id,
      title: t.title,
      artist: t.artist,
      album: t.album,
      isrc: t.isrc,
      durationMs: t.durationMs > 0 ? t.durationMs : s.durationMs,
      startedAt: Math.floor(now() / 1000),
      ctx: getContext(),
      lastTimeMs: s.currentTimeMs,
      lastWallMs: now(),
      msPlayed: 0,
      heard: false,
    }
  }

  function onState(s: PlayerState) {
    if (!s.current) {
      if (cur) emit(cur, outcomeFor(cur))
      cur = null
      return
    }
    if (!cur || cur.id !== s.current.id) {
      if (cur) emit(cur, outcomeFor(cur))
      cur = begin(s)
      return
    }
    if (s.durationMs > 0) cur.durationMs = s.durationMs
    const delta = s.currentTimeMs - cur.lastTimeMs
    const wall = now()
    const wallDelta = wall - cur.lastWallMs
    cur.lastWallMs = wall
    if (delta < 0) {
      // Repeat-one loop: the track ran to its end and started over.
      if (cur.lastTimeMs >= cur.durationMs - END_WITHIN_MS && s.currentTimeMs <= RELOOP_NEAR_ZERO_MS) {
        emit(cur, 'completed')
        cur = begin(s)
        if (cur) cur.heard = s.playing
        return
      }
      cur.lastTimeMs = s.currentTimeMs
      return
    }
    if (s.playing) {
      cur.heard = true
      if (delta > 0 && delta <= wallDelta * MAX_RATE + WALL_SLACK_MS) cur.msPlayed += delta
    }
    cur.lastTimeMs = s.currentTimeMs
  }

  // Closing the tab mid-song: neither a finish nor a skip.
  function onPageHide() {
    if (cur) emit(cur, cur.durationMs > 0 && cur.lastTimeMs >= cur.durationMs - END_WITHIN_MS ? 'completed' : 'stopped', true)
    cur = null
  }

  const unsub = engine.subscribe(onState)
  win?.addEventListener('pagehide', onPageHide)
  return () => {
    unsub()
    win?.removeEventListener('pagehide', onPageHide)
  }
}
