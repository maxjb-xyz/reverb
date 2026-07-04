import { Icon } from '../ui/Icon'
import type { DownloadJob } from '../../lib/types'

// StatusLabel is the honest per-status text used in pane + page rows.
export function StatusLabel({ job }: { job: DownloadJob }) {
  switch (job.status) {
    case 'queued':
      return <span className="text-xs font-semibold text-text-muted">Queued</span>
    case 'running':
      return (
        <span className="text-xs font-semibold text-accent">
          {job.progress >= 0 ? `${job.progress}%` : 'Downloading'}
        </span>
      )
    case 'completed':
      return (
        <span className="inline-flex items-center gap-1 text-xs font-semibold text-success">
          <Icon name="check" className="w-3 h-3" />
          Done
        </span>
      )
    case 'canceled':
      return <span className="text-xs font-semibold text-text-muted">Canceled</span>
    case 'failed':
      return (
        <span className="inline-flex items-center gap-1 text-xs font-semibold text-error">
          <Icon name="warn" className="w-3 h-3" />
          Failed
        </span>
      )
    default:
      return null
  }
}

// DownloadProgress is the thin progress bar; it pulses when indeterminate (<0).
export function DownloadProgress({ progress }: { progress: number }) {
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
