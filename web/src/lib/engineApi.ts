import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api'

export interface IndexStatus {
  running: boolean
  tracks: number
  artists: number
  artistsWithSimilar: number
  lastIndexedAt: number
  lastError?: string
}

export interface SimilarArtist {
  key: string
  name: string
  source: string
  externalId?: string
  imageUrl?: string
  rank: number
  ownedTracks: number
  libraryArtistId?: string
}

export const getIndexStatus = () => api.get<IndexStatus>('/engine/index')
export const rebuildIndex = () => api.post<null>('/engine/index/rebuild')
export const getSimilarArtists = (name: string) =>
  api.get<{ artist: string; similar: SimilarArtist[] }>(`/artists/similar?name=${encodeURIComponent(name)}`)

export function useIndexStatus(enabled: boolean) {
  return useQuery({
    queryKey: ['engine', 'index'],
    queryFn: getIndexStatus,
    enabled,
    // While a run is going, poll so the numbers move.
    refetchInterval: (q) => (q.state.data?.running ? 3000 : false),
  })
}

export function useRebuildIndex() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: rebuildIndex,
    onSuccess: () => setTimeout(() => void qc.invalidateQueries({ queryKey: ['engine', 'index'] }), 1000),
  })
}

export function useSimilarArtists(name: string | undefined) {
  return useQuery({
    queryKey: ['engine', 'similar', name],
    queryFn: () => getSimilarArtists(name as string),
    enabled: !!name,
    staleTime: 10 * 60_000,
  })
}
