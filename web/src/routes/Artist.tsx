import { useState, useMemo } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useArtistDetail } from '../lib/coverageApi'
import { useCoverageStream } from '../lib/coverageStore'
import { postBatchDownload } from '../lib/downloadApi'
import { coverUrl } from '../lib/libraryApi'
import { Cover, Skeleton, EmptyState, MediaCard } from '../components/ui'
import { Chip } from '../components/ui/Chip'
import { Button } from '../components/ui/Button'
import type { DiscographyAlbum } from '../lib/types'

type KindFilter = 'all' | 'album' | 'single'

export default function Artist() {
  const { source = 'library', id = '' } = useParams()
  const { data: detail, isLoading, isError } = useArtistDetail(source, id)
  const coverage = useCoverageStream(source, id, detail?.resolved === true)
  const navigate = useNavigate()
  const [filter, setFilter] = useState<KindFilter>('all')

  // Aggregate all missing tracks across all albums for "Download all missing".
  // Hoisted above the early returns so the hook order stays stable across renders
  // (loading → loaded): calling useMemo only after a conditional return triggers
  // React error #310 ("rendered more hooks than during the previous render").
  const allMissingTracks = useMemo(
    () => Object.values(coverage).flatMap((c) => c.missingTracks),
    [coverage],
  )

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

  if (isError || !detail) {
    return (
      <EmptyState
        icon="browse"
        title="Artist not found"
        hint="This artist may have been removed from your library."
      />
    )
  }

  // Resolve artist cover image: prefer library proxy, fall back to direct URL
  const coverSrc = detail.coverArtId
    ? coverUrl(detail.coverArtId, 300)
    : detail.coverUrl

  const albums = detail.albums ?? []

  // Compute stat counts from the coverage map
  const fullCount = albums.filter(
    (a) => coverage[a.externalId]?.state === 'full',
  ).length
  const partialCount = albums.filter(
    (a) => coverage[a.externalId]?.state === 'partial',
  ).length
  const missingCount = albums.filter(
    (a) => coverage[a.externalId]?.state === 'none',
  ).length
  const hasPending = detail.resolved && albums.some(
    (a) => !coverage[a.externalId] || coverage[a.externalId].state === 'pending',
  )

  // Filter albums by kind
  const visibleAlbums: DiscographyAlbum[] = albums.filter((a) => {
    if (filter === 'all') return true
    if (filter === 'album') return a.kind === 'album'
    return a.kind === 'single'
  })

  return (
    <div className="space-y-8">
      {/* Artist header */}
      <header className="flex items-end gap-6 pt-4">
        <Cover
          src={coverSrc}
          alt={detail.name}
          size={188}
          rounded="full"
          className="shadow-cover flex-none"
        />
        <div className="min-w-0 pb-1">
          <div className="text-xs font-semibold uppercase tracking-widest text-text-muted mb-1">
            Artist
          </div>
          <h1 className="text-5xl font-black leading-tight tracking-tight text-text-primary truncate">
            {detail.name}
          </h1>

          {/* Stat line */}
          <p className="mt-2 text-sm text-text-secondary flex flex-wrap items-center gap-x-1">
            <span>
              {fullCount} of {albums.length} {albums.length === 1 ? 'album' : 'albums'} in your library
            </span>
            {detail.resolved && partialCount > 0 && (
              <>
                <span className="text-text-muted">·</span>
                <span className="text-accent">{partialCount} partial</span>
              </>
            )}
            {detail.resolved && missingCount > 0 && (
              <>
                <span className="text-text-muted">·</span>
                <span className="text-text-muted">{missingCount} missing</span>
              </>
            )}
            {hasPending && (
              <>
                <span className="text-text-muted">·</span>
                <span className="text-text-muted">checking…</span>
              </>
            )}
          </p>

          {/* Action row — only when there are missing tracks. Guarded by a confirm
              so a stray click can't enqueue a large batch (spec §10). */}
          {detail.resolved && allMissingTracks.length > 0 && (
            <div className="mt-4">
              <Button
                variant="secondary"
                size="md"
                onClick={() => {
                  if (
                    window.confirm(
                      `Download ${allMissingTracks.length} missing tracks?`,
                    )
                  ) {
                    postBatchDownload(allMissingTracks)
                  }
                }}
              >
                Download all missing · {allMissingTracks.length}
              </Button>
            </div>
          )}
        </div>
      </header>

      {/* Kind filters */}
      <div className="flex items-center gap-2">
        <Chip selected={filter === 'all'} onClick={() => setFilter('all')}>
          All
        </Chip>
        <Chip selected={filter === 'album'} onClick={() => setFilter('album')}>
          Albums
        </Chip>
        <Chip selected={filter === 'single'} onClick={() => setFilter('single')}>
          Singles &amp; EPs
        </Chip>
      </div>

      {/* Discography grid */}
      <section>
        {visibleAlbums.length > 0 ? (
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
            {visibleAlbums.map((al) => {
              const cov = coverage[al.externalId]
              const hasMissing = detail.resolved && cov && cov.missingTracks.length > 0
              return (
                <MediaCard
                  key={al.externalId}
                  title={al.name}
                  subtitle={String(al.year)}
                  coverSrc={al.coverUrl}
                  rounded="md"
                  coverage={
                    detail.resolved
                      ? {
                          state: cov?.state ?? 'pending',
                          owned: cov?.ownedCount ?? 0,
                          total: cov?.totalCount || al.totalTracks,
                        }
                      : undefined
                  }
                  onDownload={
                    hasMissing ? () => postBatchDownload(cov.missingTracks) : undefined
                  }
                  onClick={() => navigate(`/album/${al.source}/${al.externalId}`)}
                />
              )
            })}
          </div>
        ) : (
          <EmptyState icon="browse" title="No albums" />
        )}
      </section>
    </div>
  )
}
