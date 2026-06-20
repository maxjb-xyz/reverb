import { useEffect, useReducer } from 'react'
import type { EnvelopeStatus, ExternalResult, SearchEnvelope } from './types'
import { SearchStream } from './searchStream'

export interface SourceStatus {
  source: string
  status: EnvelopeStatus
}

export interface EverywhereState {
  tracks: ExternalResult[]
  albums: ExternalResult[]
  artists: ExternalResult[]
  sources: SourceStatus[]
}

export const emptyEverywhere: EverywhereState = { tracks: [], albums: [], artists: [], sources: [] }

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
  const out = existing.slice() // preserve order; never reflow
  for (const r of incoming) {
    const k = dedupKey(r)
    if (seen.has(k)) continue
    seen.add(k)
    out.push(r)
  }
  return out
}

export function applyEnvelope(state: EverywhereState, env: SearchEnvelope): EverywhereState {
  const incTracks = env.results.filter((r) => r.type === 'track')
  const incAlbums = env.results.filter((r) => r.type === 'album')
  const incArtists = env.results.filter((r) => r.type === 'artist')

  const sources = state.sources.some((s) => s.source === env.source)
    ? state.sources.map((s) => (s.source === env.source ? { source: env.source, status: env.status } : s))
    : [...state.sources, { source: env.source, status: env.status }]

  return {
    tracks: appendSection(state.tracks, incTracks),
    albums: appendSection(state.albums, incAlbums),
    artists: appendSection(state.artists, incArtists),
    sources,
  }
}

type Action = { type: 'reset' } | { type: 'envelope'; env: SearchEnvelope }

function reducer(state: EverywhereState, action: Action): EverywhereState {
  switch (action.type) {
    case 'reset':
      return emptyEverywhere
    case 'envelope':
      return applyEnvelope(state, action.env)
  }
}

// useEverywhere opens a SearchStream for (q,type) when enabled, accumulating
// per-source envelopes via the pure reducer. The stream is closed on unmount /
// when q/type/enabled change (no leaked connections).
export function useEverywhere(q: string, type: 'track' | 'album' | 'artist', enabled: boolean): EverywhereState {
  const [state, dispatch] = useReducer(reducer, emptyEverywhere)

  useEffect(() => {
    dispatch({ type: 'reset' })
    if (!enabled || q.trim() === '') return
    const stream = new SearchStream(q, type, { onEnvelope: (env) => dispatch({ type: 'envelope', env }) })
    return () => stream.close()
  }, [q, type, enabled])

  return state
}
