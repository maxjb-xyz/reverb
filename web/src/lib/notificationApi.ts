import { create } from 'zustand'
import { api } from './api'

export interface Notification {
  id: string
  userId: string
  type: string
  title: string
  body: string
  requestId?: string
  read: boolean
  createdAt: number
}

// --- API functions ---

export function getNotifications(): Promise<{ notifications: Notification[]; unread: number }> {
  return api.get<{ notifications: Notification[]; unread: number }>('/notifications')
}

export function postMarkRead(ids?: string[]): Promise<{ unread: number }> {
  return api.post<{ unread: number }>('/notifications/read', { ids: ids ?? [] })
}

// --- Zustand store ---

interface NotificationStore {
  byId: Record<string, Notification>
  unread: number
  /** Replace the entire store with server data. unread is the server's value (source of truth). */
  setAll(notifications: Notification[], unread: number): void
  /** Insert a new notification. If unread, increment unread. */
  add(n: Notification): void
  /** Mark specific notifications as read; recompute unread from byId. */
  markRead(ids: string[]): void
  /** Mark all notifications read; unread becomes 0. */
  markAllRead(): void
  /** Notifications sorted newest-first by createdAt. */
  items(): Notification[]
}

export const useNotificationStore = create<NotificationStore>((set, get) => ({
  byId: {},
  unread: 0,

  setAll: (notifications, unread) => {
    const byId: Record<string, Notification> = {}
    for (const n of notifications) byId[n.id] = n
    set({ byId, unread })
  },

  add: (n) =>
    set((s) => ({
      byId: { ...s.byId, [n.id]: n },
      unread: n.read ? s.unread : s.unread + 1,
    })),

  markRead: (ids) =>
    set((s) => {
      const byId = { ...s.byId }
      for (const id of ids) {
        if (byId[id]) byId[id] = { ...byId[id], read: true }
      }
      const unread = Object.values(byId).filter((n) => !n.read).length
      return { byId, unread }
    }),

  markAllRead: () =>
    set((s) => {
      const byId: Record<string, Notification> = {}
      for (const [id, n] of Object.entries(s.byId)) {
        byId[id] = { ...n, read: true }
      }
      return { byId, unread: 0 }
    }),

  items: () =>
    Object.values(get().byId).sort((a, b) => b.createdAt - a.createdAt),
}))
