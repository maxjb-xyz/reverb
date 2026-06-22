/**
 * PlayerBar — desktop bottom player bar (Phase 3 Spotify-faithful rebuild).
 * Hidden below md; mobile uses MiniPlayer instead.
 *
 * Layout: 3-column grid (30 / 40 / 30) mirroring the mockup .player rule.
 *   Left   — Cover + title/artist + heart
 *   Center — transport controls (shuffle/prev/play/next/repeat) + scrubber
 *   Right  — lyrics / queue / device / volume / mini / full icon buttons
 *
 * Wiring: usePlayer (playerStore) + useUI (uiStore). Keyboard shortcuts preserved
 * from the original PlayerBar (Space, Arrow{Left,Right}, Shift+Arrow{Left,Right}).
 */
import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { usePlayer } from '../../lib/playerStore'
import { useUI } from '../../lib/uiStore'
import { coverUrl } from '../../lib/libraryApi'
import { formatDuration } from '../../lib/types'
import { useAlbumPalette } from '../../lib/useAlbumPalette'
import { rgbToCss } from '../../lib/palette'
import { Cover } from '../ui/Cover'
import { IconButton } from '../ui/IconButton'
import { Icon } from '../ui/Icon'
import { AddToPlaylistMenu } from '../AddToPlaylistMenu'

// ---------------------------------------------------------------------------
// SeekBar — thin 4 px track with a thumb that appears on hover, driven by
// position/duration from the player store. Click-to-seek updates seekMs.
// ---------------------------------------------------------------------------
function SeekBar() {
  const currentTimeMs = usePlayer((s) => s.currentTimeMs)
  const durationMs = usePlayer((s) => s.durationMs)
  const bufferedMs = usePlayer((s) => s.bufferedMs)
  const seekMs = usePlayer((s) => s.seekMs)

  const pct = durationMs > 0 ? (currentTimeMs / durationMs) * 100 : 0
  const bufPct = durationMs > 0 ? (bufferedMs / durationMs) * 100 : 0

  function onClick(e: React.MouseEvent<HTMLDivElement>) {
    if (durationMs <= 0) return
    const rect = e.currentTarget.getBoundingClientRect()
    const ratio = (e.clientX - rect.left) / rect.width
    seekMs(Math.max(0, Math.min(1, ratio)) * durationMs)
  }

  return (
    <div className="flex w-full max-w-[560px] items-center gap-2.5 text-xs text-text-muted">
      <span className="w-9 text-right tabular-nums">{formatDuration(currentTimeMs)}</span>

      {/* Track rail */}
      <div
        role="slider"
        aria-label="Seek"
        aria-valuemin={0}
        aria-valuemax={durationMs}
        aria-valuenow={currentTimeMs}
        onClick={onClick}
        className="group relative h-1 flex-1 cursor-pointer rounded-full bg-border-subtle"
      >
        {/* Buffered range */}
        <div
          className="pointer-events-none absolute inset-y-0 left-0 rounded-full bg-raised-hover"
          style={{ width: `${bufPct}%` }}
        />
        {/* Played range */}
        <div
          className="pointer-events-none absolute inset-y-0 left-0 rounded-full bg-text-primary group-hover:bg-accent"
          style={{ width: `${pct}%` }}
        />
        {/* Thumb — visible on hover */}
        <div
          className="pointer-events-none absolute top-1/2 hidden h-3 w-3 -translate-x-1/2 -translate-y-1/2 rounded-full bg-text-primary group-hover:block"
          style={{ left: `${pct}%` }}
        />
      </div>

      <span className="w-9 tabular-nums">{formatDuration(durationMs)}</span>
    </div>
  )
}

// ---------------------------------------------------------------------------
// PlayerBar (exported)
// ---------------------------------------------------------------------------
export function PlayerBar() {
  const current = usePlayer((s) => s.current)
  const playing = usePlayer((s) => s.playing)
  const shuffle = usePlayer((s) => s.shuffle)
  const repeat = usePlayer((s) => s.repeat)
  const volume = usePlayer((s) => s.volume)
  const toggle = usePlayer((s) => s.toggle)
  const next = usePlayer((s) => s.next)
  const prev = usePlayer((s) => s.prev)
  const seekMs = usePlayer((s) => s.seekMs)
  const currentTimeMs = usePlayer((s) => s.currentTimeMs)
  const setVolume = usePlayer((s) => s.setVolume)
  const toggleShuffle = usePlayer((s) => s.toggleShuffle)
  const cycleRepeat = usePlayer((s) => s.cycleRepeat)

  const togglePanel = useUI((s) => s.togglePanel)
  const rightPanel = useUI((s) => s.rightPanel)

  const navigate = useNavigate()
  const [addMenuOpen, setAddMenuOpen] = useState(false)

  const palette = useAlbumPalette(current?.coverArtId ? coverUrl(current.coverArtId, 80) : undefined)

  // Global keyboard shortcuts. Ignore when typing in an input/textarea.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      const el = e.target as HTMLElement | null
      if (el && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.isContentEditable)) return
      if (e.code === 'Space') {
        e.preventDefault()
        toggle()
      } else if (e.key === 'ArrowRight' && e.shiftKey) {
        e.preventDefault()
        next()
      } else if (e.key === 'ArrowLeft' && e.shiftKey) {
        e.preventDefault()
        prev()
      } else if (e.key === 'ArrowRight') {
        e.preventDefault()
        seekMs(currentTimeMs + 5000)
      } else if (e.key === 'ArrowLeft') {
        e.preventDefault()
        seekMs(Math.max(0, currentTimeMs - 5000))
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [toggle, next, prev, seekMs, currentTimeMs])

  const coverSrc = current?.coverArtId ? coverUrl(current.coverArtId, 80) : undefined

  return (
    <div
      data-testid="player-bar"
      className={[
        'relative hidden h-20 md:grid',
        'grid-cols-[30%_40%_30%] items-center px-2',
        palette ? '' : 'border-t border-border-subtle bg-surface',
      ].join(' ')}
      style={
        palette
          ? { backgroundColor: rgbToCss(palette.rgb), color: palette.text }
          : undefined
      }
    >
      {palette?.scrim && (
        <div className="pointer-events-none absolute inset-0 bg-black/20" />
      )}

      {/* ── LEFT: cover + meta (hugs left; add-to-playlist control lands here) ─ */}
      <div className="relative z-10 flex items-center gap-3.5 pl-2">
        <Cover
          src={coverSrc}
          alt={current?.title ?? 'Nothing playing'}
          size={56}
          rounded="md"
          className="shadow-cover flex-none"
        />
        <div className="min-w-0">
          <div className="truncate text-sm font-semibold text-text-primary">
            {current ? current.title : 'Nothing playing'}
          </div>
          {current?.artist && current.artistId ? (
            <button
              type="button"
              onClick={() => navigate(`/artist/library/${current.artistId}`)}
              className="block max-w-full truncate text-left text-xs text-text-secondary hover:text-text-primary hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
            >
              {current.artist}
            </button>
          ) : (
            <div className="truncate text-xs text-text-secondary">
              {current?.artist ?? ''}
            </div>
          )}
        </div>

        {current && (
          <div className="relative flex-none">
            <IconButton
              name="plus"
              label="Add to playlist"
              size="sm"
              active={addMenuOpen}
              onClick={() => setAddMenuOpen((o) => !o)}
            />
            {addMenuOpen && (
              <AddToPlaylistMenu
                trackId={current.id}
                onClose={() => setAddMenuOpen(false)}
              />
            )}
          </div>
        )}
      </div>

      {/* ── CENTER: transport + scrubber ────────────────────────────────── */}
      <div className="relative z-10 flex flex-col items-center gap-2">
        {/* Transport row */}
        <div className="flex items-center gap-5">
          <IconButton
            name="shuffle"
            label="Shuffle"
            active={shuffle}
            size="sm"
            onClick={toggleShuffle}
          />
          <IconButton
            name="prev"
            label="Previous"
            size="sm"
            onClick={prev}
          />

          {/* Play/pause — white circle, Spotify style */}
          <button
            type="button"
            aria-label={playing ? 'Pause' : 'Play'}
            onClick={toggle}
            className={[
              'inline-grid h-9 w-9 place-items-center rounded-full',
              'bg-text-primary text-surface',
              'transition-transform hover:scale-105',
              'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
            ].join(' ')}
          >
            <Icon name={playing ? 'pause' : 'play'} className="h-[18px] w-[18px]" />
          </button>

          <IconButton
            name="next"
            label="Next"
            size="sm"
            onClick={next}
          />
          <IconButton
            name="repeat"
            label={`Repeat ${repeat}`}
            active={repeat !== 'off'}
            size="sm"
            onClick={cycleRepeat}
          />
        </div>

        {/* Scrubber */}
        <SeekBar />
      </div>

      {/* ── RIGHT: queue + volume ───────────────────────────────────────── */}
      <div className="relative z-10 flex items-center justify-end gap-3 pr-2">
        <IconButton
          name="queue"
          label="Queue"
          active={rightPanel === 'nowplaying'}
          size="sm"
          onClick={() => togglePanel('nowplaying')}
        />

        {/* Volume — icon + slider (styled thumb + accent fill in index.css) */}
        <div className="flex items-center gap-1.5">
          <IconButton name="vol" label="Volume" size="sm" />
          <input
            type="range"
            min={0}
            max={1}
            step={0.01}
            value={volume}
            aria-label="Volume"
            onChange={(e) => setVolume(Number(e.target.value))}
            className="rvb-range h-1 w-24 rounded-full focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
            style={{
              background: `linear-gradient(to right, rgb(var(--color-accent)) ${volume * 100}%, var(--border-subtle) ${volume * 100}%)`,
            }}
          />
        </div>
      </div>
    </div>
  )
}
