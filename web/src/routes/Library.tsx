import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAlbums, useArtists, useLibraryStatus } from '../lib/libraryApi'
import { useSyncedPlaylists } from '../lib/syncedPlaylistApi'
import { Chip, MediaCard, Skeleton, EmptyState, Button } from '../components/ui'
import { ImportPlaylistDialog } from '../components/ImportPlaylistDialog'
import { useDocumentTitle } from '../lib/useDocumentTitle'

type Filter = 'albums' | 'artists' | 'playlists'

const FILTERS: { key: Filter; label: string }[] = [
  { key: 'albums', label: 'Albums' },
  { key: 'artists', label: 'Artists' },
  { key: 'playlists', label: 'Playlists' },
]

const SKELETON_COUNT = 10

function SkeletonGrid({ rounded = 'md' }: { rounded?: 'md' | 'full' }) {
  return (
    <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
      {Array.from({ length: SKELETON_COUNT }).map((_, i) => (
        <div key={i} className="space-y-2 p-3 rounded-lg bg-raised">
          <Skeleton
            className="aspect-square w-full"
            rounded={rounded}
            data-testid="skeleton-cover"
          />
          <Skeleton className="h-3 w-3/4" />
          <Skeleton className="h-2 w-1/2" />
        </div>
      ))}
    </div>
  )
}

/** Distinct error state for a failed library query — separate from the "empty
 *  library" message so an outage never reads as "you have no music". */
function LibraryError({ onRetry }: { onRetry: () => void }) {
  return (
    <EmptyState
      icon="warn"
      title="Couldn't load your library"
      hint="Something went wrong reaching the server. Check your connection and try again."
      action={
        <Button size="sm" variant="secondary" onClick={onRetry}>
          Retry
        </Button>
      }
    />
  )
}

export default function Library() {
  useDocumentTitle('Library')
  const [filter, setFilter] = useState<Filter>('albums')
  const [importOpen, setImportOpen] = useState(false)
  const navigate = useNavigate()

  const albums = useAlbums('newest')
  const artists = useArtists()
  const syncedPlaylists = useSyncedPlaylists()
  const libStatus = useLibraryStatus()

  return (
    <div className="space-y-6">
      {/* Library status banners */}
      {libStatus.data?.state === 'starting' && (
        <div className="rounded-lg border border-border-subtle bg-raised px-4 py-2 text-sm text-text-secondary">
          Library starting… the bundled music server is coming up.
        </div>
      )}
      {libStatus.data?.state === 'degraded' && (
        <div className="rounded-lg border border-error/40 bg-error/10 px-4 py-2 text-sm text-error">
          Library unavailable — the bundled server failed to start. Check logs or switch to an external server in Settings.
        </div>
      )}

      {/* Page header */}
      <div className="flex items-center justify-between gap-3">
        <h1 className="text-2xl font-bold text-text-primary">Your Library</h1>
        <div className="flex items-center gap-2">
          {/* Collection is a lens on the library (what you're missing), so it
              lives here rather than as a standalone rail entry. */}
          <Button
            size="sm"
            variant="ghost"
            aria-label="Open collection"
            onClick={() => navigate('/collection')}
          >
            Collection
          </Button>
          <Button
            size="sm"
            variant="secondary"
            aria-label="Import from Spotify"
            onClick={() => setImportOpen(true)}
          >
            + Import from Spotify
          </Button>
        </div>
      </div>

      {/* Filter chips */}
      <div className="flex gap-2 flex-wrap" role="group" aria-label="Library filter">
        {FILTERS.map(({ key, label }) => (
          <Chip
            key={key}
            selected={filter === key}
            onClick={() => setFilter(key)}
          >
            {label}
          </Chip>
        ))}
      </div>

      {/* Albums grid */}
      {filter === 'albums' && (
        <>
          {albums.isLoading ? (
            <SkeletonGrid rounded="md" />
          ) : albums.isError ? (
            <LibraryError onRetry={() => void albums.refetch()} />
          ) : (albums.data ?? []).length === 0 ? (
            <EmptyState
              icon="browse"
              title="Nothing here yet"
              hint="Download some music to start building your library."
            />
          ) : (
            <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
              {(albums.data ?? []).map((al) => (
                <MediaCard
                  key={al.id}
                  title={al.name}
                  subtitle={al.artist}
                  coverId={al.coverArtId || undefined}
                  rounded="md"
                  onClick={() => navigate(`/album/library/${al.id}`)}
                />
              ))}
            </div>
          )}
        </>
      )}

      {/* Artists grid */}
      {filter === 'artists' && (
        <>
          {artists.isLoading ? (
            <SkeletonGrid rounded="full" />
          ) : artists.isError ? (
            <LibraryError onRetry={() => void artists.refetch()} />
          ) : (artists.data ?? []).length === 0 ? (
            <EmptyState
              icon="browse"
              title="Nothing here yet"
              hint="Download some music to start building your library."
            />
          ) : (
            <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
              {(artists.data ?? []).map((ar) => (
                <MediaCard
                  key={ar.id}
                  title={ar.name}
                  coverId={ar.coverArtId || undefined}
                  rounded="full"
                  onClick={() => navigate(`/artist/library/${ar.id}`)}
                />
              ))}
            </div>
          )}
        </>
      )}

      {/* Playlists grid — all managed playlists */}
      {filter === 'playlists' && (
        <>
          {syncedPlaylists.isLoading ? (
            <SkeletonGrid rounded="md" />
          ) : syncedPlaylists.isError ? (
            <LibraryError onRetry={() => void syncedPlaylists.refetch()} />
          ) : (syncedPlaylists.data ?? []).length === 0 ? (
            <EmptyState
              icon="browse"
              title="Nothing here yet"
              hint="Create a playlist or download some music to get started."
            />
          ) : (
            <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
              {(syncedPlaylists.data ?? []).map((pl) => (
                <MediaCard
                  key={pl.id}
                  title={pl.name}
                  subtitle={`${pl.trackCount} track${pl.trackCount !== 1 ? 's' : ''}`}
                  coverSrc={pl.coverUrl}
                  rounded="md"
                  onClick={() => navigate(`/playlist/${pl.id}`)}
                />
              ))}
            </div>
          )}
        </>
      )}

      <ImportPlaylistDialog open={importOpen} onClose={() => setImportOpen(false)} />
    </div>
  )
}
