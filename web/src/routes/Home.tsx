import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAlbums, usePlaylists, coverUrl } from '../lib/libraryApi'
import { useDownloads } from '../lib/downloadStore'
import { usePlayer } from '../lib/playerStore'
import { api } from '../lib/api'
import {
  Chip,
  Carousel,
  MediaCard,
  Cover,
  Button,
  Skeleton,
  Equalizer,
  Icon,
} from '../components/ui'
import type { Album, DownloadJob } from '../lib/types'

type FilterChip = 'All' | 'Music' | 'Downloads'

// ------------------------------------------------------------------
// ShortcutTile — compact 2-col grid item (56px height)
// ------------------------------------------------------------------
interface ShortcutTileProps {
  title: string
  coverId?: string
  isPlaying?: boolean
  onClick?: () => void
}

function ShortcutTile({ title, coverId, isPlaying, onClick }: ShortcutTileProps) {
  const src = coverId ? coverUrl(coverId, 56) : undefined
  return (
    <button
      type="button"
      aria-label={title}
      onClick={onClick}
      className={[
        'group relative flex items-center gap-3 h-14 rounded overflow-hidden',
        'bg-raised hover:bg-raised-hover transition-colors text-left',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
        isPlaying ? 'text-accent' : 'text-text-primary',
      ].join(' ')}
    >
      {/* Cover art (fixed 56×56 square flush left) */}
      <div className="w-14 h-14 flex-none">
        <Cover src={src} alt={title} size="full" rounded="md" className="w-full h-full" />
      </div>

      {/* Title */}
      <span className="flex-1 truncate text-sm font-bold pr-2">{title}</span>

      {/* Right decoration: Equalizer if playing, else play button on hover */}
      {isPlaying ? (
        <span className="mr-4 flex-none">
          <Equalizer />
        </span>
      ) : (
        <span
          aria-hidden
          className={[
            'mr-3 flex-none w-10 h-10 rounded-full bg-accent',
            'inline-grid place-items-center shadow-cover text-surface',
            'opacity-0 translate-y-1.5 group-hover:opacity-100 group-hover:translate-y-0',
            'transition-all duration-150',
          ].join(' ')}
        >
          <Icon name="play" className="w-4 h-4" />
        </span>
      )}
    </button>
  )
}

// ------------------------------------------------------------------
// SkeletonShortcutTile
// ------------------------------------------------------------------
function SkeletonShortcutTile() {
  return (
    <div className="flex items-center gap-3 h-14 rounded overflow-hidden bg-raised">
      <Skeleton className="w-14 h-14 flex-none rounded-none" />
      <Skeleton className="flex-1 h-4 mr-4" />
    </div>
  )
}

// ------------------------------------------------------------------
// SkeletonCardRow — loading state for a carousel
// ------------------------------------------------------------------
function SkeletonCardRow({ count = 5 }: { count?: number }) {
  return (
    <div className="grid grid-flow-col gap-4 overflow-x-auto pb-2" style={{ gridAutoColumns: '160px' }}>
      {Array.from({ length: count }).map((_, i) => (
        <div key={i} className="p-3 rounded-lg bg-raised">
          <Skeleton className="w-full aspect-square mb-3 rounded-md" />
          <Skeleton className="h-3.5 w-3/4 mb-2" />
          <Skeleton className="h-3 w-1/2" />
        </div>
      ))}
    </div>
  )
}

// ------------------------------------------------------------------
// Main Home component
// ------------------------------------------------------------------
export default function Home() {
  const [activeChip, setActiveChip] = useState<FilterChip>('All')
  const navigate = useNavigate()

  // Data hooks
  const newestQuery = useAlbums('newest')
  const recentQuery = useAlbums('recent')
  const playlistsQuery = usePlaylists()

  // Completed downloads — newest first
  const allJobs = useDownloads((s) => s.jobs)
  const completedJobs: DownloadJob[] = Object.values(allJobs)
    .filter((j) => j.status === 'completed')
    .sort((a, b) => b.finishedAt - a.finishedAt)

  // Player state — use selectors to avoid re-renders on every scrubber tick
  const current = usePlayer((s) => s.current)
  const playTrackList = usePlayer((s) => s.playTrackList)

  // ------------------------------------------------------------------
  // Derived data
  // ------------------------------------------------------------------
  const isLoading = newestQuery.isLoading || recentQuery.isLoading

  // Shortcut grid: up to 8 items from recent albums + playlists combined
  const recentAlbums: Album[] = recentQuery.data ?? []
  const playlists = playlistsQuery.data ?? []
  const shortcutItems: Array<{ id: string; name: string; coverId: string; type: 'album' | 'playlist' }> = [
    ...recentAlbums.map((a) => ({ id: a.id, name: a.name, coverId: a.coverArtId, type: 'album' as const })),
    ...playlists.map((p) => ({ id: p.id, name: p.name, coverId: p.coverArtId, type: 'playlist' as const })),
  ].slice(0, 8)

  // Hero: first item from newest albums
  const newestAlbums: Album[] = newestQuery.data ?? []
  const heroAlbum = newestAlbums[0] ?? null

  // "Jump back in" carousel: recent albums
  const jumpBackAlbums = recentAlbums

  // First-run / nothing-to-show: no library content and no downloads yet. This is
  // the common state before a library provider is connected or anything is
  // downloaded — guide the user instead of rendering an empty void.
  const isEmpty =
    !isLoading &&
    shortcutItems.length === 0 &&
    !heroAlbum &&
    jumpBackAlbums.length === 0 &&
    completedJobs.length === 0

  // ------------------------------------------------------------------
  // Handlers
  // ------------------------------------------------------------------
  function handleShortcutClick(item: { id: string; type: 'album' | 'playlist' }) {
    if (item.type === 'album') navigate(`/album/${item.id}`)
    // playlists don't have a route yet — no-op
  }

  async function handleHeroPlay() {
    if (!heroAlbum) return
    const full = await api.get<Album>(`/library/album/${heroAlbum.id}`)
    if (full.tracks?.length) playTrackList(full.tracks, 0)
  }

  async function handleAlbumPlay(album: Album) {
    const full = await api.get<Album>(`/library/album/${album.id}`)
    if (full.tracks?.length) playTrackList(full.tracks, 0)
  }

  // ------------------------------------------------------------------
  // Render
  // ------------------------------------------------------------------
  // First-run welcome — replaces the whole feed when there's nothing to show.
  if (isEmpty) {
    return (
      <div className="flex flex-col items-center justify-center gap-6 py-24 text-center">
        <span className="grid h-16 w-16 place-items-center rounded-full bg-raised text-text-secondary">
          <Icon name="browse" className="text-3xl" />
        </span>
        <div className="space-y-2">
          <h1 className="text-2xl font-black tracking-tight text-text-primary">Welcome to Reverb</h1>
          <p className="mx-auto max-w-md text-sm text-text-secondary">
            Search for any song or album to download it into your library — or connect an
            existing music library to browse what you already have.
          </p>
        </div>
        <div className="flex flex-wrap items-center justify-center gap-3">
          <Button variant="primary" onClick={() => navigate('/search')} aria-label="Search music">
            <Icon name="search" className="mr-1.5 h-4 w-4" />
            Search music
          </Button>
          <Button variant="secondary" onClick={() => navigate('/admin')} aria-label="Connect a library">
            Connect a library
          </Button>
        </div>
      </div>
    )
  }

  return (
    <div className="relative">
      {/* Chip filter row */}
      <div className="flex gap-2 mb-6">
        {(['All', 'Music', 'Downloads'] as FilterChip[]).map((chip) => (
          <Chip key={chip} selected={activeChip === chip} onClick={() => setActiveChip(chip)}>
            {chip}
          </Chip>
        ))}
      </div>

      {/* Shortcut grid — 2-column, 8 items */}
      {isLoading ? (
        <div className="grid grid-cols-2 gap-2 mb-8" data-testid="shortcut-grid-skeleton">
          {Array.from({ length: 8 }).map((_, i) => (
            <SkeletonShortcutTile key={i} />
          ))}
        </div>
      ) : shortcutItems.length > 0 ? (
        <div className="grid grid-cols-2 gap-2 mb-8" data-testid="shortcut-grid">
          {shortcutItems.map((item) => {
            const isPlaying = current?.albumId === item.id
            return (
              <ShortcutTile
                key={item.id}
                title={item.name}
                coverId={item.coverId}
                isPlaying={isPlaying}
                onClick={() => handleShortcutClick(item)}
              />
            )
          })}
        </div>
      ) : null}

      {/* Hero — "Just added to your library" */}
      {!isLoading && heroAlbum && (
        <section className="flex gap-6 items-center mb-10" aria-label="Just added to your library">
          {/* Cover */}
          <div className="w-48 h-48 flex-none shadow-cover rounded-md overflow-hidden">
            <Cover
              src={heroAlbum.coverArtId ? coverUrl(heroAlbum.coverArtId, 200) : undefined}
              alt={heroAlbum.name}
              size="full"
              rounded="md"
              className="w-full h-full"
            />
          </div>

          {/* Info */}
          <div className="min-w-0">
            <p className="flex items-center gap-1.5 text-xs font-bold text-accent mb-2">
              <Icon name="dl" className="w-3.5 h-3.5" />
              Just added to your library
            </p>
            <p className="text-xs font-semibold text-text-secondary mb-1.5">
              Album · {heroAlbum.artist}
            </p>
            <h1 className="text-4xl font-black tracking-tight text-text-primary leading-tight mb-5 truncate">
              {heroAlbum.name}
            </h1>
            <div className="flex items-center gap-5">
              <Button
                variant="primary"
                size="md"
                aria-label={`Play ${heroAlbum.name}`}
                onClick={handleHeroPlay}
              >
                <Icon name="play" className="w-5 h-5 mr-1" />
                Play
              </Button>
            </div>
          </div>
        </section>
      )}

      {/* Loading hero skeleton */}
      {isLoading && (
        <div className="flex gap-6 items-center mb-10">
          <Skeleton className="w-48 h-48 flex-none" />
          <div className="flex-1 space-y-3">
            <Skeleton className="h-3 w-32" />
            <Skeleton className="h-3 w-24" />
            <Skeleton className="h-8 w-64" />
            <Skeleton className="h-10 w-28 rounded-full" />
          </div>
        </div>
      )}

      {/* "Jump back in" carousel */}
      {isLoading ? (
        <section className="mb-8">
          <Skeleton className="h-7 w-36 mb-4" />
          <SkeletonCardRow />
        </section>
      ) : jumpBackAlbums.length > 0 ? (
        <div className="mb-8">
          <Carousel title="Jump back in">
            {jumpBackAlbums.map((album) => (
              <MediaCard
                key={album.id}
                title={album.name}
                subtitle={album.artist}
                coverId={album.coverArtId}
                onClick={() => navigate(`/album/${album.id}`)}
                onPlay={() => handleAlbumPlay(album)}
              />
            ))}
          </Carousel>
        </div>
      ) : null}

      {/* "Recently downloaded" carousel — hidden when no completed downloads */}
      {completedJobs.length > 0 && (
        <div className="mb-8">
          <Carousel title="Recently downloaded">
            {completedJobs.map((job) => (
              <MediaCard
                key={job.id}
                title={job.album ?? job.title ?? 'Unknown'}
                subtitle={job.artist}
                coverId={undefined}
                badge={
                  <span
                    aria-hidden
                    className="w-5 h-5 rounded-full bg-accent block"
                  />
                }
              />
            ))}
          </Carousel>
        </div>
      )}
    </div>
  )
}
