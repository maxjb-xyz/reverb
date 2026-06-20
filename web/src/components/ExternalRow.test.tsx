import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { ExternalRow } from './ExternalRow'
import { useDownloads } from '../lib/downloadStore'
import type { ExternalResult, DownloadJob } from '../lib/types'

vi.mock('../lib/downloadApi', () => ({
  postDownload: vi.fn(() => Promise.resolve({ id: 'job-sp1', source: 'spotify', externalId: 'sp1', status: 'queued', progress: 0, dedupKey: 'dk', downloaderName: 'spotdl', priority: 0, attempts: 0, playWhenReady: false, createdAt: 1, startedAt: 0, finishedAt: 0 } as DownloadJob)),
}))
import { postDownload } from '../lib/downloadApi'

const playTrackList = vi.fn()
vi.mock('../lib/playerStore', () => ({
  usePlayer: (sel: (s: { playTrackList: typeof playTrackList }) => unknown) => sel({ playTrackList }),
}))

function result(p: Partial<ExternalResult>): ExternalResult {
  return { source: 'spotify', externalId: 'sp1', title: 'Song', artist: 'Artist', album: 'Album', durationMs: 200000, type: 'track', ...p }
}

function job(p: Partial<DownloadJob>): DownloadJob {
  return { id: 'j1', dedupKey: 'dk', status: 'running', progress: 50, downloaderName: 'spotdl', priority: 0, attempts: 0, source: 'spotify', externalId: 'sp1', playWhenReady: false, createdAt: 1, startedAt: 0, finishedAt: 0, ...p }
}

describe('ExternalRow', () => {
  beforeEach(() => {
    useDownloads.setState({ jobs: {} })
    vi.clearAllMocks()
  })

  it('in-library row shows ✓ and plays the matched track', () => {
    render(<ExternalRow result={result({ match: { status: 'in_library', libraryTrackId: 't3', method: 'isrc', confidence: 1 } })} />)
    expect(screen.getByTitle(/in library/i)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button'))
    expect(playTrackList).toHaveBeenCalled()
  })

  it('not-in-library row shows ↓ and posts a download', async () => {
    render(<ExternalRow result={result({ match: { status: 'not_in_library', libraryTrackId: '', method: 'none', confidence: 0 } })} />)
    const dl = screen.getByRole('button', { name: /download/i })
    fireEvent.click(dl)
    await waitFor(() => expect(postDownload).toHaveBeenCalled())
  })

  it('active job shows the ⟳ progress ring (determinate)', () => {
    useDownloads.getState().upsert(job({ status: 'running', progress: 50 }))
    render(<ExternalRow result={result({})} />)
    expect(screen.getByLabelText(/downloading/i)).toBeInTheDocument()
  })

  it('completed job with libraryTrackId flips to ✓ and plays', () => {
    useDownloads.getState().upsert(job({ status: 'completed', progress: 100, libraryTrackId: 't9' }))
    render(<ExternalRow result={result({})} />)
    expect(screen.getByTitle(/in library/i)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button'))
    expect(playTrackList).toHaveBeenCalled()
  })
})
