import { useState, useMemo, useCallback, useEffect } from 'react'
import { createPortal } from 'react-dom'
import { useNavigate, useParams } from 'react-router-dom'
import { useArtistDetail } from '../lib/coverageApi'
import { useCoverageStream } from '../lib/coverageStore'
import { postBatchDownload } from '../lib/downloadApi'
import { useDownloads } from '../lib/downloadStore'
import { coverUrl } from '../lib/libraryApi'
import { postBatchRequest } from '../lib/requestApi'
import { useAuthStore } from '../lib/authStore'
import { useToastStore } from '../lib/toastStore'
import { Cover, Skeleton, EmptyState, MediaCard } from '../components/ui'
import { Chip } from '../components/ui/Chip'
import { Button } from '../components/ui/Button'
import type { AlbumCoverage, DiscographyAlbum } from '../lib/types'
import { useAlbumPalette } from '../lib/useAlbumPalette'
import { rgbToCss } from '../lib/palette'
import * as statsApi from '../lib/statsApi'
import type { EntityStats } from '../lib/statsApi'
import { presetRange, msToHuman } from '../lib/range'
import { useDocumentTitle } from '../lib/useDocumentTitle'

type KindFilter = 'all' | 'album' | 'single'

// ---------------------------------------------------------------------------
// AlbumCard — per-card component so each card has its own hook subscriptions.
// ---------------------------------------------------------------------------

interface AlbumCardProps {
  album: DiscographyAlbum
  cov: AlbumCoverage | undefined
  resolved: boolean
  canAutoApprove: boolean
  onNavigate: () => void
}

function AlbumCard({ album, cov, resolved, canAutoApprove, onNavigate }: AlbumCardProps) {
  const [optimistic, setOptimistic] = useState(false)

  // Build the set of externalIds for this album's missing tracks so we can
  // look them up in the download store without storing derived data.
  const missingIds = useMemo(
    () => new Set((cov?.missingTracks ?? []).map((t) => t.externalId)),
    [cov?.missingTracks],
  )

  // Subscribe to the download store, aggregating state across all missing-track jobs.
  const downloadState = useDownloads(
    useCallback(
      (s) => {
        const jobs = Object.values(s.jobs).filter(
          (j) => j.source === album.source && missingIds.has(j.externalId),
        )
        const activeJobs = jobs.filter(
          (j) => j.status === 'queued' || j.status === 'running',
        )
        const runningJobs = jobs.filter((j) => j.status === 'running')
        const active = optimistic || activeJobs.length > 0
        const value =
          runningJobs.length > 0
            ? runningJobs.reduce((sum, j) => sum + j.progress, 0) / runningJobs.length
            : 0
        const indeterminate = active && (activeJobs.length === 0 || runningJobs.every((j) => j.progress <= 0))
        return { active, value, indeterminate }
      },
      [album.source, missingIds, optimistic],
    ),
  )

  const hasMissing = resolved && cov && cov.missingTracks.length > 0

  function handleDownload() {
    if (!cov) return
    setOptimistic(true)
    void postBatchDownload(cov.missingTracks).finally(() => setOptimistic(false))
  }

  return (
    <MediaCard
      title={album.name}
      subtitle={album.year ? String(album.year) : undefined}
      coverSrc={album.coverUrl}
      rounded="md"
      coverage={
        resolved
          ? {
              state: cov?.state ?? 'pending',
              owned: cov?.ownedCount ?? 0,
              total: cov?.totalCount || album.totalTracks,
            }
          : undefined
      }
      ghost={!album.libraryAlbumId}
      onDownload={canAutoApprove && hasMissing && !downloadState.active ? handleDownload : undefined}
      downloadProgress={downloadState.active ? downloadState : undefined}
      onClick={onNavigate}
    />
  )
}

export default function Artist() {
  const { source = 'library', id = '' } = useParams()
  const { data: detail, isLoading, isError } = useArtistDetail(source, id)
  useDocumentTitle(detail?.name ?? 'Artist')
  const coverage = useCoverageStream(source, id, detail?.resolved === true)
  const navigate = useNavigate()
  const [filter, setFilter] = useState<KindFilter>('all')
  const canRequest = useAuthStore((s) => s.can('request'))
  const canAutoApprove = useAuthStore((s) => s.can('auto_approve'))
  const [bulkSubmitting, setBulkSubmitting] = useState(false)
  const [requestAllOpen, setRequestAllOpen] = useState(false)

  // Per-entity listening stats — fetched once detail.name is known
  // DEFERRED: album-page strip needs backend entity kind="album"; per-track "played N×"
  //   needs a per-track-counts endpoint. Both are small follow-ups outside this task.
  const [entityStats, setEntityStats] = useState<EntityStats | null>(null)
  useEffect(() => {
    if (!detail?.name) return
    statsApi.entity('artist', detail.name, presetRange('all'))
      .then(setEntityStats)
      .catch(() => setEntityStats(null))
  }, [detail?.name])

  // Aggregate all missing tracks across all albums for "Download all missing".
  // Hoisted above the early returns so the hook order stays stable across renders
  // (loading → loaded): calling useMemo only after a conditional return triggers
  // React error #310 ("rendered more hooks than during the previous render").
  const allMissingTracks = useMemo(
    () => Object.values(coverage).flatMap((c) => c.missingTracks),
    [coverage],
  )

  // Build the list of not-fully-owned albums for "Request all".
  // An album is not fully owned when: no coverage entry, or ownedCount < totalCount.
  const notOwnedRequestItems = useMemo(() => {
    if (!detail) return []
    return detail.albums
      .filter((album) => {
        const cov = coverage[album.externalId]
        if (!cov) return true // no coverage → treat as not owned
        const total = cov.totalCount > 0 ? cov.totalCount : album.totalTracks
        return cov.ownedCount < total
      })
      .map((album) => ({
        kind: 'album' as const,
        source: album.source,
        externalId: album.externalId,
        title: album.name,
        album: album.name,
        artist: detail.name,
        coverUrl: album.coverUrl,
        trackCount: album.totalTracks,
      }))
  }, [detail, coverage])

  const palette = useAlbumPalette(detail?.coverArtId ? coverUrl(detail.coverArtId, 300) : detail?.coverUrl)

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
      {/* Subtle gradient wash behind header */}
      <div
        className="relative -mx-4 -mt-4 px-4 pt-4 pb-6 rounded-b-2xl overflow-hidden bg-gradient-to-b from-raised to-transparent"
        style={palette ? { background: `linear-gradient(to bottom, ${rgbToCss(palette.rgb, 0.55)} 0%, transparent 100%)` } : undefined}
      >
        {/* Artist header */}
        <header className="relative z-10 flex items-end gap-6 pt-2">
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

            {/* Listening history stat strip — hidden when Plays === 0 */}
            {entityStats !== null && entityStats.Plays > 0 && (
              <p className="mt-1 text-xs text-text-secondary flex flex-wrap items-center gap-x-1" data-testid="artist-stat-strip">
                <span>you</span>
                <span className="text-text-muted">·</span>
                <span>{entityStats.Plays} plays</span>
                <span className="text-text-muted">·</span>
                <span>{msToHuman(entityStats.MsPlayed)} listened</span>
                {entityStats.FirstPlayed > 0 && (
                  <>
                    <span className="text-text-muted">·</span>
                    <span>since {new Date(entityStats.FirstPlayed * 1000).getFullYear()}</span>
                  </>
                )}
              </p>
            )}

            {/* Acquisition action row — ONE capability-gated, mutually-exclusive
                button. auto_approve → "Download all missing" (direct); else request
                → "Request all" (pending); else neither. A user with both caps sees
                only Download (the auto_approve branch wins). The download path is
                guarded by a confirm so a stray click can't enqueue a large batch
                (spec §10). */}
            {(canAutoApprove && detail.resolved && allMissingTracks.length > 0) ||
            (!canAutoApprove && canRequest && notOwnedRequestItems.length > 0) ? (
              <div className="mt-4 flex items-center gap-3">
                {canAutoApprove && detail.resolved && allMissingTracks.length > 0 && (
                  <Button
                    variant="secondary"
                    size="md"
                    disabled={bulkSubmitting}
                    onClick={() => {
                      if (
                        window.confirm(
                          `Download ${allMissingTracks.length} missing tracks?`,
                        )
                      ) {
                        setBulkSubmitting(true)
                        void postBatchDownload(allMissingTracks).finally(() => setBulkSubmitting(false))
                      }
                    }}
                  >
                    {bulkSubmitting ? 'Starting downloads…' : `Download all missing · ${allMissingTracks.length}`}
                  </Button>
                )}
                {!canAutoApprove && canRequest && notOwnedRequestItems.length > 0 && (
                  <Button
                    variant="secondary"
                    size="md"
                    aria-label="Request all"
                    onClick={() => setRequestAllOpen(true)}
                  >
                    Request all · {notOwnedRequestItems.length}
                  </Button>
                )}
              </div>
            ) : null}
          </div>
        </header>
      </div>

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

      {/* In your library */}
      {detail.libraryAlbums && detail.libraryAlbums.length > 0 && (
        <section data-testid="library-albums-section">
          <h2 className="text-base font-bold text-text-primary mb-4">In your library</h2>
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
            {detail.libraryAlbums.map((al) => (
              <MediaCard
                key={al.libraryAlbumId ?? al.externalId}
                title={al.name}
                subtitle={al.year ? String(al.year) : undefined}
                coverSrc={al.coverUrl || undefined}
                coverId={al.libraryAlbumId}
                rounded="md"
                onClick={() =>
                  navigate(`/album/library/${al.libraryAlbumId ?? al.externalId}`)
                }
              />
            ))}
          </div>
        </section>
      )}

      {/* Discography grid */}
      <section>
        {visibleAlbums.length > 0 ? (
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
            {visibleAlbums.map((al) => (
              <AlbumCard
                key={al.externalId}
                album={al}
                cov={coverage[al.externalId]}
                resolved={detail.resolved}
                canAutoApprove={canAutoApprove}
                onNavigate={() =>
                navigate(
                  al.libraryAlbumId
                    ? `/album/library/${al.libraryAlbumId}`
                    : `/album/${al.source}/${al.externalId}`,
                )
              }
              />
            ))}
          </div>
        ) : (
          <EmptyState icon="browse" title="No albums" />
        )}
      </section>

      {/* Request all disclosure modal */}
      {requestAllOpen &&
        createPortal(
          <>
            <div
              className="fixed inset-0 z-40"
              aria-hidden="true"
              onClick={() => setRequestAllOpen(false)}
            />
            <div
              role="dialog"
              aria-modal="true"
              aria-label="Request all albums"
              className="fixed left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 z-50 w-80 max-w-[calc(100vw-2rem)] rounded-xl border border-border-subtle bg-raised p-4 shadow-pop"
              onClick={(e) => e.stopPropagation()}
            >
              <p className="text-sm font-bold text-text-primary">
                Request all {notOwnedRequestItems.length} album{notOwnedRequestItems.length === 1 ? '' : 's'} by {detail.name} not in your library?
              </p>
              <p className="mt-1 text-xs text-text-secondary">
                Each album will be queued for download once approved.
              </p>
              <div className="mt-3 flex justify-end gap-2">
                <Button
                  variant="ghost"
                  size="sm"
                  aria-label="Cancel"
                  onClick={() => setRequestAllOpen(false)}
                >
                  Cancel
                </Button>
                <Button
                  variant="primary"
                  size="sm"
                  aria-label="Confirm request all"
                  onClick={() => {
                    setRequestAllOpen(false)
                    postBatchRequest(notOwnedRequestItems)
                      .then((result) => {
                        const skippedNote = result.skipped > 0 ? ` (${result.skipped} already requested)` : ''
                        const cappedNote = result.quotaCapped > 0 ? ` — ${result.quotaCapped} not requested (limit reached)` : ''
                        const msg = canAutoApprove
                          ? `Requested ${result.created} album${result.created === 1 ? '' : 's'}${skippedNote}${cappedNote}`
                          : `Requested ${result.created} album${result.created === 1 ? '' : 's'} — pending approval${skippedNote}${cappedNote}`
                        useToastStore.getState().push(msg, 'success')
                      })
                      .catch((err) => {
                        console.error('[Artist] postBatchRequest failed:', err)
                        useToastStore.getState().push("Couldn't file your requests", 'error')
                      })
                  }}
                >
                  Confirm
                </Button>
              </div>
            </div>
          </>,
          document.body,
        )}
    </div>
  )
}
