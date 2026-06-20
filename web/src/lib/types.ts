export interface Track {
  id: string
  title: string
  albumId: string
  album: string
  artistId: string
  artist: string
  coverArtId: string
  trackNumber: number
  discNumber: number
  durationMs: number
  bitRate: number
  suffix: string
  contentType: string
  isrc?: string
}

export interface Album {
  id: string
  name: string
  artistId: string
  artist: string
  coverArtId: string
  year: number
  songCount: number
  durationMs: number
  tracks?: Track[]
}

export interface Artist {
  id: string
  name: string
  coverArtId: string
  albumCount: number
  albums?: Album[]
}

export interface Playlist {
  id: string
  name: string
  coverArtId: string
  songCount: number
  durationMs: number
  tracks?: Track[]
}

export interface SearchResults {
  tracks: Track[]
  albums: Album[]
  artists: Artist[]
}

export function formatDuration(ms: number): string {
  const total = Math.floor(ms / 1000)
  const m = Math.floor(total / 60)
  const s = total % 60
  return `${m}:${s.toString().padStart(2, '0')}`
}

export type MatchStatus = 'in_library' | 'not_in_library' | 'unknown'
export type MatchMethod = 'isrc' | 'mbid' | 'fuzzy' | 'none'

export interface MatchResult {
  status: MatchStatus
  libraryTrackId: string
  method: MatchMethod
  confidence: number
}

export interface ExternalResult {
  source: string
  externalId: string
  title: string
  artist: string
  album: string
  durationMs: number
  isrc?: string
  mbid?: string
  coverUrl?: string
  coverArtId?: string
  type: 'track' | 'album' | 'artist'
  match?: MatchResult
}

export type EnvelopeStatus = 'ok' | 'timeout' | 'error'

export interface SearchEnvelope {
  source: string
  status: EnvelopeStatus
  results: ExternalResult[]
  cursor?: string
  error?: string
}
