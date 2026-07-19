import { useEffect, useState } from 'react'
import { trackCoverUrl } from '../lib/libraryApi'
import { useLyrics } from '../lib/lyricsApi'
import { rgbToCss } from '../lib/palette'
import { usePlayer } from '../lib/playerStore'
import { formatDuration } from '../lib/types'
import { useUI } from '../lib/uiStore'
import { useActiveLyricLine } from '../lib/useActiveLyricLine'
import { useAlbumPalette } from '../lib/useAlbumPalette'
import { usePeaks } from '../lib/peaksApi'
import { Cover } from './ui/Cover'
import { Icon } from './ui/Icon'
import { LyricsLines } from './lyrics/LyricsLines'

export function CinemaView() {
  const open = useUI((s) => s.cinemaOpen)
  const close = useUI((s) => s.closeCinema)
  const current = usePlayer((s) => s.current)
  const playing = usePlayer((s) => s.playing)
  const queue = usePlayer((s) => s.queue)
  const index = usePlayer((s) => s.index)
  const jumpTo = usePlayer((s) => s.jumpTo)
  const toggle = usePlayer((s) => s.toggle)
  const next = usePlayer((s) => s.next)
  const prev = usePlayer((s) => s.prev)
  const seekMs = usePlayer((s) => s.seekMs)
  const currentTimeMs = usePlayer((s) => s.currentTimeMs)
  const durationMs = usePlayer((s) => s.durationMs)
  const palette = useAlbumPalette(current ? trackCoverUrl(current, 80) : undefined)
  const peaks = usePeaks(current?.id).data
  const [sideView, setSideView] = useState<'queue' | 'lyrics'>('queue')
  const { data: lyrics } = useLyrics(open ? current : null)
  const activeIndex = useActiveLyricLine(lyrics?.synced ? lyrics.lines : undefined)

  useEffect(() => {
    if (!open) return
    function onKey(event: KeyboardEvent) {
      if (event.key === 'Escape') close()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, close])

  useEffect(() => {
    /* eslint-disable react-hooks/set-state-in-effect -- intentional: reset side column when the track changes */
    setSideView('queue')
    /* eslint-enable react-hooks/set-state-in-effect */
  }, [current?.id])

  if (!open) return null

  // Full-screen deserves a full wash: the dominant color carries the whole view
  // (Spotify-cinema style) rather than a thin header tint fading to black.
  const ambient = palette
    ? {
        background: `linear-gradient(180deg, ${rgbToCss(palette.rgb, 0.75)} 0%, ${rgbToCss(palette.rgb, 0.3)} 55%, var(--bg-base) 100%)`,
        color: palette.text,
      }
    : undefined
  const pct = durationMs > 0 ? (currentTimeMs / durationMs) * 100 : 0
  function seek(event: React.MouseEvent<HTMLDivElement>) {
    if (durationMs <= 0) return
    const rect = event.currentTarget.getBoundingClientRect()
    seekMs(Math.max(0, Math.min(1, (event.clientX - rect.left) / rect.width)) * durationMs)
  }

  // Keyboard operability for the slider role — mirrors PlayerBar's SeekBar
  // shortcuts (±5s via Arrow keys, Home/End to jump to the ends of the track).
  function onSeekKeyDown(event: React.KeyboardEvent<HTMLDivElement>) {
    if (durationMs <= 0) return
    switch (event.key) {
      case 'ArrowRight':
      case 'ArrowUp':
        event.preventDefault()
        seekMs(Math.min(durationMs, currentTimeMs + 5000))
        break
      case 'ArrowLeft':
      case 'ArrowDown':
        event.preventDefault()
        seekMs(Math.max(0, currentTimeMs - 5000))
        break
      case 'Home':
        event.preventDefault()
        seekMs(0)
        break
      case 'End':
        event.preventDefault()
        seekMs(durationMs)
        break
    }
  }

  return (
    <div data-testid="cinema-view" className="fixed inset-0 z-40 hidden flex-col bg-canvas p-8 md:flex" style={ambient}>
      <div className="flex justify-end">
        <button type="button" aria-label="Close full screen" onClick={close} className="flex h-11 w-11 items-center justify-center rounded-full text-text-secondary transition-colors hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent">
          <Icon name="chevron-down" className="h-5 w-5" />
        </button>
      </div>
      <div className="mx-auto flex min-h-0 w-full max-w-5xl flex-1 items-center justify-center gap-16">
        <div className="w-full max-w-[420px]">
          <Cover src={current ? trackCoverUrl(current, 600) || undefined : undefined} alt={current?.title ?? 'Nothing playing'} size="full" rounded="md" className="aspect-square shadow-pop" />
        </div>
        <div className="flex h-full max-h-[60vh] w-full max-w-xs flex-col">
          <div className="mb-2 flex items-center justify-between">
            <h2 className="text-xs font-bold uppercase tracking-widest text-text-muted">
              {sideView === 'lyrics' ? 'Lyrics' : 'Up next'}
            </h2>
            {lyrics != null && (
              <button
                type="button"
                onClick={() => setSideView(sideView === 'lyrics' ? 'queue' : 'lyrics')}
                className="text-xs font-bold uppercase tracking-widest text-text-secondary hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
              >
                {sideView === 'lyrics' ? 'Show queue' : 'Show lyrics'}
              </button>
            )}
          </div>
          {sideView === 'lyrics' && lyrics != null ? (
            <div className="min-h-0 flex-1">
              {lyrics.synced ? (
                <LyricsLines lines={lyrics.lines} activeIndex={activeIndex} onLineClick={(line) => seekMs(line.timeMs)} size="md" />
              ) : (
                <div className="h-full overflow-y-auto whitespace-pre-wrap text-sm font-semibold leading-6 text-text-secondary">
                  {lyrics.plain}
                </div>
              )}
            </div>
          ) : (
            <ul className="space-y-1">
              {queue.slice(index + 1, index + 6).map((track, offset) => (
                <li key={`${track.id}-${offset}`}>
                  <button type="button" onClick={() => jumpTo(index + 1 + offset)} className="flex w-full items-center gap-3 rounded p-2 text-left hover:bg-raised-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent">
                    <Cover src={trackCoverUrl(track, 80) || undefined} alt={track.title} size={40} rounded="md" />
                    <span className="min-w-0"><span className="block truncate text-sm font-semibold text-text-primary">{track.title}</span><span className="block truncate text-xs text-text-secondary">{track.artist}</span></span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
      <div className="mx-auto w-full max-w-5xl">
        <div className="truncate text-3xl font-black text-text-primary">{current?.title ?? 'Nothing playing'}</div>
        <div className="mb-4 truncate text-sm text-text-secondary">{current?.artist ?? ''}</div>
        <div className="mb-1 flex items-center justify-between text-xs tabular-nums text-text-muted"><span>{formatDuration(currentTimeMs)}</span><span>{formatDuration(durationMs)}</span></div>
        <div role="slider" aria-label="Seek" aria-valuemin={0} aria-valuemax={durationMs} aria-valuenow={currentTimeMs} tabIndex={0} onClick={seek} onKeyDown={onSeekKeyDown} className="group relative h-1 w-full cursor-pointer rounded-full bg-border-subtle focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent">
          {peaks?.length ? (
            <div data-testid="waveform" className="absolute inset-x-0 top-1/2 flex h-8 -translate-y-1/2 items-center gap-px">
              {peaks.map((peak, i) => <div key={i} className={i / peaks.length * 100 <= pct ? 'flex-1 rounded-full bg-text-primary group-hover:bg-accent' : 'flex-1 rounded-full bg-border-subtle'} style={{ minHeight: '2px', height: `${Math.max(8, peak * 100)}%` }} />)}
            </div>
          ) : (
            <div className="h-full rounded-full bg-text-primary" style={{ width: `${pct}%` }} />
          )}
        </div>
        <div className="mt-6 flex items-center justify-center gap-8">
          <button type="button" aria-label="Previous" onClick={prev} className="flex h-11 w-11 items-center justify-center rounded-full text-text-secondary transition-colors hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"><Icon name="prev" className="h-6 w-6" /></button>
          <button type="button" aria-label={playing ? 'Pause' : 'Play'} onClick={toggle} className="flex h-16 w-16 items-center justify-center rounded-full bg-text-primary text-surface transition-transform hover:scale-105 active:scale-95 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"><Icon name={playing ? 'pause' : 'play'} className="h-7 w-7" /></button>
          <button type="button" aria-label="Next" onClick={next} className="flex h-11 w-11 items-center justify-center rounded-full text-text-secondary transition-colors hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"><Icon name="next" className="h-6 w-6" /></button>
        </div>
      </div>
    </div>
  )
}
