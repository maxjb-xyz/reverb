import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, it, expect, beforeEach, vi } from 'vitest'
import Downloads from './Downloads'
import { useDownloads } from '../lib/downloadStore'
import type { DownloadJob } from '../lib/types'

vi.mock('../lib/downloadApi', async (orig) => {
  const actual = await orig<typeof import('../lib/downloadApi')>()
  return {
    ...actual,
    pauseQueue: vi.fn(async () => ({ paused: true })),
    resumeQueue: vi.fn(async () => ({ paused: false })),
    clearDownloads: vi.fn(async () => ({ removed: 1 })),
    cancelDownload: vi.fn(async () => ({})),
    retryDownload: vi.fn(async () => ({}) as never),
  }
})
import { pauseQueue, clearDownloads } from '../lib/downloadApi'

function job(id: string, status: DownloadJob['status'], extra: Partial<DownloadJob> = {}): DownloadJob {
  return { id, dedupKey: id, status, progress: 0, downloaderName: 'spotdl', priority: 0, attempts: 0, source: 'spotify', externalId: id, title: id, artist: 'A', playWhenReady: false, createdAt: 1, startedAt: 0, finishedAt: 0, ...extra }
}

function renderPage() {
  return render(<MemoryRouter><Downloads /></MemoryRouter>)
}

describe('Downloads page', () => {
  beforeEach(() => {
    useDownloads.setState({ jobs: {}, paused: false })
    vi.clearAllMocks()
  })

  it('renders grouped sections and a summary', () => {
    useDownloads.getState().setAll([job('r', 'running', { progress: 30 }), job('q', 'queued'), job('c', 'completed')])
    renderPage()
    expect(screen.getByRole('heading', { name: 'Downloads' })).toBeInTheDocument()
    // Summary line reflects the counts.
    expect(screen.getByText(/1 downloading · 1 queued · 1 finished/)).toBeInTheDocument()
    // Each job's title renders in its group (titles are unique: 'r','q','c').
    expect(screen.getByText('r')).toBeInTheDocument()
    expect(screen.getByText('q')).toBeInTheDocument()
    expect(screen.getByText('c')).toBeInTheDocument()
  })

  it('filters by chip', () => {
    useDownloads.getState().setAll([job('r', 'running'), job('f', 'failed')])
    renderPage()
    fireEvent.click(screen.getByRole('button', { name: 'Failed' }))
    expect(screen.getByText('f')).toBeInTheDocument()
    expect(screen.queryByText('r')).not.toBeInTheDocument()
  })

  it('searches by title', () => {
    useDownloads.getState().setAll([job('alpha', 'queued', { title: 'Alpha' }), job('beta', 'queued', { title: 'Beta' })])
    renderPage()
    fireEvent.change(screen.getByPlaceholderText(/search/i), { target: { value: 'alph' } })
    expect(screen.getByText('Alpha')).toBeInTheDocument()
    expect(screen.queryByText('Beta')).not.toBeInTheDocument()
  })

  it('pause button calls pauseQueue and optimistically flips', async () => {
    renderPage()
    fireEvent.click(screen.getByRole('button', { name: /pause queue/i }))
    expect(pauseQueue).toHaveBeenCalled()
    await waitFor(() => expect(useDownloads.getState().paused).toBe(true))
  })

  it('Clear finished calls clearDownloads with no ids', () => {
    useDownloads.getState().setAll([job('c', 'completed')])
    renderPage()
    fireEvent.click(screen.getByRole('button', { name: /clear finished/i }))
    expect(clearDownloads).toHaveBeenCalledWith(undefined)
  })

  it('clearFinished clears selection (bulk bar disappears)', () => {
    useDownloads.getState().setAll([job('c', 'completed')])
    renderPage()
    // Select the completed row so the bulk bar shows.
    fireEvent.click(screen.getByLabelText('Select c'))
    expect(screen.getByText(/1 selected/)).toBeInTheDocument()
    // Click "Clear finished" — the bulk bar must disappear.
    fireEvent.click(screen.getByRole('button', { name: /clear finished/i }))
    expect(screen.queryByText(/selected/)).not.toBeInTheDocument()
  })

  it('selecting rows shows a bulk bar and bulk-clears', () => {
    useDownloads.getState().setAll([job('c1', 'completed'), job('c2', 'completed')])
    renderPage()
    fireEvent.click(screen.getByLabelText('Select c1'))
    expect(screen.getByText(/1 selected/)).toBeInTheDocument()
    // The bulk-bar Clear button's accessible name is "Clear selected" (aria-label).
    fireEvent.click(screen.getByRole('button', { name: /clear selected/i }))
    expect(clearDownloads).toHaveBeenCalledWith(['c1'])
  })
})
