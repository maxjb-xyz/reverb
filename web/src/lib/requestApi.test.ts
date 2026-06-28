import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { postRequest, getMyRequests, getAllRequests, approveRequest, denyRequest, cancelRequest } from './requestApi'
import { useRequestStore } from './requestApi'
import type { Request } from './requestApi'

function mkRequest(id: string, status: Request['status'], requestedBy = 'u1'): Request {
  return {
    id,
    requestedBy,
    source: 'spotify',
    externalId: id,
    title: 'Song ' + id,
    artist: 'Artist',
    status,
    createdAt: 1000,
  }
}

// --- API function tests ---

describe('requestApi HTTP calls', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify(mkRequest('r1', 'pending')), { status: 200 })))
  })
  afterEach(() => vi.unstubAllGlobals())

  it('postRequest POSTs /api/v1/requests with the item body', async () => {
    const item = { source: 'spotify', externalId: 'sp1', title: 'Song', artist: 'Artist' }
    await postRequest(item)
    const call = (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls.at(-1)!
    expect(call[0]).toBe('/api/v1/requests')
    expect((call[1] as RequestInit).method).toBe('POST')
    expect(JSON.parse((call[1] as RequestInit).body as string)).toEqual(item)
  })

  it('postRequest includes kind in the body when provided', async () => {
    const item = { source: 'spotify', externalId: 'al1', title: 'Kid A', artist: 'Radiohead', kind: 'album' as const }
    await postRequest(item)
    const call = (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls.at(-1)!
    expect(JSON.parse((call[1] as RequestInit).body as string)).toMatchObject({ kind: 'album' })
  })

  it('getMyRequests GETs /api/v1/requests/mine', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify([mkRequest('r1', 'pending')]), { status: 200 })))
    await getMyRequests()
    expect(fetch).toHaveBeenCalledWith('/api/v1/requests/mine', expect.objectContaining({ method: 'GET' }))
  })

  it('getAllRequests GETs /api/v1/requests without status when omitted', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify([]), { status: 200 })))
    await getAllRequests()
    expect(fetch).toHaveBeenCalledWith('/api/v1/requests', expect.objectContaining({ method: 'GET' }))
  })

  it('getAllRequests GETs /api/v1/requests?status=pending when status given', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify([]), { status: 200 })))
    await getAllRequests('pending')
    expect(fetch).toHaveBeenCalledWith('/api/v1/requests?status=pending', expect.objectContaining({ method: 'GET' }))
  })

  it('approveRequest POSTs /api/v1/requests/{id}/approve', async () => {
    await approveRequest('r1')
    expect(fetch).toHaveBeenCalledWith('/api/v1/requests/r1/approve', expect.objectContaining({ method: 'POST' }))
  })

  it('denyRequest POSTs /api/v1/requests/{id}/deny with optional reason', async () => {
    await denyRequest('r1', 'Not available')
    const call = (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls.at(-1)!
    expect(call[0]).toBe('/api/v1/requests/r1/deny')
    expect((call[1] as RequestInit).method).toBe('POST')
    expect(JSON.parse((call[1] as RequestInit).body as string)).toEqual({ reason: 'Not available' })
  })

  it('cancelRequest POSTs /api/v1/requests/{id}/cancel', async () => {
    await cancelRequest('r1')
    expect(fetch).toHaveBeenCalledWith('/api/v1/requests/r1/cancel', expect.objectContaining({ method: 'POST' }))
  })
})

// --- Store tests ---

describe('useRequestStore', () => {
  beforeEach(() => {
    useRequestStore.setState({ byId: {} })
  })

  it('upsert adds a request keyed by id', () => {
    const req = mkRequest('r1', 'pending')
    useRequestStore.getState().upsert(req)
    expect(useRequestStore.getState().byId['r1']).toEqual(req)
  })

  it('setMine merges entries without removing unrelated ids', () => {
    useRequestStore.getState().upsert(mkRequest('r0', 'pending', 'u0'))
    useRequestStore.getState().setMine([mkRequest('r1', 'pending'), mkRequest('r2', 'approved')])
    expect(Object.keys(useRequestStore.getState().byId)).toHaveLength(3)
    expect(useRequestStore.getState().byId['r0']).toBeDefined()
  })

  it('setQueue then setMine — pending() still returns queue entries (no-clobber regression)', () => {
    const reqA = mkRequest('rA', 'pending', 'other')
    const reqB = mkRequest('rB', 'pending', 'other')
    const reqC = mkRequest('rC', 'pending', 'me')
    useRequestStore.getState().setQueue([reqA, reqB])
    useRequestStore.getState().setMine([reqC])
    const pendingIds = useRequestStore.getState().pending().map((r) => r.id).sort()
    expect(pendingIds).toEqual(['rA', 'rB', 'rC'])
    const mineIds = useRequestStore.getState().mine('me').map((r) => r.id)
    expect(mineIds).toEqual(['rC'])
  })

  it('setQueue merges entries without removing unrelated ids', () => {
    useRequestStore.getState().upsert(mkRequest('r1', 'pending', 'u1'))
    useRequestStore.getState().setQueue([mkRequest('r2', 'pending', 'u2')])
    expect(useRequestStore.getState().byId['r1']).toBeDefined()
    expect(useRequestStore.getState().byId['r2']).toBeDefined()
  })

  it('applyRequestEvent upserts payload.request', () => {
    const req = mkRequest('r1', 'approved')
    useRequestStore.getState().applyRequestEvent({ request: req, targetUserId: 'u1', forManagers: false })
    expect(useRequestStore.getState().byId['r1'].status).toBe('approved')
  })

  it('applyRequestEvent updates an existing request', () => {
    useRequestStore.getState().upsert(mkRequest('r1', 'pending'))
    const updated = mkRequest('r1', 'approved')
    useRequestStore.getState().applyRequestEvent({ request: updated, targetUserId: 'u1', forManagers: false })
    expect(useRequestStore.getState().byId['r1'].status).toBe('approved')
  })

  it('pending() returns only pending-status requests', () => {
    useRequestStore.getState().setMine([
      mkRequest('r1', 'pending'),
      mkRequest('r2', 'approved'),
      mkRequest('r3', 'denied'),
      mkRequest('r4', 'pending'),
    ])
    const pending = useRequestStore.getState().pending()
    expect(pending.map((r) => r.id).sort()).toEqual(['r1', 'r4'])
  })

  it('mine(userId) returns only requests by that user', () => {
    useRequestStore.getState().setMine([
      mkRequest('r1', 'pending', 'alice'),
      mkRequest('r2', 'approved', 'bob'),
      mkRequest('r3', 'pending', 'alice'),
    ])
    const alices = useRequestStore.getState().mine('alice')
    expect(alices.map((r) => r.id).sort()).toEqual(['r1', 'r3'])
  })

  it('mine() with no arg returns all requests', () => {
    useRequestStore.getState().setMine([mkRequest('r1', 'pending', 'alice'), mkRequest('r2', 'approved', 'bob')])
    expect(useRequestStore.getState().mine()).toHaveLength(2)
  })

  // ── byExternal ────────────────────────────────────────────────────────────

  it('byExternal returns undefined when no matching request exists', () => {
    expect(useRequestStore.getState().byExternal('spotify', 'sp1')).toBeUndefined()
  })

  it('byExternal returns the matching request by source+externalId', () => {
    const req = mkRequest('r1', 'pending')
    req.source = 'spotify'
    req.externalId = 'sp1'
    useRequestStore.getState().upsert(req)
    expect(useRequestStore.getState().byExternal('spotify', 'sp1')).toEqual(req)
  })

  it('byExternal prefers an open (pending) request when multiple exist for same source+externalId', () => {
    const denied: Request = { ...mkRequest('r1', 'denied'), source: 'spotify', externalId: 'sp1' }
    const open: Request = { ...mkRequest('r2', 'pending'), source: 'spotify', externalId: 'sp1' }
    useRequestStore.getState().upsert(denied)
    useRequestStore.getState().upsert(open)
    expect(useRequestStore.getState().byExternal('spotify', 'sp1')?.id).toBe('r2')
  })

  it('byExternal returns undefined for a different source or externalId', () => {
    const req: Request = { ...mkRequest('r1', 'pending'), source: 'spotify', externalId: 'sp1' }
    useRequestStore.getState().upsert(req)
    expect(useRequestStore.getState().byExternal('tidal', 'sp1')).toBeUndefined()
    expect(useRequestStore.getState().byExternal('spotify', 'other')).toBeUndefined()
  })
})
