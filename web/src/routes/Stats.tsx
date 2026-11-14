import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { presetRange, presetKeyOf, currentRange } from '../lib/range'
import type { Range } from '../lib/range'
import { summary, topTracks, topArtists, topAlbums, recent, timeline, clock } from '../lib/statsApi'
import { RangeSelector } from '../components/stats/RangeSelector'
import { SummaryCards } from '../components/stats/SummaryCards'
import { TopList } from '../components/stats/TopList'
import { RecentList } from '../components/stats/RecentList'
import { TimelineChart } from '../components/stats/TimelineChart'
import { ClockHeatmap } from '../components/stats/ClockHeatmap'
import { Skeleton } from '../components/ui/Skeleton'
import { Button } from '../components/ui/Button'
import { PageHeader } from '../components/ui/PageHeader'

// Preset ranges ("Last 30 days") are keyed by name and recomputed on every
// fetch, so a refetch after a new play reaches up to now. Custom ranges are
// fixed and keyed by their bounds.
function rangeKey(r: Range): (string | number)[] {
  const preset = presetKeyOf(r)
  return preset ? ['preset', preset] : ['custom', r.from, r.to]
}

export default function Stats() {
  const [range, setRange] = useState<Range>(() => presetRange('30d'))

  const summaryQ = useQuery({
    queryKey: ['stats', 'summary', ...rangeKey(range)],
    queryFn: () => summary(currentRange(range)),
    staleTime: 60_000,
  })

  const topTracksQ = useQuery({
    queryKey: ['stats', 'top', 'tracks', ...rangeKey(range)],
    queryFn: () => topTracks(currentRange(range), 10),
    staleTime: 60_000,
  })

  const topArtistsQ = useQuery({
    queryKey: ['stats', 'top', 'artists', ...rangeKey(range)],
    queryFn: () => topArtists(currentRange(range), 10),
    staleTime: 60_000,
  })

  const topAlbumsQ = useQuery({
    queryKey: ['stats', 'top', 'albums', ...rangeKey(range)],
    queryFn: () => topAlbums(currentRange(range), 10),
    staleTime: 60_000,
  })

  const recentQ = useQuery({
    queryKey: ['stats', 'recent', ...rangeKey(range)],
    queryFn: () => recent(currentRange(range).to, 20),
    staleTime: 60_000,
  })

  const timelineQ = useQuery({
    queryKey: ['stats', 'timeline', ...rangeKey(range)],
    queryFn: () => timeline(currentRange(range)),
    staleTime: 60_000,
  })

  const clockQ = useQuery({
    queryKey: ['stats', 'clock', ...rangeKey(range)],
    queryFn: () => clock(currentRange(range)),
    staleTime: 60_000,
  })

  const summaryData = summaryQ.data
  const tracks = topTracksQ.data ?? []
  const artists = topArtistsQ.data ?? []
  const albums = topAlbumsQ.data ?? []
  const recentRows = recentQ.data ?? []
  const timelineBuckets = timelineQ.data ?? []
  const clockCells = clockQ.data ?? []

  const isLoading = summaryQ.isLoading
  const isError = summaryQ.isError

  // Empty state: loaded, but no plays recorded in this range
  const isEmpty = !isLoading && !isError && summaryData !== undefined && summaryData.Plays === 0

  return (
    <div className="space-y-8">
      {/* Page header + range selector */}
      <div className="space-y-4">
        <PageHeader title="Stats" />
        <RangeSelector value={range} onChange={setRange} />
      </div>

      {/* Error state — a failed summary query previously rendered a blank page */}
      {isError && (
        <div className="flex flex-col items-center justify-center gap-3 py-20 text-center" role="alert">
          <p className="text-lg font-semibold text-text-primary">Couldn't load your stats</p>
          <p className="text-sm text-text-muted max-w-sm">
            Something went wrong reaching the server. Try again in a moment.
          </p>
          <Button size="sm" variant="secondary" onClick={() => void summaryQ.refetch()}>
            Retry
          </Button>
        </div>
      )}

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
          <p className="text-lg font-semibold text-text-primary">No listening history yet</p>
          <p className="text-sm text-text-muted max-w-sm">
            Your play stats will appear here once you start listening.
          </p>
        </div>
      )}

      {/* Content: only show when loaded and has data */}
      {!isLoading && !isEmpty && summaryData && (
        <div className="space-y-8">
          {/* Summary cards */}
          <SummaryCards data={summaryData} />

          {/* Listening over time */}
          <section aria-label="Listening over time">
            <h2 className="text-base font-bold text-text-primary mb-3">Listening over time</h2>
            <div className="rounded-lg bg-raised px-4 pt-4 pb-2">
              <TimelineChart data={timelineBuckets} metric="plays" range={currentRange(range)} />
            </div>
          </section>

          {/* When you listen heatmap */}
          <section aria-label="When you listen">
            <h2 className="text-base font-bold text-text-primary mb-3">When you listen</h2>
            <div className="rounded-lg bg-raised px-4 py-4">
              <ClockHeatmap data={clockCells} />
            </div>
          </section>

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
