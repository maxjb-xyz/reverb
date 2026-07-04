import type { DownloadJob } from '../../lib/types'

// failureMessage maps known error substrings to friendly copy framed with the
// track title + downloader. Always descriptive — never a bare "Failed"/"Error".
// Lives in its own module (not parts.tsx) so that the component file can export
// only components (keeps react-refresh fast-refresh happy).
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
