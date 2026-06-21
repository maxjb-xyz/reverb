import { useEffect, useRef } from 'react'
import { Icon } from '../ui'

interface Downloader {
  id: string
  name: string
}

interface DownloadPopoverProps {
  downloaders: Downloader[]
  trackTitle: string
  onPick: (name: string) => void
  onClose: () => void
}

const FOCUSABLE = 'button, [href], input, [tabindex]:not([tabindex="-1"])'

export function DownloadPopover({
  downloaders,
  trackTitle,
  onPick,
  onClose,
}: DownloadPopoverProps) {
  const panelRef = useRef<HTMLDivElement>(null)

  // Focus trap + Esc close
  useEffect(() => {
    const previouslyFocused = document.activeElement as HTMLElement | null

    // Move focus to first focusable element inside the panel
    const panel = panelRef.current
    if (panel) {
      const focusable = Array.from(panel.querySelectorAll<HTMLElement>(FOCUSABLE))
      focusable[0]?.focus()
    }

    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        onClose()
        return
      }

      if (e.key === 'Tab' && panelRef.current) {
        const focusable = Array.from(
          panelRef.current.querySelectorAll<HTMLElement>(FOCUSABLE),
        ).filter((el) => !el.hasAttribute('disabled'))

        if (focusable.length === 0) return

        const first = focusable[0]
        const last = focusable[focusable.length - 1]

        if (e.shiftKey) {
          // Shift+Tab on first → wrap to last
          if (document.activeElement === first) {
            e.preventDefault()
            last.focus()
          }
        } else {
          // Tab on last → wrap to first
          if (document.activeElement === last) {
            e.preventDefault()
            first.focus()
          }
        }
      }
    }

    document.addEventListener('keydown', handleKey)
    return () => {
      document.removeEventListener('keydown', handleKey)
      // Restore focus to the element that was focused before the popover opened
      previouslyFocused?.focus()
    }
  }, [onClose])

  return (
    <>
      {/* Backdrop — covers the viewport, click closes */}
      <div
        data-testid="popover-backdrop"
        className="fixed inset-0 z-20"
        aria-hidden="true"
        onClick={onClose}
      />

      {/* Popover panel */}
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-label={`Download "${trackTitle}"`}
        className="absolute right-0 top-full z-30 mt-2 w-72 rounded-xl border border-border-subtle bg-raised shadow-pop"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="px-3 pb-1 pt-3">
          <p className="text-sm font-bold text-text-primary">
            Download &ldquo;{trackTitle}&rdquo;
          </p>
          <p className="mt-0.5 text-xs text-text-muted">
            We&rsquo;ll fetch the closest match and add it to your library.
          </p>
        </div>

        {/* Downloader list */}
        <ul className="p-1.5" role="list">
          {downloaders.map((dl, idx) => (
            <li key={dl.id}>
              <button
                type="button"
                aria-label={dl.name}
                onClick={() => onPick(dl.name)}
                className="flex w-full items-center gap-3 rounded-lg px-2.5 py-2 text-left transition-colors hover:bg-raised-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent active:opacity-80"
              >
                {/* Downloader icon placeholder */}
                <span className="flex h-8 w-8 flex-none items-center justify-center rounded-lg bg-surface text-accent">
                  <Icon name="dl" className="text-base" />
                </span>

                <span className="flex-1 min-w-0">
                  <span className="block text-sm font-bold text-text-primary">{dl.name}</span>
                </span>

                {idx === 0 && (
                  <span className="text-xs font-extrabold uppercase tracking-wider text-accent">
                    Recommended
                  </span>
                )}
              </button>
            </li>
          ))}
        </ul>
      </div>
    </>
  )
}
