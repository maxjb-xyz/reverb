import { render, screen, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { DownloadTray } from './DownloadTray'
import { useDownloads } from '../lib/downloadStore'
import { useUI } from '../lib/uiStore'
import type { DownloadJob } from '../lib/types'

function job(id: string, status: DownloadJob['status'], extra: Partial<DownloadJob> = {}): DownloadJob {
  return { id, dedupKey: id, status, progress: 0, downloaderName: 'spotdl', priority: 0, attempts: 0, source: 'spotify', externalId: id, title: id, artist: 'A', playWhenReady: false, createdAt: 1, startedAt: 0, finishedAt: 0, ...extra }
}

function renderTray() {
  return render(
    <MemoryRouter>
      <DownloadTray />
    </MemoryRouter>,
  )
}

describe('DownloadTray', () => {
  beforeEach(() => {
    useUI.setState({ rightPanel: 'downloads' })
    useDownloads.setState({ jobs: {}, paused: false })
    vi.useRealTimers()
  })

  it('shows a calm empty state when there are no downloads', () => {
    renderTray()
    expect(screen.getByText(/Nothing downloading/i)).toBeInTheDocument()
  })

  it('groups active jobs and shows a See all link with a total count', () => {
    useDownloads.getState().setAll([job('a', 'running', { progress: 60 }), job('b', 'queued'), job('c', 'queued')])
    renderTray()
    expect(screen.getByText(/Downloading/)).toBeInTheDocument()
    expect(screen.getByText(/Queued/)).toBeInTheDocument()
    // "See all" with total count 3
    expect(screen.getByRole('link', { name: /see all/i })).toHaveTextContent('3')
  })

  it('auto-tidies completed jobs ~5s after the queue goes idle; failed stay', () => {
    vi.useFakeTimers()
    useDownloads.setState({ jobs: {}, paused: false })
    useDownloads.getState().setAll([job('done', 'completed'), job('bad', 'failed')])
    renderTray()
    // Completed visible immediately
    expect(screen.getByText('done')).toBeInTheDocument()
    act(() => { vi.advanceTimersByTime(5200) })
    // Completed tidied away; failed sticky
    expect(screen.queryByText('done')).not.toBeInTheDocument()
    expect(screen.getByText('bad')).toBeInTheDocument()
  })
})
