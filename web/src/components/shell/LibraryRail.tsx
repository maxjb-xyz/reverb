import { useState } from 'react'
import { NavLink, useNavigate } from 'react-router-dom'
import { IconButton, Chip, Cover, Skeleton, EmptyState, Equalizer, Icon } from '../ui'
import { usePlaylists, useArtists, useAlbums, coverUrl } from '../../lib/libraryApi'
import { usePlayer } from '../../lib/playerStore'
import type { Playlist, Album, Artist } from '../../lib/types'

type Filter = 'playlists' | 'albums' | 'artists'

const NAV_ITEMS = [
  { to: '/search', label: 'Search' },
  { to: '/library', label: 'Library' },
  { to: '/settings', label: 'Settings' },
]

export function LibraryRail() {
  const [filter, setFilter] = useState<Filter>('playlists')
  const current = usePlayer((s) => s.current)

  const playlists = usePlaylists()
  const albums = useAlbums()
  const artists = useArtists()

  // Derive which query is active
  const activeQuery =
    filter === 'playlists' ? playlists :
    filter === 'albums' ? albums :
    artists

  const isLoading = activeQuery.isLoading

  return (
    <aside className="flex flex-col min-h-0 bg-surface rounded-lg overflow-hidden">
      {/* Header */}
      <div className="px-4 pt-4 pb-2">
        <div className="flex items-center">
          <span className="flex items-center gap-2.5 font-bold text-base text-text-primary">
            <Icon name="browse" className="w-4 h-4 text-text-secondary" />
            Your Library
          </span>
          <div className="ml-auto flex gap-1.5 text-text-secondary">
            <IconButton name="plus" label="Add to library" size="sm" />
            <IconButton name="expand" label="Expand library" size="sm" />
          </div>
        </div>

        {/* Nav links */}
        <nav className="mt-3 flex flex-col gap-px">
          {NAV_ITEMS.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              aria-label={item.label}
              className={({ isActive }) =>
                [
                  'block rounded px-2 py-1.5 text-sm font-semibold transition-colors',
                  isActive
                    ? 'bg-accent/20 text-accent'
                    : 'text-text-secondary hover:text-text-primary hover:bg-raised',
                ].join(' ')
              }
            >
              {item.label}
            </NavLink>
          ))}
        </nav>
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

      {/* Sub-toolbar */}
      <div className="flex items-center px-4 pb-2 text-text-secondary">
        <IconButton name="sort" label="Sort" size="sm" />
        <button
          type="button"
          aria-label="Sort"
          onClick={() => { /* sort not wired yet */ }}
          className="ml-auto flex items-center gap-1.5 text-sm font-semibold text-text-primary select-none hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent rounded"
        >
          Recents
          <Icon name="fwd" className="w-3.5 h-3.5 rotate-90" />
        </button>
      </div>

      {/* List */}
      <div className="flex-1 overflow-y-auto min-h-0 px-2 pb-4">
        {isLoading ? (
          <SkeletonRows />
        ) : filter === 'playlists' ? (
          <PlaylistList items={playlists.data ?? []} />
        ) : filter === 'albums' ? (
          <AlbumList items={albums.data ?? []} current={current} />
        ) : (
          <ArtistList items={artists.data ?? []} current={current} />
        )}
      </div>
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

// ---- Playlist list ----
function PlaylistList({ items }: { items: Playlist[] }) {
  if (items.length === 0) {
    return <EmptyState icon="queue" title="No playlists yet" hint="Create your first playlist to get started." />
  }

  return (
    <>
      {items.map((p) => (
        <LibItem
          key={p.id}
          coverSrc={coverUrl(p.coverArtId)}
          name={p.name}
          meta={`Playlist · ${p.songCount} songs`}
          rounded="md"
          isPlaying={false} // no playlist-context signal on Track yet
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
            onClick={() => navigate(`/album/${al.id}`)}
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
            onClick={() => navigate(`/artist/${ar.id}`)}
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
        <div
          className={[
            'text-sm font-semibold truncate',
            isPlaying ? 'text-accent' : 'text-text-primary',
          ].join(' ')}
        >
          {name}
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
