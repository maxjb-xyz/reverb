import { useEffect, useState } from 'react'
import { api } from './api'

export interface SessionStatus {
  loading: boolean
  setupRequired: boolean
  authenticated: boolean
}

export function useSessionStatus(): SessionStatus {
  const [s, setS] = useState<SessionStatus>({ loading: true, setupRequired: false, authenticated: false })
  useEffect(() => {
    ;(async () => {
      try {
        const setup = await api.get<{ setupRequired: boolean }>('/setup/status')
        let authenticated = false
        if (!setup.setupRequired) {
          try {
            await api.get('/me')
            authenticated = true
          } catch {
            authenticated = false
          }
        }
        setS({ loading: false, setupRequired: setup.setupRequired, authenticated })
      } catch {
        setS({ loading: false, setupRequired: false, authenticated: false })
      }
    })()
  }, [])
  return s
}
