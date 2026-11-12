import { useEffect, useState } from 'react'
import { usePlayer } from '../lib/playerStore'
import { useLyrics } from '../lib/lyricsApi'
import { useActiveLyricLine } from '../lib/useActiveLyricLine'
import { LyricsLines } from './lyrics/LyricsLines'
import { AddToPlaylistMenu } from './AddToPlaylistMenu'
import { NotForMeMenu } from './NotForMeMenu'
import { useUI } from '../lib/uiStore'
import { trackCoverUrl } from '../lib/libraryApi'
import { formatDuration } from '../lib/types'
import { useAlbumPalette } from '../lib/useAlbumPalette'
import { rgbToCss } from '../lib/palette'
import { Cover } from './ui/Cover'
import { Icon } from './ui/Icon'

// NowPlayingOverlay is the mobile fullscreen now-playing view (iOS-Music style),
// toggled from the mini player. It reuses the SAME playerStore — no duplicate state.
// It carries everything the desktop bar does: shuffle, repeat, seeking, the
// queue, lyrics and add-to-playlist. On a phone this sheet is the whole player.
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
  const seekMs = usePlayer((s) => s.seekMs)
  const shuffle = usePlayer((s) => s.shuffle)
  const repeat = usePlayer((s) => s.repeat)
  const toggleShuffle = usePlayer((s) => s.toggleShuffle)
  const cycleRepeat = usePlayer((s) => s.cycleRepeat)
  const queue = usePlayer((s) => s.queue)
  const index = usePlayer((s) => s.index)
  const jumpTo = usePlayer((s) => s.jumpTo)
  const palette = useAlbumPalette(current ? trackCoverUrl(current, 80) : undefined)
  const { data: lyrics } = useLyrics(open ? current : null)
  const activeLine = useActiveLyricLine(lyrics?.synced ? lyrics.lines : undefined)
  // What the middle of the sheet shows: the artwork, the queue, or lyrics.
  const [view, setView] = useState<'cover' | 'queue' | 'lyrics'>('cover')
  const [addOpen, setAddOpen] = useState(false)

  const trackId = current?.id
  useEffect(() => {
    /* eslint-disable react-hooks/set-state-in-effect -- intentional: lyrics belong to one track */
    setView((v) => (v === 'lyrics' ? 'cover' : v))
    /* eslint-enable react-hooks/set-state-in-effect */
  }, [trackId])

  if (!open) return null

  const upNext = queue.slice(index + 1)

  function onSeek(e: React.MouseEvent<HTMLDivElement>) {
    if (durationMs <= 0) return
    const rect = e.currentTarget.getBoundingClientRect()
    if (rect.width <= 0) return
    seekMs(Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width)) * durationMs)
  }

  const iconBtn = (active = false) =>
    [
      'flex h-11 w-11 items-center justify-center rounded-full transition-colors',
      'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
      active ? 'text-accent' : 'text-text-secondary hover:text-text-primary',
    ].join(' ')

  const ambient = palette
    ? {
        // Translucent wash over an opaque base (see CinemaView) so the page
        // behind the sheet never shows through.
        background: `linear-gradient(180deg, ${rgbToCss(palette.rgb, 0.45)} 0%, var(--bg-base) 70%), var(--bg-base)`,
        color: palette.text,
      }
    : undefined

  const pct = durationMs > 0 ? (currentTimeMs / durationMs) * 100 : 0

  return (
    <div
      data-testid="now-playing-overlay"
      className="fixed inset-0 z-40 flex flex-col bg-canvas p-6 md:hidden"
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

      {/* Middle: artwork, queue or lyrics */}
      {view === 'cover' ? (
        <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-6">
          <div className="w-full max-w-xs">
            <Cover
              src={current ? trackCoverUrl(current, 600) || undefined : undefined}
              alt={current?.title ?? 'Nothing playing'}
              size="full"
              rounded="md"
              className="aspect-square shadow-pop"
            />
          </div>
        </div>
      ) : view === 'queue' ? (
        <div className="min-h-0 flex-1 overflow-y-auto py-4" data-testid="now-playing-queue">
          <h2 className="mb-2 text-base font-bold text-text-primary">Up next</h2>
          {upNext.length === 0 ? (
            <p className="text-sm text-text-secondary">Nothing queued after this track.</p>
          ) : (
            <ul className="space-y-1">
              {upNext.map((t, offset) => (
                <li key={`${t.id}-${offset}`}>
                  <button
                    type="button"
                    onClick={() => jumpTo(index + 1 + offset)}
                    className="flex w-full items-center gap-3 rounded p-2 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
                  >
                    <Cover src={trackCoverUrl(t, 80) || undefined} alt={t.title} size={40} rounded="md" />
                    <span className="min-w-0">
                      <span className="block truncate text-sm font-semibold text-text-primary">{t.title}</span>
                      <span className="block truncate text-xs text-text-secondary">{t.artist}</span>
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      ) : (
        <div className="min-h-0 flex-1 py-4" data-testid="now-playing-lyrics">
          {lyrics?.synced ? (
            <LyricsLines lines={lyrics.lines} activeIndex={activeLine} onLineClick={(line) => seekMs(line.timeMs)} size="md" />
          ) : (
            <div className="h-full overflow-y-auto whitespace-pre-wrap text-sm font-semibold leading-6 text-text-secondary">
              {lyrics?.plain}
            </div>
          )}
        </div>
      )}

      {/* Title + add to playlist */}
      <div className="mx-auto mb-4 flex w-full max-w-xs items-center gap-3">
        <div className="min-w-0 flex-1">
          <div className="truncate text-xl font-bold text-text-primary">
            {current ? current.title : 'Nothing playing'}
          </div>
          <div className="mt-1 truncate text-sm text-text-secondary">{current?.artist ?? ''}</div>
        </div>
        {current && <NotForMeMenu track={current} size="md" />}
        {current && (
          <button type="button" aria-label="Add to playlist" onClick={() => setAddOpen(true)} className={iconBtn()}>
            <Icon name="plus" className="h-5 w-5" />
          </button>
        )}
      </div>

      {/* Seek bar — tap anywhere on it to jump */}
      <div className="mx-auto mb-2 w-full max-w-xs">
        <div
          role="slider"
          aria-label="Seek"
          aria-valuemin={0}
          aria-valuemax={durationMs}
          aria-valuenow={currentTimeMs}
          tabIndex={0}
          onClick={onSeek}
          className="flex h-6 w-full cursor-pointer items-center focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
        >
          <div className="h-1 w-full rounded-full bg-border-subtle">
            <div className="h-full rounded-full bg-text-primary" style={{ width: `${pct}%` }} />
          </div>
        </div>
        <div className="flex items-center justify-between text-xs tabular-nums text-text-muted">
          <span>{formatDuration(currentTimeMs)}</span>
          <span>{formatDuration(durationMs)}</span>
        </div>
      </div>

      {/* Transport controls */}
      <div className="mx-auto mb-2 flex w-full max-w-xs items-center justify-between">
        <button
          type="button"
          aria-label="Shuffle"
          aria-pressed={shuffle}
          onClick={toggleShuffle}
          className={iconBtn(shuffle)}
        >
          <Icon name="shuffle" className="h-5 w-5" />
        </button>

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

        <button
          type="button"
          aria-label={`Repeat ${repeat}`}
          aria-pressed={repeat !== 'off'}
          onClick={cycleRepeat}
          className={`relative ${iconBtn(repeat !== 'off')}`}
        >
          <Icon name="repeat" className="h-5 w-5" />
          {repeat === 'one' && (
            <span className="absolute right-1.5 top-1.5 text-[10px] font-extrabold leading-none">1</span>
          )}
        </button>
      </div>

      {/* Secondary row: lyrics and queue swap the middle of the sheet */}
      <div className="mx-auto mb-2 flex w-full max-w-xs items-center justify-between">
        {lyrics != null ? (
          <button
            type="button"
            aria-label="Lyrics"
            aria-pressed={view === 'lyrics'}
            onClick={() => setView(view === 'lyrics' ? 'cover' : 'lyrics')}
            className={iconBtn(view === 'lyrics')}
          >
            <Icon name="mic" className="h-5 w-5" />
          </button>
        ) : (
          <div className="h-11 w-11" aria-hidden="true" />
        )}
        <button
          type="button"
          aria-label="Queue"
          aria-pressed={view === 'queue'}
          onClick={() => setView(view === 'queue' ? 'cover' : 'queue')}
          className={iconBtn(view === 'queue')}
        >
          <Icon name="queue" className="h-5 w-5" />
        </button>
      </div>

      {addOpen && current && <AddToPlaylistMenu track={current} onClose={() => setAddOpen(false)} />}
    </div>
  )
}
