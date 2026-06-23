import { useState, useEffect } from 'react'
import { createPortal } from 'react-dom'
import { Badge, ProgressRing, Icon, Button } from '../ui'
import { DownloadPopover } from './DownloadPopover'
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

  // Failed-state modal
  const [linkModalOpen, setLinkModalOpen] = useState(false)
  const [urlValue, setUrlValue] = useState('')
  const [urlError, setUrlError] = useState<string | null>(null)

  const job = useDownloads((s) => s.byExternal(result.source, result.externalId))
  const downloaders = useDownloaders()

  // A completed job that matched a library track is treated as in-library.
  const inLibraryTrackId =
    (result.match?.status === 'in_library' && result.match.libraryTrackId) ||
    (job?.status === 'completed' && job.libraryTrackId) ||
    ''

  // Esc key closes the link modal
  useEffect(() => {
    if (!linkModalOpen) return
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        setLinkModalOpen(false)
        setUrlValue('')
        setUrlError(null)
      }
    }
    document.addEventListener('keydown', handleKey)
    return () => document.removeEventListener('keydown', handleKey)
  }, [linkModalOpen])

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

    function closeLinkModal() {
      setLinkModalOpen(false)
      setUrlValue('')
      setUrlError(null)
    }

    function handleUrlSubmit(e: React.FormEvent) {
      e.preventDefault()
      const url = urlValue.trim()
      if (!url || !url.startsWith('http')) {
        setUrlError('Please enter a valid URL starting with http')
        return
      }
      setUrlError(null)
      void retryDownload(failedJob.id, url)
        .then((j) => {
          useDownloads.getState().upsert(j)
          closeLinkModal()
        })
        .catch((err: unknown) => {
          const msg =
            err instanceof Error ? err.message : 'Download failed — check the URL'
          setUrlError(msg)
        })
    }

    return (
      <span className="relative inline-flex items-center gap-2">
        <Icon name="warn" className="text-xs text-text-muted" />

        {/* Direct Retry button — single click retries immediately */}
        <button
          type="button"
          aria-label="Retry download"
          onClick={(e) => {
            e.stopPropagation()
            void retryDownload(failedJob.id)
              .then((j) => useDownloads.getState().upsert(j))
              .catch((err) => console.error('[DownloadAction] retry failed:', err))
          }}
          className="inline-flex items-center gap-1 rounded-full border border-border-subtle px-2.5 py-1 text-xs font-bold text-text-primary transition-colors hover:bg-raised-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent active:opacity-80"
        >
          <Icon name="retry" className="text-xs" />
          Retry
        </button>

        {/* "Download from a link" secondary affordance */}
        <button
          type="button"
          aria-label="Download from a link"
          onClick={(e) => {
            e.stopPropagation()
            setLinkModalOpen(true)
          }}
          className="inline-flex items-center gap-1 rounded-full border border-border-subtle px-2.5 py-1 text-xs text-text-muted transition-colors hover:bg-raised-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent active:opacity-80"
        >
          <Icon name="dl" className="text-xs" />
          Link
        </button>

        {/* Stable centered modal — does NOT close on scroll */}
        {linkModalOpen &&
          createPortal(
            <>
              {/* Backdrop — click closes */}
              <div
                className="fixed inset-0 z-40"
                aria-hidden="true"
                onClick={closeLinkModal}
              />

              {/* Modal panel — centered via fixed positioning */}
              <div
                role="dialog"
                aria-modal="true"
                aria-label="Download from a link"
                className="fixed left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 z-50 w-80 max-w-[calc(100vw-2rem)] rounded-xl border border-border-subtle bg-raised shadow-pop"
                onClick={(e) => e.stopPropagation()}
              >
                <div className="px-4 pt-4 pb-2 flex items-center justify-between">
                  <p className="text-sm font-bold text-text-primary">Download from a link</p>
                  <button
                    type="button"
                    aria-label="Close"
                    onClick={closeLinkModal}
                    className="inline-grid h-7 w-7 place-items-center rounded-lg text-text-muted transition-colors hover:bg-raised-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
                  >
                    <Icon name="x" className="text-sm" />
                  </button>
                </div>

                <form onSubmit={handleUrlSubmit} className="px-4 pb-4 pt-1">
                  <input
                    autoFocus
                    type="url"
                    value={urlValue}
                    onChange={(e) => setUrlValue(e.target.value)}
                    placeholder="Paste a YouTube link"
                    aria-label="Manual download URL"
                    className="w-full rounded-lg border border-border-subtle bg-surface px-2.5 py-1.5 text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-2 focus:ring-accent"
                  />
                  {urlError && (
                    <p role="alert" className="mt-1 text-xs text-text-muted">
                      {urlError}
                    </p>
                  )}
                  <button
                    type="submit"
                    className="mt-3 w-full rounded-lg bg-accent px-2.5 py-1.5 text-sm font-bold text-on-accent hover:opacity-90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent active:opacity-80"
                  >
                    Download
                  </button>
                </form>
              </div>
            </>,
            document.body,
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
