import { useQuery } from '@tanstack/react-query'
import { api } from './api'
import type { AlbumDetail, ArtistDetail, Playlist } from './types'

export interface ArtistProfile {
  name: string
  coverUrl?: string
  coverArtId?: string
  source: string
  externalId: string
}

export function useArtistProfile(source: string, id: string) {
  return useQuery({
    queryKey: ['artist-profile', source, id],
    queryFn: () => api.get<ArtistProfile>(`/artist/${encodeURIComponent(source)}/${encodeURIComponent(id)}/profile`),
    enabled: !!source && !!id,
  })
}

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
