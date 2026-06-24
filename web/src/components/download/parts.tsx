import { Icon } from '../ui/Icon'
import type { DownloadJob } from '../../lib/types'

// failureMessage maps known error substrings to friendly copy framed with the
// track title + downloader. Always descriptive — never a bare "Failed"/"Error".
export function failureMessage(job: DownloadJob): string {
  const title = job.title ?? job.externalId
  const dl = job.downloaderName || 'the downloader'
  const err = (job.error ?? '').toLowerCase()

  if (!err) return `Couldn't download "${title}" on ${dl}`
  if (err.includes('no match') || err.includes('no matching') || err.includes('source not found'))
    return `No matching source found for "${title}" on ${dl}`
  if (err.includes('timeout') || err.includes('timed out')) return `Timed out while downloading "${title}" on ${dl}`
  if (err.includes('exit') || err.includes('crashed') || err.includes('killed'))
    return `${dl} exited with an error while downloading "${title}"`
  if (err.includes('not found') || err.includes('404')) return `"${title}" was not found on ${dl}`
  if (err.includes('auth') || err.includes('unauthorized') || err.includes('forbidden'))
    return `${dl} authentication failed - check your credentials`
  return `Couldn't download "${title}" on ${dl}`
}

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
