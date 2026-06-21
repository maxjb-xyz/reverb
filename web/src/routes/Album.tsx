import { Link, useParams } from 'react-router-dom'
import { useAlbum, coverUrl } from '../lib/libraryApi'
import { TrackRow } from '../components/ui/TrackRow'
import { formatDuration } from '../lib/types'
import { usePlayer } from '../lib/playerStore'
import { Button, IconButton, Cover, Skeleton, EmptyState } from '../components/ui'

export default function Album() {
  const { id = '' } = useParams()
  const { data: album, isLoading, isError } = useAlbum(id)
  const playTrackList = usePlayer((s) => s.playTrackList)

  if (isLoading) {
    return (
      <div data-testid="album-skeleton" className="space-y-6">
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

  if (isError || !album) {
    return (
      <EmptyState
        icon="browse"
        title="Album not found"
        hint="This album may have been removed from your library."
      />
    )
  }

  const tracks = album.tracks ?? []

  const coverSrc = album.coverArtId ? coverUrl(album.coverArtId, 300) : undefined

  return (
    <div className="space-y-6">
      {/* Subtle gradient wash behind header */}
      <div className="relative -mx-4 -mt-4 px-4 pt-4 pb-6 rounded-b-2xl overflow-hidden bg-gradient-to-b from-raised to-transparent">
        <header className="relative z-10 flex items-end gap-6 pt-2">
          <Cover
            src={coverSrc}
            alt={album.name}
            size={208}
            rounded="md"
            className="shadow-cover flex-none"
          />
          <div className="min-w-0 pb-1">
            <div className="text-xs font-semibold uppercase tracking-widest text-text-muted mb-1">
              Album
            </div>
            <h1 className="text-4xl font-black leading-tight tracking-tight text-text-primary truncate">
              {album.name}
            </h1>
            <div className="mt-2 text-sm text-text-secondary flex flex-wrap items-center gap-x-1">
              <Link
                to={`/artist/${album.artistId}`}
                className="font-semibold text-text-primary hover:text-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent rounded"
              >
                {album.artist}
              </Link>
              {album.year ? <span>· {album.year}</span> : null}
              {album.songCount ? <span>· {album.songCount} songs</span> : null}
              {album.durationMs ? <span>· {formatDuration(album.durationMs)}</span> : null}
            </div>
            <div className="mt-4 flex items-center gap-3">
              <Button
                variant="primary"
                size="md"
                disabled={tracks.length === 0}
                onClick={() => tracks.length && playTrackList(tracks, 0)}
                aria-label={`Play ${album.name}`}
              >
                Play
              </Button>
              <IconButton
                name="shuffle"
                label={`Shuffle ${album.name}`}
                onClick={() => tracks.length && playTrackList(tracks, 0)}
                disabled={tracks.length === 0}
              />
              <IconButton name="heart" label={`Like ${album.name}`} />
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
          <EmptyState icon="browse" title="No tracks in this album" />
        )}
      </div>
    </div>
  )
}
