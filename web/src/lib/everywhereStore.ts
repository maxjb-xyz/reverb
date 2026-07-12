import { useEffect, useReducer } from 'react'
import type { EnvelopeStatus, ExternalResult, SearchEnvelope } from './types'
import { SearchStream } from './searchStream'

export interface SourceStatus {
  source: string
  status: EnvelopeStatus
}

export type SearchStatus = 'idle' | 'streaming' | 'done'

export interface EverywhereState {
  tracks: ExternalResult[]
  albums: ExternalResult[]
  artists: ExternalResult[]
  playlists: ExternalResult[]
  sources: SourceStatus[]
  status: SearchStatus
}

export const emptyEverywhere: EverywhereState = { tracks: [], albums: [], artists: [], playlists: [], sources: [], status: 'idle' }

// normalize mirrors the backend matching.Normalize closely enough for client-side
// dedup: lowercase, strip feat groups, &→and, drop non-alphanumerics, collapse ws.
function normalize(s: string): string {
  return s
    .toLowerCase()
    .replace(/\s*[([]?\s*\b(feat\.?|featuring|ft\.?)\b.*$/i, '')
    .replace(/&/g, ' and ')
    .replace(/[^\p{L}\p{N}\s]+/gu, ' ')
    .replace(/\s+/g, ' ')
    .trim()
}

export function dedupKey(r: ExternalResult): string {
  if (r.isrc) return `isrc:${r.isrc.toLowerCase()}`
  return `nf:${normalize(r.artist)}␟${normalize(r.title)}`
}

function appendSection(existing: ExternalResult[], incoming: ExternalResult[]): ExternalResult[] {
  const seen = new Set(existing.map(dedupKey))
  let out: ExternalResult[] | null = null // allocate only when something new arrives
  for (const r of incoming) {
    const k = dedupKey(r)
    if (seen.has(k)) continue
    seen.add(k)
    if (out === null) out = existing.slice() // preserve order; never reflow
    out.push(r)
  }
  // Same reference when nothing new was added, so re-delivered envelopes (e.g. an
  // EventSource reconnect replaying the same frame) do not churn a new array and
  // force needless re-renders of the result rows.
  return out ?? existing
}

export function applyEnvelope(state: EverywhereState, env: SearchEnvelope): EverywhereState {
  const incTracks = env.results.filter((r) => r.type === 'track')
  const incAlbums = env.results.filter((r) => r.type === 'album')
  const incArtists = env.results.filter((r) => r.type === 'artist')
  const incPlaylists = env.results.filter((r) => r.type === 'playlist')

  const existingSrc = state.sources.find((s) => s.source === env.source)
  const sources =
    existingSrc && existingSrc.status === env.status
      ? state.sources // unchanged source/status → keep the same reference
      : existingSrc
        ? state.sources.map((s) => (s.source === env.source ? { source: env.source, status: env.status } : s))
        : [...state.sources, { source: env.source, status: env.status }]

  const tracks = appendSection(state.tracks, incTracks)
  const albums = appendSection(state.albums, incAlbums)
  const artists = appendSection(state.artists, incArtists)
  const playlists = appendSection(state.playlists, incPlaylists)

  // Fully idempotent: if a re-delivered envelope changes nothing, return the SAME
  // state reference so React/Zustand skip the re-render entirely.
  if (
    tracks === state.tracks &&
    albums === state.albums &&
    artists === state.artists &&
    playlists === state.playlists &&
    sources === state.sources
  ) {
    return state
  }

  return {
    tracks,
    albums,
    artists,
    playlists,
    sources,
    status: state.status, // envelope does not change streaming flag
  }
}

type Action =
  | { type: 'reset' }
  | { type: 'startSearch' }
  | { type: 'finishSearch' }
  | { type: 'envelope'; env: SearchEnvelope }

function reducer(state: EverywhereState, action: Action): EverywhereState {
  switch (action.type) {
    case 'reset':
      return emptyEverywhere
    case 'startSearch':
      return { ...emptyEverywhere, status: 'streaming' }
    case 'finishSearch':
      // Idempotent: return the SAME reference once done so a repeated finishSearch
      // (e.g. EventSource firing onerror more than once) does not churn a new state
      // object and force needless re-renders of the result rows.
      return state.status === 'done' ? state : { ...state, status: 'done' }
    case 'envelope':
      return applyEnvelope(state, action.env)
  }
}

// useEverywhere opens a SearchStream for (q,type) when enabled, accumulating
// per-source envelopes via the pure reducer. The stream is closed on unmount /
// when q/type/enabled change (no leaked connections).
export function useEverywhere(q: string, type: 'track' | 'album' | 'artist' | 'playlist', enabled: boolean): EverywhereState {
  const [state, dispatch] = useReducer(reducer, emptyEverywhere)

  useEffect(() => {
    dispatch({ type: 'reset' })
    if (!enabled || q.trim() === '') return
    dispatch({ type: 'startSearch' })
    const stream = new SearchStream(q, type, {
      onEnvelope: (env) => dispatch({ type: 'envelope', env }),
      onError: () => dispatch({ type: 'finishSearch' }),
    })
    return () => stream.close()
  }, [q, type, enabled])

  return state
}
