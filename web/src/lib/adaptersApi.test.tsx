import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { listAdapters, createAdapter, testAdapter, deleteAdapter } from './adaptersApi'

const fetchMock = vi.fn()

beforeEach(() => {
  vi.stubGlobal('fetch', fetchMock)
  fetchMock.mockReset()
})
afterEach(() => {
  vi.unstubAllGlobals()
})

function ok(body: unknown) {
  return Promise.resolve({ ok: true, status: 200, text: () => Promise.resolve(JSON.stringify(body)) } as Response)
}

describe('adaptersApi', () => {
  it('listAdapters GETs /adapters', async () => {
    fetchMock.mockReturnValue(ok([{ id: 'a1', type: 'search', name: 'spotify', enabled: true, priority: 0, config: { client_id: 'x', client_secret__isSet: true } }]))
    const out = await listAdapters()
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/adapters', expect.objectContaining({ method: 'GET' }))
    expect(out[0].name).toBe('spotify')
    expect(out[0].config.client_secret__isSet).toBe(true)
  })

  it('createAdapter POSTs and returns wrapped data', async () => {
    fetchMock.mockReturnValue(ok({ data: { id: 'a2', type: 'search', name: 'spotify', enabled: true, priority: 0, config: {} }, pendingRestart: true }))
    const res = await createAdapter({ type: 'search', name: 'spotify', enabled: true, priority: 0, config: { client_id: 'x', client_secret: 'shh' } })
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/adapters', expect.objectContaining({ method: 'POST' }))
    expect(res.pendingRestart).toBe(true)
    expect(res.data.id).toBe('a2')
  })

  it('testAdapter POSTs /adapters/test', async () => {
    fetchMock.mockReturnValue(ok({ ok: false, error: 'connection refused' }))
    const res = await testAdapter('spotify', { client_id: 'x', client_secret: 'shh' })
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/adapters/test', expect.objectContaining({ method: 'POST' }))
    expect(res.ok).toBe(false)
    expect(res.error).toBe('connection refused')
  })

  it('deleteAdapter DELETEs /adapters/:id', async () => {
    fetchMock.mockReturnValue(ok({ ok: true, pendingRestart: true }))
    const res = await deleteAdapter('a1')
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/adapters/a1', expect.objectContaining({ method: 'DELETE' }))
    expect(res.ok).toBe(true)
  })
})
