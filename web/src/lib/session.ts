import { useEffect, useState } from 'react'
import { api, ApiError } from './api'

/** POST /auth/login — throws ApiError on failure (caller checks .status). */
export async function login(username: string, password: string): Promise<void> {
  await api.post('/auth/login', { username, password })
}

/** POST /auth/signup — throws ApiError on failure. */
export async function signup(username: string, password: string, invite?: string): Promise<void> {
  await api.post('/auth/signup', { username, password, ...(invite ? { invite } : {}) })
}

/** POST /setup/admin — throws ApiError on failure. */
export async function setupOwner(username: string, password: string): Promise<void> {
  await api.post('/setup/admin', { username, password })
}

export type SessionKind = 'setup' | 'authenticated' | 'unauthenticated' | 'error'

export interface SessionStatus {
  loading: boolean
  setupRequired: boolean
  authenticated: boolean
  error: boolean
}

// probeSession classifies the session using an injected `get`, so it is unit-testable
// without a network or a rendered component. A 401 on /me means "not logged in";
// anything else (5xx, network failure) means "can't determine" → error (do NOT treat
// a transient server error as logged-out and bounce the user to Login).
export async function probeSession(get: <T>(p: string) => Promise<T>): Promise<SessionKind> {
  let setup: { setupRequired: boolean }
  try {
    setup = await get<{ setupRequired: boolean }>('/setup/status')
  } catch {
    return 'error'
  }
  if (setup.setupRequired) return 'setup'
  try {
    await get('/me')
    return 'authenticated'
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) return 'unauthenticated'
    return 'error'
  }
}

export function useSessionStatus(): SessionStatus {
  const [s, setS] = useState<SessionStatus>({
    loading: true,
    setupRequired: false,
    authenticated: false,
    error: false,
  })
  useEffect(() => {
    ;(async () => {
      const kind = await probeSession(api.get)
      setS({
        loading: false,
        setupRequired: kind === 'setup',
        authenticated: kind === 'authenticated',
        error: kind === 'error',
      })
    })()
  }, [])
  return s
}
