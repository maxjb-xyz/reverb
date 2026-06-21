import { usePlayer } from '../lib/playerStore'
import { useUI } from '../lib/uiStore'
import { coverUrl } from '../lib/libraryApi'
import { formatDuration } from '../lib/types'
import { useAlbumPalette } from '../lib/useAlbumPalette'
import { rgbToCss } from '../lib/palette'
import { Cover } from './ui/Cover'
import { Icon } from './ui/Icon'

// NowPlayingOverlay is the mobile fullscreen now-playing view (iOS-Music style),
// toggled from the mini player. It reuses the SAME playerStore — no duplicate state.
export function NowPlayingOverlay() {
  const open = useUI((s) => s.nowPlayingOpen)
  const close = useUI((s) => s.closeNowPlaying)
  const current = usePlayer((s) => s.current)
  const playing = usePlayer((s) => s.playing)
  const toggle = usePlayer((s) => s.toggle)
  const next = usePlayer((s) => s.next)
  const prev = usePlayer((s) => s.prev)
  const currentTimeMs = usePlayer((s) => s.currentTimeMs)
  const durationMs = usePlayer((s) => s.durationMs)
  const palette = useAlbumPalette(current?.coverArtId ? coverUrl(current.coverArtId, 80) : undefined)

  if (!open) return null

  const ambient = palette
    ? { background: `linear-gradient(180deg, ${rgbToCss(palette.rgb, 0.45)} 0%, var(--bg-base) 70%)`, color: palette.text }
    : undefined

  const pct = durationMs > 0 ? (currentTimeMs / durationMs) * 100 : 0

  return (
    <div
      data-testid="now-playing-overlay"
      className="fixed inset-0 z-40 flex flex-col bg-base p-6 md:hidden"
      style={ambient}
    >
      {/* Header row — close + label */}
      <div className="flex items-center justify-between">
        <button
          type="button"
          aria-label="Close now playing"
          onClick={close}
          className={[
            'flex h-11 w-11 items-center justify-center rounded-full',
            'text-text-secondary hover:text-text-primary',
            'transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
          ].join(' ')}
        >
          <Icon name="chevron-down" className="w-5 h-5" />
        </button>
        <span className="text-xs font-semibold uppercase tracking-widest text-text-muted">
          Now Playing
        </span>
        <div className="h-11 w-11" aria-hidden="true" />
      </div>

      {/* Cover + meta */}
      <div className="flex flex-1 flex-col items-center justify-center gap-6">
        <div className="w-full max-w-xs">
          <Cover
            src={current?.coverArtId ? coverUrl(current.coverArtId, 600) : undefined}
            alt={current?.title ?? 'Nothing playing'}
            size="full"
            rounded="md"
            className="aspect-square shadow-pop"
          />
        </div>
        <div className="w-full max-w-xs text-center">
          <div className="truncate text-xl font-bold text-text-primary">
            {current ? current.title : 'Nothing playing'}
          </div>
          <div className="truncate text-sm text-text-secondary mt-1">{current?.artist ?? ''}</div>
        </div>
      </div>

      {/* Seek bar */}
      <div className="mb-2 max-w-xs mx-auto w-full">
        <div className="mb-1 flex items-center justify-between text-xs tabular-nums text-text-muted">
          <span>{formatDuration(currentTimeMs)}</span>
          <span>{formatDuration(durationMs)}</span>
        </div>
        <div className="h-1 w-full rounded-full bg-border-subtle">
          <div
            className="h-full rounded-full bg-text-primary"
            style={{ width: `${pct}%` }}
          />
        </div>
      </div>

      {/* Transport controls */}
      <div className="mb-6 flex items-center justify-center gap-8 max-w-xs mx-auto w-full">
        <button
          type="button"
          aria-label="Previous"
          onClick={prev}
          className={[
            'flex h-11 w-11 items-center justify-center rounded-full',
            'text-text-secondary hover:text-text-primary',
            'transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
          ].join(' ')}
        >
          <Icon name="prev" className="w-6 h-6" />
        </button>

        <button
          type="button"
          aria-label={playing ? 'Pause' : 'Play'}
          onClick={toggle}
          className={[
            'flex h-16 w-16 items-center justify-center rounded-full',
            'bg-text-primary text-surface',
            'transition-transform hover:scale-105 active:scale-95',
            'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
          ].join(' ')}
        >
          <Icon name={playing ? 'pause' : 'play'} className="w-7 h-7" />
        </button>

        <button
          type="button"
          aria-label="Next"
          onClick={next}
          className={[
            'flex h-11 w-11 items-center justify-center rounded-full',
            'text-text-secondary hover:text-text-primary',
            'transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
          ].join(' ')}
        >
          <Icon name="next" className="w-6 h-6" />
        </button>
      </div>
    </div>
  )
}
