import { useEffect } from 'react'
import { usePlayer } from '../lib/playerStore'
import { useUI } from '../lib/uiStore'
import { coverUrl } from '../lib/libraryApi'
import { formatDuration } from '../lib/types'

// SeekBar is waveform-STYLED (CSS bars, not real peaks). It shows played
// progress, the buffered range, and accepts click-to-seek. True peaks deferred.
function SeekBar() {
  const currentTimeMs = usePlayer((s) => s.currentTimeMs)
  const durationMs = usePlayer((s) => s.durationMs)
  const bufferedMs = usePlayer((s) => s.bufferedMs)
  const seekMs = usePlayer((s) => s.seekMs)

  const pct = durationMs > 0 ? (currentTimeMs / durationMs) * 100 : 0
  const bufPct = durationMs > 0 ? (bufferedMs / durationMs) * 100 : 0

  // 48 static "waveform" bars; heights are deterministic so SSR/test is stable.
  const bars = Array.from({ length: 48 }, (_, i) => 30 + ((i * 37) % 70))

  function onClick(e: React.MouseEvent<HTMLDivElement>) {
    if (durationMs <= 0) return
    const rect = e.currentTarget.getBoundingClientRect()
    const ratio = (e.clientX - rect.left) / rect.width
    seekMs(Math.max(0, Math.min(1, ratio)) * durationMs)
  }

  return (
    <div className="flex items-center gap-2 text-xs text-neutral-500">
      <span className="w-10 text-right tabular-nums">{formatDuration(currentTimeMs)}</span>
      <div
        role="slider"
        aria-label="Seek"
        aria-valuemin={0}
        aria-valuemax={durationMs}
        aria-valuenow={currentTimeMs}
        onClick={onClick}
        className="relative h-8 flex-1 cursor-pointer overflow-hidden rounded"
      >
        {/* buffered range */}
        <div className="absolute inset-y-0 left-0 bg-neutral-700/40" style={{ width: `${bufPct}%` }} />
        {/* waveform bars */}
        <div className="absolute inset-0 flex items-center gap-px px-px">
          {bars.map((h, i) => {
            const barPct = ((i + 0.5) / bars.length) * 100
            const played = barPct <= pct
            return (
              <div
                key={i}
                className={`flex-1 rounded-sm ${played ? 'bg-accent' : 'bg-neutral-600'}`}
                style={{ height: `${h}%` }}
              />
            )
          })}
        </div>
      </div>
      <span className="w-10 tabular-nums">{formatDuration(durationMs)}</span>
    </div>
  )
}

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

  return (
    <div className="flex h-20 items-center gap-4 border-t border-neutral-800 px-4">
      {/* left: art + meta */}
      <div className="flex min-w-0 flex-1 items-center gap-3">
        {current?.coverArtId ? (
          <img src={coverUrl(current.coverArtId, 80)} alt="" className="h-12 w-12 rounded object-cover" />
        ) : (
          <div className="h-12 w-12 rounded bg-neutral-800" />
        )}
        <div className="min-w-0">
          <div className="truncate text-sm font-medium">{current ? current.title : 'Nothing playing'}</div>
          <div className="truncate text-xs text-neutral-400">{current?.artist ?? ''}</div>
        </div>
      </div>

      {/* center: transport + seek */}
      <div className="flex min-w-0 flex-[2] flex-col gap-1">
        <div className="flex items-center justify-center gap-4">
          <button
            type="button"
            aria-label="Shuffle"
            onClick={toggleShuffle}
            className={shuffle ? 'text-accent' : 'text-neutral-400 hover:text-neutral-200'}
          >
            ⤮
          </button>
          <button type="button" aria-label="Previous" onClick={prev} className="text-neutral-300 hover:text-white">
            ⏮
          </button>
          <button
            type="button"
            aria-label={playing ? 'Pause' : 'Play'}
            onClick={toggle}
            className="flex h-9 w-9 items-center justify-center rounded-full bg-white text-black"
          >
            {playing ? '⏸' : '▶'}
          </button>
          <button type="button" aria-label="Next" onClick={next} className="text-neutral-300 hover:text-white">
            ⏭
          </button>
          <button
            type="button"
            aria-label={`Repeat ${repeat}`}
            onClick={cycleRepeat}
            className={repeat !== 'off' ? 'text-accent' : 'text-neutral-400 hover:text-neutral-200'}
          >
            {repeat === 'one' ? '🔂' : '🔁'}
          </button>
        </div>
        <SeekBar />
      </div>

      {/* right: volume + panel buttons */}
      <div className="flex flex-1 items-center justify-end gap-3">
        <input
          type="range"
          min={0}
          max={1}
          step={0.01}
          value={volume}
          aria-label="Volume"
          onChange={(e) => setVolume(Number(e.target.value))}
          className="w-24 accent-[rgb(var(--color-accent))]"
        />
        <button
          type="button"
          onClick={() => togglePanel('queue')}
          className={`rounded px-2 py-1 text-sm ${rightPanel === 'queue' ? 'text-accent' : 'text-neutral-300 hover:text-white'}`}
        >
          Queue
        </button>
        <button
          type="button"
          disabled
          title="Downloads (coming in M3)"
          className="cursor-not-allowed rounded px-2 py-1 text-sm text-neutral-600"
        >
          Downloads
        </button>
      </div>
    </div>
  )
}
