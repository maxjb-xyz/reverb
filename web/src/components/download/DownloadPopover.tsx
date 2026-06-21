import { useEffect } from 'react'
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

export function DownloadPopover({
  downloaders,
  trackTitle,
  onPick,
  onClose,
}: DownloadPopoverProps) {
  // Esc closes
  useEffect(() => {
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', handleKey)
    return () => document.removeEventListener('keydown', handleKey)
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
