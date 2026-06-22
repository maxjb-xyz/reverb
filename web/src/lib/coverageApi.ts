import { useQuery } from '@tanstack/react-query'
import { api } from './api'
import type { AlbumDetail, ArtistDetail, Playlist } from './types'

export function useArtistDetail(source: string, id: string) {
  return useQuery({
    queryKey: ['artist-detail', source, id],
    queryFn: () => api.get<ArtistDetail>(`/artist/${encodeURIComponent(source)}/${encodeURIComponent(id)}`),
    enabled: !!source && !!id,
  })
}

export function useAlbumDetail(source: string, id: string) {
  return useQuery({
    queryKey: ['album-detail', source, id],
    queryFn: () => api.get<AlbumDetail>(`/album/${encodeURIComponent(source)}/${encodeURIComponent(id)}`),
    enabled: !!source && !!id,
  })
}

export function usePlaylistDetail(id: string) {
  return useQuery({
    queryKey: ['playlist-detail', id],
    queryFn: () => api.get<Playlist>(`/library/playlist/${encodeURIComponent(id)}`),
    enabled: !!id,
  })
}
