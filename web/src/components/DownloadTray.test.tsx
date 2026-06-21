import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { DownloadTray } from './DownloadTray'
import { useDownloads } from '../lib/downloadStore'
import { useUI } from '../lib/uiStore'
import type { DownloadJob } from '../lib/types'

vi.mock('../lib/downloadApi', () => ({
  cancelDownload: vi.fn(() => Promise.resolve()),
  retryDownload: vi.fn(() => Promise.resolve({} as DownloadJob)),
}))
import { cancelDownload, retryDownload } from '../lib/downloadApi'

function job(p: Partial<DownloadJob>): DownloadJob {
  return {
    id: 'j1', dedupKey: 'dk', status: 'running', progress: 40, downloaderName: 'spotdl',
    priority: 0, attempts: 0, source: 'spotify', externalId: 'sp1', playWhenReady: false,
    createdAt: 1, startedAt: 0, finishedAt: 0, ...p,
  }
}

describe('DownloadTray', () => {
  beforeEach(() => {
    useDownloads.setState({ jobs: {} })
    useUI.setState({ rightPanel: 'downloads' })
  })

  it('renders nothing when the panel is not downloads', () => {
    useUI.setState({ rightPanel: 'queue' })
    const { container } = render(<DownloadTray />)
    expect(container.firstChild).toBeNull()
  })

  it('lists jobs and cancels an active one', () => {
    useDownloads.getState().upsert(job({ id: 'j1', status: 'running', progress: 40, title: 'Song' } as Partial<DownloadJob>))
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

  it('is a full-screen sheet on mobile and a side panel on desktop (responsive classes)', () => {
    render(<DownloadTray />)
    const aside = screen.getByRole('complementary')
    expect(aside.className).toMatch(/inset-0/)
    expect(aside.className).toMatch(/md:w-80/)
  })
})
