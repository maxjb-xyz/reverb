import { useDownloads } from '../lib/downloadStore'
import { useUI } from '../lib/uiStore'
import { cancelDownload, retryDownload } from '../lib/downloadApi'
import { IconButton } from './ui/IconButton'
import type { DownloadJob } from '../lib/types'

function statusLabel(j: DownloadJob): string {
  switch (j.status) {
    case 'queued':
      return 'Queued'
    case 'running':
      return j.progress >= 0 ? `${j.progress}%` : 'Downloading...'
    case 'completed':
      return 'Done'
    case 'failed':
      return j.error ? `Failed: ${j.error}` : 'Failed'
    case 'canceled':
      return 'Canceled'
  }
}

function ProgressBar({ progress }: { progress: number }) {
  // progress < 0 → indeterminate; otherwise determinate width.
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

export function DownloadTray() {
  const rightPanel = useUI((s) => s.rightPanel)
  const closePanel = useUI((s) => s.closePanel)
  const jobs = useDownloads((s) => s.jobs)

  if (rightPanel !== 'downloads') return null

  const list = Object.values(jobs).sort((a, b) => b.createdAt - a.createdAt)

  return (
    <aside
      className={[
        // Mobile: full-screen sheet
        'absolute inset-0 z-30 flex h-full w-full flex-col bg-surface',
        // Desktop md+: side panel on right
        'md:inset-y-0 md:left-auto md:right-0 md:z-20 md:w-80',
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
        <ul className="space-y-1">
          {list.map((j) => {
            const active = j.status === 'queued' || j.status === 'running'
            return (
              <li key={j.id} className="rounded-md px-2 py-2 hover:bg-raised transition-colors">
                <div className="flex items-center gap-2">
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-sm font-medium text-text-primary">
                      {j.title ?? j.externalId}
                    </div>
                    <div className="truncate text-xs text-text-secondary">{statusLabel(j)}</div>
                  </div>
                  {active && (
                    <button
                      type="button"
                      aria-label={`Cancel ${j.id}`}
                      onClick={() => void cancelDownload(j.id)}
                      className={[
                        'text-sm font-medium text-text-secondary',
                        'hover:text-accent transition-colors',
                        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent rounded',
                      ].join(' ')}
                    >
                      Cancel
                    </button>
                  )}
                  {(j.status === 'failed' || j.status === 'canceled') && (
                    <button
                      type="button"
                      aria-label={`Retry ${j.id}`}
                      onClick={() => void retryDownload(j.id)}
                      className={[
                        'text-sm font-medium text-text-secondary',
                        'hover:text-accent transition-colors',
                        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent rounded',
                      ].join(' ')}
                    >
                      Retry
                    </button>
                  )}
                </div>
                {active && (
                  <div className="mt-1.5">
                    <ProgressBar progress={j.progress} />
                  </div>
                )}
              </li>
            )
          })}
        </ul>
      </div>
    </aside>
  )
}
