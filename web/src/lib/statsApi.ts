import { api } from './api'
import type { Range } from './range'

// ── Response types ────────────────────────────────────────────────────────────
// Field names match the Go struct exported field names verbatim (PascalCase),
// because the Go structs have no json tags (Go's encoding/json uses PascalCase
// when no tag is present).

export interface SummaryStats {
  Plays: number
  DistinctTracks: number
  DistinctArtists: number
  DistinctAlbums: number
  MsPlayed: number
}

export interface TopRow {
  CatalogID: string
  Title: string
  Artist: string
  Album: string
  Source?: string
  ExternalID?: string
  CoverURL?: string
  ArtistExternalID?: string
  AlbumExternalID?: string
  Plays: number
  MsPlayed: number
}

export interface TimeBucket {
  Start: number
  Plays: number
  MsPlayed: number
}

export interface ClockCell {
  Weekday: number
  Hour: number
  Plays: number
  MsPlayed: number
}

export interface RecentRow {
  ID: string
  CatalogID: string
  Title: string
  Artist: string
  Album: string
  PlayedAt: number
}

export interface EntityStats {
  Plays: number
  MsPlayed: number
  FirstPlayed: number
  LastPlayed: number
  TopTracks: TopRow[]
}

// ── play-counts request/response ──────────────────────────────────────────────
// These match the backend's EXPLICIT lowercase json tags exactly. A casing
// mismatch silently yields zero counts (the backend keys the response map on the
// request item's `key`, so the request `key` and response keys must round-trip).

export interface PlayCountTrack {
  key: string
  title: string
  artist: string
  album: string
  durationMs: number
  isrc?: string
}

export interface PlayCountsResponse {
  counts: Record<string, number>
}

// ── Query string helpers ──────────────────────────────────────────────────────

function rangeParams(r: Range): URLSearchParams {
  const p = new URLSearchParams()
  p.set('from', String(r.from))
  p.set('to', String(r.to))
  p.set('bucket', r.bucket)
  p.set('tzOffsetMinutes', String(r.tzOffsetMinutes))
  return p
}

function qs(params: URLSearchParams): string {
  const s = params.toString()
  return s ? `?${s}` : ''
}

// ── API functions ─────────────────────────────────────────────────────────────

export function summary(r: Range): Promise<SummaryStats> {
  return api.get<SummaryStats>(`/stats/summary${qs(rangeParams(r))}`)
}

export function topTracks(r: Range, limit = 10): Promise<TopRow[]> {
  const p = rangeParams(r)
  p.set('limit', String(limit))
  return api.get<TopRow[]>(`/stats/top/tracks${qs(p)}`)
}

export function topArtists(r: Range, limit = 10): Promise<TopRow[]> {
  const p = rangeParams(r)
  p.set('limit', String(limit))
  return api.get<TopRow[]>(`/stats/top/artists${qs(p)}`)
}

export function topAlbums(r: Range, limit = 10): Promise<TopRow[]> {
  const p = rangeParams(r)
  p.set('limit', String(limit))
  return api.get<TopRow[]>(`/stats/top/albums${qs(p)}`)
}

export function timeline(r: Range): Promise<TimeBucket[]> {
  return api.get<TimeBucket[]>(`/stats/timeline${qs(rangeParams(r))}`)
}

export function clock(r: Range): Promise<ClockCell[]> {
  return api.get<ClockCell[]>(`/stats/clock${qs(rangeParams(r))}`)
}

export function recent(before: number, limit = 20): Promise<RecentRow[]> {
  const p = new URLSearchParams()
  p.set('before', String(before))
  p.set('limit', String(limit))
  return api.get<RecentRow[]>(`/stats/recent${qs(p)}`)
}

// entity fetches per-entity listening stats. For kind="album" the album's
// artist is REQUIRED server-side (album titles collide across artists), so pass
// it via the optional `artist` arg; artist/track callers omit it unchanged.
export function entity(kind: string, id: string, r: Range, artist?: string): Promise<EntityStats> {
  const p = rangeParams(r)
  p.set('kind', kind)
  p.set('id', id)
  if (artist) p.set('artist', artist)
  return api.get<EntityStats>(`/stats/entity${qs(p)}`)
}

// playCounts looks up per-track play counts for the current user. Lookup-only:
// never-played tracks resolve to 0 and no entity is minted. Returns the counts
// map keyed by each request item's `key`.
export function playCounts(tracks: PlayCountTrack[]): Promise<Record<string, number>> {
  return api.post<PlayCountsResponse>('/stats/play-counts', { tracks }).then((r) => r.counts)
}

// deletePlay removes a single play OWNED BY the current user. Owner-scoping is
// enforced server-side (the backend keys the delete on the session user id);
// this primitive only carries the play id. Resolves on 204.
export function deletePlay(id: string): Promise<void> {
  return api.del(`/plays/${encodeURIComponent(id)}`)
}
