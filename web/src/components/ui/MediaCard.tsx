import type { ReactNode } from 'react'
import { Cover } from './Cover'
import { Icon } from './Icon'
import { coverUrl } from '../../lib/libraryApi'
import { CoverageChip } from './CoverageChip'
import type { CoverageState } from '../../lib/types'

interface MediaCardProps {
  title: string
  subtitle?: string
  coverId?: string
  rounded?: 'md' | 'full'
  onClick?: () => void
  onPlay?: () => void
  badge?: ReactNode
  coverage?: { state: CoverageState; owned: number; total: number }
  onDownload?: () => void
}

export function MediaCard({
  title,
  subtitle,
  coverId,
  rounded = 'md',
  onClick,
  onPlay,
  badge,
  coverage,
  onDownload,
}: MediaCardProps) {
  const src = coverId ? coverUrl(coverId, 300) : undefined

  function handlePlay(e: React.MouseEvent) {
    e.stopPropagation()
    onPlay?.()
  }

  return (
    <button
      type="button"
      aria-label={title}
      onClick={onClick}
      className={[
        'group relative w-full text-left p-3 rounded-lg',
        'bg-raised hover:bg-raised-hover transition-colors',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
      ].join(' ')}
    >
      {/* Cover */}
      <div
        className={['relative mb-3', rounded === 'full' ? 'rounded-full overflow-hidden' : ''].filter(Boolean).join(' ')}
        data-testid="mediacard-cover"
      >
        <Cover
          src={src}
          alt={title}
          size="full"
          rounded={rounded}
          className="aspect-square w-full shadow-cover"
        />
        {/* Badge / Coverage slot (top-left overlay) — coverage takes precedence */}
        {coverage && (
          <div className="absolute left-2 top-2"><CoverageChip state={coverage.state} owned={coverage.owned} total={coverage.total} /></div>
        )}
        {badge && !coverage && <div className="absolute left-2 top-2">{badge}</div>}
        {/* Play button — accent reveal on hover */}
        {onPlay && (
          <button
            type="button"
            aria-label={`Play ${title}`}
            onClick={handlePlay}
            className={[
              'absolute right-3 bottom-3',
              'w-10 h-10 rounded-full bg-accent text-surface',
              'inline-grid place-items-center shadow-cover',
              'opacity-0 translate-y-2 group-hover:opacity-100 group-hover:translate-y-0',
              'transition-all duration-150',
              'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
              'focus-visible:opacity-100 focus-visible:translate-y-0',
            ].join(' ')}
          >
            <Icon name="play" className="w-4 h-4" />
          </button>
        )}
        {/* Download button — accent reveal on hover, only when no onPlay */}
        {!onPlay && onDownload && (
          <button
            type="button"
            aria-label={`Download ${title}`}
            onClick={(e) => { e.stopPropagation(); onDownload() }}
            className={[
              'absolute right-3 bottom-3 w-10 h-10 rounded-full bg-accent text-surface',
              'inline-grid place-items-center shadow-cover',
              'opacity-0 translate-y-2 group-hover:opacity-100 group-hover:translate-y-0',
              'transition-all duration-150 focus-visible:opacity-100',
            ].join(' ')}
          >
            <Icon name="dl" className="w-4 h-4" />
          </button>
        )}
      </div>

      {/* Title */}
      <p className="truncate text-sm font-semibold text-text-primary leading-snug">
        {title}
      </p>

      {/* Subtitle */}
      {subtitle && (
        <p
          data-testid="mediacard-subtitle"
          className="mt-1 text-xs text-text-secondary line-clamp-2 leading-snug"
        >
          {subtitle}
        </p>
      )}
    </button>
  )
}
