import { useMemo, useState } from 'react'
import { PageHeader } from '../components/ui/PageHeader'
import { useNavigate } from 'react-router-dom'
import { useAlbums, useArtists, useLibraryStatus } from '../lib/libraryApi'
import { useSyncedPlaylists } from '../lib/syncedPlaylistApi'
import { Chip, MediaCard, Skeleton, EmptyState, Button, Select, Icon } from '../components/ui'
import { ImportPlaylistDialog } from '../components/ImportPlaylistDialog'
import { useDocumentTitle } from '../lib/useDocumentTitle'

type Filter = 'albums' | 'artists' | 'playlists'

// Same order as the library rail, so the two never disagree.
const FILTERS: { key: Filter; label: string }[] = [
  { key: 'playlists', label: 'Playlists' },
  { key: 'albums', label: 'Albums' },
  { key: 'artists', label: 'Artists' },
]

type AlbumSort = 'newest' | 'title' | 'artist'

const ALBUM_SORTS: { value: AlbumSort; label: string }[] = [
  { value: 'newest', label: 'Recently added' },
  { value: 'title', label: 'Title' },
  { value: 'artist', label: 'Artist' },
]

const byText = (a: string, b: string) => a.localeCompare(b, undefined, { sensitivity: 'base' })

/** Shown when the filter box excludes everything (distinct from an empty library). */
function NoMatches({ query }: { query: string }) {
  return <EmptyState icon="search" title="No matches" hint={`Nothing in this view matches "${query}".`} />
}

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
  const [query, setQuery] = useState('')
  const [albumSort, setAlbumSort] = useState<AlbumSort>('newest')

  const q = query.trim().toLowerCase()
  const matches = (...fields: (string | undefined)[]) =>
    q === '' || fields.some((f) => f?.toLowerCase().includes(q))

  const albumList = useMemo(() => {
    const list = (albums.data ?? []).filter((al) => matches(al.name, al.artist))
    if (albumSort === 'title') return [...list].sort((a, b) => byText(a.name, b.name))
    if (albumSort === 'artist') return [...list].sort((a, b) => byText(a.artist, b.artist) || byText(a.name, b.name))
    return list // API order is newest first
    // eslint-disable-next-line react-hooks/exhaustive-deps -- `matches` only depends on q
  }, [albums.data, albumSort, q])
  const artistList = (artists.data ?? []).filter((ar) => matches(ar.name))
  const playlistList = (syncedPlaylists.data ?? []).filter((pl) => matches(pl.name))

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
      <PageHeader
        title="Your Library"
        actions={
          <>
          {/* Discographies is a lens on the library (what you're missing), so
              it lives here rather than as a standalone rail entry. */}
          <Button
            size="sm"
            variant="ghost"
            aria-label="Open discographies"
            onClick={() => navigate('/discographies')}
          >
            Discographies
          </Button>
          <Button
            size="sm"
            variant="secondary"
            aria-label="Import from Spotify"
            onClick={() => setImportOpen(true)}
          >
            + Import from Spotify
          </Button>
          </>
        }
      />

      {/* View chips, then find-in-view and sort */}
      <div className="flex flex-wrap items-center gap-x-4 gap-y-3">
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
        <div className="ml-auto flex items-center gap-3">
          <label className="flex h-9 w-44 items-center gap-2 rounded-full bg-input px-3 focus-within:ring-2 focus-within:ring-accent sm:w-56">
            <Icon name="search" className="h-3.5 w-3.5 flex-none text-text-secondary" />
            <input
              type="search"
              aria-label={`Filter ${filter}`}
              placeholder={`Filter ${filter}`}
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              className="w-full min-w-0 bg-transparent text-sm text-text-primary outline-none placeholder:text-text-secondary"
            />
          </label>
          {filter === 'albums' && (
            <Select
              label="Sort albums"
              value={albumSort}
              options={ALBUM_SORTS}
              onChange={(v) => setAlbumSort(v as AlbumSort)}
            />
          )}
        </div>
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
          ) : albumList.length === 0 ? (
            <NoMatches query={query.trim()} />
          ) : (
            <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
              {albumList.map((al) => (
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
          ) : artistList.length === 0 ? (
            <NoMatches query={query.trim()} />
          ) : (
            <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
              {artistList.map((ar) => (
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
          ) : playlistList.length === 0 ? (
            <NoMatches query={query.trim()} />
          ) : (
            <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
              {playlistList.map((pl) => (
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
