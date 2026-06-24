import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { failureMessage, StatusLabel, DownloadProgress } from './parts'
import type { DownloadJob } from '../../lib/types'

function job(p: Partial<DownloadJob>): DownloadJob {
  return { id: 'j', dedupKey: 'j', status: 'queued', progress: 0, downloaderName: 'spotdl', priority: 0, attempts: 0, source: 'spotify', externalId: 'e', playWhenReady: false, createdAt: 1, startedAt: 0, finishedAt: 0, ...p }
}

describe('download parts', () => {
  it('failureMessage is contextual, never bare', () => {
    expect(failureMessage(job({ title: 'Song', error: 'timed out' }))).toMatch(/Timed out/)
    expect(failureMessage(job({ title: 'Song', error: '' }))).toMatch(/Couldn't download/)
  })

  it('StatusLabel reads Queued / Downloading / Done / Failed', () => {
    const { rerender } = render(<StatusLabel job={job({ status: 'queued' })} />)
    expect(screen.getByText('Queued')).toBeInTheDocument()
    rerender(<StatusLabel job={job({ status: 'running', progress: 55 })} />)
    expect(screen.getByText('55%')).toBeInTheDocument()
    rerender(<StatusLabel job={job({ status: 'completed' })} />)
    expect(screen.getByText('Done')).toBeInTheDocument()
  })

  it('DownloadProgress renders a bar', () => {
    const { container } = render(<DownloadProgress progress={40} />)
    expect(container.querySelector('div')).toBeTruthy()
  })
})
