import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { pauseQueue, resumeQueue, getQueueState, clearDownload, clearDownloads } from './downloadApi'

describe('downloadApi queue/clear', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ paused: true, removed: 2 }), { status: 200 })))
  })
  afterEach(() => vi.unstubAllGlobals())

  it('pauseQueue POSTs /downloads/pause', async () => {
    await pauseQueue()
    expect(fetch).toHaveBeenCalledWith('/api/v1/downloads/pause', expect.objectContaining({ method: 'POST' }))
  })

  it('resumeQueue POSTs /downloads/resume', async () => {
    await resumeQueue()
    expect(fetch).toHaveBeenCalledWith('/api/v1/downloads/resume', expect.objectContaining({ method: 'POST' }))
  })

  it('getQueueState GETs /downloads/queue', async () => {
    const q = await getQueueState()
    expect(fetch).toHaveBeenCalledWith('/api/v1/downloads/queue', expect.objectContaining({ method: 'GET' }))
    expect(q.paused).toBe(true)
  })

  it('clearDownload POSTs /downloads/{id}/clear', async () => {
    await clearDownload('abc')
    expect(fetch).toHaveBeenCalledWith('/api/v1/downloads/abc/clear', expect.objectContaining({ method: 'POST' }))
  })

  it('clearDownloads POSTs /downloads/clear with ids when given', async () => {
    await clearDownloads(['a', 'b'])
    const call = (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls.at(-1)!
    expect(call[0]).toBe('/api/v1/downloads/clear')
    expect(JSON.parse((call[1] as RequestInit).body as string)).toEqual({ ids: ['a', 'b'] })
  })

  it('clearDownloads (no args) POSTs /downloads/clear with body {}', async () => {
    await clearDownloads()
    const call = (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls.at(-1)!
    expect(call[0]).toBe('/api/v1/downloads/clear')
    expect(JSON.parse((call[1] as RequestInit).body as string)).toEqual({})
  })
})
