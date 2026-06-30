import { useQuery } from '@tanstack/react-query'
import { api } from './api'
import type { Album, Artist, SearchResults, SyncedPlaylistDetail } from './types'

export interface LibraryStatus {
  mode: string
  state: string // 'starting' | 'ready' | 'degraded' | 'external' | 'unconfigured'
}

export function getLibraryStatus(): Promise<LibraryStatus> {
  return api.get<LibraryStatus>('/library/status')
}

export function useLibraryStatus() {
  return useQuery({
    queryKey: ['library', 'status'],
    queryFn: getLibraryStatus,
    refetchInterval: (q) => (q.state.data?.state === 'starting' ? 3000 : false),
  })
}

export function streamUrl(id: string): string {
  return `/api/v1/stream/${encodeURIComponent(id)}`
}

// Task 9 Part A — DEFERRED to SP3:
// coverUrl(id) already accepts a canonical id in its path (no change needed for P1).
// A `?v=<libraryVersion>` cache-bust has no consumer in P1: live views use backend ids that
// already change on a swap, Tasks 10/11 still emit backend ids server-side, and
// `library_version` is not threaded to the FE today.
// Revisit in SP3 when canonical cover URLs (stats surfaces) get a real consumer.

export function coverUrl(id: string, size = 300): string {
  if (!id) return ''
  return `/api/v1/cover/${encodeURIComponent(id)}?size=${size}`
}

// A track's display cover is its album's cover — per-song artwork is usually
// absent (Navidrome 404s it), while the album cover reliably resolves. Prefer
// albumId; fall back to the song's own coverArtId only when there's no album.
export function trackCoverUrl(track: { albumId?: string; coverArtId?: string }, size = 300): string {
  const id = track.albumId || track.coverArtId || ''
  return id ? coverUrl(id, size) : ''
}

export function useLibrarySearch(q: string) {
  return useQuery({
    queryKey: ['library', 'search', q],
    queryFn: () => api.get<SearchResults>(`/library/search?q=${encodeURIComponent(q)}`),
    enabled: q.trim().length > 0,
  })
}

export function useArtist(id: string) {
  return useQuery({
    queryKey: ['library', 'artist', id],
    queryFn: () => api.get<Artist>(`/library/artist/${encodeURIComponent(id)}`),
    enabled: !!id,
  })
}

export function useAlbum(id: string) {
  return useQuery({
    queryKey: ['library', 'album', id],
    queryFn: () => api.get<Album>(`/library/album/${encodeURIComponent(id)}`),
    enabled: !!id,
  })
}

export function useArtists() {
  return useQuery({
    queryKey: ['library', 'artists'],
    queryFn: () => api.get<Artist[]>('/library/artists'),
  })
}

export function useAlbums(type = 'newest') {
  return useQuery({
    queryKey: ['library', 'albums', type],
    queryFn: () => api.get<Album[]>(`/library/albums?type=${encodeURIComponent(type)}`),
  })
}

export function createPlaylist(name: string): Promise<SyncedPlaylistDetail> {
  return api.post<SyncedPlaylistDetail>('/playlists', { name })
}

export function importPlaylistOnce(url: string): Promise<SyncedPlaylistDetail> {
  return api.post<SyncedPlaylistDetail>('/playlists/import', { url })
}
