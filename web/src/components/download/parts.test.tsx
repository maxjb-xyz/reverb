import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { StatusLabel, DownloadProgress } from './parts'
import { failureMessage } from './failureMessage'
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

  it('StatusLabel renders "Canceled" for canceled status', () => {
    render(<StatusLabel job={job({ status: 'canceled' })} />)
    expect(screen.getByText('Canceled')).toBeInTheDocument()
  })

  it('StatusLabel renders "Downloading" for running with progress -1 (indeterminate)', () => {
    render(<StatusLabel job={job({ status: 'running', progress: -1 })} />)
    expect(screen.getByText('Downloading')).toBeInTheDocument()
  })

  it('DownloadProgress determinate: inner fill has correct width style', () => {
    const { container } = render(<DownloadProgress progress={40} />)
    const outer = container.querySelector('div')!
    const inner = outer.querySelector('div')!
    expect(inner.style.width).toBe('40%')
  })

  it('DownloadProgress indeterminate (progress=-1): renders animate-pulse element', () => {
    const { container } = render(<DownloadProgress progress={-1} />)
    const pulse = container.querySelector('.animate-pulse')
    expect(pulse).toBeTruthy()
  })
})
