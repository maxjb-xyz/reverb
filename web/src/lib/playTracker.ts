import type { PlayerState } from './audioEngine'
import { recordPlay, type PlayInput } from './playApi'

// Minimal interface — the real AudioEngine satisfies this; tests supply a fake.
interface Enginelike {
  subscribe(cb: (s: PlayerState) => void): () => void
}

interface TrackState {
  currentId: string
  title: string
  artist: string
  album: string
  durationMs: number
  isrc?: string
  lastTimeMs: number
  msPlayed: number
  fired: boolean
}

const QUALIFY_MIN_DURATION_MS = 30_000
const QUALIFY_THRESHOLD_MS = 240_000
const COMPLETE_WITHIN_MS = 1_500
// currentTimeMs threshold for detecting a repeat-one re-loop back to near-0.
const RELOOP_NEAR_ZERO_MS = 3_000
// Maximum single-tick forward delta to accrue. The real audio engine fires
// timeupdate ~4×/s so genuine listening produces deltas of ~250 ms; a delta
// at or above this cap is a FORWARD SEEK (the user skipped ahead), and the
// skipped span was never listened to, so it must NOT count toward msPlayed.
// This is orthogonal to the backward-seek guard (which prevents *double*-
// counting replayed seconds): the forward cap prevents counting *skipped*
// seconds as listened ones.
const MAX_DELTA_MS = 5_000

function qualify(state: TrackState): boolean {
  const { durationMs, msPlayed } = state
  if (durationMs <= QUALIFY_MIN_DURATION_MS) return false
  return msPlayed >= durationMs / 2 || msPlayed >= QUALIFY_THRESHOLD_MS
}

/**
 * startPlayTracker subscribes to the given engine and calls `recordFn` once per
 * qualified play event.  Returns an unsubscribe function.
 *
 * Qualification rules:
 *  - Track must have durationMs > 30 000 ms.
 *  - msPlayed must reach durationMs/2 OR 240 000 ms — whichever comes first.
 *  - Only genuine forward play time accrues: 0 < delta < 5 000 ms while playing.
 *    A backward seek (delta < 0) resets lastTimeMs without accruing — prevents
 *    DOUBLE-counting replayed seconds. A large forward jump (delta >= 5 000 ms)
 *    is a forward SEEK and is NOT accrued — prevents counting SKIPPED seconds as
 *    listened ones. These two guards are orthogonal.
 *  - Repeat-one re-loop (backward jump to near-0 on the SAME track after fire)
 *    resets the per-play counters so a second qualification can fire.
 *  - completed = currentTimeMs >= durationMs - 1 500 at fire time.
 */
export function startPlayTracker(
  engine: Enginelike,
  recordFn: (input: PlayInput) => Promise<void> = recordPlay,
): () => void {
  let track: TrackState | null = null

  function handleState(s: PlayerState): void {
    const { current, playing, currentTimeMs, durationMs } = s

    // ── No current track ──────────────────────────────────────────────────
    if (!current) {
      track = null
      return
    }

    // ── Track changed ─────────────────────────────────────────────────────
    if (!track || track.currentId !== current.id) {
      // Previous track: already fired or didn't qualify — discard; start fresh.
      // Seed durationMs from the Track metadata (set at load time) so we don't
      // have to wait for the engine's durationchange to settle; fall back to the
      // engine-state durationMs if the Track's is missing.
      track = {
        currentId: current.id,
        title: current.title,
        artist: current.artist,
        album: current.album,
        durationMs: current.durationMs > 0 ? current.durationMs : durationMs,
        isrc: current.isrc,
        lastTimeMs: currentTimeMs,
        msPlayed: 0,
        fired: false,
      }
      return
    }

    // ── Same track ────────────────────────────────────────────────────────

    // Update settled durationMs (the engine may emit 0 initially then settle).
    // Prefer a non-zero engine-state value; otherwise keep the Track-seeded one.
    if (durationMs > 0) track.durationMs = durationMs

    const delta = currentTimeMs - track.lastTimeMs

    // Backward jump: seek detected.
    if (delta < 0) {
      // Repeat-one re-loop: if already fired AND we're back near the start,
      // reset so this loop iteration can qualify as a new play.
      if (track.fired && currentTimeMs <= RELOOP_NEAR_ZERO_MS) {
        track.msPlayed = 0
        track.fired = false
      }
      // In all backward-jump cases: reset lastTimeMs, accrue nothing.
      track.lastTimeMs = currentTimeMs
      return
    }

    // Forward delta — accrue only genuine playback: positive, while playing, and
    // below the seek cap. A delta >= MAX_DELTA_MS is a forward seek (skipped
    // time) and is NOT accrued (lastTimeMs still advances below, so subsequent
    // ticks resume correctly from the new position).
    if (playing && delta > 0 && delta < MAX_DELTA_MS) {
      track.msPlayed += delta
    }

    track.lastTimeMs = currentTimeMs

    // ── Check qualification ───────────────────────────────────────────────
    if (!track.fired && track.durationMs > 0 && qualify(track)) {
      track.fired = true
      const completed = currentTimeMs >= track.durationMs - COMPLETE_WITHIN_MS

      void recordFn({
        libraryTrackId: track.currentId,
        title: track.title,
        artist: track.artist,
        album: track.album,
        durationMs: track.durationMs,
        ...(track.isrc ? { isrc: track.isrc } : {}),
        msPlayed: track.msPlayed,
        completed,
      })
    }
  }

  return engine.subscribe(handleState)
}
