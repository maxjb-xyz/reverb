import { useState, useEffect, useRef } from 'react'
import { createPortal } from 'react-dom'
import { Badge, ProgressRing, Icon, Button } from '../ui'
import { DownloadPopover } from './DownloadPopover'
import { useDownloads } from '../../lib/downloadStore'
import { postDownload, retryDownload, reqFromResult } from '../../lib/downloadApi'
import { postRequest, useRequestStore } from '../../lib/requestApi'
import { useAdapters } from '../../lib/adaptersApi'
import { useSettings } from '../../lib/settingsApi'
import { useAuthStore } from '../../lib/authStore'
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
  const modalPanelRef = useRef<HTMLDivElement>(null)

  // Lidarr album disclosure — holds the downloader name awaiting user confirm
  const [pendingLidarr, setPendingLidarr] = useState<string | null>(null)

  const settings = useSettings()
  const defaultDownloader = settings.data?.defaultDownloader ?? ''

  const job = useDownloads((s) => s.byExternal(result.source, result.externalId))
  const downloaders = useDownloaders()

  // Defense-in-depth: the backend enforces these caps regardless.
  const canDownload = useAuthStore((s) => s.can('auto_approve'))
  const canRequest = useAuthStore((s) => s.can('request'))

  // For request-only users: find a matching pending/approved request so we can
  // show "Requested" instead of "Request" once one exists.
  const matchingRequest = useRequestStore((s) =>
    canRequest && !canDownload ? s.byExternal(result.source, result.externalId) : undefined,
  )

  // A completed job that matched a library track is treated as in-library.
  const inLibraryTrackId =
    (result.match?.status === 'in_library' && result.match.libraryTrackId) ||
    (job?.status === 'completed' && job.libraryTrackId) ||
    ''

  // Reset modal state whenever the job leaves the failed status (prevents auto-reopen
  // after a failed → running → failed cycle without a user gesture).
  useEffect(() => {
    if (job?.status !== 'failed') {
      setLinkModalOpen(false)
      setUrlValue('')
      setUrlError(null)
    }
  }, [job?.status])

  // Focus trap + Esc close for the link modal — mirrors ImportPlaylistDialog pattern.
  const FOCUSABLE = 'button, [href], input, [tabindex]:not([tabindex="-1"])'
  useEffect(() => {
    if (!linkModalOpen) return
    const previouslyFocused = document.activeElement as HTMLElement | null

    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        setLinkModalOpen(false)
        setUrlValue('')
        setUrlError(null)
        return
      }
      if (e.key === 'Tab' && modalPanelRef.current) {
        const focusable = Array.from(
          modalPanelRef.current.querySelectorAll<HTMLElement>(FOCUSABLE),
        ).filter((el) => !el.hasAttribute('disabled'))
        if (focusable.length === 0) return
        const first = focusable[0]
        const last = focusable[focusable.length - 1]
        if (e.shiftKey) {
          if (document.activeElement === first) {
            e.preventDefault()
            last.focus()
          }
        } else if (document.activeElement === last) {
          e.preventDefault()
          first.focus()
        }
      }
    }

    document.addEventListener('keydown', handleKey)
    return () => {
      document.removeEventListener('keydown', handleKey)
      previouslyFocused?.focus()
    }
  }, [linkModalOpen])

  // ── helper: enqueue with an optional downloader name ──────────────────────
  function enqueue(downloaderName?: string) {
    setOptimistic(true)
    postDownload(reqFromResult(result, downloaderName))
      .then((j) => useDownloads.getState().upsert(j))
      .catch(() => setOptimistic(false))
  }

  function chooseDownloader(name: string) {
    if (name === 'lidarr') {
      setPendingLidarr(name)
      return
    }
    enqueue(name)
  }

  function handleDownloadClick(e: React.MouseEvent) {
    e.stopPropagation()
    if (downloaders.length === 1) {
      chooseDownloader(downloaders[0].name)
      return
    }
    // ≥2 downloaders: use the default if it's set AND still enabled, else ask.
    const def = downloaders.find((d) => d.name === defaultDownloader)
    if (def) {
      chooseDownloader(def.name)
    } else {
      setPopoverOpen(true)
    }
  }

  function handlePick(name: string) {
    setPopoverOpen(false)
    chooseDownloader(name)
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

  // ── Capability gate ───────────────────────────────────────────────────────
  // Three branches: auto_approve → full download control; request → Request
  // affordance; neither → nothing.
  if (!canDownload) {
    if (canRequest) {
      // "Requested" if a pending/approved request exists, else "Request" button.
      const isRequested =
        matchingRequest?.status === 'pending' || matchingRequest?.status === 'approved'
      return isRequested ? (
        <button
          type="button"
          disabled
          aria-label="Requested"
          className="inline-flex items-center gap-1 rounded-full border border-border-subtle px-2.5 py-1 text-xs font-bold text-text-muted opacity-60 cursor-default"
        >
          Requested
        </button>
      ) : (
        <button
          type="button"
          aria-label="Request"
          onClick={(e) => {
            e.stopPropagation()
            postRequest({
              source: result.source,
              externalId: result.externalId,
              title: result.title,
              artist: result.artist,
              album: result.album,
              isrc: result.isrc,
              durationMs: result.durationMs,
              coverArtId: result.coverArtId,
              coverUrl: result.coverUrl,
            })
              .then((req) => useRequestStore.getState().upsert(req))
              .catch((err) => console.error('[DownloadAction] postRequest failed:', err))
          }}
          className="inline-flex items-center gap-1 rounded-full border border-border-subtle px-2.5 py-1 text-xs font-bold text-text-primary transition-colors hover:bg-raised-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent active:opacity-80"
        >
          Request
        </button>
      )
    }
    return null
  }

  // ── 2a. Queued (incl. optimistic post-click) ─────────────────────────────
  // A worker hasn't picked it up yet — show "Queued" with an indeterminate ring,
  // NOT a fake progress %. (New jobs are created queued, so the optimistic state
  // is honestly "queued".)
  if ((optimistic && !job) || job?.status === 'queued') {
    return (
      <span className="inline-flex items-center gap-2">
        <ProgressRing value={0} size={24} indeterminate />
        <Badge kind="status">Queued</Badge>
      </span>
    )
  }

  // ── 2b. Running (a worker is downloading it) ─────────────────────────────
  if (job?.status === 'running') {
    const isIndeterminate = job.progress <= 0
    return (
      <span className="inline-flex items-center gap-2">
        <ProgressRing value={isIndeterminate ? 0 : job.progress} size={24} indeterminate={isIndeterminate} />
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
      try {
        const parsed = new URL(url)
        if (!/^https?:$/.test(parsed.protocol)) {
          setUrlError('Enter a valid URL')
          return
        }
      } catch {
        setUrlError('Enter a valid URL')
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
                ref={modalPanelRef}
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
                    <p role="alert" className="mt-1 text-xs text-error">
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
  const hasActiveDefault = downloaders.length > 1 && downloaders.some((d) => d.name === defaultDownloader)
  return (
    <span className="relative inline-flex items-center justify-end gap-1">
      <Button
        variant="secondary"
        size="sm"
        aria-label={`Download ${result.title}`}
        onClick={handleDownloadClick}
      >
        Download
      </Button>

      {hasActiveDefault && (
        <button
          type="button"
          aria-label="Choose downloader"
          onClick={(e) => { e.stopPropagation(); setPopoverOpen(true) }}
          className="inline-grid h-7 w-6 place-items-center rounded-full border border-border-subtle text-text-secondary transition-colors hover:bg-raised-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
        >
          <Icon name="dl" className="text-xs" />
        </button>
      )}

      {popoverOpen && (
        <DownloadPopover
          downloaders={downloaders.map((d) => ({ id: d.id, name: d.name }))}
          trackTitle={result.title}
          onPick={handlePick}
          onClose={() => setPopoverOpen(false)}
        />
      )}

      {pendingLidarr &&
        createPortal(
          <>
            <div className="fixed inset-0 z-40" aria-hidden="true" onClick={() => setPendingLidarr(null)} />
            <div
              role="dialog"
              aria-modal="true"
              aria-label="Confirm Lidarr download"
              className="fixed left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 z-50 w-80 max-w-[calc(100vw-2rem)] rounded-xl border border-border-subtle bg-raised p-4 shadow-pop"
              onClick={(e) => e.stopPropagation()}
            >
              <p className="text-sm font-bold text-text-primary">Download the whole album?</p>
              <p className="mt-1 text-xs text-text-secondary">
                Lidarr fetches the full album{result.album ? ` "${result.album}"` : ''}, not just "{result.title}".
              </p>
              <div className="mt-3 flex justify-end gap-2">
                <Button variant="ghost" size="sm" aria-label="Cancel" onClick={() => setPendingLidarr(null)}>
                  Cancel
                </Button>
                <Button
                  variant="primary"
                  size="sm"
                  aria-label="Confirm Lidarr album download"
                  onClick={() => { const n = pendingLidarr; setPendingLidarr(null); enqueue(n!) }}
                >
                  Download album
                </Button>
              </div>
            </div>
          </>,
          document.body,
        )}
    </span>
  )
}
