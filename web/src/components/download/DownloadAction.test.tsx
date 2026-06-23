import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { DownloadAction } from './DownloadAction'
import { useDownloads } from '../../lib/downloadStore'
import type { ExternalResult, DownloadJob } from '../../lib/types'

// ── mocks ────────────────────────────────────────────────────────────────────

const postDownloadMock = vi.fn(
  // eslint-disable-next-line @typescript-eslint/no-unused-vars -- req param exists for TypeScript compatibility with the mock wrapper
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

const retryDownloadMock = vi.fn(
  (_id: string, _manualUrl?: string): Promise<DownloadJob> =>
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
  retryDownload: (...args: Parameters<typeof retryDownloadMock>) => retryDownloadMock(...args),
  reqFromResult: (r: { source: string; externalId: string; artist: string; title: string; album: string; isrc?: string; durationMs?: number }, downloader?: string) => ({
    source: r.source,
    externalId: r.externalId,
    artist: r.artist,
    title: r.title,
    album: r.album,
    isrc: r.isrc,
    durationMs: r.durationMs,
    downloader,
  }),
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
  it('running job with progress -1 → indeterminate ring (aria-label "Loading")', () => {
    useDownloads.getState().upsert(makeJob({ status: 'running', progress: -1 }))
    render(<DownloadAction result={makeResult()} onPlay={onPlay} />)

    // Indeterminate ring has aria-label "Loading" and aria-busy
    const ring = screen.getByRole('img', { name: /loading/i })
    expect(ring).toBeInTheDocument()
    expect(ring).toHaveAttribute('aria-busy', 'true')
  })

  // ── 4. job queued ─────────────────────────────────────────────────────────
  it('queued job → renders indeterminate ProgressRing with aria-label "Loading"', () => {
    useDownloads.getState().upsert(makeJob({ status: 'queued', progress: -1 }))
    render(<DownloadAction result={makeResult()} onPlay={onPlay} />)

    const ring = screen.getByRole('img', { name: /loading/i })
    expect(ring).toBeInTheDocument()
    expect(ring).toHaveAttribute('aria-busy', 'true')
    expect(screen.getByText(/downloading/i)).toBeInTheDocument()
  })

  // ── 5. job completed ──────────────────────────────────────────────────────
  it('completed job → renders downloaded badge', () => {
    useDownloads.getState().upsert(makeJob({ status: 'completed', progress: 100 }))
    render(<DownloadAction result={makeResult()} onPlay={onPlay} />)

    expect(screen.getByText('Downloaded')).toBeInTheDocument()
  })

  // ── 6. job failed ─────────────────────────────────────────────────────────
  it('failed job → renders Retry affordance without "Failed" text', () => {
    useDownloads.getState().upsert(makeJob({ status: 'failed', progress: 0 }))
    render(<DownloadAction result={makeResult()} onPlay={onPlay} />)

    expect(screen.getByRole('button', { name: /retry download/i })).toBeInTheDocument()
    expect(screen.queryByText(/^failed$/i)).not.toBeInTheDocument()
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

  // ── 10. failed: Retry option visible ─────────────────────────────────────
  it('failed job → clicking trigger opens menu with Retry option', () => {
    useDownloads.getState().upsert(makeJob({ status: 'failed', progress: 0 }))
    render(<DownloadAction result={makeResult()} onPlay={onPlay} />)

    const trigger = screen.getByRole('button', { name: /retry download/i })
    fireEvent.click(trigger)

    // Menu should be open; PortalMenu renders role="menu"
    expect(screen.getByRole('menu', { name: /retry options/i })).toBeInTheDocument()
    // Retry menuitem visible
    expect(screen.getByRole('menuitem', { name: /^retry$/i })).toBeInTheDocument()
  })

  // ── 11. failed: "Download from a link…" affordance visible ───────────────
  it('failed job → menu shows "Download from a link…" affordance', () => {
    useDownloads.getState().upsert(makeJob({ status: 'failed', progress: 0 }))
    render(<DownloadAction result={makeResult()} onPlay={onPlay} />)

    fireEvent.click(screen.getByRole('button', { name: /retry download/i }))

    expect(screen.getByRole('menuitem', { name: /download from a link/i })).toBeInTheDocument()
  })

  // ── 12. failed: valid URL submit calls retryDownload(id, url) ────────────
  it('entering a valid URL and submitting calls retryDownload with jobId and url', async () => {
    useDownloads.getState().upsert(makeJob({ status: 'failed', progress: 0 }))
    render(<DownloadAction result={makeResult()} onPlay={onPlay} />)

    // Open menu
    fireEvent.click(screen.getByRole('button', { name: /retry download/i }))
    // Click "Download from a link…" to reveal input
    fireEvent.click(screen.getByRole('menuitem', { name: /download from a link/i }))

    const input = screen.getByRole('textbox', { name: /manual download url/i })
    fireEvent.change(input, { target: { value: 'https://youtube.com/watch?v=abc' } })

    const submitBtn = screen.getByRole('button', { name: /^download$/i })
    fireEvent.click(submitBtn)

    await waitFor(() => expect(retryDownloadMock).toHaveBeenCalledTimes(1))
    expect(retryDownloadMock).toHaveBeenCalledWith('job-1', 'https://youtube.com/watch?v=abc')
  })

  // ── 13. failed: plain Retry calls retryDownload(id) with no second arg ───
  it('plain Retry calls retryDownload with jobId only (no manualUrl)', async () => {
    useDownloads.getState().upsert(makeJob({ status: 'failed', progress: 0 }))
    render(<DownloadAction result={makeResult()} onPlay={onPlay} />)

    // Open menu then click Retry menuitem
    fireEvent.click(screen.getByRole('button', { name: /retry download/i }))
    fireEvent.click(screen.getByRole('menuitem', { name: /^retry$/i }))

    await waitFor(() => expect(retryDownloadMock).toHaveBeenCalledTimes(1))
    expect(retryDownloadMock).toHaveBeenCalledWith('job-1')
    expect(retryDownloadMock).not.toHaveBeenCalledWith('job-1', expect.anything())
  })

  // ── 14. non-failed states: no retry/link affordance ──────────────────────
  it.each([
    ['missing', undefined],
    ['queued', makeJob({ status: 'queued', progress: 0 })],
    ['running', makeJob({ status: 'running', progress: 50 })],
    ['completed', makeJob({ status: 'completed', progress: 100, libraryTrackId: undefined })],
  ])('%s state → no "Download from a link" affordance', (_label, jobOrUndefined) => {
    if (jobOrUndefined) useDownloads.getState().upsert(jobOrUndefined)
    render(<DownloadAction result={makeResult()} onPlay={onPlay} />)

    expect(screen.queryByRole('button', { name: /retry download/i })).not.toBeInTheDocument()
    expect(screen.queryByText(/download from a link/i)).not.toBeInTheDocument()
  })

  it('in-library state → no "Download from a link" affordance', () => {
    const result = makeResult({
      match: { status: 'in_library', libraryTrackId: 'lib-t3', method: 'isrc', confidence: 1 },
    })
    render(<DownloadAction result={result} onPlay={onPlay} />)

    expect(screen.queryByRole('button', { name: /retry download/i })).not.toBeInTheDocument()
    expect(screen.queryByText(/download from a link/i)).not.toBeInTheDocument()
  })
})
