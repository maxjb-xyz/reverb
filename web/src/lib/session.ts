import { useEffect, useState } from 'react'
import { api, ApiError } from './api'

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
