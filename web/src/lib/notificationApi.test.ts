import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { getNotifications, postMarkRead, useNotificationStore } from './notificationApi'
import type { Notification } from './notificationApi'

function mkNotification(id: string, read: boolean, createdAt = 1000): Notification {
  return {
    id,
    userId: 'u1',
    type: 'request.approved',
    title: 'Your request was approved',
    body: `Request ${id} was approved`,
    read,
    createdAt,
  }
}

// --- API function tests ---

describe('notificationApi HTTP calls', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        new Response(
          JSON.stringify({ notifications: [mkNotification('n1', false)], unread: 1 }),
          { status: 200 },
        ),
      ),
    )
  })
  afterEach(() => vi.unstubAllGlobals())

  it('getNotifications GETs /api/v1/notifications', async () => {
    await getNotifications()
    expect(fetch).toHaveBeenCalledWith(
      '/api/v1/notifications',
      expect.objectContaining({ method: 'GET' }),
    )
  })

  it('getNotifications returns { notifications, unread }', async () => {
    const result = await getNotifications()
    expect(result.notifications).toHaveLength(1)
    expect(result.unread).toBe(1)
  })

  it('postMarkRead POSTs /api/v1/notifications/read with { ids }', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response(JSON.stringify({ unread: 0 }), { status: 200 })),
    )
    await postMarkRead(['x'])
    const call = (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls.at(-1)!
    expect(call[0]).toBe('/api/v1/notifications/read')
    expect((call[1] as RequestInit).method).toBe('POST')
    expect(JSON.parse((call[1] as RequestInit).body as string)).toEqual({ ids: ['x'] })
  })

  it('postMarkRead with no argument POSTs { ids: [] }', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response(JSON.stringify({ unread: 0 }), { status: 200 })),
    )
    await postMarkRead()
    const call = (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls.at(-1)!
    expect(JSON.parse((call[1] as RequestInit).body as string)).toEqual({ ids: [] })
  })

  it('postMarkRead returns { unread }', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response(JSON.stringify({ unread: 3 }), { status: 200 })),
    )
    const result = await postMarkRead(['a'])
    expect(result.unread).toBe(3)
  })
})

// --- Store tests ---

describe('useNotificationStore', () => {
  beforeEach(() => {
    useNotificationStore.setState({ byId: {}, unread: 0 })
  })

  it('setAll populates byId and sets unread from server value', () => {
    const n1 = mkNotification('n1', false, 2000)
    const n2 = mkNotification('n2', true, 1000)
    useNotificationStore.getState().setAll([n1, n2], 1)
    expect(Object.keys(useNotificationStore.getState().byId)).toHaveLength(2)
    expect(useNotificationStore.getState().byId['n1']).toEqual(n1)
    expect(useNotificationStore.getState().unread).toBe(1)
  })

  it('setAll replaces existing byId (does not merge old entries)', () => {
    useNotificationStore.getState().setAll([mkNotification('old', true, 100)], 0)
    useNotificationStore.getState().setAll([mkNotification('n1', false, 200)], 1)
    expect(useNotificationStore.getState().byId['old']).toBeUndefined()
    expect(useNotificationStore.getState().byId['n1']).toBeDefined()
  })

  it('items() returns notifications sorted newest-first by createdAt', () => {
    const n1 = mkNotification('n1', false, 1000)
    const n2 = mkNotification('n2', false, 3000)
    const n3 = mkNotification('n3', true, 2000)
    useNotificationStore.getState().setAll([n1, n2, n3], 2)
    const ids = useNotificationStore.getState().items().map((n) => n.id)
    expect(ids).toEqual(['n2', 'n3', 'n1'])
  })

  it('add inserts an unread notification into byId and increments unread', () => {
    const n = mkNotification('n1', false, 5000)
    useNotificationStore.getState().add(n)
    expect(useNotificationStore.getState().byId['n1']).toEqual(n)
    expect(useNotificationStore.getState().unread).toBe(1)
  })

  it('add an already-read notification does NOT increment unread', () => {
    const n = mkNotification('n1', true, 5000)
    useNotificationStore.getState().add(n)
    expect(useNotificationStore.getState().byId['n1']).toEqual(n)
    expect(useNotificationStore.getState().unread).toBe(0)
  })

  it('add makes the new notification appear first in items() (newest-first)', () => {
    useNotificationStore.getState().setAll([mkNotification('old', false, 100)], 1)
    useNotificationStore.getState().add(mkNotification('new', false, 9999))
    const ids = useNotificationStore.getState().items().map((n) => n.id)
    expect(ids[0]).toBe('new')
    expect(ids[1]).toBe('old')
  })

  it('markRead flips those notifications to read=true and recomputes unread', () => {
    useNotificationStore.getState().setAll(
      [mkNotification('n1', false, 1), mkNotification('n2', false, 2), mkNotification('n3', true, 3)],
      2,
    )
    useNotificationStore.getState().markRead(['n1'])
    expect(useNotificationStore.getState().byId['n1'].read).toBe(true)
    expect(useNotificationStore.getState().byId['n2'].read).toBe(false)
    // unread recomputed from byId: only n2 remains unread
    expect(useNotificationStore.getState().unread).toBe(1)
  })

  it('markRead ignores ids not in byId without throwing', () => {
    useNotificationStore.getState().setAll([mkNotification('n1', false, 1)], 1)
    expect(() => useNotificationStore.getState().markRead(['does-not-exist'])).not.toThrow()
    expect(useNotificationStore.getState().unread).toBe(1)
  })

  it('markAllRead sets all notifications to read and unread to 0', () => {
    useNotificationStore.getState().setAll(
      [mkNotification('n1', false, 1), mkNotification('n2', false, 2)],
      2,
    )
    useNotificationStore.getState().markAllRead()
    expect(useNotificationStore.getState().unread).toBe(0)
    expect(useNotificationStore.getState().byId['n1'].read).toBe(true)
    expect(useNotificationStore.getState().byId['n2'].read).toBe(true)
  })

  it('unread stays consistent with byId after multiple operations', () => {
    // Start with 2 unread
    useNotificationStore.getState().setAll(
      [mkNotification('n1', false, 1), mkNotification('n2', false, 2)],
      2,
    )
    // Add another unread → 3
    useNotificationStore.getState().add(mkNotification('n3', false, 3))
    expect(useNotificationStore.getState().unread).toBe(3)
    // Mark n1 + n3 read → 1 remains (n2)
    useNotificationStore.getState().markRead(['n1', 'n3'])
    expect(useNotificationStore.getState().unread).toBe(1)
    // Mark all → 0
    useNotificationStore.getState().markAllRead()
    expect(useNotificationStore.getState().unread).toBe(0)
  })
})
