import { create } from 'zustand'
import { api, ApiError } from './api'

export type Me = {
  id: string
  username: string
  roleId: string
  roleName: string
  isOwner: boolean
  capabilities: string[]
  createdAt: number
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
      // fetchMe returns null on a 401 (genuinely logged out) and throws on any
      // other failure (5xx / network).
      const me = await fetchMe()
      set({ me, loading: false })
    } catch (e) {
      // ONLY a 401 means "logged out" → clear me. Any other error (5xx / network)
      // is a transient blip: leave the existing me intact so we never bounce a
      // logged-in user out over a momentary server hiccup.
      if (e instanceof ApiError && e.status === 401) {
        set({ me: null, loading: false })
      } else {
        set({ loading: false })
      }
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

/**
 * A "manager" can reach the Admin surface — i.e. has any of the management
 * capabilities. Defined once so the TopBar entry and the /admin route guard
 * agree on exactly the same predicate.
 */
export const MANAGER_CAPS = ['is_admin', 'can_manage_library', 'can_manage_users'] as const

/** True iff the given capability list grants access to a management surface. */
export function isManagerCaps(capabilities: string[] | undefined): boolean {
  if (!capabilities) return false
  return MANAGER_CAPS.some((c) => capabilities.includes(c))
}
