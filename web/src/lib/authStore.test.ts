import { describe, it, expect, vi, beforeEach } from 'vitest'
import { useAuthStore, fetchMe } from './authStore'

beforeEach(() => {
  useAuthStore.setState({ me: null, loading: false })
  vi.restoreAllMocks()
})

it('can() reflects capabilities from /me', async () => {
  vi.spyOn(global, 'fetch').mockResolvedValue(new Response(JSON.stringify({
    id: 'u1', username: 'owner', roleId: 'role-admin', roleName: 'Admin', isOwner: true,
    capabilities: ['is_admin', 'can_download'],
  }), { status: 200 }))
  await useAuthStore.getState().refresh()
  expect(useAuthStore.getState().can('is_admin')).toBe(true)
  expect(useAuthStore.getState().can('can_request')).toBe(false)
})

describe('fetchMe', () => {
  it('returns null on 401', async () => {
    vi.spyOn(global, 'fetch').mockResolvedValue(new Response('Unauthorized', { status: 401 }))
    const result = await fetchMe()
    expect(result).toBeNull()
  })

  it('returns the Me object on success', async () => {
    const payload = {
      id: 'u2', username: 'alice', roleId: 'role-user', roleName: 'User', isOwner: false,
      capabilities: ['can_request'],
    }
    vi.spyOn(global, 'fetch').mockResolvedValue(new Response(JSON.stringify(payload), { status: 200 }))
    const result = await fetchMe()
    expect(result).toEqual(payload)
  })
})
