import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { useDownloads } from '../lib/downloadStore'
import { useUI } from '../lib/uiStore'
import { cancelDownload, retryDownload, clearDownload } from '../lib/downloadApi'
import { IconButton } from './ui/IconButton'
import { Button } from './ui/Button'
import { Cover } from './ui/Cover'
import { Icon } from './ui/Icon'
import { StatusLabel, DownloadProgress, failureMessage } from './download/parts'
import type { DownloadJob } from '../lib/types'

const TIDY_DELAY_MS = 5000

// ── Row ─────────────────────────────────────────────────────────────────────
function TrayRow({ j }: { j: DownloadJob }) {
  const active = j.status === 'queued' || j.status === 'running'
  const failed = j.status === 'failed'

  return (
    <li className="rounded-lg px-2 py-2 transition-colors hover:bg-raised-hover">
      <div className="flex items-center gap-3">
        <Cover src={undefined} alt={j.title ?? j.externalId} size={36} />
        <div className="min-w-0 flex-1">
          <div className="truncate text-[13px] font-semibold text-text-primary">{j.title ?? j.externalId}</div>
          {j.artist && <div className="truncate text-xs text-text-secondary">{j.artist}</div>}
          <div className="mt-1">
            {failed ? (
              <span data-testid={`failure-${j.id}`} className="text-xs font-medium text-error">
                {failureMessage(j)}
              </span>
            ) : j.status !== 'queued' ? (
              <StatusLabel job={j} />
            ) : null}
          </div>
        </div>

        {/* Trailing action */}
        <div className="flex-none">
          {active && (
            <IconButton name="x" label={`Cancel ${j.title ?? j.id}`} size="sm" onClick={() => void cancelDownload(j.id)} />
          )}
          {failed && (
            <Button variant="secondary" size="sm" aria-label={`Retry ${j.title ?? j.id}`} onClick={() => void retryDownload(j.id)}>
              <Icon name="retry" className="w-3 h-3 mr-1" />
              Retry
            </Button>
          )}
          {(j.status === 'completed' || j.status === 'canceled') && (
            <IconButton name="x" label={`Clear ${j.title ?? j.id}`} size="sm" onClick={() => void clearDownload(j.id)} />
          )}
        </div>
      </div>
      {j.status === 'running' && (
        <div className="mt-2">
          <DownloadProgress progress={j.progress} />
        </div>
      )}
    </li>
  )
}

function Section({ title, count, jobs }: { title: string; count: number; jobs: DownloadJob[] }) {
  if (jobs.length === 0) return null
  return (
    <div>
      <div className="mt-3 mb-1 px-2 text-[11px] font-bold uppercase tracking-wider text-text-muted">
        {title} · {count}
      </div>
      <ul className="space-y-1">
        {jobs.map((j) => (
          <TrayRow key={j.id} j={j} />
        ))}
      </ul>
    </div>
  )
}

// ── DownloadTray ──────────────────────────────────────────────────────────────
export function DownloadTray() {
  const rightPanel = useUI((s) => s.rightPanel)
  const closePanel = useUI((s) => s.closePanel)
  const jobs = useDownloads((s) => s.jobs)
  const running = useDownloads((s) => s.running)
  const queued = useDownloads((s) => s.queued)
  const completed = useDownloads((s) => s.completed)
  const failed = useDownloads((s) => s.failed)

  const runningJobs = running()
  const queuedJobs = queued()
  const completedJobs = completed()
  const failedJobs = failed()
  const total = Object.keys(jobs).length
  const activeCount = runningJobs.length + queuedJobs.length

  // Auto-tidy: hide completed rows ~5s after the queue goes fully idle. Failed
  // rows are sticky; active rows always show. View-only — never deletes records.
  const [tidied, setTidied] = useState(false)
  useEffect(() => {
    if (activeCount > 0) {
      setTidied(false)
      return
    }
    const t = setTimeout(() => setTidied(true), TIDY_DELAY_MS)
    return () => clearTimeout(t)
  }, [activeCount])
  const shownCompleted = tidied ? [] : completedJobs

  if (rightPanel !== 'downloads') return null

  const isEmpty = total === 0
  const nothingToShow = runningJobs.length + queuedJobs.length + shownCompleted.length + failedJobs.length === 0

  return (
    <aside className="flex h-full w-full flex-col border-l border-border-subtle bg-surface md:w-80">
      <div className="flex items-center justify-between border-b border-border-subtle px-4 py-3.5">
        <h2 className="text-base font-bold text-text-primary">Downloads</h2>
        <IconButton name="x" label="Close downloads" size="sm" onClick={closePanel} />
      </div>

      {!isEmpty && (
        <Link
          to="/downloads"
          onClick={closePanel}
          aria-label="See all downloads"
          className="mx-3 mt-3 flex items-center justify-between rounded-lg bg-raised px-3 py-2 text-[13px] font-semibold text-text-primary transition-colors hover:bg-raised-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
        >
          <span>See all downloads</span>
          <span className="text-accent">{total} →</span>
        </Link>
      )}

      <div className="flex-1 overflow-auto px-1 pb-3">
        {isEmpty || nothingToShow ? (
          <div className="flex h-full flex-col items-center justify-center gap-2 px-6 text-center">
            <div className="grid h-14 w-14 place-items-center rounded-full bg-raised">
              <Icon name="dl" className="h-6 w-6 text-text-muted" />
            </div>
            <p className="text-sm font-bold text-text-primary">Nothing downloading</p>
            <p className="text-xs text-text-muted">Search for a track and hit download — it'll show up here and land in your library.</p>
          </div>
        ) : (
          <>
            <Section title="Downloading" count={runningJobs.length} jobs={runningJobs} />
            <Section title="Queued" count={queuedJobs.length} jobs={queuedJobs} />
            <Section title="Done" count={shownCompleted.length} jobs={shownCompleted} />
            <Section title="Failed" count={failedJobs.length} jobs={failedJobs} />
          </>
        )}
      </div>
    </aside>
  )
}
