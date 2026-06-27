import { create } from 'zustand'
import { api, ApiError } from './api'

export type Me = {
  id: string
  username: string
  roleId: string
  roleName: string
  isOwner: boolean
  capabilities: string[]
}

/** GET /api/v1/me — returns null on 401 (not logged in), throws on other errors. */
export async function fetchMe(): Promise<Me | null> {
  try {
    return await api.get<Me>('/me')
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) return null
    throw e
  }
}

interface AuthStore {
  me: Me | null
  loading: boolean
  /** Re-fetch /me and update state. */
  refresh(): Promise<void>
  /** True iff the current user has the given capability. */
  can(cap: string): boolean
  /** POST /auth/logout then clear me. */
  logout(): Promise<void>
}

export const useAuthStore = create<AuthStore>((set, get) => ({
  me: null,
  loading: false,

  refresh: async () => {
    set({ loading: true })
    try {
      const me = await fetchMe()
      set({ me, loading: false })
    } catch {
      set({ me: null, loading: false })
    }
  },

  can: (cap: string) => get().me?.capabilities.includes(cap) ?? false,

  logout: async () => {
    try {
      await api.post('/auth/logout')
    } finally {
      set({ me: null })
    }
  },
}))
