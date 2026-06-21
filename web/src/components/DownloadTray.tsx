import { useDownloads } from '../lib/downloadStore'
import { useUI } from '../lib/uiStore'
import { cancelDownload, retryDownload } from '../lib/downloadApi'
import type { DownloadJob } from '../lib/types'

function statusLabel(j: DownloadJob): string {
  switch (j.status) {
    case 'queued':
      return 'Queued'
    case 'running':
      return j.progress >= 0 ? `${j.progress}%` : 'Downloading…'
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
    return <div className="h-1 w-full overflow-hidden rounded bg-neutral-800"><div className="h-full w-1/3 animate-pulse bg-accent" /></div>
  }
  return (
    <div className="h-1 w-full overflow-hidden rounded bg-neutral-800">
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
    <aside className="absolute inset-0 z-30 flex h-full w-full flex-col border-neutral-800 bg-neutral-950/95 backdrop-blur md:inset-y-0 md:left-auto md:right-0 md:z-20 md:w-80 md:border-l">
      <div className="flex items-center justify-between border-b border-neutral-800 p-4">
        <h2 className="text-lg font-bold">Download Tray</h2>
        <button type="button" aria-label="Close downloads" onClick={closePanel} className="text-neutral-400 hover:text-white">
          ✕
        </button>
      </div>
      <div className="flex-1 overflow-auto p-2">
        {list.length === 0 && <div className="px-2 py-4 text-sm text-neutral-500">No downloads yet.</div>}
        <ul className="space-y-2">
          {list.map((j) => {
            const active = j.status === 'queued' || j.status === 'running'
            return (
              <li key={j.id} className="rounded px-2 py-2 hover:bg-neutral-900">
                <div className="flex items-center gap-2">
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-sm">{j.title ?? j.externalId}</div>
                    <div className="truncate text-xs text-neutral-400">{statusLabel(j)}</div>
                  </div>
                  {active && (
                    <button
                      type="button"
                      aria-label={`Cancel ${j.id}`}
                      onClick={() => void cancelDownload(j.id)}
                      className="text-neutral-500 hover:text-accent"
                    >
                      Cancel
                    </button>
                  )}
                  {(j.status === 'failed' || j.status === 'canceled') && (
                    <button
                      type="button"
                      aria-label={`Retry ${j.id}`}
                      onClick={() => void retryDownload(j.id)}
                      className="text-neutral-400 hover:text-accent"
                    >
                      Retry
                    </button>
                  )}
                </div>
                {active && <div className="mt-1"><ProgressBar progress={j.progress} /></div>}
              </li>
            )
          })}
        </ul>
      </div>
    </aside>
  )
}
