import { useState, useRef } from 'react'
import { Badge, ProgressRing, Icon, Button } from '../ui'
import { DownloadPopover } from './DownloadPopover'
import { PortalMenu } from '../PortalMenu'
import { useDownloads } from '../../lib/downloadStore'
import { postDownload, retryDownload, reqFromResult } from '../../lib/downloadApi'
import { useAdapters } from '../../lib/adaptersApi'
import type { ExternalResult } from '../../lib/types'

interface Props {
  result: ExternalResult
  onPlay?: (libraryTrackId: string) => void
}

/** Returns the list of enabled downloader adapter instances, sorted by priority. */
function useDownloaders() {
  const { data } = useAdapters()
  if (!data) return []
  return data
    .filter((a) => a.type === 'downloader' && a.enabled)
    .sort((a, b) => a.priority - b.priority)
}

export function DownloadAction({ result, onPlay }: Props) {
  const [popoverOpen, setPopoverOpen] = useState(false)
  // Optimistic: flip to "Downloading" the instant the user clicks, before the
  // POST round-trips. The real job (from the store) takes over once it lands.
  const [optimistic, setOptimistic] = useState(false)

  // Failed-state popover
  const [failedMenuOpen, setFailedMenuOpen] = useState(false)
  const [showUrlInput, setShowUrlInput] = useState(false)
  const [urlValue, setUrlValue] = useState('')
  const failedTriggerRef = useRef<HTMLButtonElement>(null)

  const job = useDownloads((s) => s.byExternal(result.source, result.externalId))
  const downloaders = useDownloaders()

  // A completed job that matched a library track is treated as in-library.
  const inLibraryTrackId =
    (result.match?.status === 'in_library' && result.match.libraryTrackId) ||
    (job?.status === 'completed' && job.libraryTrackId) ||
    ''

  // ── helper: enqueue with an optional downloader name ──────────────────────
  function enqueue(downloaderName?: string) {
    setOptimistic(true)
    postDownload(reqFromResult(result, downloaderName))
      .then((j) => useDownloads.getState().upsert(j))
      .catch(() => setOptimistic(false))
  }

  function handleDownloadClick(e: React.MouseEvent) {
    e.stopPropagation()
    if (downloaders.length === 1) {
      enqueue(downloaders[0].name)
    } else {
      setPopoverOpen(true)
    }
  }

  function handlePick(name: string) {
    setPopoverOpen(false)
    enqueue(name)
  }

  // ── 1. In library ─────────────────────────────────────────────────────────
  if (inLibraryTrackId) {
    return (
      <button
        type="button"
        title="In Library"
        aria-label={`Play ${result.title}`}
        onClick={(e) => {
          e.stopPropagation()
          onPlay?.(inLibraryTrackId)
        }}
        className="inline-flex items-center gap-1.5 rounded-full transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
      >
        <Badge kind="in-library">
          <Icon name="check" className="text-xs" />
          In Library
        </Badge>
      </button>
    )
  }

  // ── 2. Active (running or queued) ─────────────────────────────────────────
  // Also covers the optimistic state (clicked, server not yet acknowledged) so
  // the row reads "Downloading" immediately. progress <= 0 stays indeterminate so
  // a just-started (running, 0%) job spins rather than showing an empty ring.
  if ((optimistic && !job) || job?.status === 'running' || job?.status === 'queued') {
    const isIndeterminate = !job || job.status === 'queued' || job.progress <= 0
    return (
      <span className="inline-flex items-center gap-2">
        <ProgressRing
          value={isIndeterminate ? 0 : job.progress}
          size={24}
          indeterminate={isIndeterminate}
        />
        <Badge kind="downloading">Downloading</Badge>
      </span>
    )
  }

  // ── 3. Completed ──────────────────────────────────────────────────────────
  if (job?.status === 'completed') {
    return (
      <Badge kind="downloaded">
        <Icon name="check" className="text-xs" />
        Downloaded
      </Badge>
    )
  }

  // ── 4. Failed ────────────────────────────────────────────────────────────
  if (job?.status === 'failed') {
    const failedJob = job

    function closeFailedMenu() {
      setFailedMenuOpen(false)
      setShowUrlInput(false)
      setUrlValue('')
    }

    function handleUrlSubmit(e: React.FormEvent) {
      e.preventDefault()
      const url = urlValue.trim()
      if (!url || !url.startsWith('http')) return
      void retryDownload(failedJob.id, url)
        .then((j) => {
          useDownloads.getState().upsert(j)
          closeFailedMenu()
        })
        .catch((err) => console.error('[DownloadAction] manual URL retry failed:', err))
    }

    return (
      <span className="relative inline-flex items-center gap-2">
        <Icon name="warn" className="text-xs text-text-muted" />
        <button
          ref={failedTriggerRef}
          type="button"
          aria-label="Retry download"
          onClick={(e) => {
            e.stopPropagation()
            setFailedMenuOpen((o) => !o)
          }}
          className="inline-flex items-center gap-1 rounded-full border border-border-subtle px-2.5 py-1 text-xs font-bold text-text-primary transition-colors hover:bg-raised-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent active:opacity-80"
        >
          <Icon name="retry" className="text-xs" />
          Retry
        </button>

        {failedMenuOpen && (
          <PortalMenu
            triggerRef={failedTriggerRef}
            onClose={closeFailedMenu}
            label="Retry options"
            widthClass="w-56"
          >
            {/* Plain retry */}
            <button
              type="button"
              role="menuitem"
              onClick={(e) => {
                e.stopPropagation()
                void retryDownload(failedJob.id).then((j) => {
                  useDownloads.getState().upsert(j)
                  closeFailedMenu()
                })
              }}
              className="flex w-full items-center gap-2 rounded-t-xl px-3 py-2.5 text-sm text-text-primary hover:bg-raised-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
            >
              <Icon name="retry" className="text-xs" />
              Retry
            </button>

            {/* Download from a link */}
            {!showUrlInput ? (
              <button
                type="button"
                role="menuitem"
                onClick={(e) => {
                  e.stopPropagation()
                  setShowUrlInput(true)
                }}
                className="flex w-full items-center gap-2 rounded-b-xl px-3 py-2.5 text-sm text-text-primary hover:bg-raised-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
              >
                <Icon name="dl" className="text-xs" />
                Download from a link…
              </button>
            ) : (
              <form
                onSubmit={handleUrlSubmit}
                onClick={(e) => e.stopPropagation()}
                className="px-3 pb-3 pt-1"
              >
                <input
                  autoFocus
                  type="url"
                  value={urlValue}
                  onChange={(e) => setUrlValue(e.target.value)}
                  placeholder="Paste a YouTube link"
                  className="w-full rounded-lg border border-border-subtle bg-surface px-2.5 py-1.5 text-xs text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-2 focus:ring-accent"
                  aria-label="Manual download URL"
                />
                <button
                  type="submit"
                  className="mt-2 w-full rounded-lg bg-accent px-2.5 py-1.5 text-xs font-bold text-white hover:opacity-90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent active:opacity-80"
                >
                  Download
                </button>
              </form>
            )}
          </PortalMenu>
        )}
      </span>
    )
  }

  // ── 5. No downloaders ─────────────────────────────────────────────────────
  if (downloaders.length === 0) {
    return (
      <Badge kind="disabled">
        <Icon name="dl" className="text-xs" />
        No downloader
      </Badge>
    )
  }

  // ── 6. Available (≥1 downloader, no active job) ───────────────────────────
  // Just the Download button — the redundant "Available" badge only widened the
  // row's right slot (and shifted the album column as the state changed).
  return (
    <span className="relative inline-flex items-center justify-end gap-2">
      <Button
        variant="secondary"
        size="sm"
        aria-label={`Download ${result.title}`}
        onClick={handleDownloadClick}
      >
        Download
      </Button>

      {popoverOpen && (
        <DownloadPopover
          downloaders={downloaders.map((d) => ({ id: d.id, name: d.name }))}
          trackTitle={result.title}
          onPick={handlePick}
          onClose={() => setPopoverOpen(false)}
        />
      )}
    </span>
  )
}
