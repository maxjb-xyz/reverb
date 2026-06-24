import { describe, it, expect, beforeEach } from 'vitest'
import { useDownloads } from './downloadStore'
import type { DownloadEvent, DownloadJob } from './types'

function mkJob(id: string, status: DownloadJob['status'], createdAt = 1): DownloadJob {
  return {
    id, dedupKey: id, status, progress: 0, downloaderName: '', priority: 0, attempts: 0,
    source: 'spotify', externalId: id, playWhenReady: false, createdAt, startedAt: 0, finishedAt: 0,
  }
}

describe('downloadStore paused/remove/selectors', () => {
  beforeEach(() => {
    useDownloads.setState({ jobs: {}, paused: false })
  })

  it('setPaused toggles paused', () => {
    useDownloads.getState().setPaused(true)
    expect(useDownloads.getState().paused).toBe(true)
  })

  it('remove deletes the given ids', () => {
    useDownloads.getState().setAll([mkJob('a', 'completed'), mkJob('b', 'failed')])
    useDownloads.getState().remove(['a'])
    expect(useDownloads.getState().jobs['a']).toBeUndefined()
    expect(useDownloads.getState().jobs['b']).toBeDefined()
  })

  it('status selectors partition jobs', () => {
    useDownloads.getState().setAll([
      mkJob('r', 'running'), mkJob('q', 'queued'), mkJob('c', 'completed'), mkJob('f', 'failed'),
    ])
    const s = useDownloads.getState()
    expect(s.running().map((j) => j.id)).toEqual(['r'])
    expect(s.queued().map((j) => j.id)).toEqual(['q'])
    expect(s.completed().map((j) => j.id)).toEqual(['c'])
    expect(s.failed().map((j) => j.id)).toEqual(['f'])
    expect(s.active().map((j) => j.id).sort()).toEqual(['q', 'r'])
  })
})

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

  it('complete event sets coverArtId and does not clobber existing value', () => {
    // Completion event WITH coverArtId sets it.
    useDownloads.getState().upsert(job({ id: 'j1' }))
    useDownloads.getState().applyEvent({ jobId: 'j1', dedupKey: 'dk', status: 'completed', progress: 100, source: 'spotify', externalId: 'sp1', coverArtId: 'art-42' })
    expect(useDownloads.getState().jobs['j1'].coverArtId).toBe('art-42')

    // A subsequent event WITHOUT coverArtId must NOT clobber the stored value.
    useDownloads.getState().applyEvent({ jobId: 'j1', dedupKey: 'dk', status: 'completed', progress: 100, source: 'spotify', externalId: 'sp1' })
    expect(useDownloads.getState().jobs['j1'].coverArtId).toBe('art-42')
  })

  it('jobFromEvent (unknown job) carries coverArtId from the event', () => {
    useDownloads.getState().applyEvent({ jobId: 'jnew', dedupKey: 'dk', status: 'completed', progress: 100, source: 'spotify', externalId: 'sp1', coverArtId: 'art-99' })
    expect(useDownloads.getState().jobs['jnew'].coverArtId).toBe('art-99')
  })

  it('completed() returns newest-first when two jobs differ in createdAt', () => {
    useDownloads.getState().setAll([
      job({ id: 'older', status: 'completed', createdAt: 10 }),
      job({ id: 'newer', status: 'completed', createdAt: 20 }),
    ])
    const result = useDownloads.getState().completed()
    expect(result.map((j) => j.id)).toEqual(['newer', 'older'])
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
