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
  artistExternalId?: string
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

export type CoverageState = 'pending' | 'none' | 'partial' | 'full'

export interface ExternalTrackRef {
  source: string
  externalId: string
  title: string
  artist?: string
  album?: string
  isrc?: string
  durationMs: number
}

export interface AlbumCoverage {
  source: string
  externalAlbumId: string
  state: CoverageState
  ownedCount: number
  totalCount: number
  libraryAlbumId?: string
  missingTracks: ExternalTrackRef[]
}

export interface DiscographyAlbum {
  source: string
  externalId: string
  name: string
  coverUrl?: string
  year: number
  kind: 'album' | 'single'
  totalTracks: number
  libraryAlbumId?: string
}

export interface ArtistDetail {
  source: string
  id: string
  name: string
  coverArtId?: string
  coverUrl?: string
  libraryArtistId?: string
  externalArtistId?: string
  resolved: boolean
  albums: DiscographyAlbum[]
  libraryAlbums?: DiscographyAlbum[]
}

export interface AlbumDetailTrack {
  state: CoverageState
  libraryTrack?: Track
  externalRef?: ExternalTrackRef
  key?: { source: string; externalId: string }
  title: string
  artist: string
  album?: string
  trackNumber: number
  durationMs: number
  coverUrl?: string
  /** Spotify artist id — links to /artist/spotify/:id when present */
  artistExternalId?: string
  /** Spotify album id — links to /album/spotify/:id when present */
  albumExternalId?: string
}

export interface AlbumDetail {
  source: string
  id: string
  name: string
  artist: string
  artistId?: string
  coverArtId?: string
  coverUrl?: string
  year: number
  libraryAlbumId?: string
  ownedCount: number
  totalCount: number
  tracks: AlbumDetailTrack[]
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
  artistExternalId?: string
  albumExternalId?: string
}

export type EnvelopeStatus = 'ok' | 'timeout' | 'error'

export interface SearchEnvelope {
  source: string
  status: EnvelopeStatus
  results: ExternalResult[]
  cursor?: string
  error?: string
}

export type DownloadStatus = 'queued' | 'running' | 'completed' | 'failed' | 'canceled'

export interface DownloadJob {
  id: string
  dedupKey: string
  status: DownloadStatus
  progress: number // 0-100, or -1 = unknown (indeterminate)
  error?: string
  outputPath?: string
  libraryTrackId?: string
  coverArtId?: string
  downloaderName: string
  priority: number
  attempts: number
  source: string
  externalId: string
  // Request fields carried from request_json (mirrors core.DownloadJob), so the
  // client can build a playable Track for play-when-ready auto-play.
  artist?: string
  title?: string
  album?: string
  isrc?: string
  playWhenReady: boolean
  createdAt: number
  startedAt: number
  finishedAt: number
}

export interface DownloadEvent {
  jobId: string
  dedupKey: string
  status: DownloadStatus
  progress: number
  error?: string
  source: string
  externalId: string
  libraryTrackId?: string
  coverArtId?: string
  artistId?: string
  albumId?: string
}

export interface LibraryUpdatedEvent {
  artistIds: string[]
  albumIds: string[]
}

// RealtimeEvent is one WS frame: {type, payload}. type is the EventBus topic.
export interface RealtimeEvent {
  type: string
  payload: unknown
}

export interface QueueStateEvent {
  paused: boolean
}

export interface DownloadRemovedEvent {
  jobIds: string[]
}

export interface SyncedPlaylist {
  id: string
  source: string
  externalId: string
  name: string
  coverUrl?: string
  syncEnabled: boolean
  syncIntervalSec: number
  autoDownload: boolean
  lastSyncedAt: number
  trackCount: number
  mode?: 'synced' | 'once'
}

export interface SyncedPlaylistDetail extends SyncedPlaylist {
  ownedCount: number
  totalCount: number
  tracks: AlbumDetailTrack[]
}
