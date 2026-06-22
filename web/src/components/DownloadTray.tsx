import { useState } from 'react'
import { useDownloads } from '../lib/downloadStore'
import { useUI } from '../lib/uiStore'
import { cancelDownload, retryDownload, postDownload } from '../lib/downloadApi'
import { useAdapters } from '../lib/adaptersApi'
import { IconButton } from './ui/IconButton'
import { Button } from './ui/Button'
import { Cover } from './ui/Cover'
import { Badge } from './ui/Badge'
import { ProgressRing } from './ui/ProgressRing'
import { Icon } from './ui/Icon'
import type { DownloadJob } from '../lib/types'
import type { AdapterInstance } from '../lib/adaptersApi'

// ── failureMessage ────────────────────────────────────────────────────────────
// Pure helper: maps known error substrings to friendly copy framed with track
// title + downloader context. Always returns a descriptive string — never a
// bare "Failed" or "Error".

// eslint-disable-next-line react-refresh/only-export-components -- failureMessage is a pure helper exported for unit tests alongside the component
export function failureMessage(job: DownloadJob): string {
  const title = job.title ?? job.externalId
  const dl = job.downloaderName || 'the downloader'
  const err = (job.error ?? '').toLowerCase()

  if (!err) {
    return `Couldn't download "${title}" on ${dl}`
  }

  if (err.includes('no match') || err.includes('no matching') || err.includes('source not found')) {
    return `No matching source found for "${title}" on ${dl}`
  }

  if (err.includes('timeout') || err.includes('timed out')) {
    return `Timed out while downloading "${title}" on ${dl}`
  }

  if (err.includes('exit') || err.includes('crashed') || err.includes('killed')) {
    return `${dl} exited with an error while downloading "${title}"`
  }

  if (err.includes('not found') || err.includes('404')) {
    return `"${title}" was not found on ${dl}`
  }

  if (err.includes('auth') || err.includes('unauthorized') || err.includes('forbidden')) {
    return `${dl} authentication failed - check your credentials`
  }

  // Generic fallback: contextual but never bare "Error" / "Failed"
  return `Couldn't download "${title}" on ${dl}`
}

// ── Helpers ────────────────────────────────────────────────────────────────────

function useDownloaders(): AdapterInstance[] {
  const { data } = useAdapters()
  if (!data) return []
  return data
    .filter((a) => a.type === 'downloader' && a.enabled)
    .sort((a, b) => a.priority - b.priority)
}

function nextDownloader(current: string, list: AdapterInstance[]): AdapterInstance | undefined {
  const idx = list.findIndex((d) => d.name === current)
  return idx >= 0 && idx < list.length - 1 ? list[idx + 1] : list.find((d) => d.name !== current)
}

function ProgressBar({ progress }: { progress: number }) {
  if (progress < 0) {
    return (
      <div className="h-1 w-full overflow-hidden rounded-full bg-border-subtle">
        <div className="h-full w-1/3 animate-pulse bg-accent" />
      </div>
    )
  }
  return (
    <div className="h-1 w-full overflow-hidden rounded-full bg-border-subtle">
      <div className="h-full bg-accent" style={{ width: `${progress}%` }} />
    </div>
  )
}

// ── JobRow ────────────────────────────────────────────────────────────────────

function JobRow({ j, downloaders }: { j: DownloadJob; downloaders: AdapterInstance[] }) {
  const [expanded, setExpanded] = useState(false)
  const active = j.status === 'queued' || j.status === 'running'
  const failed = j.status === 'failed'
  const showCancel = active
  const showRetry = failed || j.status === 'canceled'

  const next = failed && downloaders.length > 1 ? nextDownloader(j.downloaderName, downloaders) : undefined

  function handleRetry() {
    void retryDownload(j.id)
  }

  function handleCancel() {
    void cancelDownload(j.id)
  }

  function handleTryNext(downloaderName: string) {
    void postDownload({
      source: j.source,
      externalId: j.externalId,
      artist: j.artist ?? '',
      title: j.title ?? j.externalId,
      album: j.album ?? '',
      isrc: j.isrc,
      downloader: downloaderName,
    }).then((newJob) => useDownloads.getState().upsert(newJob))
  }

  return (
    <li className="rounded-lg border border-border-subtle bg-surface-raised px-3 py-3 transition-colors hover:bg-raised">
      <div className="flex items-center gap-3">
        {/* Cover */}
        <Cover
          src={undefined}
          alt={j.title ?? j.externalId}
          size={40}
        />

        {/* Title + status */}
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-semibold text-text-primary">
            {j.title ?? j.externalId}
          </div>
          {j.artist && (
            <div className="truncate text-xs text-text-secondary">{j.artist}</div>
          )}

          {/* Status line */}
          <div className="mt-1 flex items-center gap-1.5">
            {j.status === 'queued' && (
              <Badge kind="status">Queued</Badge>
            )}
            {j.status === 'running' && (
              <Badge kind="downloading">
                {j.progress >= 0 ? `${j.progress}%` : 'Downloading'}
              </Badge>
            )}
            {j.status === 'completed' && (
              <Badge kind="in-library">
                <Icon name="check" className="w-3 h-3" />
                Done
              </Badge>
            )}
            {j.status === 'canceled' && (
              <Badge kind="status" tone="warning">Canceled</Badge>
            )}
            {failed && (
              <span
                data-testid={`failure-message-${j.id}`}
                className="flex items-center gap-1 text-xs font-medium text-error"
              >
                <Icon name="warn" className="w-3 h-3 flex-none" />
                {failureMessage(j)}
              </span>
            )}
          </div>

          {/* Expandable raw error */}
          {failed && j.error && (
            <button
              type="button"
              aria-label={expanded ? 'Hide raw error' : 'Show raw error'}
              onClick={() => setExpanded((v) => !v)}
              className="mt-0.5 text-xs text-text-muted underline-offset-2 hover:text-text-secondary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent rounded"
            >
              {expanded ? 'Hide details' : 'Show details'}
            </button>
          )}
          {expanded && j.error && (
            <p className="mt-1 rounded bg-raised px-2 py-1 font-mono text-xs text-text-muted break-all">
              {j.error}
            </p>
          )}
        </div>

        {/* Progress ring for active jobs */}
        {(j.status === 'running' || j.status === 'queued') && (
          <ProgressRing
            value={j.progress >= 0 ? j.progress : 0}
            size={28}
            indeterminate={j.status === 'queued' || j.progress < 0}
          />
        )}

        {/* Action buttons */}
        <div className="flex items-center gap-2 flex-none">
          {showCancel && (
            <Button
              variant="ghost"
              size="sm"
              aria-label={`Cancel download of ${j.title ?? j.id}`}
              onClick={handleCancel}
            >
              Cancel
            </Button>
          )}
          {showRetry && (
            <Button
              variant="secondary"
              size="sm"
              aria-label={`Retry download of ${j.title ?? j.id}`}
              onClick={handleRetry}
            >
              <Icon name="retry" className="w-3 h-3 mr-1" />
              Retry
            </Button>
          )}
          {next && (
            <Button
              variant="primary"
              size="sm"
              aria-label={`Try ${next.name} for ${j.title ?? j.id}`}
              onClick={() => handleTryNext(next.name)}
            >
              Try {next.name}
            </Button>
          )}
        </div>
      </div>

      {/* Progress bar for active jobs */}
      {active && (
        <div className="mt-2">
          <ProgressBar progress={j.progress} />
        </div>
      )}
    </li>
  )
}

// ── DownloadTray ──────────────────────────────────────────────────────────────

export function DownloadTray() {
  const rightPanel = useUI((s) => s.rightPanel)
  const closePanel = useUI((s) => s.closePanel)
  const jobs = useDownloads((s) => s.jobs)
  const downloaders = useDownloaders()

  if (rightPanel !== 'downloads') return null

  const list = Object.values(jobs).sort((a, b) => b.createdAt - a.createdAt)

  return (
    <aside
      className={[
        // Sits cleanly inside the right column — no self-gate absolute positioning
        'flex h-full w-full flex-col bg-surface',
        'md:w-80',
        'border-l border-border-subtle',
      ].join(' ')}
    >
      <div className="flex items-center justify-between border-b border-border-subtle px-4 py-3.5">
        <h2 className="text-base font-bold text-text-primary">Download Tray</h2>
        <IconButton name="x" label="Close downloads" size="sm" onClick={closePanel} />
      </div>

      <div className="flex-1 overflow-auto p-2">
        {list.length === 0 && (
          <div className="px-2 py-4 text-sm text-text-muted">No downloads yet.</div>
        )}
        <ul className="space-y-2">
          {list.map((j) => (
            <JobRow key={j.id} j={j} downloaders={downloaders} />
          ))}
        </ul>
      </div>
    </aside>
  )
}
