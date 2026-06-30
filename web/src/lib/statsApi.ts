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

export function entity(kind: string, id: string, r: Range): Promise<EntityStats> {
  const p = rangeParams(r)
  return api.get<EntityStats>(`/stats/entity/${encodeURIComponent(kind)}/${encodeURIComponent(id)}${qs(p)}`)
}
