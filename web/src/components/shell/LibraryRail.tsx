import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { IconButton, Chip, Cover, Skeleton, EmptyState, Equalizer, Icon } from '../ui'
import { useArtists, useAlbums, coverUrl, createPlaylist } from '../../lib/libraryApi'
import { useSyncedPlaylists } from '../../lib/syncedPlaylistApi'
import { usePlayer } from '../../lib/playerStore'
import { ImportPlaylistDialog } from '../ImportPlaylistDialog'
import type { Album, Artist, SyncedPlaylist } from '../../lib/types'

type Filter = 'playlists' | 'albums' | 'artists'

export function LibraryRail() {
  const navigate = useNavigate()
  const qc = useQueryClient()
  const [filter, setFilter] = useState<Filter>('playlists')
  const [creating, setCreating] = useState(false)
  const [importOpen, setImportOpen] = useState(false)
  const current = usePlayer((s) => s.current)

  const albums = useAlbums()
  const artists = useArtists()
  const syncedPlaylists = useSyncedPlaylists()

  // Derive which query is active
  const activeQuery =
    filter === 'playlists' ? syncedPlaylists :
    filter === 'albums' ? albums :
    artists

  const isLoading = activeQuery.isLoading

  // Create a managed playlist, then navigate to it.
  async function handleCreatePlaylist() {
    if (creating) return
    setCreating(true)
    try {
      const pl = await createPlaylist('New Playlist')
      await qc.invalidateQueries({ queryKey: ['synced-playlists'] })
      setFilter('playlists')
      navigate(`/synced-playlist/${pl.id}`)
    } catch {
      // Needs a connected library provider; nothing to do otherwise.
    } finally {
      setCreating(false)
    }
  }

  return (
    <aside className="flex flex-col h-full min-h-0 bg-surface rounded-lg overflow-hidden">
      {/* Header — "Your Library" opens the full library page */}
      <div className="px-4 pt-4 pb-2">
        <div className="flex items-center">
          <button
            type="button"
            onClick={() => navigate('/library')}
            aria-label="Open your library"
            className="flex items-center gap-2.5 font-bold text-base text-text-secondary hover:text-text-primary transition-colors rounded focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
          >
            <Icon name="browse" className="w-4 h-4" />
            Your Library
          </button>
          <div className="ml-auto flex gap-1.5 text-text-secondary">
            <IconButton
              name="dl"
              label="Import from Spotify"
              size="sm"
              onClick={() => setImportOpen(true)}
            />
            <IconButton
              name="plus"
              label="Create playlist"
              size="sm"
              disabled={creating}
              onClick={() => void handleCreatePlaylist()}
            />
          </div>
        </div>
      </div>

      {/* Filter chips */}
      <div className="flex gap-2 px-4 pb-3 flex-wrap">
        <Chip selected={filter === 'playlists'} onClick={() => setFilter('playlists')}>
          Playlists
        </Chip>
        <Chip selected={filter === 'albums'} onClick={() => setFilter('albums')}>
          Albums
        </Chip>
        <Chip selected={filter === 'artists'} onClick={() => setFilter('artists')}>
          Artists
        </Chip>
      </div>

      {/* List */}
      <div className="flex-1 overflow-y-auto min-h-0 px-2 pb-4">
        {isLoading ? (
          <SkeletonRows />
        ) : filter === 'playlists' ? (
          <PlaylistList items={syncedPlaylists.data ?? []} />
        ) : filter === 'albums' ? (
          <AlbumList items={albums.data ?? []} current={current} />
        ) : (
          <ArtistList items={artists.data ?? []} current={current} />
        )}
      </div>

      <ImportPlaylistDialog open={importOpen} onClose={() => setImportOpen(false)} />
    </aside>
  )
}

// ---- Skeleton rows ----
function SkeletonRows() {
  return (
    <>
      {Array.from({ length: 5 }, (_, i) => (
        <div key={i} className="flex items-center gap-3 p-2 mb-1">
          <Skeleton data-testid="lib-skeleton" className="w-12 h-12 flex-none" />
          <div className="flex-1 space-y-2">
            <Skeleton className="h-3.5 w-3/4" />
            <Skeleton className="h-3 w-1/2" />
          </div>
        </div>
      ))}
    </>
  )
}

// ---- Managed playlist list ----
function PlaylistList({ items }: { items: SyncedPlaylist[] }) {
  const navigate = useNavigate()
  if (items.length === 0) {
    return <EmptyState icon="queue" title="No playlists yet" hint="Create your first playlist to get started." />
  }

  return (
    <>
      {items.map((p) => (
        <LibItem
          key={p.id}
          coverSrc={p.coverUrl ?? ''}
          name={p.name}
          meta={`Playlist · ${p.trackCount} tracks`}
          rounded="md"
          isPlaying={false}
          onClick={() => navigate(`/synced-playlist/${p.id}`)}
        />
      ))}
    </>
  )
}

// ---- Album list ----
function AlbumList({ items, current }: { items: Album[]; current: ReturnType<typeof usePlayer.getState>['current'] }) {
  const navigate = useNavigate()
  if (items.length === 0) {
    return <EmptyState icon="browse" title="No albums yet" hint="Your downloaded albums will appear here." />
  }

  return (
    <>
      {items.map((al) => {
        const isPlaying = !!current && current.albumId === al.id
        return (
          <LibItem
            key={al.id}
            coverSrc={coverUrl(al.coverArtId)}
            name={al.name}
            meta={`Album · ${al.artist}`}
            rounded="md"
            isPlaying={isPlaying}
            onClick={() => navigate(`/album/library/${al.id}`)}
          />
        )
      })}
    </>
  )
}

// ---- Artist list ----
function ArtistList({ items, current }: { items: Artist[]; current: ReturnType<typeof usePlayer.getState>['current'] }) {
  const navigate = useNavigate()
  if (items.length === 0) {
    return <EmptyState icon="mic" title="No artists yet" hint="Artists from your downloads appear here." />
  }

  return (
    <>
      {items.map((ar) => {
        const isPlaying = !!current && current.artistId === ar.id
        return (
          <LibItem
            key={ar.id}
            coverSrc={coverUrl(ar.coverArtId)}
            name={ar.name}
            meta={`Artist · ${ar.albumCount} album${ar.albumCount !== 1 ? 's' : ''}`}
            rounded="full"
            isPlaying={isPlaying}
            onClick={() => navigate(`/artist/library/${ar.id}`)}
          />
        )
      })}
    </>
  )
}

// ---- Shared row ----
interface LibItemProps {
  coverSrc: string
  name: string
  meta: string
  rounded: 'md' | 'full'
  isPlaying: boolean
  onClick?: () => void
}

function LibItem({ coverSrc, name, meta, rounded, isPlaying, onClick }: LibItemProps) {
  const inner = (
    <>
      <Cover src={coverSrc} alt={name} size={48} rounded={rounded} />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-1.5">
          <div
            className={[
              'text-sm font-semibold truncate',
              isPlaying ? 'text-accent' : 'text-text-primary',
            ].join(' ')}
          >
            {name}
          </div>
        </div>
        <div className="flex items-center gap-1.5 mt-0.5">
          {isPlaying && <Equalizer />}
          <span className="text-xs text-text-secondary truncate">{meta}</span>
        </div>
      </div>
    </>
  )

  if (onClick) {
    return (
      <button
        type="button"
        onClick={onClick}
        aria-label={name}
        className={[
          'w-full flex items-center gap-3 p-2 rounded-md cursor-pointer transition-colors text-left',
          'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
          isPlaying ? 'bg-raised' : 'hover:bg-raised',
        ].join(' ')}
      >
        {inner}
      </button>
    )
  }

  return (
    <div
      className={[
        'flex items-center gap-3 p-2 rounded-md transition-colors',
        isPlaying ? 'bg-raised' : '',
      ].join(' ')}
    >
      {inner}
    </div>
  )
}
