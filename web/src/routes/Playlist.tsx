import { useParams } from 'react-router-dom'
import { usePlaylistDetail } from '../lib/coverageApi'
import { coverUrl } from '../lib/libraryApi'
import { formatDuration } from '../lib/types'
import type { Track } from '../lib/types'
import { usePlayer } from '../lib/playerStore'
import { TrackRow } from '../components/ui/TrackRow'
import { Button, IconButton, Cover, Skeleton, EmptyState } from '../components/ui'

export default function Playlist() {
  const { id = '' } = useParams()
  const { data: playlist, isLoading, isError } = usePlaylistDetail(id)
  const playTrackList = usePlayer((s) => s.playTrackList)
  const toggleShuffle = usePlayer((s) => s.toggleShuffle)
  const shuffle = usePlayer((s) => s.shuffle)

  if (isLoading) {
    return (
      <div data-testid="playlist-skeleton" className="space-y-6">
        {/* Header skeleton */}
        <header className="flex items-end gap-6 pt-4">
          <Skeleton className="h-52 w-52 flex-none" rounded="md" />
          <div className="flex-1 space-y-3 pb-2">
            <Skeleton className="h-3 w-12" />
            <Skeleton className="h-10 w-64" />
            <Skeleton className="h-3 w-48" />
            <Skeleton className="h-10 w-28 rounded-full" rounded="md" />
          </div>
        </header>
        {/* Track row skeletons */}
        <div className="space-y-1">
          {Array.from({ length: 8 }).map((_, i) => (
            <Skeleton key={i} className="h-14 w-full" rounded="md" />
          ))}
        </div>
      </div>
    )
  }

  if (isError || !playlist) {
    return (
      <EmptyState
        icon="browse"
        title="Playlist not found"
        hint="This playlist may have been removed from your library."
      />
    )
  }

  const tracks: Track[] = playlist.tracks ?? []
  const hasTracks = tracks.length > 0

  const coverSrc = playlist.coverArtId ? coverUrl(playlist.coverArtId, 300) : undefined

  return (
    <div className="space-y-6">
      {/* Subtle gradient wash behind header */}
      <div className="relative -mx-4 -mt-4 px-4 pt-4 pb-6 rounded-b-2xl overflow-hidden bg-gradient-to-b from-raised to-transparent">
        <header className="relative z-10 flex items-end gap-6 pt-2">
          <Cover
            src={coverSrc}
            alt={playlist.name}
            size={208}
            rounded="md"
            className="shadow-cover flex-none"
          />
          <div className="min-w-0 pb-1">
            <div className="text-xs font-semibold uppercase tracking-widest text-text-muted mb-1">
              Playlist
            </div>
            <h1 className="text-4xl font-black leading-tight tracking-tight text-text-primary truncate">
              {playlist.name}
            </h1>
            <div className="mt-2 text-sm text-text-secondary flex flex-wrap items-center gap-x-1">
              {playlist.songCount > 0 ? <span>{playlist.songCount} songs</span> : null}
              {playlist.durationMs > 0 ? <span>· {formatDuration(playlist.durationMs)}</span> : null}
            </div>
            <div className="mt-4 flex items-center gap-3">
              <Button
                variant="primary"
                size="md"
                disabled={!hasTracks}
                onClick={() => hasTracks && playTrackList(tracks, 0)}
                aria-label={`Play ${playlist.name}`}
              >
                Play
              </Button>
              <IconButton
                name="shuffle"
                label={`Shuffle ${playlist.name}`}
                onClick={() => {
                  if (!hasTracks) return
                  if (!shuffle) toggleShuffle()
                  playTrackList(tracks, 0)
                }}
                disabled={!hasTracks}
              />
            </div>
          </div>
        </header>
      </div>

      {/* Track list */}
      <div className="space-y-0.5">
        {tracks.map((t, i) => (
          <TrackRow
            key={t.id}
            track={t}
            index={i}
            onPlay={() => playTrackList(tracks, i)}
          />
        ))}
        {tracks.length === 0 && (
          <EmptyState icon="browse" title="No tracks in this playlist" />
        )}
      </div>
    </div>
  )
}
