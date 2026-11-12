import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api'
import type { Track } from './types'

export type ListenOutcome = 'completed' | 'skip_early' | 'skip_late' | 'stopped'

export interface ListenInput {
  libraryTrackId: string
  title: string
  artist: string
  album: string
  isrc?: string
  durationMs: number
  msPlayed: number
  outcome: ListenOutcome
  contextKind: string
  contextId?: string
  startedAt: number // unix seconds
}

/**
 * Report one listen outcome. `keepalive` lets the request outlive the page
 * (used when the tab is closing mid-song).
 */
export async function recordListen(input: ListenInput, opts: { keepalive?: boolean } = {}): Promise<void> {
  if (opts.keepalive) {
    await fetch('/api/v1/listens', {
      method: 'POST',
      credentials: 'include',
      keepalive: true,
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(input),
    })
    return
  }
  await api.post<null>('/listens', input)
}

export interface TasteFeedback {
  targetKind: 'track' | 'artist'
  targetKey: string
  label: string
  sublabel?: string
  libraryId?: string
  createdAt: number
}

export interface ArtistTaste {
  key: string
  name: string
  score: number
  listens: number
  completions: number
  earlySkips: number
  lastListened: number
}

export interface TrackTaste {
  catalogId: string
  libraryTrackId?: string
  title: string
  artist: string
  artistKey: string
  score: number
  listens: number
  completions: number
  earlySkips: number
  lateSkips: number
  lastListened: number
}

export interface TasteProfile {
  topArtists: ArtistTaste[]
  topTracks: TrackTaste[]
  trackCount: number
  artistCount: number
  hiddenTracks: number
  hiddenArtists: number
}

export const getTasteFeedback = () => api.get<TasteFeedback[]>('/taste/feedback')
export const getTasteProfile = () => api.get<TasteProfile>('/taste/profile')

export function hideTrack(t: Track) {
  return api.put<TasteFeedback>('/taste/feedback', {
    targetKind: 'track',
    libraryTrackId: t.id,
    title: t.title,
    artist: t.artist,
    album: t.album,
    durationMs: t.durationMs,
    ...(t.isrc ? { isrc: t.isrc } : {}),
  })
}

export function hideArtist(t: Pick<Track, 'artist' | 'artistId'>) {
  return api.put<TasteFeedback>('/taste/feedback', {
    targetKind: 'artist',
    artist: t.artist,
    ...(t.artistId ? { libraryArtistId: t.artistId } : {}),
  })
}

export function unhide(f: Pick<TasteFeedback, 'targetKind' | 'targetKey'>) {
  return api.del<null>(`/taste/feedback/${f.targetKind}/${encodeURIComponent(f.targetKey)}`)
}

export function useTasteFeedback() {
  return useQuery({ queryKey: ['taste', 'feedback'], queryFn: getTasteFeedback })
}

export function useUnhide() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: unhide,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['taste'] }),
  })
}
