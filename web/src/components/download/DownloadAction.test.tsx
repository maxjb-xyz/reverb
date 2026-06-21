import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { DownloadAction } from './DownloadAction'
import { useDownloads } from '../../lib/downloadStore'
import type { ExternalResult, DownloadJob } from '../../lib/types'

// ── mocks ────────────────────────────────────────────────────────────────────

const postDownloadMock = vi.fn(
  (_req?: unknown): Promise<DownloadJob> =>
    Promise.resolve({
      id: 'job-1',
      source: 'spotify',
      externalId: 'sp1',
      status: 'queued',
      progress: 0,
      dedupKey: 'dk',
      downloaderName: 'spotDL',
      priority: 0,
      attempts: 0,
      playWhenReady: false,
      createdAt: 1,
      startedAt: 0,
      finishedAt: 0,
    } as DownloadJob),
)

vi.mock('../../lib/downloadApi', () => ({
  postDownload: (req: unknown) => postDownloadMock(req),
  retryDownload: vi.fn(() => Promise.resolve()),
}))

// Mock adaptersApi — controlled per test via useAdaptersMock
let useAdaptersMock = vi.fn(() => ({ data: undefined as unknown }))
vi.mock('../../lib/adaptersApi', () => ({
  useAdapters: () => useAdaptersMock(),
}))

// ── helpers ──────────────────────────────────────────────────────────────────

function makeResult(p: Partial<ExternalResult> = {}): ExternalResult {
  return {
    source: 'spotify',
    externalId: 'sp1',
    title: 'Song',
    artist: 'Artist',
    album: 'Album',
    durationMs: 200_000,
    type: 'track',
    ...p,
  }
}

function makeJob(p: Partial<DownloadJob> = {}): DownloadJob {
  return {
    id: 'job-1',
    dedupKey: 'dk',
    status: 'running',
    progress: 62,
    downloaderName: 'spotDL',
    priority: 0,
    attempts: 0,
    source: 'spotify',
    externalId: 'sp1',
    playWhenReady: false,
    createdAt: 1,
    startedAt: 0,
    finishedAt: 0,
    ...p,
  }
}

// ── suite ────────────────────────────────────────────────────────────────────

describe('DownloadAction', () => {
  const onPlay = vi.fn()

  beforeEach(() => {
    useDownloads.setState({ jobs: {} })
    vi.clearAllMocks()
    // default: 1 enabled downloader
    useAdaptersMock = vi.fn(() => ({
      data: [{ id: 'a1', type: 'downloader', name: 'spotDL', enabled: true, priority: 1, config: {} }],
    }))
  })

  // ── 1. in_library ──────────────────────────────────────────────────────────
  it('in_library → renders in-library badge and calls onPlay with libraryTrackId', () => {
    const result = makeResult({
      match: { status: 'in_library', libraryTrackId: 'lib-t3', method: 'isrc', confidence: 1 },
    })
    render(<DownloadAction result={result} onPlay={onPlay} />)

    expect(screen.getByText('In Library')).toBeInTheDocument()

    const btn = screen.getByRole('button', { name: /play/i })
    fireEvent.click(btn)
    expect(onPlay).toHaveBeenCalledWith('lib-t3')
  })

  // ── 2. job running ─────────────────────────────────────────────────────────
  it('running job → renders ProgressRing with the job progress', () => {
    useDownloads.getState().upsert(makeJob({ status: 'running', progress: 62 }))
    render(<DownloadAction result={makeResult()} onPlay={onPlay} />)

    expect(screen.getByRole('img', { name: /62%/i })).toBeInTheDocument()
    expect(screen.getByText(/downloading/i)).toBeInTheDocument()
  })

  // ── 3. job running indeterminate ──────────────────────────────────────────
  it('running job with progress -1 → indeterminate ring (value=0)', () => {
    useDownloads.getState().upsert(makeJob({ status: 'running', progress: -1 }))
    render(<DownloadAction result={makeResult()} onPlay={onPlay} />)

    // ProgressRing with value=0 (indeterminate passed as 0) or the label says 0%
    const ring = screen.getByRole('img', { name: /0%/i })
    expect(ring).toBeInTheDocument()
  })

  // ── 4. job queued ─────────────────────────────────────────────────────────
  it('queued job → renders ProgressRing (indeterminate, progress -1)', () => {
    useDownloads.getState().upsert(makeJob({ status: 'queued', progress: -1 }))
    render(<DownloadAction result={makeResult()} onPlay={onPlay} />)

    expect(screen.getByRole('img', { name: /0%/i })).toBeInTheDocument()
    expect(screen.getByText(/downloading/i)).toBeInTheDocument()
  })

  // ── 5. job completed ──────────────────────────────────────────────────────
  it('completed job → renders downloaded badge', () => {
    useDownloads.getState().upsert(makeJob({ status: 'completed', progress: 100 }))
    render(<DownloadAction result={makeResult()} onPlay={onPlay} />)

    expect(screen.getByText('Downloaded')).toBeInTheDocument()
  })

  // ── 6. job failed ─────────────────────────────────────────────────────────
  it('failed job → renders "Failed · Retry" affordance', () => {
    useDownloads.getState().upsert(makeJob({ status: 'failed', progress: 0 }))
    render(<DownloadAction result={makeResult()} onPlay={onPlay} />)

    expect(screen.getByText(/failed/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument()
  })

  // ── 7. no job, 1 downloader → immediate postDownload ─────────────────────
  it('1 downloader → Download click calls postDownload immediately (no popover)', async () => {
    render(<DownloadAction result={makeResult()} onPlay={onPlay} />)

    const btn = screen.getByRole('button', { name: /download song/i })
    fireEvent.click(btn)

    await waitFor(() => expect(postDownloadMock).toHaveBeenCalledTimes(1))
    expect(postDownloadMock).toHaveBeenCalledWith(
      expect.objectContaining({
        source: 'spotify',
        externalId: 'sp1',
        artist: 'Artist',
        title: 'Song',
        album: 'Album',
        downloader: 'spotDL',
      }),
    )
    // Popover must NOT be present
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  // ── 8. no job, 2 downloaders → opens popover (no immediate post) ──────────
  it('2 downloaders → Download click opens popover without calling postDownload', () => {
    useAdaptersMock = vi.fn(() => ({
      data: [
        { id: 'a1', type: 'downloader', name: 'spotDL', enabled: true, priority: 1, config: {} },
        { id: 'a2', type: 'downloader', name: 'Lidarr', enabled: true, priority: 2, config: {} },
      ],
    }))

    render(<DownloadAction result={makeResult()} onPlay={onPlay} />)

    const btn = screen.getByRole('button', { name: /download song/i })
    fireEvent.click(btn)

    expect(postDownloadMock).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  // ── 9. no job, 0 downloaders → disabled ───────────────────────────────────
  it('0 downloaders → disabled "No downloader" badge', () => {
    useAdaptersMock = vi.fn(() => ({ data: [] }))

    render(<DownloadAction result={makeResult()} onPlay={onPlay} />)

    expect(screen.getByText(/no downloader/i)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /download/i })).not.toBeInTheDocument()
  })
})
