import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { DownloadAction } from './DownloadAction'
import { useDownloads } from '../../lib/downloadStore'
import { useAuthStore } from '../../lib/authStore'
import type { ExternalResult, DownloadJob } from '../../lib/types'

function setCaps(capabilities: string[]) {
  useAuthStore.setState({
    me: { id: 'u', username: 'u', roleId: 'r', roleName: 'R', isOwner: false, capabilities },
    loading: false,
  })
}

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

vi.mock('../../lib/settingsApi', () => ({
  useSettings: () => ({ data: { accentColor: '#F0354B', dynamicBackground: true, defaultDownloader: 'spotdl' } }),
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
    // default: user can download, 1 enabled downloader
    setCaps(['auto_approve'])
    useAdaptersMock = vi.fn(() => ({
      data: [{ id: 'a1', type: 'downloader', name: 'spotDL', enabled: true, priority: 1, config: {} }],
    }))
  })

  // ── capability gating ────────────────────────────────────────────────────────
  it('without auto_approve → does NOT render the Download button for a not-in-library result', () => {
    setCaps([]) // user lacks auto_approve
    render(<DownloadAction result={makeResult()} onPlay={onPlay} />)

    expect(screen.queryByRole('button', { name: /download/i })).not.toBeInTheDocument()
    expect(screen.queryByText(/no downloader/i)).not.toBeInTheDocument()
  })

  it('without auto_approve → still renders the in-library Play affordance', () => {
    setCaps([]) // user lacks auto_approve
    const result = makeResult({
      match: { status: 'in_library', libraryTrackId: 'lib-t3', method: 'isrc', confidence: 1 },
    })
    render(<DownloadAction result={result} onPlay={onPlay} />)

    expect(screen.getByText('In Library')).toBeInTheDocument()
    const btn = screen.getByRole('button', { name: /play/i })
    fireEvent.click(btn)
    expect(onPlay).toHaveBeenCalledWith('lib-t3')
  })

  it('with auto_approve → renders the Download button for a not-in-library result', () => {
    setCaps(['auto_approve'])
    render(<DownloadAction result={makeResult()} onPlay={onPlay} />)
    expect(screen.getByRole('button', { name: /download song/i })).toBeInTheDocument()
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
  it('queued job → renders indeterminate ProgressRing with aria-label "Loading" and "Queued" badge', () => {
    useDownloads.getState().upsert(makeJob({ status: 'queued', progress: -1 }))
    render(<DownloadAction result={makeResult()} onPlay={onPlay} />)

    const ring = screen.getByRole('img', { name: /loading/i })
    expect(ring).toBeInTheDocument()
    expect(ring).toHaveAttribute('aria-busy', 'true')
    expect(screen.getByText('Queued')).toBeInTheDocument()
    expect(screen.queryByText(/downloading/i)).not.toBeInTheDocument()
  })

  // ── 4b. queued vs running ─────────────────────────────────────────────────
  it('shows Queued for a queued job and Downloading for a running job', () => {
    useDownloads.setState({
      jobs: {
        j: { id: 'j', dedupKey: 'j', status: 'queued', progress: 0, downloaderName: 'spotdl', priority: 0, attempts: 0, source: 'spotify', externalId: 'ext1', playWhenReady: false, createdAt: 1, startedAt: 0, finishedAt: 0 },
      },
      paused: false,
    })
    const result = { source: 'spotify', externalId: 'ext1', title: 'T', artist: 'A', album: 'Al' } as never
    const { rerender } = render(<DownloadAction result={result} />)
    expect(screen.getByText('Queued')).toBeInTheDocument()

    useDownloads.setState({
      jobs: {
        j: { id: 'j', dedupKey: 'j', status: 'running', progress: 42, downloaderName: 'spotdl', priority: 0, attempts: 0, source: 'spotify', externalId: 'ext1', playWhenReady: false, createdAt: 1, startedAt: 0, finishedAt: 0 },
      },
      paused: false,
    })
    rerender(<DownloadAction result={result} />)
    expect(screen.getByText('Downloading')).toBeInTheDocument()
    // Symmetric negative: once running, 'Queued' must be gone.
    expect(screen.queryByText('Queued')).not.toBeInTheDocument()
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

  // ── 10. failed → direct Retry button calls retryDownload(id) with no url ──
  it('failed job → clicking Retry button calls retryDownload(id) immediately with no url', async () => {
    useDownloads.getState().upsert(makeJob({ status: 'failed', progress: 0 }))
    render(<DownloadAction result={makeResult()} onPlay={onPlay} />)

    const retryBtn = screen.getByRole('button', { name: /retry download/i })
    fireEvent.click(retryBtn)

    await waitFor(() => expect(retryDownloadMock).toHaveBeenCalledTimes(1))
    expect(retryDownloadMock).toHaveBeenCalledWith('job-1')
    expect(retryDownloadMock).not.toHaveBeenCalledWith('job-1', expect.anything())

    // No menu/dialog should open
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  // ── 11. failed → "Download from a link" trigger opens modal ───────────────
  it('failed job → "Download from a link" button opens modal (role="dialog")', () => {
    useDownloads.getState().upsert(makeJob({ status: 'failed', progress: 0 }))
    render(<DownloadAction result={makeResult()} onPlay={onPlay} />)

    const linkBtn = screen.getByRole('button', { name: /download from a link/i })
    fireEvent.click(linkBtn)

    expect(screen.getByRole('dialog', { name: /download from a link/i })).toBeInTheDocument()
  })

  // ── 12. modal → submitting valid URL calls retryDownload(id, url) ─────────
  it('entering a valid URL in the modal and submitting calls retryDownload with jobId and url', async () => {
    useDownloads.getState().upsert(makeJob({ status: 'failed', progress: 0 }))
    render(<DownloadAction result={makeResult()} onPlay={onPlay} />)

    // Open modal
    fireEvent.click(screen.getByRole('button', { name: /download from a link/i }))

    const input = screen.getByRole('textbox', { name: /manual download url/i })
    fireEvent.change(input, { target: { value: 'https://youtube.com/watch?v=abc' } })

    const submitBtn = screen.getByRole('button', { name: /^download$/i })
    fireEvent.click(submitBtn)

    await waitFor(() => expect(retryDownloadMock).toHaveBeenCalledTimes(1))
    expect(retryDownloadMock).toHaveBeenCalledWith('job-1', 'https://youtube.com/watch?v=abc')
  })

  // ── 13. modal → does NOT close on window scroll ───────────────────────────
  it('modal stays open when window scroll event fires', () => {
    useDownloads.getState().upsert(makeJob({ status: 'failed', progress: 0 }))
    render(<DownloadAction result={makeResult()} onPlay={onPlay} />)

    // Open modal
    fireEvent.click(screen.getByRole('button', { name: /download from a link/i }))
    expect(screen.getByRole('dialog')).toBeInTheDocument()

    // Dispatch a scroll event on the window
    window.dispatchEvent(new Event('scroll'))

    // Modal must still be open
    expect(screen.getByRole('dialog')).toBeInTheDocument()
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

  // ── 15. modal auto-resets when status leaves failed ───────────────────────
  it('modal closes and does NOT auto-reopen when status cycles failed→running→failed', async () => {
    useDownloads.getState().upsert(makeJob({ status: 'failed', progress: 0 }))
    const { rerender } = render(<DownloadAction result={makeResult()} onPlay={onPlay} />)

    // Open the modal
    fireEvent.click(screen.getByRole('button', { name: /download from a link/i }))
    expect(screen.getByRole('dialog')).toBeInTheDocument()

    // Job transitions to running — modal must close
    useDownloads.getState().upsert(makeJob({ status: 'running', progress: 10 }))
    rerender(<DownloadAction result={makeResult()} onPlay={onPlay} />)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    // Job fails again — modal must NOT auto-reopen (no user gesture)
    useDownloads.getState().upsert(makeJob({ status: 'failed', progress: 0 }))
    rerender(<DownloadAction result={makeResult()} onPlay={onPlay} />)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  // ── 16. focus trap: Tab from last focusable wraps to first ────────────────
  it('modal traps focus: Tab from last focusable element wraps to first', () => {
    useDownloads.getState().upsert(makeJob({ status: 'failed', progress: 0 }))
    render(<DownloadAction result={makeResult()} onPlay={onPlay} />)

    fireEvent.click(screen.getByRole('button', { name: /download from a link/i }))
    expect(screen.getByRole('dialog')).toBeInTheDocument()

    // The modal contains: Close button, URL input, Download submit button.
    // Tab from the last focusable (submit) should wrap to the first (close button or input).
    const submitBtn = screen.getByRole('button', { name: /^download$/i })
    submitBtn.focus()
    expect(document.activeElement).toBe(submitBtn)

    fireEvent.keyDown(document, { key: 'Tab', shiftKey: false })

    // Focus should have wrapped inside the modal (not escaped to page behind)
    const modal = screen.getByRole('dialog')
    expect(modal.contains(document.activeElement)).toBe(true)
  })

  // ── 17. invalid URL ("httpfoo") shows error, does NOT call retryDownload ──
  it('invalid URL "httpfoo" shows inline error and does not call retryDownload', () => {
    useDownloads.getState().upsert(makeJob({ status: 'failed', progress: 0 }))
    render(<DownloadAction result={makeResult()} onPlay={onPlay} />)

    fireEvent.click(screen.getByRole('button', { name: /download from a link/i }))

    const input = screen.getByRole('textbox', { name: /manual download url/i })
    fireEvent.change(input, { target: { value: 'httpfoo' } })

    const form = input.closest('form')!
    fireEvent.submit(form)

    expect(screen.getByRole('alert')).toBeInTheDocument()
    expect(retryDownloadMock).not.toHaveBeenCalled()
  })

  // ── 18. valid URL calls retryDownload ─────────────────────────────────────
  it('valid https URL calls retryDownload with the URL', async () => {
    useDownloads.getState().upsert(makeJob({ status: 'failed', progress: 0 }))
    render(<DownloadAction result={makeResult()} onPlay={onPlay} />)

    fireEvent.click(screen.getByRole('button', { name: /download from a link/i }))

    const input = screen.getByRole('textbox', { name: /manual download url/i })
    fireEvent.change(input, { target: { value: 'https://youtube.com/watch?v=ok' } })

    fireEvent.click(screen.getByRole('button', { name: /^download$/i }))

    await waitFor(() => expect(retryDownloadMock).toHaveBeenCalledTimes(1))
    expect(retryDownloadMock).toHaveBeenCalledWith('job-1', 'https://youtube.com/watch?v=ok')
  })

  // ── 19. default downloader: normal click enqueues spotdl directly ──────────
  it('default downloader: a normal click enqueues spotdl directly (no popover)', () => {
    useAdaptersMock = vi.fn(() => ({
      data: [
        { id: 'a1', type: 'downloader', name: 'spotdl', enabled: true, priority: 1, config: {} },
        { id: 'a2', type: 'downloader', name: 'lidarr', enabled: true, priority: 2, config: {} },
      ],
    }))
    const result = { source: 'spotify', externalId: 'e1', title: 'T', artist: 'A', album: 'Al' } as never
    render(<DownloadAction result={result} />)
    fireEvent.click(screen.getByRole('button', { name: 'Download T' }))
    expect(postDownloadMock).toHaveBeenCalledWith(expect.objectContaining({ downloader: 'spotdl' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  // ── 20. override caret → pick Lidarr → album disclosure → confirm enqueues lidarr
  it('override caret → pick Lidarr → album disclosure → confirm enqueues lidarr', () => {
    useAdaptersMock = vi.fn(() => ({
      data: [
        { id: 'a1', type: 'downloader', name: 'spotdl', enabled: true, priority: 1, config: {} },
        { id: 'a2', type: 'downloader', name: 'lidarr', enabled: true, priority: 2, config: {} },
      ],
    }))
    const result = { source: 'spotify', externalId: 'e2', title: 'T2', artist: 'A', album: 'Discovery' } as never
    render(<DownloadAction result={result} />)
    // The split-button caret opens the picker even though a default is set.
    fireEvent.click(screen.getByRole('button', { name: 'Choose downloader' }))
    fireEvent.click(screen.getByRole('button', { name: 'lidarr' }))
    // Lidarr routes through the album disclosure — NOT enqueued yet.
    expect(screen.getByText(/whole album/i)).toBeInTheDocument()
    expect(screen.getByText(/Discovery/)).toBeInTheDocument()
    expect(postDownloadMock).not.toHaveBeenCalledWith(expect.objectContaining({ downloader: 'lidarr' }))
    // Confirm → enqueues via lidarr.
    fireEvent.click(screen.getByRole('button', { name: 'Confirm Lidarr album download' }))
    expect(postDownloadMock).toHaveBeenCalledWith(expect.objectContaining({ downloader: 'lidarr' }))
  })
})
