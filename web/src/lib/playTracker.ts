import type { PlayerState } from './audioEngine'
import { recordPlay, type PlayInput, type PlayProgress } from './playApi'

// Minimal interface — the real AudioEngine satisfies this; tests supply a fake.
interface Enginelike {
  subscribe(cb: (s: PlayerState) => void): () => void
}

/** Records a qualified play; resolves to the new play's id when the server gives one. */
export type RecordFn = (input: PlayInput) => Promise<string | null | void>
/** Reports the final listened time of a recorded play. */
export type UpdateFn = (id: string, progress: PlayProgress, keepalive: boolean) => Promise<void>

export interface PlayTrackerOptions {
  /** Sends the final listened time once the play ends. Omit to only record plays. */
  update?: UpdateFn
  /** Wall clock in ms. Injected by tests. */
  now?: () => number
  /** Where 'pagehide' is heard. Defaults to window. */
  page?: EventTarget | null
}

interface TrackState {
  currentId: string
  title: string
  artist: string
  album: string
  durationMs: number
  isrc?: string
  lastTimeMs: number
  lastWallMs: number
  playing: boolean
  msPlayed: number
  reachedEnd: boolean
  fired: boolean
  // Set once the play is recorded: the id (null if the server sent none), and
  // what the server already knows, so updates go out only when there is news.
  playId: string | null | undefined
  idReady: Promise<string | null> | null
  reportedMs: number
  reportedCompleted: boolean
}

const QUALIFY_MIN_DURATION_MS = 30_000
const QUALIFY_THRESHOLD_MS = 240_000
const COMPLETE_WITHIN_MS = 1_500
// currentTimeMs threshold for detecting a repeat-one re-loop back to near-0.
const RELOOP_NEAR_ZERO_MS = 3_000
// A forward delta below this always counts as listening. The audio engine fires
// timeupdate ~4×/s, so genuine listening produces deltas of ~250 ms.
const MAX_DELTA_MS = 5_000
// A larger forward delta still counts when the wall clock moved at least as
// far (plus this slack). Background tabs and locked phones deliver progress
// seconds apart; a FORWARD SEEK moves the song much further than the clock.
const CLOCK_SLACK_MS = 1_500
// Don't send an update for less than this much new listening.
const MIN_UPDATE_MS = 1_000

function qualify(state: TrackState): boolean {
  const { durationMs, msPlayed } = state
  if (durationMs <= QUALIFY_MIN_DURATION_MS) return false
  return msPlayed >= durationMs / 2 || msPlayed >= QUALIFY_THRESHOLD_MS
}

/**
 * startPlayTracker subscribes to the given engine and calls `recordFn` once per
 * qualified play event, then `options.update` with the final listened time.
 * Returns an unsubscribe function.
 *
 * Qualification rules:
 *  - Track must have durationMs > 30 000 ms.
 *  - msPlayed must reach durationMs/2 OR 240 000 ms — whichever comes first.
 *  - Only genuine forward play time accrues while playing: a forward delta under
 *    5 000 ms, or one no bigger than the wall-clock time that passed (throttled
 *    background updates). A backward seek resets lastTimeMs without accruing —
 *    prevents DOUBLE-counting replayed seconds. A forward jump bigger than the
 *    time that passed is a SEEK and is NOT accrued — prevents counting SKIPPED
 *    seconds as listened ones.
 *  - Repeat-one re-loop (backward jump to near-0 on the SAME track after fire)
 *    closes out the first play and resets so a second qualification can fire.
 *  - completed = currentTimeMs >= durationMs - 1 500 at fire time; the final
 *    update marks it completed if listening reached that close to the end
 *    (widened by the crossfade length, when crossfade is on).
 *
 * The play is recorded as soon as it qualifies (so it is scrobbled and shows up
 * in stats right away), when only half the song may have been heard. When it
 * ends — the song changes, the queue stops, playback pauses or the page closes —
 * the listened time and whether it was heard to the end are sent as an update.
 */
export function startPlayTracker(
  engine: Enginelike,
  recordFn: RecordFn = recordPlay,
  options: PlayTrackerOptions = {},
): () => void {
  const update = options.update
  const now = options.now ?? (() => Date.now())
  const page = options.page !== undefined ? options.page : typeof window !== 'undefined' ? window : null
  let track: TrackState | null = null

  function freshState(s: PlayerState): TrackState {
    const current = s.current!
    return {
      currentId: current.id,
      title: current.title,
      artist: current.artist,
      album: current.album,
      // Seed durationMs from the Track metadata (set at load time) so we don't
      // have to wait for the engine's durationchange to settle; fall back to the
      // engine-state durationMs if the Track's is missing.
      durationMs: current.durationMs > 0 ? current.durationMs : s.durationMs,
      isrc: current.isrc,
      lastTimeMs: s.currentTimeMs,
      lastWallMs: now(),
      playing: s.playing,
      msPlayed: 0,
      reachedEnd: false,
      fired: false,
      playId: undefined,
      idReady: null,
      reportedMs: 0,
      reportedCompleted: false,
    }
  }

  /** Sends the play's final listened time if it grew since the last report. */
  function flush(t: TrackState, keepalive: boolean): void {
    if (!update || !t.fired) return
    const progress: PlayProgress = { msPlayed: t.msPlayed, completed: t.reachedEnd }
    if (progress.msPlayed < t.reportedMs + MIN_UPDATE_MS && progress.completed === t.reportedCompleted) return
    t.reportedMs = progress.msPlayed
    t.reportedCompleted = progress.completed
    const send = (id: string | null | undefined) => {
      if (id) void update(id, progress, keepalive).catch(() => {})
    }
    // The id is usually known by now; if the record request is still in
    // flight, send once it lands.
    if (t.playId !== undefined) send(t.playId)
    else void t.idReady?.then(send)
  }

  function record(t: TrackState, completed: boolean): void {
    t.fired = true
    t.reportedMs = t.msPlayed
    t.reportedCompleted = completed
    t.idReady = Promise.resolve(
      recordFn({
        libraryTrackId: t.currentId,
        title: t.title,
        artist: t.artist,
        album: t.album,
        durationMs: t.durationMs,
        ...(t.isrc ? { isrc: t.isrc } : {}),
        msPlayed: t.msPlayed,
        completed,
      }),
    )
      .then((id) => (typeof id === 'string' && id ? id : null))
      .catch(() => null)
      .then((id) => {
        t.playId = id
        return id
      })
  }

  function handleState(s: PlayerState): void {
    const { current, playing, currentTimeMs, durationMs } = s

    // ── No current track ──────────────────────────────────────────────────
    if (!current) {
      if (track) flush(track, false)
      track = null
      return
    }

    // ── Track changed ─────────────────────────────────────────────────────
    if (!track || track.currentId !== current.id) {
      // Previous track: close out its play (if it qualified); start fresh.
      if (track) flush(track, false)
      track = freshState(s)
      return
    }

    // ── Same track ────────────────────────────────────────────────────────
    const t = track
    const wallNow = now()
    const wallDelta = wallNow - t.lastWallMs
    t.lastWallMs = wallNow

    // Update settled durationMs (the engine may emit 0 initially then settle).
    // Prefer a non-zero engine-state value; otherwise keep the Track-seeded one.
    if (durationMs > 0) t.durationMs = durationMs

    const delta = currentTimeMs - t.lastTimeMs

    // Backward jump: seek detected.
    if (delta < 0) {
      // Repeat-one re-loop: if already fired AND we're back near the start,
      // close out that play and reset so this loop can qualify as a new one.
      if (t.fired && currentTimeMs <= RELOOP_NEAR_ZERO_MS) {
        flush(t, false)
        track = freshState(s)
        return
      }
      // In all backward-jump cases: reset lastTimeMs, accrue nothing.
      t.lastTimeMs = currentTimeMs
      t.playing = playing
      return
    }

    // Forward delta — accrue only genuine playback (see the doc comment). A
    // seek is NOT accrued (lastTimeMs still advances below, so subsequent ticks
    // resume correctly from the new position).
    const listened = playing && delta > 0 && (delta < MAX_DELTA_MS || delta <= wallDelta + CLOCK_SLACK_MS)
    if (listened) {
      t.msPlayed += delta
      // With crossfade on, the next song takes over crossfadeMs before the end.
      const endWindow = COMPLETE_WITHIN_MS + Math.max(0, s.crossfadeMs || 0)
      if (t.durationMs > 0 && currentTimeMs >= t.durationMs - endWindow) t.reachedEnd = true
    }

    t.lastTimeMs = currentTimeMs

    // ── Check qualification ───────────────────────────────────────────────
    if (!t.fired && t.durationMs > 0 && qualify(t)) {
      record(t, currentTimeMs >= t.durationMs - COMPLETE_WITHIN_MS)
    }

    // Playback stopped (pause, or the queue ran out): report what was heard.
    if (t.playing && !playing) flush(t, false)
    t.playing = playing
  }

  const onPageHide = () => {
    if (track) flush(track, true)
  }
  page?.addEventListener('pagehide', onPageHide)
  const unsubscribe = engine.subscribe(handleState)

  return () => {
    unsubscribe()
    page?.removeEventListener('pagehide', onPageHide)
    if (track) flush(track, false)
  }
}
