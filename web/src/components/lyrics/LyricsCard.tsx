import { useLyrics } from '../../lib/lyricsApi'
import { usePlayer } from '../../lib/playerStore'
import { useUI } from '../../lib/uiStore'
import { useActiveLyricLine } from '../../lib/useActiveLyricLine'

// Compact lyrics teaser for the Now Playing panel: a 3-line window around the
// active line; clicking opens the fullscreen lyrics view.
export function LyricsCard() {
  const current = usePlayer((s) => s.current)
  const openLyrics = useUI((s) => s.openLyrics)
  const { data: lyrics } = useLyrics(current)
  const activeIndex = useActiveLyricLine(lyrics?.synced ? lyrics.lines : undefined)

  if (!lyrics) return null

  let preview: { text: string; active: boolean }[]
  if (lyrics.synced) {
    const start = Math.max(0, (activeIndex < 0 ? 0 : activeIndex) - 1)
    preview = lyrics.lines.slice(start, start + 3).map((line, i) => ({
      text: line.text || '♪',
      active: start + i === activeIndex,
    }))
  } else {
    preview = lyrics.plain
      .split('\n')
      .filter((l) => l.trim() !== '')
      .slice(0, 3)
      .map((text) => ({ text, active: false }))
  }

  return (
    <button
      type="button"
      data-testid="lyrics-card"
      onClick={openLyrics}
      className="mt-3.5 block w-full overflow-hidden rounded-lg bg-raised p-3.5 text-left transition-colors hover:bg-raised-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
    >
      <div className="mb-2 text-sm font-bold text-text-primary">Lyrics</div>
      {preview.map((line, i) => (
        <div
          key={i}
          data-active={line.active || undefined}
          className={[
            'truncate text-sm font-semibold leading-6',
            line.active ? 'text-text-primary' : 'text-text-secondary',
          ].join(' ')}
        >
          {line.text}
        </div>
      ))}
    </button>
  )
}
