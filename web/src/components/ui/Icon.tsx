import React from 'react'

export type IconName =
  | 'home'
  | 'search'
  | 'browse'
  | 'back'
  | 'fwd'
  | 'dl'
  | 'plus'
  | 'play'
  | 'pause'
  | 'prev'
  | 'next'
  | 'shuffle'
  | 'repeat'
  | 'heart'
  | 'queue'
  | 'mic'
  | 'device'
  | 'vol'
  | 'mini'
  | 'full'
  | 'sort'
  | 'expand'
  | 'bell'
  | 'check'
  | 'x'
  | 'warn'
  | 'retry'

/**
 * Per-icon descriptor.
 *
 * `filled`  — the glyph renders with fill="currentColor" stroke="none" on the SVG root.
 *             Applies to play, pause, heart (always drawn filled in the design).
 * `content` — the inner SVG markup (paths, circles, rects, etc.).
 *             Paths are copied verbatim from the spotify-faithful.html sprite where available;
 *             clean equivalents were written for: pause, check, x, warn, retry
 *             (those names are absent from the mockup sprite).
 *             The `mini` symbol mixes stroke + inline-filled rect; both are preserved here.
 */
interface IconDef {
  filled?: boolean
  content: React.ReactNode
}

const icons: Record<IconName, IconDef> = {
  // --- From mockup sprite (verbatim path data) ---
  home: {
    filled: true,
    content: <path d="M3 9.5 12 3l9 6.5V20a1 1 0 0 1-1 1h-5v-7H9v7H4a1 1 0 0 1-1-1z" />,
  },
  search: {
    content: (
      <>
        <circle cx="11" cy="11" r="7" />
        <path d="M21 21l-4.3-4.3" />
      </>
    ),
  },
  browse: {
    content: (
      <>
        <rect x="3" y="3" width="7" height="7" rx="1" />
        <rect x="14" y="3" width="7" height="7" rx="1" />
        <rect x="14" y="14" width="7" height="7" rx="1" />
        <rect x="3" y="14" width="7" height="7" rx="1" />
      </>
    ),
  },
  back: {
    content: <path d="M15 18l-6-6 6-6" />,
  },
  fwd: {
    content: <path d="M9 18l6-6-6-6" />,
  },
  dl: {
    content: (
      <>
        <path d="M12 3v12" />
        <path d="M7 10l5 5 5-5" />
        <path d="M5 21h14" />
      </>
    ),
  },
  plus: {
    content: (
      <>
        <circle cx="12" cy="12" r="9" />
        <path d="M12 8v8M8 12h8" />
      </>
    ),
  },
  play: {
    filled: true,
    content: <path d="M6 4l14 8-14 8z" />,
  },
  // pause: clean filled two-bar design (not in mockup sprite)
  pause: {
    filled: true,
    content: (
      <>
        <rect x="6" y="4" width="4" height="16" rx="1" />
        <rect x="14" y="4" width="4" height="16" rx="1" />
      </>
    ),
  },
  prev: {
    content: (
      <>
        <path d="M19 20 9 12l10-8z" />
        <path d="M5 19V5" />
      </>
    ),
  },
  next: {
    content: (
      <>
        <path d="M5 4l10 8-10 8z" />
        <path d="M19 5v14" />
      </>
    ),
  },
  shuffle: {
    content: (
      <>
        <path d="M16 3h5v5" />
        <path d="M4 20 21 3" />
        <path d="M21 16v5h-5" />
        <path d="M15 15l6 6" />
        <path d="M4 4l5 5" />
      </>
    ),
  },
  repeat: {
    content: (
      <>
        <path d="m17 2 4 4-4 4" />
        <path d="M3 11v-1a4 4 0 0 1 4-4h14" />
        <path d="m7 22-4-4 4-4" />
        <path d="M21 13v1a4 4 0 0 1-4 4H3" />
      </>
    ),
  },
  heart: {
    filled: true,
    content: (
      <path d="M19 14c1.49-1.46 3-3.21 3-5.5A5.5 5.5 0 0 0 16.5 3c-1.76 0-3 .5-4.5 2-1.5-1.5-2.74-2-4.5-2A5.5 5.5 0 0 0 2 8.5c0 2.29 1.49 4.04 3 5.5l7 7z" />
    ),
  },
  queue: {
    content: (
      <>
        <path d="M3 6h13M3 12h13M3 18h8" />
        <circle cx="18" cy="17" r="3" />
        <path d="M21 17V7l-3 1" />
      </>
    ),
  },
  mic: {
    content: (
      <>
        <rect x="9" y="2" width="6" height="12" rx="3" />
        <path d="M5 10a7 7 0 0 0 14 0" />
        <path d="M12 19v3" />
      </>
    ),
  },
  device: {
    content: (
      <>
        <rect x="3" y="4" width="18" height="13" rx="2" />
        <path d="M8 21h8" />
      </>
    ),
  },
  vol: {
    content: (
      <>
        <path d="M11 5 6 9H2v6h4l5 4z" />
        <path d="M15.5 8.5a5 5 0 0 1 0 7" />
      </>
    ),
  },
  // mini: stroke outline rect + a filled inset rect (mixed fill/stroke within same icon)
  mini: {
    content: (
      <>
        <rect x="3" y="5" width="18" height="14" rx="2" />
        <rect x="12" y="12" width="7" height="5" rx="1" fill="currentColor" stroke="none" />
      </>
    ),
  },
  full: {
    content: (
      <path d="M8 3H5a2 2 0 0 0-2 2v3M21 8V5a2 2 0 0 0-2-2h-3M3 16v3a2 2 0 0 0 2 2h3M16 21h3a2 2 0 0 0 2-2v-3" />
    ),
  },
  sort: {
    content: (
      <>
        <path d="M4 6h11M4 12h8M4 18h5" />
        <path d="M17 9l3 3 3-3" />
      </>
    ),
  },
  expand: {
    content: (
      <path d="M4 14h6v6M20 10h-6V4M10 14l-6 6M14 10l6-6" />
    ),
  },
  bell: {
    content: (
      <>
        <path d="M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9" />
        <path d="M13.7 21a2 2 0 0 1-3.4 0" />
      </>
    ),
  },

  // --- Clean equivalents for names absent from mockup sprite ---
  // check: standard checkmark
  check: {
    content: <path d="M20 6 9 17l-5-5" />,
  },
  // x: close / dismiss
  x: {
    content: (
      <>
        <path d="M18 6 6 18" />
        <path d="M6 6l12 12" />
      </>
    ),
  },
  // warn: triangle with exclamation
  warn: {
    content: (
      <>
        <path d="M10.3 3.3 2 20h20L13.7 3.3a2 2 0 0 0-3.4 0z" />
        <path d="M12 9v5" />
        <circle cx="12" cy="17" r=".5" fill="currentColor" />
      </>
    ),
  },
  // retry: circular arrow (refresh/retry)
  retry: {
    content: (
      <>
        <path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8" />
        <path d="M3 3v5h5" />
      </>
    ),
  },
}

interface IconProps {
  name: IconName
  className?: string
  'aria-label'?: string
}

export function Icon({ name, className, 'aria-label': ariaLabel }: IconProps) {
  const def = icons[name]
  const isFilled = def.filled ?? false

  const accessibilityProps = ariaLabel
    ? { role: 'img' as const, 'aria-label': ariaLabel }
    : { 'aria-hidden': 'true' as const }

  return (
    <svg
      viewBox="0 0 24 24"
      width="1em"
      height="1em"
      fill={isFilled ? 'currentColor' : 'none'}
      stroke={isFilled ? 'none' : 'currentColor'}
      strokeWidth={isFilled ? undefined : 2}
      strokeLinecap={isFilled ? undefined : 'round'}
      strokeLinejoin={isFilled ? undefined : 'round'}
      className={className}
      {...accessibilityProps}
    >
      {def.content}
    </svg>
  )
}
