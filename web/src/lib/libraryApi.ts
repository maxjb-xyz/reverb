import { useQuery } from '@tanstack/react-query'
import { api } from './api'
import type { Album, Artist, Playlist, SearchResults } from './types'

export function streamUrl(id: string): string {
  return `/api/v1/stream/${encodeURIComponent(id)}`
}

export function coverUrl(id: string, size = 300): string {
  if (!id) return ''
  return `/api/v1/cover/${encodeURIComponent(id)}?size=${size}`
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

export function usePlaylists() {
  return useQuery({
    queryKey: ['library', 'playlists'],
    queryFn: () => api.get<Playlist[]>('/library/playlists'),
  })
}

export function createPlaylist(name: string): Promise<Playlist> {
  return api.post<Playlist>('/library/playlists', { name })
}

export function addTracksToPlaylist(id: string, trackIds: string[]): Promise<{ ok: boolean }> {
  return api.post(`/library/playlists/${encodeURIComponent(id)}/tracks`, { trackIds })
}

export function renamePlaylist(id: string, name: string): Promise<{ ok: boolean }> {
  return api.put(`/library/playlist/${encodeURIComponent(id)}`, { name })
}

export function deletePlaylist(id: string): Promise<{ ok: boolean }> {
  return api.del(`/library/playlist/${encodeURIComponent(id)}`)
}

export function removePlaylistTrack(id: string, index: number): Promise<{ ok: boolean }> {
  return api.post(`/library/playlist/${encodeURIComponent(id)}/remove`, { indices: [index] })
}
