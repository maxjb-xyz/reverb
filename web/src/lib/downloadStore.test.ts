import { describe, it, expect, beforeEach } from 'vitest'
import { useDownloads } from './downloadStore'
import type { DownloadEvent, DownloadJob } from './types'

function job(partial: Partial<DownloadJob>): DownloadJob {
  return {
    id: 'j1', dedupKey: 'dk', status: 'queued', progress: 0, downloaderName: 'spotdl',
    priority: 0, attempts: 0, source: 'spotify', externalId: 'sp1', playWhenReady: false,
    createdAt: 1, startedAt: 0, finishedAt: 0, ...partial,
  }
}

describe('downloadStore', () => {
  beforeEach(() => {
    useDownloads.setState({ jobs: {} })
  })

  it('upserts and looks up by externalId+source', () => {
    useDownloads.getState().upsert(job({ id: 'j1', source: 'spotify', externalId: 'sp1' }))
    const found = useDownloads.getState().byExternal('spotify', 'sp1')
    expect(found?.id).toBe('j1')
    expect(useDownloads.getState().byExternal('spotify', 'nope')).toBeUndefined()
  })

  it('applyEvent patches an existing job and creates a new one', () => {
    useDownloads.getState().upsert(job({ id: 'j1', status: 'queued', progress: 0 }))
    const ev: DownloadEvent = { jobId: 'j1', dedupKey: 'dk', status: 'running', progress: 55, source: 'spotify', externalId: 'sp1' }
    useDownloads.getState().applyEvent(ev)
    expect(useDownloads.getState().jobs['j1'].status).toBe('running')
    expect(useDownloads.getState().jobs['j1'].progress).toBe(55)

    // Unknown job → created from the event.
    useDownloads.getState().applyEvent({ jobId: 'j2', dedupKey: 'dk2', status: 'queued', progress: 0, source: 'spotify', externalId: 'sp2' })
    expect(useDownloads.getState().jobs['j2']).toBeDefined()
  })

  it('complete event sets libraryTrackId', () => {
    useDownloads.getState().upsert(job({ id: 'j1' }))
    useDownloads.getState().applyEvent({ jobId: 'j1', dedupKey: 'dk', status: 'completed', progress: 100, source: 'spotify', externalId: 'sp1', libraryTrackId: 't9' })
    expect(useDownloads.getState().jobs['j1'].libraryTrackId).toBe('t9')
    expect(useDownloads.getState().jobs['j1'].status).toBe('completed')
  })

  it('active() returns only queued/running newest-first', () => {
    useDownloads.getState().setAll([
      job({ id: 'a', status: 'completed', createdAt: 3 }),
      job({ id: 'b', status: 'running', createdAt: 2 }),
      job({ id: 'c', status: 'queued', createdAt: 1 }),
    ])
    const active = useDownloads.getState().active()
    expect(active.map((j) => j.id)).toEqual(['b', 'c'])
  })
})
