import { useEffect, useRef, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { Icon } from './Icon'
import { rgbToCss } from '../../lib/palette'
import type { RGB } from '../../lib/palette'

/** The id of the zero-height sticky slot AppShell keeps at the top of <main>. */
export const STICKY_SLOT_ID = 'rvb-sticky-slot'

interface PageHeroProps {
  /** Dominant cover colour; without one the wash falls back to a neutral grey. */
  rgb?: RGB | null
  children: ReactNode
  /** Called with false once the hero has scrolled out of view, true when it is back. */
  onVisibleChange?: (visible: boolean) => void
}

/**
 * PageHero — the colour-washed header band on album, artist and playlist
 * pages. It bleeds to the edges of the content panel and fades into the
 * surface, and it reports when it has scrolled away so the page can show its
 * compact sticky bar.
 */
export function PageHero({ rgb, children, onVisibleChange }: PageHeroProps) {
  const ref = useRef<HTMLDivElement>(null)
  const cb = useRef(onVisibleChange)
  useEffect(() => { cb.current = onVisibleChange }, [onVisibleChange])

  useEffect(() => {
    const el = ref.current
    if (!el || typeof IntersectionObserver === 'undefined') return
    const root = el.closest('main')
    // Treat the hero as gone once only its last ~72px is still on screen, so
    // the bar arrives as the Play button slides under the top edge.
    const io = new IntersectionObserver(
      ([entry]) => cb.current?.(entry.isIntersecting),
      { root, rootMargin: '-72px 0px 0px 0px', threshold: 0 },
    )
    io.observe(el)
    return () => io.disconnect()
  }, [])

  return (
    <div
      ref={ref}
      data-testid="page-hero"
      className={[
        'relative overflow-hidden',
        // Bleed to the panel edges (matches AppShell's page padding).
        '-mx-4 -mt-5 px-4 pt-5 pb-6 sm:-mx-6 sm:-mt-6 sm:px-6 sm:pt-6 lg:-mx-8 lg:px-8',
        'transition-[background] duration-700',
      ].join(' ')}
      style={{
        background: rgb
          ? `linear-gradient(to bottom, ${rgbToCss(rgb, 0.62)} 0%, ${rgbToCss(rgb, 0.22)} 55%, transparent 100%)`
          : 'linear-gradient(to bottom, var(--bg-raised-hover) 0%, transparent 100%)',
      }}
    >
      {children}
    </div>
  )
}

interface StickyHeroBarProps {
  visible: boolean
  title: string
  rgb?: RGB | null
  onPlay?: () => void
  playLabel?: string
  playDisabled?: boolean
}

/**
 * StickyHeroBar — a compact title + Play bar that slides in at the top of the
 * content panel once the page hero has scrolled away. It is portalled into
 * AppShell's sticky slot so it spans the panel and never shifts page layout.
 */
export function StickyHeroBar({ visible, title, rgb, onPlay, playLabel, playDisabled }: StickyHeroBarProps) {
  // Looked up at render: the slot is AppShell's, so it is already in the DOM by
  // the time a page renders (and the bar re-renders whenever it is toggled).
  const slot = typeof document === 'undefined' ? null : document.getElementById(STICKY_SLOT_ID)
  if (!slot) return null

  const tint = rgb ? rgbToCss(rgb, 0.5) : 'transparent'
  return createPortal(
    <div
      data-testid="sticky-hero-bar"
      aria-hidden={!visible}
      inert={!visible}
      className={[
        'absolute inset-x-0 top-0 flex h-16 items-center gap-3 px-4 sm:px-6 lg:px-8',
        'border-b border-white/[.06] shadow-float backdrop-blur-xl',
        'transition-[opacity,transform] duration-300 ease-smooth',
        visible ? 'opacity-100 translate-y-0' : 'pointer-events-none opacity-0 -translate-y-2',
      ].join(' ')}
      style={{ background: `linear-gradient(${tint}, ${tint}), rgb(18 18 18 / .82)` }}
    >
      {onPlay && (
        <button
          type="button"
          aria-label={playLabel ?? `Play ${title}`}
          disabled={playDisabled}
          onClick={onPlay}
          className={[
            'rvb-press grid h-10 w-10 flex-none place-items-center rounded-full bg-accent text-on-accent',
            'hover:scale-105 hover:shadow-glow disabled:opacity-40',
            'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-surface',
          ].join(' ')}
        >
          <Icon name="play" className="h-4 w-4" />
        </button>
      )}
      <span className="truncate text-xl font-extrabold tracking-tight text-text-primary">{title}</span>
    </div>,
    slot,
  )
}
