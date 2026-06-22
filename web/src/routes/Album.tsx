import { Link, useParams } from 'react-router-dom'
import { useAlbumDetail } from '../lib/coverageApi'
import { coverUrl } from '../lib/libraryApi'
import { TrackRow } from '../components/ui/TrackRow'
import { DownloadAction } from '../components/download/DownloadAction'
import { postBatchDownload } from '../lib/downloadApi'
import { formatDuration } from '../lib/types'
import type { AlbumDetailTrack, ExternalResult, ExternalTrackRef, Track } from '../lib/types'
import { usePlayer } from '../lib/playerStore'
import { Button, IconButton, Cover, Skeleton, EmptyState } from '../components/ui'
import { useAlbumPalette } from '../lib/useAlbumPalette'
import { rgbToCss } from '../lib/palette'

// ── Local helpers ─────────────────────────────────────────────────────────────

/** Build a display-only Track from a missing AlbumDetailTrack (no library id). */
function asTrack(t: AlbumDetailTrack): Track {
  return {
    id: '',
    title: t.title,
    album: '',
    albumId: '',
    artist: t.artist,
    artistId: '',
    coverArtId: '',
    trackNumber: t.trackNumber,
    discNumber: 1,
    durationMs: t.durationMs,
    bitRate: 0,
    suffix: '',
    contentType: '',
  }
}

/** Build an ExternalResult from an ExternalTrackRef so DownloadAction can drive it. */
function refToExternalResult(ref: ExternalTrackRef, albumName: string, albumArtist: string): ExternalResult {
  return {
    source: ref.source,
    externalId: ref.externalId,
    title: ref.title,
    artist: ref.artist ?? albumArtist,
    album: ref.album ?? albumName,
    durationMs: ref.durationMs,
    isrc: ref.isrc,
    type: 'track',
  }
}

// ── Component ─────────────────────────────────────────────────────────────────

export default function Album() {
  const { source = 'library', id = '' } = useParams()
  const { data: album, isLoading, isError } = useAlbumDetail(source, id)
  const playTrackList = usePlayer((s) => s.playTrackList)
  const toggleShuffle = usePlayer((s) => s.toggleShuffle)
  const shuffle = usePlayer((s) => s.shuffle)

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

  // ── Derived data ────────────────────────────────────────────────────────────

  // Owned tracks in order — used for Play/Shuffle and ownedIndexOf
  const ownedTracks: Track[] = album.tracks
    .filter((t) => t.state === 'full' && t.libraryTrack)
    .map((t) => t.libraryTrack!)

  // Missing externalRefs for batch download
  const missingRefs: ExternalTrackRef[] = album.tracks
    .filter((t) => t.state === 'none' && t.externalRef)
    .map((t) => t.externalRef!)

  const hasMissing = album.ownedCount < album.totalCount

  // Map from libraryTrack id → index within ownedTracks (for per-row onPlay)
  const ownedIndexMap = new Map<string, number>(
    ownedTracks.map((t, i) => [t.id, i]),
  )

  // Cover source: prefer coverArtId proxy, fall back to direct coverUrl
  const coverSrc = album.coverArtId ? coverUrl(album.coverArtId, 300) : album.coverUrl

  const palette = useAlbumPalette(coverSrc)

  // Total duration: sum across all tracks (owned + missing)
  const totalDurationMs = album.tracks.reduce((acc, t) => acc + t.durationMs, 0)

  return (
    <div className="space-y-6">
      {/* Subtle gradient wash behind header */}
      <div
        className="relative -mx-4 -mt-4 px-4 pt-4 pb-6 rounded-b-2xl overflow-hidden bg-gradient-to-b from-raised to-transparent"
        style={palette ? { background: `linear-gradient(to bottom, ${rgbToCss(palette.rgb, 0.55)} 0%, transparent 100%)` } : undefined}
      >
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
              {album.artistId ? (
                <Link
                  to={`/artist/library/${album.artistId}`}
                  className="font-semibold text-text-primary hover:text-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent rounded"
                >
                  {album.artist}
                </Link>
              ) : (
                <span className="font-semibold text-text-primary">{album.artist}</span>
              )}
              {album.year ? <span>· {album.year}</span> : null}
              {album.totalCount ? <span>· {album.totalCount} songs</span> : null}
              {totalDurationMs > 0 ? <span>· {formatDuration(totalDurationMs)}</span> : null}
              {hasMissing ? (
                <span className="text-accent">· {album.ownedCount} of {album.totalCount} in library</span>
              ) : null}
            </div>
            <div className="mt-4 flex items-center gap-3">
              <Button
                variant="primary"
                size="md"
                disabled={ownedTracks.length === 0}
                onClick={() => ownedTracks.length && playTrackList(ownedTracks, 0)}
                aria-label={`Play ${album.name}`}
              >
                Play
              </Button>
              <IconButton
                name="shuffle"
                label={`Shuffle ${album.name}`}
                onClick={() => {
                  if (!ownedTracks.length) return
                  if (!shuffle) toggleShuffle()
                  playTrackList(ownedTracks, 0)
                }}
                disabled={ownedTracks.length === 0}
              />
              {hasMissing && (
                <Button
                  variant="secondary"
                  size="md"
                  onClick={() => postBatchDownload(missingRefs)}
                  aria-label={`Download missing · ${missingRefs.length}`}
                >
                  Download missing · {missingRefs.length}
                </Button>
              )}
              <IconButton name="heart" label={`Like ${album.name}`} />
            </div>
          </div>
        </header>
      </div>

      {/* Track list */}
      <div className="space-y-0.5">
        {album.tracks.map((t, i) => {
          if (t.state === 'full' && t.libraryTrack) {
            const ownedIdx = ownedIndexMap.get(t.libraryTrack.id) ?? 0
            return (
              <TrackRow
                key={t.libraryTrack.id}
                track={t.libraryTrack}
                index={i}
                onPlay={() => playTrackList(ownedTracks, ownedIdx)}
                coverSrc={t.libraryTrack.coverArtId ? undefined : t.coverUrl}
              />
            )
          }

          // Fallback: any other state (none, partial, pending, or unexpected) renders a
          // non-playable row so no track ever silently vanishes from the list.
          const displayTrack = asTrack(t)
          const right = t.externalRef
            ? <DownloadAction result={refToExternalResult(t.externalRef, album.name, album.artist)} />
            : undefined
          return (
            <TrackRow
              key={t.libraryTrack?.id ?? t.externalRef?.externalId ?? i}
              track={displayTrack}
              index={i}
              onPlay={() => {}}
              coverSrc={t.coverUrl ?? album.coverUrl}
              right={right}
              rightWidth={right ? '120px' : undefined}
            />
          )
        })}
        {album.tracks.length === 0 && (
          <EmptyState icon="browse" title="No tracks in this album" />
        )}
      </div>
    </div>
  )
}
