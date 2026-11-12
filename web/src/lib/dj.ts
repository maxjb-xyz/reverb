// The AI DJ: short blocks of songs from the engine's lanes, with a line from
// the DJ at the start of each block.
//
// startDJ asks the engine for a first block (4 songs from one lane: on repeat,
// sounds like, new to you...) and plays it with the context { kind: 'dj' }.
// Each song also carries its lane as a finer context, so a skip counts against
// the lane that picked it. startDJWatcher follows the player: when a block's
// first song starts it shows the DJ's line and speaks it over the intro (the
// music is ducked while it talks), and when the queue is about to run out it
// fetches the next lane's block. Playing anything else ends the DJ.
//
// The engine picks the songs; the line writer only puts the engine's reasons
// into words. A line is prepared as soon as its block is queued, so the voice
// is usually ready before the song starts.

import { create } from 'zustand'
import type { AudioEngine, PlayerState } from './audioEngine'
import { getNext, type EngineItem, type EngineNextRequest, type EngineResult, type EngineStation, type Lane } from './engineApi'
import { getDJStatus, readRequest, sayLine, writeLine, type DJClip, type DJIntent, type DJLine, type DJSegment, type DJStatus, type DJTrack } from './djApi'
import { getPlayContext, setTrackContexts, type PlayContext } from './playContext'
import type { Track } from './types'
import { ApiError } from './api'

/** The order the DJ moves through lanes; empty lanes are skipped. */
export const DJ_LANES: Lane[] = ['on-repeat', 'sounds-like', 'new-to-you', 'throwback', 'deep-cuts', 'fresh-finds']
/** Songs per block: one DJ break every few songs. */
export const BLOCK_SIZE = 4
/** Fetch the next block when this many songs (or fewer) are left after the current one. */
export const REFILL_AT = 1
/** The DJ only talks over the opening of a song; after this the moment has passed. */
export const TALK_WINDOW_MS = 20_000
/** How long a line stays on screen when there is no voice. */
export const CARD_MS = 12_000
/** How long a spoken line stays on screen after the voice ends. */
const CARD_AFTER_VOICE_MS = 4_000
/** How many blocks a request steers before the DJ goes back to its lanes. */
export const REQUEST_BLOCKS = 2
/** Wait this long before trying again after every lane came back empty or failed. */
const RETRY_MS = 30_000
const MAX_EXCLUDE = 500

export const DJ_CONTEXT: PlayContext = { kind: 'dj', id: 'dj' }

export class DJEmptyError extends Error {
  constructor() {
    super('Not enough in the library for the DJ yet')
  }
}

export interface DJDeps {
  next: (r: EngineNextRequest) => Promise<EngineResult>
  line: (s: DJSegment) => Promise<DJLine>
  say: (text: string) => Promise<DJClip>
  status: () => Promise<DJStatus>
  intent: (text: string) => Promise<DJIntent>
  /** The player's playTrackList. */
  play: (tracks: Track[], startIndex: number, context?: PlayContext) => void
  /** The listener's name, for the greeting. */
  listener: () => string | undefined
  now: () => Date
  random: () => number
}

const defaults: Omit<DJDeps, 'play'> = {
  next: getNext,
  line: writeLine,
  say: sayLine,
  status: getDJStatus,
  intent: readRequest,
  listener: () => undefined,
  now: () => new Date(),
  random: Math.random,
}

// ---- What the UI shows ----

export interface DJCard {
  key: number
  label: string
  text: string
}

export interface DJView {
  active: boolean
  /** Starting, or changing lanes. */
  busy: boolean
  /** The lane playing now ("On repeat", "Sounds like Bonobo"). */
  label: string
  /** The DJ's latest line, while it is on screen. */
  card: DJCard | null
  /** Whether a voice is configured (otherwise lines are shown, not spoken). */
  voice: boolean
  /** An artist the listener asked for that the library lacks: offer to find it. */
  missing: string | null
}

const idle: DJView = { active: false, busy: false, label: '', card: null, voice: false, missing: null }
export const useDJ = create<DJView>(() => ({ ...idle }))

// ---- Session ----

interface Prepared {
  text: string
  url?: string
}

interface Block {
  lane: Lane | 'request'
  /** What the bar shows while the block plays. */
  label: string
  station: EngineStation
  tracks: Track[]
  prepared: Promise<Prepared>
  announced: boolean
}

interface Session {
  deps: DJDeps
  voice: boolean
  /** Index into DJ_LANES of the next lane to try. */
  lane: number
  /** Blocks by the id of their first song. */
  blocks: Map<string, Block>
  /** Every song the DJ has queued, so lanes don't repeat each other. */
  handed: string[]
  fetching: boolean
  /** A listener request steering the next blocks. */
  request?: ActiveRequest
}

interface ActiveRequest {
  req: EngineNextRequest
  text: string
  /** How the request shows in the DJ's controls. */
  label: string
  left: number
}

let session: Session | null = null
let generation = 0
let bound: Pick<AudioEngine, 'getState' | 'enqueue' | 'speak'> | null = null
let cardSeq = 0
let cardTimer: ReturnType<typeof setTimeout> | null = null

function laneContext(b: { station: EngineStation }): PlayContext {
  return { kind: 'dj', id: b.station.id }
}

function toDJ(t: Track, year?: number): DJTrack {
  return { title: t.title, artist: t.artist, album: t.album || undefined, year: year || undefined }
}

/** A throwback period: a five-year window that ended 5 to 20 years ago. */
export function throwbackYears(now: Date, r: number): { fromYear: number; toYear: number } {
  const toYear = now.getFullYear() - 5 - Math.floor(Math.min(Math.max(r, 0), 0.999) * 16)
  return { fromYear: toYear - 4, toYear }
}

/** Said when the line writer can't be reached at all. */
function plainLine(b: { label: string; tracks: Track[] }): string {
  const t = b.tracks[0]
  return `${b.label}. Starting with ${t.title} by ${t.artist}.`
}

function segmentFor(s: Session, lane: Lane | 'request', items: EngineItem[], previous?: Track, request?: string): DJSegment {
  // seedTitle comes with the engine's optional audio analysis ("sounds like <song>").
  const r: EngineItem['reason'] & { seedTitle?: string } = items[0].reason
  return {
    lane,
    request,
    reason: { artist: r.artist, play_count: r.playCount, seed: r.seedTitle ?? r.seedArtist },
    previous: previous ? toDJ(previous) : undefined,
    next: items.slice(0, 5).map((it) => toDJ(it.track as Track, it.year)),
    listener: s.deps.listener(),
    hour: s.deps.now().getHours(),
  }
}

async function prepare(s: Session, seg: DJSegment, b: { label: string; tracks: Track[] }): Promise<Prepared> {
  let text: string
  try {
    text = (await s.deps.line(seg)).text
  } catch {
    text = plainLine(b)
  }
  if (!s.voice) return { text }
  try {
    const clip = await s.deps.say(text)
    return { text, url: clip.url }
  } catch {
    // Warming up, rate limited or failing: show the line instead of saying it.
    return { text }
  }
}

/** Turn an engine answer into a block, or null when nothing in it can play. */
function makeBlock(
  s: Session,
  lane: Lane | 'request',
  label: string | null,
  res: EngineResult,
  previous: Track | undefined,
  skip: Set<string>,
  request?: string,
): Block | null {
  const items = res.items.filter((it) => it.owned && it.track && !skip.has(it.track.id))
  if (items.length === 0) return null
  const tracks = items.map((it) => it.track as Track)
  const shown = label ?? res.station.label
  const block: Block = {
    lane,
    label: shown,
    station: res.station,
    tracks,
    prepared: prepare(s, segmentFor(s, lane, items, previous, request), { label: shown, tracks }),
    announced: false,
  }
  s.blocks.set(tracks[0].id, block)
  for (const t of tracks) s.handed.push(t.id)
  if (s.handed.length > MAX_EXCLUDE * 2) s.handed = s.handed.slice(-MAX_EXCLUDE)
  return block
}

/** The next block for the active request, or null (and the request ends) when it has run out. */
async function requestBlock(s: Session, previous: Track | undefined, skip: Set<string>): Promise<Block | null> {
  const r = s.request
  if (!r || r.left <= 0) {
    s.request = undefined
    return null
  }
  r.left--
  let b: Block | null
  try {
    const exclude = s.handed.slice(-MAX_EXCLUDE)
    b = makeBlock(s, 'request', r.label, await s.deps.next({ ...r.req, n: BLOCK_SIZE, exclude }), previous, skip, r.text)
    if (!b && r.req.seed && r.req.lane) {
      // "More like X" with nothing similar on hand: play X itself.
      r.req = { ...r.req, lane: undefined }
      b = makeBlock(s, 'request', r.label, await s.deps.next({ ...r.req, n: BLOCK_SIZE, exclude }), previous, skip, r.text)
    }
  } catch {
    s.request = undefined
    return null
  }
  if (!b || r.left <= 0) s.request = undefined
  return b
}

/**
 * How a request shows in the DJ's controls: what the DJ understood ("A$AP
 * Rocky", "More like Bonobo", "Hip hop, 1990s"), or the request itself in
 * quotes when it was a lane or a period alone.
 */
export function requestLabel(text: string, it?: DJIntent): string {
  const name = it?.seed?.label || it?.seed?.artist
  if (name) {
    const base = it?.lane === 'sounds-like' ? `More like ${name}` : name
    if (!it?.fromYear || !it.toYear) return base
    const decade = it.fromYear % 10 === 0 && it.toYear - it.fromYear === 9
    return `${base}, ${decade ? `${it.fromYear}s` : `${it.fromYear}–${it.toYear}`}`
  }
  const t = text.trim().replace(/\s+/g, ' ')
  return `“${t.length > 40 ? t.slice(0, 39) + '…' : t}”`
}

/**
 * The next block: the active request's, else the next lane's, trying each
 * lane once. Songs in `skip` (already queued) are left out. Null when every
 * lane is empty or failed.
 */
async function nextBlock(s: Session, previous: Track | undefined, skip: Set<string>): Promise<Block | null> {
  if (s.request) {
    const b = await requestBlock(s, previous, skip)
    if (b) return b
  }
  for (let tries = 0; tries < DJ_LANES.length; tries++) {
    const lane = DJ_LANES[s.lane % DJ_LANES.length]
    s.lane = (s.lane + 1) % DJ_LANES.length
    const req: EngineNextRequest = { lane, n: BLOCK_SIZE, exclude: s.handed.slice(-MAX_EXCLUDE) }
    if (lane === 'throwback') Object.assign(req, throwbackYears(s.deps.now(), s.deps.random()))
    let res: EngineResult
    try {
      res = await s.deps.next(req)
    } catch {
      continue
    }
    const b = makeBlock(s, lane, null, res, previous, skip)
    if (b) return b
  }
  return null
}

function showCard(label: string, text: string, hideAfterMs?: number): number {
  const key = ++cardSeq
  if (cardTimer) clearTimeout(cardTimer)
  cardTimer = null
  useDJ.setState({ card: { key, label, text } })
  if (hideAfterMs !== undefined) hideCard(key, hideAfterMs)
  return key
}

function hideCard(key: number, afterMs: number) {
  if (cardTimer) clearTimeout(cardTimer)
  cardTimer = setTimeout(() => {
    cardTimer = null
    if (useDJ.getState().card?.key === key) useDJ.setState({ card: null })
  }, afterMs)
}

async function announce(s: Session, b: Block, trackId: string) {
  useDJ.setState({ label: b.label })
  const p = await b.prepared
  const eng = bound
  if (session !== s || !eng) return
  const st = eng.getState()
  // The listener moved on, or the line took too long: the moment has passed.
  if (st.current?.id !== trackId || st.currentTimeMs > TALK_WINDOW_MS) return
  if (p.url && st.playing) {
    const key = showCard(b.label, p.text)
    await eng.speak(p.url)
    hideCard(key, CARD_AFTER_VOICE_MS)
  } else {
    showCard(b.label, p.text, CARD_MS)
  }
}

function refill(s: Session, st: PlayerState) {
  s.fetching = true
  const previous = st.queue[st.queue.length - 1]
  const skip = new Set(st.queue.map((t) => t.id))
  nextBlock(s, previous, skip)
    .then((b) => {
      const eng = bound
      if (session !== s || !eng) return
      if (!b) {
        // Nothing anywhere right now: let the queue play out and look again later.
        setTimeout(() => {
          if (session === s) s.fetching = false
        }, RETRY_MS)
        return
      }
      setTrackContexts(b.tracks.map((t) => [t.id, laneContext(b)]))
      for (const t of b.tracks) eng.enqueue(t)
      s.fetching = false
    })
    .catch(() => {
      if (session === s) s.fetching = false
    })
}

function onState(st: PlayerState, getContext: () => PlayContext) {
  const s = session
  if (!s) return
  if (getContext().kind !== DJ_CONTEXT.kind) {
    // The listener played something else: the DJ is done.
    stopDJ()
    return
  }
  const cur = st.current
  if (cur) {
    const b = s.blocks.get(cur.id)
    if (b && !b.announced) {
      b.announced = true
      void announce(s, b, cur.id)
    }
  }
  if (s.fetching || st.queue.length === 0) return
  if (st.queue.length - 1 - st.index > REFILL_AT) return
  refill(s, st)
}

// ---- Public API ----

/** Whether the DJ is on. */
export function djActive(): boolean {
  return session !== null
}

/**
 * Start the DJ: play the first lane that has something, and let the watcher
 * take it from there. Throws DJEmptyError when no lane has a song to play.
 */
export async function startDJ(deps: Partial<DJDeps> & Pick<DJDeps, 'play'>): Promise<EngineStation> {
  stopDJ()
  const gen = generation
  const d: DJDeps = { ...defaults, ...deps }
  useDJ.setState({ busy: true })
  try {
    const status = await d.status().catch(() => null)
    const s: Session = { deps: d, voice: !!status?.voice.available, lane: 0, blocks: new Map(), handed: [], fetching: true }
    const b = await nextBlock(s, undefined, new Set())
    if (gen !== generation) throw new DJEmptyError() // stopped or restarted meanwhile
    if (!b) throw new DJEmptyError()
    session = s
    s.fetching = false
    setTrackContexts(b.tracks.map((t) => [t.id, laneContext(b)]), true)
    useDJ.setState({ active: true, label: b.label, voice: s.voice })
    d.play(b.tracks, 0, DJ_CONTEXT)
    return b.station
  } finally {
    useDJ.setState({ busy: false })
  }
}

/**
 * Move to the next lane now: its block replaces what is queued and the DJ
 * introduces it. False when no other lane has anything.
 */
export async function changeVibe(): Promise<boolean> {
  const s = session
  if (!s || s.fetching) return false
  s.fetching = true
  s.request = undefined
  useDJ.setState({ busy: true, missing: null })
  try {
    const cur = bound?.getState().current ?? undefined
    const b = await nextBlock(s, cur, new Set(cur ? [cur.id] : []))
    if (session !== s || !b) return false
    playNow(s, b)
    return true
  } finally {
    if (session === s) s.fetching = false
    useDJ.setState({ busy: false })
  }
}

function playNow(s: Session, b: Block) {
  setTrackContexts(b.tracks.map((t) => [t.id, laneContext(b)]))
  useDJ.setState({ label: b.label })
  s.deps.play(b.tracks, 0, DJ_CONTEXT)
}

export type AskResult =
  /** Playing what was asked for. `missing` names an artist it couldn't include. */
  | { status: 'playing'; missing?: string }
  /** Nothing in the request could be read as music in the library. `try` names things that would work. */
  | { status: 'not-understood'; missing?: string; try?: string[] }
  /** Understood, but nothing in the library fits it. */
  | { status: 'empty'; missing?: string }
  /** The DJ is off or already busy. */
  | { status: 'busy' }
  /** The request didn't reach the DJ. `limited` when it was asked too often. */
  | { status: 'error'; limited: boolean }

function engineRequest(it: DJIntent): EngineNextRequest {
  const req: EngineNextRequest = {}
  if (it.seed) req.seed = { kind: it.seed.kind, id: it.seed.id, ...(it.seed.artist ? { artist: it.seed.artist } : {}) }
  if (it.lane) req.lane = it.lane as Lane
  if (it.fromYear && it.toYear) {
    req.fromYear = it.fromYear
    req.toYear = it.toYear
  }
  return req
}

/**
 * Ask the DJ for something ("90s hip hop", "more like Bonobo"). When it can,
 * the DJ plays a block of it now, introduces it, and keeps to it for one more
 * block before going back to its lanes.
 */
export async function askDJ(text: string): Promise<AskResult> {
  const s = session
  if (!s || s.fetching || !text.trim()) return { status: 'busy' }
  s.fetching = true
  useDJ.setState({ busy: true, missing: null })
  try {
    let it: DJIntent
    try {
      it = await s.deps.intent(text)
    } catch (e) {
      return { status: 'error', limited: e instanceof ApiError && e.status === 429 }
    }
    if (session !== s) return { status: 'busy' }
    const missing = it.missing || undefined
    if (missing) useDJ.setState({ missing })
    if (!it.understood) return { status: 'not-understood', missing, ...(it.try?.length ? { try: it.try } : {}) }
    s.request = { req: engineRequest(it), text, label: requestLabel(text, it), left: REQUEST_BLOCKS }
    const cur = bound?.getState().current ?? undefined
    const b = await requestBlock(s, cur, new Set(cur ? [cur.id] : []))
    if (session !== s) return { status: 'busy' }
    if (!b) return { status: 'empty', missing }
    playNow(s, b)
    return { status: 'playing', missing }
  } finally {
    if (session === s) s.fetching = false
    useDJ.setState({ busy: false })
  }
}

/** A voice was just set up: speak the lines from now on. */
export function setDJVoice(on: boolean): void {
  if (session) session.voice = on
  useDJ.setState({ voice: on })
}

/** Turn the DJ off. What is queued keeps playing. */
export function stopDJ(): void {
  generation++
  session = null
  if (cardTimer) clearTimeout(cardTimer)
  cardTimer = null
  useDJ.setState({ ...idle })
}

/**
 * Follow the player for the DJ: introduce blocks and keep the queue filled.
 * Returns an unsubscribe. `getContext` is injectable for tests.
 */
export function startDJWatcher(
  engine: Pick<AudioEngine, 'subscribe' | 'getState' | 'enqueue' | 'speak'>,
  getContext: () => PlayContext = getPlayContext,
): () => void {
  bound = engine
  const unsub = engine.subscribe((st) => onState(st, getContext))
  return () => {
    unsub()
    if (bound === engine) bound = null
  }
}

/** Test hook: forget all DJ state. */
export function _resetDJ(): void {
  stopDJ()
  bound = null
  cardSeq = 0
}
