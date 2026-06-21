import { usePlayer } from '../lib/playerStore'
import { useUI } from '../lib/uiStore'
import { coverUrl } from '../lib/libraryApi'
import { formatDuration } from '../lib/types'
import { useAlbumPalette } from '../lib/useAlbumPalette'
import { rgbToCss } from '../lib/palette'

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
    ? { background: `linear-gradient(180deg, ${rgbToCss(palette.rgb, 0.45)} 0%, rgb(13 13 15) 70%)`, color: palette.text }
    : undefined

  return (
    <div
      data-testid="now-playing-overlay"
      className="fixed inset-0 z-40 flex flex-col bg-base p-6 md:hidden"
      style={ambient}
    >
      <div className="flex items-center justify-between">
        <button
          type="button"
          aria-label="Close now playing"
          onClick={close}
          className="flex h-11 w-11 items-center justify-center rounded-full text-2xl"
        >
          ⌄
        </button>
        <div className="text-xs uppercase tracking-wide opacity-70">Now Playing</div>
        <div className="h-11 w-11" />
      </div>

      <div className="flex flex-1 flex-col items-center justify-center gap-6">
        {current?.coverArtId ? (
          <img src={coverUrl(current.coverArtId, 600)} alt="" className="aspect-square w-full max-w-xs rounded-lg object-cover shadow-2xl" />
        ) : (
          <div className="aspect-square w-full max-w-xs rounded-lg bg-neutral-800" />
        )}
        <div className="w-full max-w-xs text-center">
          <div className="truncate text-xl font-bold">{current ? current.title : 'Nothing playing'}</div>
          <div className="truncate text-sm opacity-80">{current?.artist ?? ''}</div>
        </div>
      </div>

      <div className="mb-2 flex items-center justify-between text-xs tabular-nums opacity-80">
        <span>{formatDuration(currentTimeMs)}</span>
        <span>{formatDuration(durationMs)}</span>
      </div>

      <div className="mb-6 flex items-center justify-center gap-8">
        <button type="button" aria-label="Previous" onClick={prev} className="flex h-11 w-11 items-center justify-center text-2xl">
          ⏮
        </button>
        <button
          type="button"
          aria-label={playing ? 'Pause' : 'Play'}
          onClick={toggle}
          className="flex h-16 w-16 items-center justify-center rounded-full bg-white text-2xl text-black"
        >
          {playing ? '⏸' : '▶'}
        </button>
        <button type="button" aria-label="Next" onClick={next} className="flex h-11 w-11 items-center justify-center text-2xl">
          ⏭
        </button>
      </div>
    </div>
  )
}
