import { create } from 'zustand'

export interface PendingPlay {
  jobId: string
  title: string
  artist: string
  coverArtId?: string
  progress: number
  failed: boolean
}

interface PendingPlayStore {
  pending: PendingPlay | null
  begin(pending: Omit<PendingPlay, 'progress' | 'failed'>): void
  update(jobId: string, progress: number): void
  fail(jobId: string): void
  clear(jobId: string): void
}

export const usePendingPlay = create<PendingPlayStore>((set) => ({
  pending: null,
  begin: (pending) => set({ pending: { ...pending, progress: -1, failed: false } }),
  update: (jobId, progress) => set((state) =>
    state.pending?.jobId === jobId ? { pending: { ...state.pending, progress } } : state),
  fail: (jobId) => set((state) =>
    state.pending?.jobId === jobId ? { pending: { ...state.pending, failed: true } } : state),
  clear: (jobId) => set((state) => state.pending?.jobId === jobId ? { pending: null } : state),
}))
