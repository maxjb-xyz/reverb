import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import type { Track } from '../../lib/types'
import { formatDuration } from '../../lib/types'
import { coverUrl } from '../../lib/libraryApi'
import { Cover } from './Cover'
import { Equalizer } from './Equalizer'
import { Icon } from './Icon'

interface TrackRowProps {
  track: Track
  index?: number
  active?: boolean
  playing?: boolean
  onPlay: () => void
  right?: ReactNode
  /** Direct cover image URL (e.g. an external Spotify image). Overrides the
   *  library coverArtId proxy URL — external results have a URL, not a cover id. */
  coverSrc?: string
  /** Fixed grid width for the right slot. Default 'auto' sizes to content — but
   *  when the right content changes width (e.g. a download badge cycling through
   *  states), 'auto' reflows the title/album columns. Pass a fixed width to keep
   *  them anchored. */
  rightWidth?: string
  /** Override the artist cell with a pre-built node (e.g. a Link for external results). */
  artistNode?: ReactNode
  /** Override the album cell with a pre-built node (e.g. a Link for external results). */
  albumNode?: ReactNode
  /** Override the artist link destination. When provided, replaces /artist/library/:artistId. */
  artistTo?: string
  /** Override the album link destination. When provided, replaces /album/library/:albumId. */
  albumTo?: string
}

export function TrackRow({ track, index, active = false, playing, onPlay, right, coverSrc, rightWidth = 'auto', artistNode, albumNode, artistTo, albumTo }: TrackRowProps) {
  const src = coverSrc ?? (track.coverArtId ? coverUrl(track.coverArtId, 80) : undefined)

  function handleKeyDown(e: React.KeyboardEvent<HTMLDivElement>) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      onPlay()
    }
  }

  return (
    <div
      role="button"
      tabIndex={0}
      onDoubleClick={onPlay}
      onKeyDown={handleKeyDown}
      className={[
        'group w-full grid items-center gap-3.5 px-2.5 py-2 rounded-md text-left',
        'transition-colors hover:bg-raised-hover cursor-default',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
        active ? 'text-accent' : 'text-text-primary',
      ].join(' ')}
      style={{ gridTemplateColumns: `26px 40px 1fr 1fr ${rightWidth} 44px` }}
    >
      {/* Lead: index or Equalizer when active */}
      <span className="grid place-items-center text-sm font-bold text-text-muted">
        {active ? (
          <Equalizer playing={playing} />
        ) : (
          <span>{index !== undefined ? index + 1 : ''}</span>
        )}
      </span>

      {/* Cover — with hover play button overlaid */}
      <div className="relative flex-none">
        <Cover src={src} alt={track.title} size={40} rounded="md" />
        {/* Hover play button: hidden by default, revealed on row hover */}
        <button
          type="button"
          aria-label={`Play ${track.title}`}
          onClick={(e) => { e.stopPropagation(); onPlay() }}
          onDoubleClick={(e) => e.stopPropagation()}
          className={[
            'absolute inset-0 rounded-md',
            'inline-grid place-items-center',
            'bg-surface/60',
            'text-text-primary',
            'opacity-0 group-hover:opacity-100',
            'transition-opacity duration-150',
            'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:opacity-100',
          ].join(' ')}
        >
          <Icon name="play" className="w-4 h-4" />
        </button>
      </div>

      {/* Title + Artist */}
      <span className="min-w-0">
        <span className="block truncate text-sm font-semibold leading-snug">
          {track.title}
        </span>
        <span className="block truncate text-xs text-text-secondary mt-0.5">
          {artistNode ?? ((artistTo ?? (track.artistId ? `/artist/library/${track.artistId}` : null)) ? (
            <Link
              to={artistTo ?? `/artist/library/${track.artistId}`}
              onClick={(e) => e.stopPropagation()}
              onDoubleClick={(e) => e.stopPropagation()}
              className="hover:underline focus-visible:outline-none focus-visible:underline"
            >
              {track.artist}
            </Link>
          ) : (
            track.artist
          ))}
        </span>
      </span>

      {/* Album */}
      <span className="truncate text-sm text-text-secondary hidden md:block">
        {albumNode ?? ((albumTo ?? (track.albumId ? `/album/library/${track.albumId}` : null)) ? (
          <Link
            to={albumTo ?? `/album/library/${track.albumId}`}
            onClick={(e) => e.stopPropagation()}
            onDoubleClick={(e) => e.stopPropagation()}
            className="hover:underline focus-visible:outline-none focus-visible:underline"
          >
            {track.album}
          </Link>
        ) : (
          track.album
        ))}
      </span>

      {/* Right slot (Phase 5: download badge) */}
      <span className="flex items-center justify-end gap-2">
        {right}
      </span>

      {/* Duration */}
      <span className="text-sm text-text-muted text-right tabular-nums">
        {formatDuration(track.durationMs)}
      </span>
    </div>
  )
}
