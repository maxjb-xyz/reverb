// Radio: a queue the recommendation engine keeps filling.
//
// startRadio asks the engine for a first batch and plays it with the play
// context { kind: 'radio', id: station.id }, so listens (and skips) count
// against the station. startRadioRefill watches the player: while that context
// is still current and the queue is about to run out, it asks for more,
// excluding everything already queued. Playing anything else ends the station.

import type { AudioEngine, PlayerState } from './audioEngine'
import { getNext, type EngineItem, type EngineNextRequest, type EngineReason, type EngineSeed, type EngineStation, type Lane } from './engineApi'
import { getPlayContext, type PlayContext } from './playContext'
import type { Track } from './types'

/** Ask for more when this many songs (or fewer) are left after the current one. */
export const REFILL_AT = 3
const FIRST_BATCH = 20
const REFILL_BATCH = 15
const MAX_EXCLUDE = 500

interface Active {
  station: EngineStation
  req: EngineNextRequest
  fetching: boolean
  exhausted: boolean
}

let active: Active | null = null
const reasons = new Map<string, EngineReason>()

function remember(items: EngineItem[]): Track[] {
  const out: Track[] = []
  for (const it of items) {
    if (!it.owned || !it.track) continue
    reasons.set(it.track.id, it.reason)
    out.push(it.track)
  }
  return out
}

/** The station the player is on, or null. */
export function currentStation(): EngineStation | null {
  return active?.station ?? null
}

/** Why the engine picked a queued song (undefined for songs it didn't pick). */
export function reasonFor(trackId: string): EngineReason | undefined {
  return reasons.get(trackId)
}

export interface StartRadioOptions {
  lane?: Lane
  fromYear?: number
  toYear?: number
}

export class EmptyStationError extends Error {
  constructor() {
    super('Nothing to play on this station yet')
  }
}

/**
 * Start a station and play its first batch. `play` is the player's
 * playTrackList (injected so this module doesn't import the store).
 */
export async function startRadio(
  seed: EngineSeed,
  play: (tracks: Track[], startIndex: number, context?: PlayContext) => void,
  opts: StartRadioOptions = {},
  fetchFn: typeof getNext = getNext,
): Promise<EngineStation> {
  const req: EngineNextRequest = { seed, ...opts }
  const res = await fetchFn({ ...req, n: FIRST_BATCH })
  reasons.clear()
  const tracks = remember(res.items)
  if (tracks.length === 0) throw new EmptyStationError()
  active = { station: res.station, req, fetching: false, exhausted: false }
  play(tracks, 0, { kind: 'radio', id: res.station.id })
  return res.station
}

/** Stop refilling (the queue stays as it is). */
export function stopRadio(): void {
  active = null
}

/**
 * Keep radio queues topped up. Returns an unsubscribe. `engine` is the audio
 * engine; `getContext` and `fetchFn` are injectable for tests.
 */
export function startRadioRefill(
  engine: Pick<AudioEngine, 'subscribe' | 'getState' | 'enqueue'>,
  fetchFn: typeof getNext = getNext,
  getContext: () => PlayContext = getPlayContext,
): () => void {
  const check = (s: PlayerState) => {
    const a = active
    if (!a) return
    const ctx = getContext()
    if (ctx.kind !== 'radio' || ctx.id !== a.station.id) {
      // The user played something else: the station is over.
      active = null
      return
    }
    if (a.fetching || a.exhausted || s.queue.length === 0) return
    const left = s.queue.length - 1 - s.index
    if (left > REFILL_AT) return
    a.fetching = true
    const exclude = s.queue.slice(-MAX_EXCLUDE).map((t) => t.id)
    fetchFn({ ...a.req, exclude, n: REFILL_BATCH })
      .then((res) => {
        if (active !== a) return // station changed while we were waiting
        const queued = new Set(engine.getState().queue.map((t) => t.id))
        const fresh = remember(res.items).filter((t) => !queued.has(t.id))
        if (fresh.length === 0) a.exhausted = true
        for (const t of fresh) engine.enqueue(t)
      })
      .catch(() => {
        // Try again on the next state change, but not in a tight loop.
        setTimeout(() => {
          if (active === a) a.fetching = false
        }, 10_000)
        return 'retry'
      })
      .then((r) => {
        if (r !== 'retry') a.fetching = false
      })
  }
  return engine.subscribe(check)
}

/** Test hook: forget all station state. */
export function _resetRadio(): void {
  active = null
  reasons.clear()
}
