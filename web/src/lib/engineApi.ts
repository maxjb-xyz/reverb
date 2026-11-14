import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api'
import type { ExternalResult, Track } from './types'

export interface IndexStatus {
  running: boolean
  tracks: number
  artists: number
  artistsWithSimilar: number
  /** Where similar artists come from; empty when nothing can look them up. */
  similarSources?: string[]
  lastIndexedAt: number
  lastError?: string
}

/** Optional audio analysis progress (managers). */
export interface AnalysisStatus {
  enabled: boolean
  running: boolean
  model?: string
  analyzed: number
  failed: number
  total: number
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

/** Server-wide recommendation switches (managers). */
export interface EngineSettings {
  /** Use Deezer for similar artists and new songs even when it isn't a search source. */
  deezer: boolean
}

export const getEngineSettings = () => api.get<EngineSettings>('/engine/settings')
export const putEngineSettings = (s: EngineSettings) => api.put<EngineSettings>('/engine/settings', s)
export const getIndexStatus = () => api.get<IndexStatus>('/engine/index')
export const rebuildIndex = () => api.post<null>('/engine/index/rebuild')
export const getAnalysisStatus = () => api.get<AnalysisStatus>('/engine/analysis')
export const runAnalysis = () => api.post<null>('/engine/analysis/run')
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

export function useAnalysisStatus(enabled: boolean) {
  return useQuery({
    queryKey: ['engine', 'analysis'],
    queryFn: getAnalysisStatus,
    enabled,
    refetchInterval: (q) => (q.state.data?.running ? 5000 : false),
  })
}

export function useEngineSettings(enabled: boolean) {
  return useQuery({ queryKey: ['engine', 'settings'], queryFn: getEngineSettings, enabled })
}

export function useSaveEngineSettings() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: putEngineSettings,
    onSuccess: (s) => {
      qc.setQueryData(['engine', 'settings'], s)
      // The index re-runs with the new sources.
      setTimeout(() => void qc.invalidateQueries({ queryKey: ['engine', 'index'] }), 1000)
    },
  })
}

export function useRunAnalysis() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: runAnalysis,
    onSuccess: () => setTimeout(() => void qc.invalidateQueries({ queryKey: ['engine', 'analysis'] }), 1000),
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

// ---- Recommendation engine: radio, DJ lanes ----

export type SeedKind = 'track' | 'artist' | 'genre' | 'taste'

export interface EngineSeed {
  kind: SeedKind
  id?: string
  title?: string
  artist?: string
}

export type Lane = '' | 'on-repeat' | 'throwback' | 'deep-cuts' | 'sounds-like' | 'new-to-you' | 'fresh-finds'

/** Why the engine picked a song. Fields beyond kind are set when they apply. */
export interface EngineReason {
  kind:
    | 'seed' | 'seed_artist' | 'similar_artist' | 'genre' | 'era' | 'favorite' | 'on_repeat'
    | 'deep_cut' | 'new_to_you' | 'fresh' | 'throwback' | 'discovery' | 'library' | 'sounds_like'
  artist?: string
  artistKey?: string
  seedArtist?: string
  /** For sounds_like from a song: that song's title. */
  seedTitle?: string
  genre?: string
  year?: number
  playCount?: number
}

export interface EngineItem {
  owned: boolean
  track?: Track
  external?: ExternalResult
  year?: number
  genres?: string
  score: number
  reason: EngineReason
}

export interface EngineStation {
  id: string
  kind: SeedKind
  lane?: Lane
  label: string
}

export interface EngineNextRequest {
  seed?: EngineSeed
  lane?: Lane
  fromYear?: number
  toYear?: number
  exclude?: string[]
  includeExternal?: boolean
  n?: number
}

export interface EngineResult {
  station: EngineStation
  items: EngineItem[]
}

export interface StationSuggestion {
  seed: EngineSeed
  label: string
  sublabel?: string
  coverArtId?: string
  libraryArtistId?: string
}

export const getNext = (req: EngineNextRequest) => api.post<EngineResult>('/engine/next', req)
export const getStations = (n = 8) =>
  api.get<{ stations: StationSuggestion[] }>(`/engine/stations?n=${n}`).then((r) => r.stations)

export function useStations(n = 8) {
  return useQuery({ queryKey: ['engine', 'stations', n], queryFn: () => getStations(n), staleTime: 5 * 60_000 })
}

/** A short human line for a reason ("Because you like Bonobo"). */
export function reasonText(r: EngineReason): string {
  switch (r.kind) {
    case 'seed':
      return 'Where this station started'
    case 'seed_artist':
      return r.artist ? `More from ${r.artist}` : 'From the same artist'
    case 'similar_artist':
      return r.seedArtist ? `Fans of ${r.seedArtist} play ${r.artist ?? 'this'}` : `Similar to what you're playing`
    case 'genre':
      return r.genre ? `More ${r.genre}` : 'Same genre'
    case 'era':
      return r.year ? `Also from ${r.year}` : 'From the same era'
    case 'favorite':
      return r.artist ? `You play a lot of ${r.artist}` : 'One of your favourites'
    case 'on_repeat':
      return r.playCount ? `You've played this ${r.playCount}×` : 'On repeat lately'
    case 'deep_cut':
      return r.artist ? `A deeper cut from ${r.artist}` : 'A deeper cut'
    case 'new_to_you':
      return 'New to you'
    case 'fresh':
      return 'Just added'
    case 'throwback':
      return r.year ? `A throwback to ${r.year}` : 'A throwback'
    case 'sounds_like':
      if (r.seedTitle) return `Sounds like ${r.seedTitle}`
      return r.seedArtist ? `Sounds like ${r.seedArtist}` : 'Sounds like what you’re playing'
    case 'discovery':
      return r.seedArtist ? `Not in your library yet, for fans of ${r.seedArtist}` : 'Not in your library yet'
    default:
      return 'From your library'
  }
}
