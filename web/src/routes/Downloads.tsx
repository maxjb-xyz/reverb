import { useMemo, useState } from 'react'
import { useDownloads } from '../lib/downloadStore'
import {
  pauseQueue,
  resumeQueue,
  clearDownloads,
  cancelDownload,
  retryDownload,
} from '../lib/downloadApi'
import { coverUrl } from '../lib/libraryApi'
import { Button, Chip, Cover, Icon, EmptyState } from '../components/ui'
import { StatusLabel, DownloadProgress } from '../components/download/parts'
import { failureMessage } from '../components/download/failureMessage'
import type { DownloadJob, DownloadStatus } from '../lib/types'

type FilterKey = 'all' | 'running' | 'queued' | 'completed' | 'failed'

const FILTERS: { key: FilterKey; label: string }[] = [
  { key: 'all', label: 'All' },
  { key: 'running', label: 'Downloading' },
  { key: 'queued', label: 'Queued' },
  { key: 'completed', label: 'Completed' },
  { key: 'failed', label: 'Failed' },
]

const TERMINAL: DownloadStatus[] = ['completed', 'failed', 'canceled']

function matchesSearch(j: DownloadJob, q: string): boolean {
  if (!q) return true
  const hay = `${j.title ?? ''} ${j.artist ?? ''}`.toLowerCase()
  return hay.includes(q.toLowerCase())
}

function PageRow({
  j,
  selected,
  onToggle,
}: {
  j: DownloadJob
  selected: boolean
  onToggle: (id: string) => void
}) {
  const active = j.status === 'queued' || j.status === 'running'
  const failed = j.status === 'failed'
  const terminal = TERMINAL.includes(j.status)
  return (
    <li className="flex items-center gap-3 rounded-lg px-2 py-2 transition-colors hover:bg-raised-hover">
      <input
        type="checkbox"
        aria-label={`Select ${j.title ?? j.id}`}
        checked={selected}
        onChange={() => onToggle(j.id)}
        className="h-4 w-4 flex-none accent-accent"
      />
      <Cover src={coverUrl(j.canonicalId || j.coverArtId || '') || undefined} alt={j.title ?? j.externalId} size={40} />
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-semibold text-text-primary">{j.title ?? j.externalId}</div>
        {j.artist && <div className="truncate text-xs text-text-secondary">{j.artist}</div>}
        {failed && <div className="mt-0.5 text-xs text-error">{failureMessage(j)}</div>}
        {j.status === 'running' && (
          <div className="mt-1 max-w-[200px]">
            <DownloadProgress progress={j.progress} />
          </div>
        )}
      </div>
      <div className="w-24 flex-none text-right">{!failed && <StatusLabel job={j} />}</div>
      <div className="flex flex-none items-center gap-2">
        {active && (
          <Button variant="ghost" size="sm" aria-label={`Cancel ${j.title ?? j.id}`} onClick={() => void cancelDownload(j.id)}>
            Cancel
          </Button>
        )}
        {failed && (
          <Button variant="secondary" size="sm" aria-label={`Retry ${j.title ?? j.id}`} onClick={() => void retryDownload(j.id)}>
            Retry
          </Button>
        )}
        {terminal && (
          <Button variant="ghost" size="sm" aria-label={`Clear ${j.title ?? j.id}`} onClick={() => void clearDownloads([j.id])}>
            Clear
          </Button>
        )}
      </div>
    </li>
  )
}

function Group({
  title,
  jobs,
  selected,
  onToggle,
}: {
  title: string
  jobs: DownloadJob[]
  selected: Set<string>
  onToggle: (id: string) => void
}) {
  if (jobs.length === 0) return null
  return (
    <section>
      <h2 className="mb-1 mt-4 px-2 text-[11px] font-bold uppercase tracking-wider text-text-muted">
        {title} · {jobs.length}
      </h2>
      <ul className="space-y-1">
        {jobs.map((j) => (
          <PageRow key={j.id} j={j} selected={selected.has(j.id)} onToggle={onToggle} />
        ))}
      </ul>
    </section>
  )
}

export default function Downloads() {
  const jobs = useDownloads((s) => s.jobs)
  const paused = useDownloads((s) => s.paused)
  const setPaused = useDownloads((s) => s.setPaused)
  const remove = useDownloads((s) => s.remove)

  const [filter, setFilter] = useState<FilterKey>('all')
  const [query, setQuery] = useState('')
  const [selected, setSelected] = useState<Set<string>>(new Set())

  const all = useMemo(
    () => Object.values(jobs).sort((a, b) => b.createdAt - a.createdAt),
    [jobs],
  )

  const counts = {
    running: all.filter((j) => j.status === 'running').length,
    queued: all.filter((j) => j.status === 'queued').length,
    finished: all.filter((j) => TERMINAL.includes(j.status)).length,
  }

  // Apply the chip filter + search to the four display groups.
  const visible = all.filter((j) => matchesSearch(j, query))
  const show = (status: DownloadStatus) => {
    if (filter === 'all') return true
    if (filter === 'completed') return status === 'completed'
    if (filter === 'failed') return status === 'failed'
    if (filter === 'running') return status === 'running'
    if (filter === 'queued') return status === 'queued'
    return true
  }
  const running = show('running') ? visible.filter((j) => j.status === 'running') : []
  const queued = show('queued') ? visible.filter((j) => j.status === 'queued') : []
  const finished =
    filter === 'all' || filter === 'completed' || filter === 'failed'
      ? visible.filter((j) => TERMINAL.includes(j.status) && show(j.status))
      : []

  const toggle = (id: string) =>
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  const clearSelection = () => setSelected(new Set())

  // Prune stale selections: if a selected job is removed from `jobs` (e.g. via
  // WS download.removed or clearFinished), drop it from the selection set so the
  // bulk bar count stays accurate. Uses React's "adjust state during render"
  // pattern (keyed on the `jobs` reference) rather than an effect, avoiding a
  // synchronous setState inside useEffect.
  const [prevJobs, setPrevJobs] = useState(jobs)
  if (jobs !== prevJobs) {
    setPrevJobs(jobs)
    if (selected.size > 0) {
      const next = new Set<string>()
      for (const id of selected) {
        if (jobs[id]) next.add(id)
      }
      if (next.size !== selected.size) setSelected(next)
    }
  }

  async function togglePause() {
    if (paused) {
      setPaused(false) // optimistic; the WS event confirms
      await resumeQueue().catch(() => setPaused(true))
    } else {
      setPaused(true)
      await pauseQueue().catch(() => setPaused(false))
    }
  }

  function clearFinished() {
    const ids = all.filter((j) => TERMINAL.includes(j.status)).map((j) => j.id)
    remove(ids) // optimistic
    clearSelection()
    void clearDownloads(undefined)
  }

  function bulkClear() {
    const ids = [...selected].filter((id) => {
      const j = jobs[id]
      return j && TERMINAL.includes(j.status)
    })
    if (ids.length) {
      remove(ids)
      void clearDownloads(ids)
    }
    clearSelection()
  }

  function bulkCancel() {
    for (const id of selected) {
      const j = jobs[id]
      if (j && (j.status === 'queued' || j.status === 'running')) void cancelDownload(id)
    }
    clearSelection()
  }

  function bulkRetry() {
    for (const id of selected) {
      const j = jobs[id]
      if (j && j.status === 'failed') void retryDownload(id)
    }
    clearSelection()
  }

  const isEmpty = all.length === 0
  const nothingVisible = running.length + queued.length + finished.length === 0

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold text-text-primary">Downloads</h1>
          <p className="mt-0.5 text-xs text-text-secondary">
            {counts.running} downloading · {counts.queued} queued · {counts.finished} finished
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="sm" aria-label={paused ? 'Resume queue' : 'Pause queue'} onClick={togglePause}>
            <span className="inline-flex items-center gap-1.5">
              <Icon name={paused ? 'play' : 'pause'} className="h-4 w-4" />
              {paused ? 'Resume queue' : 'Pause queue'}
            </span>
          </Button>
          <Button variant="secondary" size="sm" aria-label="Clear finished" onClick={clearFinished}>
            Clear finished
          </Button>
        </div>
      </div>

      {/* Toolbar: chips + search */}
      <div className="flex flex-wrap items-center gap-2">
        {FILTERS.map(({ key, label }) => (
          <Chip key={key} selected={filter === key} onClick={() => setFilter(key)}>
            {label}
          </Chip>
        ))}
        <div className="ml-auto flex items-center gap-2 rounded-lg bg-input px-3 py-1.5">
          <Icon name="search" className="h-4 w-4 text-text-muted" />
          <input
            type="text"
            aria-label="Search downloads"
            placeholder="Search downloads"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            className="w-44 bg-transparent text-sm text-text-primary placeholder:text-text-muted outline-none"
          />
        </div>
      </div>

      {/* Bulk-action bar */}
      {selected.size > 0 && (
        <div className="flex items-center gap-2 rounded-lg border border-border-subtle bg-raised px-3 py-2">
          <span className="text-sm font-bold text-text-primary">{selected.size} selected</span>
          <Button variant="ghost" size="sm" aria-label="Cancel selected" onClick={bulkCancel}>
            Cancel
          </Button>
          <Button variant="ghost" size="sm" aria-label="Retry selected" onClick={bulkRetry}>
            Retry
          </Button>
          <Button variant="ghost" size="sm" aria-label="Clear selected" onClick={bulkClear}>
            Clear
          </Button>
          <span className="ml-auto">
            <Button variant="ghost" size="sm" aria-label="Deselect all" onClick={clearSelection}>
              Deselect all
            </Button>
          </span>
        </div>
      )}

      {/* Body */}
      {isEmpty ? (
        <EmptyState icon="dl" title="No downloads yet" hint="Search for a track and hit download — it'll appear here." />
      ) : nothingVisible ? (
        <EmptyState icon="search" title="Nothing matches" hint="Try a different filter or search." />
      ) : (
        <div>
          <Group title="Downloading" jobs={running} selected={selected} onToggle={toggle} />
          <Group title="Queued" jobs={queued} selected={selected} onToggle={toggle} />
          <Group title="Finished" jobs={finished} selected={selected} onToggle={toggle} />
        </div>
      )}
    </div>
  )
}
