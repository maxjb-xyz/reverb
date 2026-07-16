import { useEffect } from 'react'
import { Link } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { Cover, EmptyState, MediaCard, Skeleton } from '../components/ui'
import { useCollection } from '../lib/collectionApi'
import { useDocumentTitle } from '../lib/useDocumentTitle'
import { useLibraryRevision } from '../lib/libraryRevisionStore'
import { postDownload } from '../lib/downloadApi'
import { useDownloads } from '../lib/downloadStore'
import { coverUrl } from '../lib/libraryApi'
import { useAuthStore } from '../lib/authStore'

export default function Collection() {
  useDocumentTitle('Collection')
  const collection = useCollection()
  const queryClient = useQueryClient()
  const revision = useLibraryRevision((state) => state.revision)
  const jobs = useDownloads((s) => s.jobs)
  const canAutoApprove = useAuthStore((s) => s.can('auto_approve'))

  useEffect(() => {
    void queryClient.invalidateQueries({ queryKey: ['collection'] })
  }, [queryClient, revision])

  if (collection.isLoading) {
    return (
      <div className="space-y-6">
        {Array.from({ length: 3 }, (_, i) => (
          <Skeleton key={i} className="h-36 w-full" />
        ))}
      </div>
    )
  }

  const summary = collection.data
  if (!summary || summary.artists.length === 0) {
    return (
      <EmptyState
        icon="browse"
        title="No coverage yet"
        hint="Open an artist page to map their discography — Reverb remembers it here."
      />
    )
  }

  return (
    <div className="space-y-8">
      {/* Page header */}
      <header>
        <h1 className="text-xl font-extrabold text-text-primary">
          Collection
        </h1>
        <p className="text-sm text-text-secondary">
          {summary.resolvedCount} of {summary.artistCount} artists mapped
        </p>
      </header>

      {/* Artist sections */}
      {summary.artists.map((artist) => (
        <section key={artist.libraryArtistId} className="space-y-3">
          {/* Artist header with coverage */}
          <div className="flex items-center gap-3">
            <Cover
              src={
                artist.coverArtId
                  ? coverUrl(artist.coverArtId, 80)
                  : undefined
              }
              alt={artist.name}
              size={48}
              rounded="full"
            />
            <div className="min-w-0 flex-1">
              <Link
                className={[
                  'font-semibold text-text-primary hover:underline',
                  'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
                ].join(' ')}
                to={`/artist/library/${artist.libraryArtistId}`}
              >
                {artist.name}
              </Link>
              <div className="mt-1 flex items-center gap-2">
                <div className="h-1 flex-1 rounded-full bg-border-subtle">
                  <div
                    className="h-1 rounded-full bg-accent"
                    style={{
                      width: `${
                        artist.totalAlbums
                          ? (artist.ownedAlbums / artist.totalAlbums) * 100
                          : 0
                      }%`,
                    }}
                  />
                </div>
                <span className="text-xs tabular-nums text-text-secondary">
                  {artist.ownedAlbums} of {artist.totalAlbums} albums
                </span>
              </div>
            </div>
          </div>

          {/* Missing albums grid */}
          {artist.missingAlbums.length > 0 && (
            <div className="grid grid-flow-col auto-cols-[10rem] gap-3 overflow-x-auto pb-1">
              {artist.missingAlbums.slice(0, 6).map((album) => {
                const job = Object.values(jobs).find(
                  (j) => j.source === album.source && j.externalId === album.externalId
                )
                return (
                  <MediaCard
                    key={`${album.source}:${album.externalId}`}
                    ghost
                    title={album.name}
                    subtitle={album.year ? String(album.year) : undefined}
                    coverSrc={album.coverUrl}
                    onDownload={
                      canAutoApprove
                        ? () =>
                            void postDownload({
                              source: album.source,
                              externalId: album.externalId,
                              artist: artist.name,
                              title: album.name,
                              album: album.name,
                            })
                              .then((next) => useDownloads.getState().upsert(next))
                              .catch(() => {})
                        : undefined
                    }
                    downloadProgress={
                      job
                        ? {
                            active:
                              job.status === 'queued' ||
                              job.status === 'running',
                            value: job.progress,
                            indeterminate: job.progress < 0,
                          }
                        : undefined
                    }
                  />
                )
              })}
            </div>
          )}
        </section>
      ))}
    </div>
  )
}
