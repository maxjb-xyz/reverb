import { useQuery } from '@tanstack/react-query'
import { api } from './api'
import type { DiscographyAlbum } from './types'

export interface CollectionArtist {
  libraryArtistId: string
  name: string
  coverArtId?: string
  source: string
  externalArtistId?: string
  ownedAlbums: number
  totalAlbums: number
  missingAlbums: DiscographyAlbum[]
}

export interface CollectionSummary { artists: CollectionArtist[]; resolvedCount: number; artistCount: number }

export function useCollection() {
  return useQuery({ queryKey: ['collection'], queryFn: () => api.get<CollectionSummary>('/collection') })
}
