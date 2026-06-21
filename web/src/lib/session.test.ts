import { describe, it, expect } from 'vitest'
import { probeSession } from './session'
import { ApiError } from './api'

function fakeGet(ok: Record<string, unknown>, errs: Record<string, Error> = {}) {
  return async <T>(p: string): Promise<T> => {
    if (errs[p]) throw errs[p]
    return ok[p] as T
  }
}

describe('probeSession', () => {
  it('returns "setup" when setup is required', async () => {
    expect(await probeSession(fakeGet({ '/setup/status': { setupRequired: true } }))).toBe('setup')
  })
  it('returns "authenticated" when /me succeeds', async () => {
    const get = fakeGet({ '/setup/status': { setupRequired: false }, '/me': { authenticated: true } })
    expect(await probeSession(get)).toBe('authenticated')
  })
  it('returns "unauthenticated" when /me is 401', async () => {
    const get = fakeGet({ '/setup/status': { setupRequired: false } }, { '/me': new ApiError('GET', '/me', 401) })
    expect(await probeSession(get)).toBe('unauthenticated')
  })
  it('returns "error" when /me fails with a server error', async () => {
    const get = fakeGet({ '/setup/status': { setupRequired: false } }, { '/me': new ApiError('GET', '/me', 500) })
    expect(await probeSession(get)).toBe('error')
  })
  it('returns "error" when the server is unreachable', async () => {
    expect(await probeSession(fakeGet({}, { '/setup/status': new TypeError('Failed to fetch') }))).toBe('error')
  })
})
