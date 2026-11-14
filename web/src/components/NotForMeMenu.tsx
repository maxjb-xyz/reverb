import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { hideArtist, hideTrack } from '../lib/tasteApi'
import { useToastStore } from '../lib/toastStore'
import { usePlayer } from '../lib/playerStore'
import type { Track } from '../lib/types'
import { Icon } from './ui/Icon'

interface Props {
  track: Track
  /** Visual size of the trigger: the desktop bar uses sm, the phone overlay md. */
  size?: 'sm' | 'md'
  className?: string
}

/**
 * Thumbs-down for the playing track. "Not this song" hides the track from
 * radio and mixes and skips it; "Less of this artist" hides the artist. Both
 * can be undone under Settings → Recommendations.
 */
export function NotForMeMenu({ track, size = 'sm', className }: Props) {
  const [open, setOpen] = useState(false)
  const [pos, setPos] = useState<{ bottom: number; left: number } | null>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  const push = useToastStore((s) => s.push)
  const next = usePlayer((s) => s.next)

  // Open upward from the trigger: it lives in the bottom player bar.
  useLayoutEffect(() => {
    if (!open || !triggerRef.current) return
    const r = triggerRef.current.getBoundingClientRect()
    setPos({ bottom: window.innerHeight - r.top + 6, left: Math.max(8, Math.min(r.left, window.innerWidth - 232)) })
  }, [open])

  useEffect(() => {
    if (!open) return
    menuRef.current?.querySelector<HTMLButtonElement>('[role="menuitem"]')?.focus()
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        e.preventDefault()
        setOpen(false)
        triggerRef.current?.focus()
      }
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [open])

  async function choose(kind: 'track' | 'artist') {
    setOpen(false)
    try {
      if (kind === 'track') {
        await hideTrack(track)
        push(`Got it. "${track.title}" won't come up in radio or mixes.`, 'success')
        next()
      } else {
        await hideArtist(track)
        push(`Got it. Less ${track.artist} from now on.`, 'success')
      }
    } catch {
      push("Couldn't save that. Try again.", 'error')
    }
  }

  const sizeClass = size === 'sm' ? 'w-7 h-7 text-base' : 'w-10 h-10 text-xl'

  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        aria-label="Not for me"
        title="Not for me"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
        className={[
          'inline-grid place-items-center rounded-full transition-colors',
          'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
          open ? 'text-accent' : 'text-text-secondary hover:text-text-primary',
          sizeClass,
          className,
        ].filter(Boolean).join(' ')}
      >
        <Icon name="thumbs-down" />
      </button>
      {open &&
        createPortal(
          <>
            <div className="fixed inset-0 z-40" aria-hidden="true" onClick={() => setOpen(false)} />
            <div
              ref={menuRef}
              role="menu"
              aria-label="Not for me"
              className="fixed z-50 w-56 rounded-lg border border-border-subtle bg-raised p-1 shadow-pop"
              style={pos ? { bottom: pos.bottom, left: pos.left } : { visibility: 'hidden' }}
            >
              <button
                type="button"
                role="menuitem"
                onClick={() => void choose('track')}
                className="block w-full rounded-md px-3 py-2 text-left text-sm text-text-primary hover:bg-raised-hover focus-visible:bg-raised-hover focus-visible:outline-none"
              >
                Not this song
                <span className="block text-xs text-text-secondary">Skip it and keep it out of radio and mixes</span>
              </button>
              {track.artist && (
                <button
                  type="button"
                  role="menuitem"
                  onClick={() => void choose('artist')}
                  className="block w-full rounded-md px-3 py-2 text-left text-sm text-text-primary hover:bg-raised-hover focus-visible:bg-raised-hover focus-visible:outline-none"
                >
                  Less of {track.artist}
                  <span className="block text-xs text-text-secondary">Stop suggesting this artist</span>
                </button>
              )}
            </div>
          </>,
          document.body,
        )}
    </>
  )
}
