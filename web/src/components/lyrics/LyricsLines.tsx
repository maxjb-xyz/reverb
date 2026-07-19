import { useEffect, useRef } from 'react'
import type { LyricLine } from '../../lib/lyricsApi'

// Shared synced-lyrics renderer: dims inactive lines, keeps the active line
// centered via auto-scroll, and pauses auto-scroll for 3s after the user
// scrolls manually (Spotify behavior).
export function LyricsLines({
  lines,
  activeIndex,
  onLineClick,
  size = 'lg',
}: {
  lines: LyricLine[]
  activeIndex: number
  onLineClick?: (line: LyricLine) => void
  size?: 'lg' | 'md'
}) {
  const containerRef = useRef<HTMLDivElement>(null)
  const activeRef = useRef<HTMLButtonElement>(null)
  const manualUntil = useRef(0)

  useEffect(() => {
    if (Date.now() < manualUntil.current) return
    activeRef.current?.scrollIntoView?.({ behavior: 'smooth', block: 'center' })
  }, [activeIndex])

  const sizeCls = size === 'lg' ? 'text-3xl leading-relaxed' : 'text-xl leading-relaxed'

  return (
    <div
      ref={containerRef}
      data-testid="lyrics-lines"
      className="h-full overflow-y-auto py-[40vh]"
      onWheel={() => {
        manualUntil.current = Date.now() + 3000
      }}
      onTouchMove={() => {
        manualUntil.current = Date.now() + 3000
      }}
    >
      {lines.map((line, i) => {
        const active = i === activeIndex
        return (
          <button
            key={`${line.timeMs}-${i}`}
            ref={active ? activeRef : undefined}
            type="button"
            data-active={active || undefined}
            aria-current={active || undefined}
            onClick={onLineClick ? () => onLineClick(line) : undefined}
            className={[
              'block w-full text-left font-black tracking-tight transition-colors duration-200',
              sizeCls,
              'py-1.5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
              active ? 'text-text-primary' : 'text-text-primary/40 hover:text-text-primary/70',
            ].join(' ')}
          >
            {line.text || '♪'}
          </button>
        )
      })}
    </div>
  )
}
