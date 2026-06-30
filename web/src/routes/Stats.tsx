import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { presetRange } from '../lib/range'
import type { Range } from '../lib/range'
import { summary, topTracks, topArtists, topAlbums, recent } from '../lib/statsApi'
import { RangeSelector } from '../components/stats/RangeSelector'
import { SummaryCards } from '../components/stats/SummaryCards'
import { TopList } from '../components/stats/TopList'
import { RecentList } from '../components/stats/RecentList'
import { Skeleton } from '../components/ui/Skeleton'

function rangeKey(r: Range): [number, number] {
  return [r.from, r.to]
}

export default function Stats() {
  const [range, setRange] = useState<Range>(() => presetRange('30d'))

  const summaryQ = useQuery({
    queryKey: ['stats', 'summary', ...rangeKey(range)],
    queryFn: () => summary(range),
    staleTime: 60_000,
  })

  const topTracksQ = useQuery({
    queryKey: ['stats', 'top', 'tracks', ...rangeKey(range)],
    queryFn: () => topTracks(range, 10),
    staleTime: 60_000,
  })

  const topArtistsQ = useQuery({
    queryKey: ['stats', 'top', 'artists', ...rangeKey(range)],
    queryFn: () => topArtists(range, 10),
    staleTime: 60_000,
  })

  const topAlbumsQ = useQuery({
    queryKey: ['stats', 'top', 'albums', ...rangeKey(range)],
    queryFn: () => topAlbums(range, 10),
    staleTime: 60_000,
  })

  const recentQ = useQuery({
    queryKey: ['stats', 'recent', range.to],
    queryFn: () => recent(range.to, 20),
    staleTime: 60_000,
  })

  const summaryData = summaryQ.data
  const tracks = topTracksQ.data ?? []
  const artists = topArtistsQ.data ?? []
  const albums = topAlbumsQ.data ?? []
  const recentRows = recentQ.data ?? []

  const isLoading = summaryQ.isLoading

  // Empty state: loaded, but no plays recorded in this range
  const isEmpty = !isLoading && summaryData !== undefined && summaryData.Plays === 0

  return (
    <div className="space-y-8">
      {/* Page header + range selector */}
      <div className="space-y-4">
        <h1 className="text-2xl font-black tracking-tight text-primary">Stats</h1>
        <RangeSelector value={range} onChange={setRange} />
      </div>

      {/* Loading skeleton */}
      {isLoading && (
        <div className="space-y-6">
          <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-3">
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} className="h-20 rounded-lg" />
            ))}
          </div>
          <Skeleton className="h-64 rounded-lg" />
        </div>
      )}

      {/* Empty state */}
      {isEmpty && (
        <div className="flex flex-col items-center justify-center gap-3 py-20 text-center">
          <p className="text-lg font-semibold text-secondary">No listening history yet</p>
          <p className="text-sm text-secondary max-w-sm">
            Your play stats will appear here once you start listening.
          </p>
        </div>
      )}

      {/* Content: only show when loaded and has data */}
      {!isLoading && !isEmpty && summaryData && (
        <div className="space-y-8">
          {/* Summary cards */}
          <SummaryCards data={summaryData} />

          {/* Top content: tracks / artists / albums side by side on wide screens */}
          <div className="grid grid-cols-1 lg:grid-cols-3 gap-8">
            {tracks.length > 0 && (
              <TopList title="Top tracks" rows={tracks} kind="track" />
            )}
            {artists.length > 0 && (
              <TopList title="Top artists" rows={artists} kind="artist" />
            )}
            {albums.length > 0 && (
              <TopList title="Top albums" rows={albums} kind="album" />
            )}
          </div>

          {/* Recently played */}
          {recentRows.length > 0 && (
            <RecentList rows={recentRows} />
          )}
        </div>
      )}
    </div>
  )
}
