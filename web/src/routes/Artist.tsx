import { useNavigate, useParams } from 'react-router-dom'
import { useArtist, coverUrl } from '../lib/libraryApi'
import { Cover, Skeleton, EmptyState, MediaCard } from '../components/ui'

export default function Artist() {
  const { id = '' } = useParams()
  const { data: artist, isLoading, isError } = useArtist(id)
  const navigate = useNavigate()

  if (isLoading) {
    return (
      <div data-testid="artist-skeleton" className="space-y-8">
        {/* Header skeleton */}
        <header className="flex items-end gap-6 pt-4">
          <Skeleton className="h-52 w-52 flex-none" rounded="full" />
          <div className="flex-1 space-y-3 pb-2">
            <Skeleton className="h-3 w-32" />
            <Skeleton className="h-12 w-72" />
            <Skeleton className="h-3 w-24" />
          </div>
        </header>
        {/* Album grid skeleton */}
        <section>
          <Skeleton className="h-6 w-24 mb-4" />
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
            {Array.from({ length: 5 }).map((_, i) => (
              <div key={i} className="space-y-2">
                <Skeleton className="aspect-square w-full" rounded="md" />
                <Skeleton className="h-3 w-3/4" />
                <Skeleton className="h-3 w-1/2" />
              </div>
            ))}
          </div>
        </section>
      </div>
    )
  }

  if (isError || !artist) {
    return (
      <EmptyState
        icon="browse"
        title="Artist not found"
        hint="This artist may have been removed from your library."
      />
    )
  }

  const albums = artist.albums ?? []
  const albumCount = artist.albumCount ?? albums.length
  const coverSrc = artist.coverArtId ? coverUrl(artist.coverArtId, 300) : undefined

  return (
    <div className="space-y-8">
      {/* Artist header */}
      <header className="flex items-end gap-6 pt-4">
        <Cover
          src={coverSrc}
          alt={artist.name}
          size={208}
          rounded="full"
          className="shadow-cover flex-none"
        />
        <div className="min-w-0 pb-1">
          <div className="text-xs font-semibold uppercase tracking-widest text-text-muted mb-1">
            Artist
          </div>
          <h1 className="text-5xl font-black leading-tight tracking-tight text-text-primary truncate">
            {artist.name}
          </h1>
          <p className="mt-2 text-sm text-text-secondary">
            In your library · {albumCount} {albumCount === 1 ? 'album' : 'albums'}
          </p>
        </div>
      </header>

      {/* Albums grid */}
      <section>
        <h2 className="text-xl font-bold text-text-primary mb-4">Albums</h2>
        {albums.length > 0 ? (
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
            {albums.map((al) => (
              <MediaCard
                key={al.id}
                title={al.name}
                subtitle={al.year ? String(al.year) : undefined}
                coverId={al.coverArtId || undefined}
                rounded="md"
                onClick={() => navigate(`/album/${al.id}`)}
              />
            ))}
          </div>
        ) : (
          <EmptyState icon="browse" title="No albums in library" />
        )}
      </section>
    </div>
  )
}
