import { useQuery } from '@tanstack/react-query'
import type { Track } from './types'

export interface LyricLine {
  timeMs: number
  text: string
}

export type LyricsPayload =
  | { synced: true; lines: LyricLine[] }
  | { synced: false; plain: string }

// data === null means the track has no lyrics (204). Modeled on usePeaks.
export function useLyrics(track: Track | null | undefined) {
  return useQuery({
    queryKey: ['lyrics', track?.id],
    queryFn: async (): Promise<LyricsPayload | null> => {
      const params = new URLSearchParams({
        artist: track!.artist,
        title: track!.title,
        album: track!.album,
        durationMs: String(track!.durationMs),
      })
      const response = await fetch(
        `/api/v1/library/track/${encodeURIComponent(track!.id)}/lyrics?${params.toString()}`,
        { credentials: 'include' },
      )
      if (response.status === 204) return null
      if (!response.ok) throw new Error(`lyrics ${response.status}`)
      return (await response.json()) as LyricsPayload
    },
    enabled: !!track?.id,
    staleTime: Infinity,
    retry: false,
  })
}
