import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { DownloadTray, failureMessage } from './DownloadTray'
import { useDownloads } from '../lib/downloadStore'
import { useUI } from '../lib/uiStore'
import type { DownloadJob } from '../lib/types'

vi.mock('../lib/downloadApi', () => ({
  cancelDownload: vi.fn(() => Promise.resolve()),
  retryDownload: vi.fn(() => Promise.resolve({} as DownloadJob)),
  postDownload: vi.fn(() => Promise.resolve({} as DownloadJob)),
}))
vi.mock('../lib/adaptersApi', () => ({
  useAdapters: vi.fn(() => ({ data: [] })),
}))
import { cancelDownload, retryDownload, postDownload } from '../lib/downloadApi'
import { useAdapters } from '../lib/adaptersApi'
// eslint-disable-next-line @typescript-eslint/no-explicit-any
type AnyQuery = any

function job(p: Partial<DownloadJob>): DownloadJob {
  return {
    id: 'j1',
    dedupKey: 'dk',
    status: 'running',
    progress: 40,
    downloaderName: 'spotdl',
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

describe('failureMessage()', () => {
  it('maps "no matching" / "no match" error to a source-specific message containing the track title', () => {
    const j = job({ id: 'j1', title: 'Bones', downloaderName: 'spotDL', error: 'no matching source found' })
    const msg = failureMessage(j)
    expect(msg).toContain('Bones')
    expect(msg).not.toBe('Failed')
    expect(msg).not.toBe('Error')
  })

  it('maps "exit" / "exited" errors to a downloader-contextual message', () => {
    const j = job({ id: 'j1', title: 'Karma Police', downloaderName: 'Lidarr', error: 'process exited with code 1' })
    const msg = failureMessage(j)
    expect(msg).not.toBe('Failed')
    expect(msg).not.toBe('Error')
    expect(msg).toMatch(/lidarr/i)
  })

  it('maps "timeout" / "timed out" error to a timeout message', () => {
    const j = job({ id: 'j1', title: 'Exit Music', downloaderName: 'spotDL', error: 'request timed out' })
    const msg = failureMessage(j)
    expect(msg.toLowerCase()).toContain('timed out')
    expect(msg).not.toBe('Failed')
  })

  it('falls back to contextual copy (track + downloader) for generic or empty error — never bare "Error"', () => {
    const j = job({ id: 'j1', title: 'Let Down', downloaderName: 'spotDL', error: '' })
    const msg = failureMessage(j)
    expect(msg).toContain('Let Down')
    expect(msg).not.toBe('Error')
    expect(msg).not.toBe('Failed')
  })

  it('falls back to contextual copy when error is undefined', () => {
    const j = job({ id: 'j1', title: 'Karma Police', downloaderName: 'Lidarr', error: undefined })
    const msg = failureMessage(j)
    expect(msg).toContain('Karma Police')
    expect(msg).not.toBe('Error')
    expect(msg).not.toBe('Failed')
  })

  it('contextual fallback includes track title in quotes', () => {
    const j = job({ id: 'j1', title: 'Electioneering', downloaderName: 'spotDL', error: 'some weird unknown error' })
    const msg = failureMessage(j)
    expect(msg).toContain('Electioneering')
    expect(msg).toContain('spotDL')
  })
})

describe('DownloadTray', () => {
  beforeEach(() => {
    useDownloads.setState({ jobs: {} })
    useUI.setState({ rightPanel: 'downloads' })
    vi.mocked(useAdapters).mockReturnValue({ data: [] } as AnyQuery)
    vi.clearAllMocks()
  })

  it('renders nothing when the panel is not downloads', () => {
    useUI.setState({ rightPanel: 'queue' })
    const { container } = render(<DownloadTray />)
    expect(container.firstChild).toBeNull()
  })

  it('lists jobs and cancels an active one', () => {
    useDownloads.getState().upsert(
      job({ id: 'j1', status: 'running', progress: 40, title: 'Song' }),
    )
    render(<DownloadTray />)
    expect(screen.getByText('Download Tray')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /cancel/i }))
    expect(cancelDownload).toHaveBeenCalledWith('j1')
  })

  it('retries a failed job', () => {
    useDownloads.getState().upsert(job({ id: 'j2', status: 'failed', progress: 0 }))
    render(<DownloadTray />)
    fireEvent.click(screen.getByRole('button', { name: /retry/i }))
    expect(retryDownload).toHaveBeenCalledWith('j2')
  })

  it('failed job shows a descriptive reason containing the track title — not bare "Failed"', () => {
    useDownloads.getState().upsert(
      job({
        id: 'j3',
        status: 'failed',
        progress: 0,
        title: 'Bones',
        downloaderName: 'spotDL',
        error: 'no matching source found',
      }),
    )
    render(<DownloadTray />)
    // Should contain "Bones" somewhere in the failure message
    const failNode = screen.getByTestId('failure-message-j3')
    expect(failNode.textContent).toContain('Bones')
    expect(failNode.textContent).not.toBe('Failed')
  })

  it('failed job with empty/generic error still renders contextual copy — never bare "Error"', () => {
    useDownloads.getState().upsert(
      job({
        id: 'j4',
        status: 'failed',
        progress: 0,
        title: 'Let Down',
        downloaderName: 'spotDL',
        error: '',
      }),
    )
    render(<DownloadTray />)
    const failNode = screen.getByTestId('failure-message-j4')
    expect(failNode.textContent).toContain('Let Down')
    expect(failNode.textContent).not.toBe('Error')
    expect(failNode.textContent).not.toBe('Failed')
  })

  it('shows "Try <next>" button when >1 downloader and job is failed, and calls postDownload with next downloader + job fields', async () => {
    vi.mocked(useAdapters).mockReturnValue({
      data: [
        { id: 'a1', type: 'downloader', name: 'spotDL', enabled: true, priority: 1, config: {} },
        { id: 'a2', type: 'downloader', name: 'Lidarr', enabled: true, priority: 2, config: {} },
      ],
    } as AnyQuery)
    useDownloads.getState().upsert(
      job({ id: 'j5', status: 'failed', progress: 0, title: 'Bones', downloaderName: 'spotDL', source: 'spotify', externalId: 'sp1' }),
    )
    render(<DownloadTray />)
    const tryBtn = screen.getByRole('button', { name: /try lidarr/i })
    expect(tryBtn).toBeInTheDocument()
    fireEvent.click(tryBtn)
    expect(postDownload).toHaveBeenCalledWith(
      expect.objectContaining({ downloader: 'Lidarr', source: 'spotify', externalId: 'sp1', title: 'Bones' }),
    )
  })

  it('is a side panel inside the layout — no absolute inset-0 z-30 on the root element', () => {
    render(<DownloadTray />)
    const aside = screen.getByRole('complementary')
    // Should NOT have the old full-screen self-gate positioning
    expect(aside.className).not.toMatch(/\babsolute\b/)
    expect(aside.className).not.toMatch(/\binset-0\b/)
    expect(aside.className).not.toMatch(/\bz-30\b/)
  })
})
