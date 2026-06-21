import { useState } from 'react'
import { Badge, ProgressRing, Icon, Button } from '../ui'
import { DownloadPopover } from './DownloadPopover'
import { useDownloads } from '../../lib/downloadStore'
import { postDownload, retryDownload } from '../../lib/downloadApi'
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

  const job = useDownloads((s) => s.byExternal(result.source, result.externalId))
  const downloaders = useDownloaders()

  // A completed job that matched a library track is treated as in-library.
  const inLibraryTrackId =
    (result.match?.status === 'in_library' && result.match.libraryTrackId) ||
    (job?.status === 'completed' && job.libraryTrackId) ||
    ''

  // ── helper: enqueue with an optional downloader name ──────────────────────
  function enqueue(downloaderName?: string) {
    void postDownload({
      source: result.source,
      externalId: result.externalId,
      artist: result.artist,
      title: result.title,
      album: result.album,
      isrc: result.isrc,
      downloader: downloaderName,
    }).then((j) => useDownloads.getState().upsert(j))
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
  if (job?.status === 'running' || job?.status === 'queued') {
    const progress = job.progress >= 0 ? job.progress : 0
    return (
      <span className="inline-flex items-center gap-2">
        <ProgressRing value={progress} size={24} />
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
    return (
      <span className="inline-flex items-center gap-2">
        <span className="text-xs font-bold text-error">Failed</span>
        <button
          type="button"
          aria-label="Retry download"
          onClick={(e) => {
            e.stopPropagation()
            void retryDownload(job.id).then((j) => useDownloads.getState().upsert(j))
          }}
          className="inline-flex items-center gap-1 rounded-full border border-border-subtle px-2.5 py-1 text-xs font-bold text-text-primary transition-colors hover:bg-raised-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent active:opacity-80"
        >
          <Icon name="retry" className="text-xs" />
          Retry
        </button>
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
  return (
    <span className="relative inline-flex items-center gap-2">
      <Badge kind="available">
        <Icon name="dl" className="text-xs" />
        Available
      </Badge>
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
