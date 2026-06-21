import { usePlayer } from '../lib/playerStore'
import { useUI } from '../lib/uiStore'
import { coverUrl } from '../lib/libraryApi'
import { useAlbumPalette } from '../lib/useAlbumPalette'
import { rgbToCss } from '../lib/palette'

// MiniPlayer is the mobile-only compact player (shown < md), sitting above the tab
// nav. Tapping the bar expands to the fullscreen now-playing overlay; the play/pause
// button is a separate ≥44px target that does NOT expand.
export function MiniPlayer() {
  const current = usePlayer((s) => s.current)
  const playing = usePlayer((s) => s.playing)
  const toggle = usePlayer((s) => s.toggle)
  const openNowPlaying = useUI((s) => s.openNowPlaying)
  const palette = useAlbumPalette(current?.coverArtId ? coverUrl(current.coverArtId, 80) : undefined)

  if (!current) return null

  return (
    <div
      data-testid="mini-player"
      className="flex items-center gap-3 border-t border-neutral-800 px-3 py-2 md:hidden"
      style={palette ? { backgroundColor: rgbToCss(palette.rgb), color: palette.text } : undefined}
    >
      <button
        type="button"
        data-testid="mini-player-expand"
        aria-label="Expand player"
        onClick={openNowPlaying}
        className="flex min-w-0 flex-1 items-center gap-3 text-left"
      >
        {current.coverArtId ? (
          <img src={coverUrl(current.coverArtId, 80)} alt="" className="h-10 w-10 rounded object-cover" />
        ) : (
          <div className="h-10 w-10 rounded bg-neutral-800" />
        )}
        <div className="min-w-0">
          <div className="truncate text-sm font-medium">{current.title}</div>
          <div className="truncate text-xs opacity-80">{current.artist}</div>
        </div>
      </button>
      <button
        type="button"
        aria-label={playing ? 'Pause' : 'Play'}
        onClick={toggle}
        className="flex h-11 w-11 items-center justify-center rounded-full bg-white text-black"
      >
        {playing ? '⏸' : '▶'}
      </button>
    </div>
  )
}
