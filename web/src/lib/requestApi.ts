import { create } from 'zustand'
import { api } from './api'

export type RequestStatus = 'pending' | 'approved' | 'denied' | 'fulfilled' | 'failed'

export interface Request {
  id: string
  requestedBy: string
  source: string
  externalId: string
  title: string
  artist: string
  album?: string
  isrc?: string
  durationMs?: number
  coverArtId?: string
  status: RequestStatus
  createdAt: number // unix seconds
  decidedBy?: string
  decidedAt?: number
  downloadJobId?: string
  denyReason?: string
}

export interface CreateRequestItem {
  source: string
  externalId: string
  title: string
  artist: string
  album?: string
  isrc?: string
  durationMs?: number
  coverArtId?: string
}

export interface RequestEventPayload {
  request: Request
  targetUserId?: string
  forManagers?: boolean
}

// --- API functions ---

export function postRequest(item: CreateRequestItem): Promise<Request> {
  return api.post<Request>('/requests', item)
}

export function getMyRequests(): Promise<Request[]> {
  return api.get<Request[]>('/requests/mine')
}

export function getAllRequests(status?: RequestStatus): Promise<Request[]> {
  const path = status ? `/requests?status=${encodeURIComponent(status)}` : '/requests'
  return api.get<Request[]>(path)
}

export function approveRequest(id: string): Promise<Request> {
  return api.post<Request>(`/requests/${encodeURIComponent(id)}/approve`)
}

export function denyRequest(id: string, reason?: string): Promise<Request> {
  return api.post<Request>(`/requests/${encodeURIComponent(id)}/deny`, reason !== undefined ? { reason } : undefined)
}

export function cancelRequest(id: string): Promise<Request> {
  return api.post<Request>(`/requests/${encodeURIComponent(id)}/cancel`)
}

// --- Zustand store ---

interface RequestStore {
  byId: Record<string, Request>
  upsert(req: Request): void
  setMine(reqs: Request[]): void
  setQueue(reqs: Request[]): void
  applyRequestEvent(payload: RequestEventPayload): void
  mine(userId?: string): Request[]
  pending(): Request[]
  /** Returns the most relevant request for the given source+externalId pair,
   *  preferring an open (pending/approved) entry when multiple exist. */
  byExternal(source: string, externalId: string): Request | undefined
}

export const useRequestStore = create<RequestStore>((set, get) => ({
  byId: {},

  upsert: (req) =>
    set((s) => ({ byId: { ...s.byId, [req.id]: req } })),

  setMine: (reqs) =>
    set((s) => {
      const byId = { ...s.byId }
      for (const r of reqs) byId[r.id] = r
      return { byId }
    }),

  setQueue: (reqs) =>
    set((s) => {
      const next = { ...s.byId }
      for (const r of reqs) next[r.id] = r
      return { byId: next }
    }),

  applyRequestEvent: (payload) =>
    set((s) => ({ byId: { ...s.byId, [payload.request.id]: payload.request } })),

  mine: (userId?: string) => {
    const all = Object.values(get().byId)
    return userId ? all.filter((r) => r.requestedBy === userId) : all
  },

  pending: () =>
    Object.values(get().byId).filter((r) => r.status === 'pending'),

  byExternal: (source, externalId) => {
    const matches = Object.values(get().byId).filter(
      (r) => r.source === source && r.externalId === externalId,
    )
    if (matches.length === 0) return undefined
    // Prefer an open (pending/approved) entry when multiple exist
    return (
      matches.find((r) => r.status === 'pending' || r.status === 'approved') ??
      matches[0]
    )
  },
}))
