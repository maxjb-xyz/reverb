import type { ReactNode } from 'react'
import type { Track } from '../../lib/types'
import { formatDuration } from '../../lib/types'
import { coverUrl } from '../../lib/libraryApi'
import { Cover } from './Cover'
import { Equalizer } from './Equalizer'

interface TrackRowProps {
  track: Track
  index?: number
  active?: boolean
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
}

export function TrackRow({ track, index, active = false, onPlay, right, coverSrc, rightWidth = 'auto' }: TrackRowProps) {
  const src = coverSrc ?? (track.coverArtId ? coverUrl(track.coverArtId, 80) : undefined)

  return (
    <button
      type="button"
      onClick={onPlay}
      className={[
        'group w-full grid items-center gap-3.5 px-2.5 py-2 rounded-md text-left',
        'transition-colors hover:bg-raised-hover',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
        active ? 'text-accent' : 'text-text-primary',
      ].join(' ')}
      style={{ gridTemplateColumns: `26px 40px 1fr 1fr ${rightWidth} 44px` }}
    >
      {/* Lead: index or Equalizer when active */}
      <span className="grid place-items-center text-sm font-bold text-text-muted">
        {active ? (
          <Equalizer />
        ) : (
          <span>{index !== undefined ? index + 1 : ''}</span>
        )}
      </span>

      {/* Cover */}
      <Cover src={src} alt={track.title} size={40} rounded="md" />

      {/* Title + Artist */}
      <span className="min-w-0">
        <span className="block truncate text-sm font-semibold leading-snug">
          {track.title}
        </span>
        <span className="block truncate text-xs text-text-secondary mt-0.5">
          {track.artist}
        </span>
      </span>

      {/* Album */}
      <span className="truncate text-sm text-text-secondary hidden md:block">
        {track.album}
      </span>

      {/* Right slot (Phase 5: download badge) */}
      <span className="flex items-center justify-end gap-2">
        {right}
      </span>

      {/* Duration */}
      <span className="text-sm text-text-muted text-right tabular-nums">
        {formatDuration(track.durationMs)}
      </span>
    </button>
  )
}
