import { useEffect } from 'react'
import { trackCoverUrl } from '../../lib/libraryApi'
import { useLyrics } from '../../lib/lyricsApi'
import { rgbToCss } from '../../lib/palette'
import { usePlayer } from '../../lib/playerStore'
import { useUI } from '../../lib/uiStore'
import { useActiveLyricLine } from '../../lib/useActiveLyricLine'
import { useAlbumPalette } from '../../lib/useAlbumPalette'
import { Icon } from '../ui/Icon'
import { LyricsLines } from './LyricsLines'

// Fullscreen lyrics view (desktop). Same overlay recipe as CinemaView: fixed,
// palette-washed, Escape to close.
export function LyricsView() {
  const open = useUI((s) => s.lyricsOpen)
  const close = useUI((s) => s.closeLyrics)
  const current = usePlayer((s) => s.current)
  const seekMs = usePlayer((s) => s.seekMs)
  const { data: lyrics } = useLyrics(open ? current : null)
  const activeIndex = useActiveLyricLine(lyrics?.synced ? lyrics.lines : undefined)
  const palette = useAlbumPalette(open && current ? trackCoverUrl(current, 80) : undefined)

  useEffect(() => {
    if (!open) return
    function onKey(event: KeyboardEvent) {
      if (event.key === 'Escape') close()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, close])

  if (!open) return null

  const ambient = palette
    ? {
        background: `linear-gradient(180deg, ${rgbToCss(palette.rgb, 0.75)} 0%, ${rgbToCss(palette.rgb, 0.3)} 55%, var(--bg-base) 100%)`,
        color: palette.text,
      }
    : undefined

  return (
    <div data-testid="lyrics-view" className="fixed inset-0 z-40 hidden flex-col bg-canvas p-8 md:flex" style={ambient}>
      <div className="flex items-center justify-between">
        <div className="min-w-0">
          <div className="truncate text-lg font-black text-text-primary">{current?.title ?? ''}</div>
          <div className="truncate text-sm text-text-secondary">{current?.artist ?? ''}</div>
        </div>
        <button type="button" aria-label="Close lyrics" onClick={close} className="flex h-11 w-11 items-center justify-center rounded-full text-text-secondary transition-colors hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent">
          <Icon name="chevron-down" className="h-5 w-5" />
        </button>
      </div>
      <div className="mx-auto min-h-0 w-full max-w-3xl flex-1">
        {lyrics == null ? (
          <div className="flex h-full items-center justify-center text-lg text-text-secondary">
            No lyrics for this track
          </div>
        ) : lyrics.synced ? (
          <LyricsLines lines={lyrics.lines} activeIndex={activeIndex} onLineClick={(line) => seekMs(line.timeMs)} size="lg" />
        ) : (
          <div className="h-full overflow-y-auto whitespace-pre-wrap py-12 text-2xl font-bold leading-relaxed text-text-primary">
            {lyrics.plain}
          </div>
        )}
      </div>
    </div>
  )
}
