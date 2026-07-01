import { create } from 'zustand'
import type { DownloadEvent, DownloadJob } from './types'

interface DownloadStore {
  jobs: Record<string, DownloadJob>
  paused: boolean
  upsert(job: DownloadJob): void
  applyEvent(ev: DownloadEvent): void
  setAll(jobs: DownloadJob[]): void
  setPaused(paused: boolean): void
  remove(ids: string[]): void
  active(): DownloadJob[]
  running(): DownloadJob[]
  queued(): DownloadJob[]
  completed(): DownloadJob[]
  failed(): DownloadJob[]
  byExternal(source: string, externalId: string): DownloadJob | undefined
}

// jobFromEvent builds a minimal DownloadJob for an event referencing an unknown
// job (e.g. progress arrived before the POST response was stored).
function jobFromEvent(ev: DownloadEvent): DownloadJob {
  return {
    id: ev.jobId,
    dedupKey: ev.dedupKey,
    status: ev.status,
    progress: ev.progress,
    error: ev.error,
    libraryTrackId: ev.libraryTrackId,
    coverArtId: ev.coverArtId,
    canonicalId: ev.canonicalId,
    downloaderName: '',
    priority: 0,
    attempts: 0,
    source: ev.source,
    externalId: ev.externalId,
    playWhenReady: false,
    createdAt: Date.now() / 1000,
    startedAt: 0,
    finishedAt: 0,
  }
}

const byCreatedDesc = (a: DownloadJob, b: DownloadJob) => b.createdAt - a.createdAt

export const useDownloads = create<DownloadStore>((set, get) => ({
  jobs: {},
  paused: false,
  upsert: (job) => set((s) => ({ jobs: { ...s.jobs, [job.id]: job } })),
  applyEvent: (ev) =>
    set((s) => {
      const existing = s.jobs[ev.jobId]
      const next: DownloadJob = existing
        ? {
            ...existing,
            status: ev.status,
            progress: ev.progress,
            error: ev.error ?? existing.error,
            libraryTrackId: ev.libraryTrackId || existing.libraryTrackId,
            coverArtId: ev.coverArtId || existing.coverArtId,
            canonicalId: ev.canonicalId || existing.canonicalId,
          }
        : jobFromEvent(ev)
      return { jobs: { ...s.jobs, [ev.jobId]: next } }
    }),
  setAll: (jobs) =>
    set(() => {
      const map: Record<string, DownloadJob> = {}
      for (const j of jobs) map[j.id] = j
      return { jobs: map }
    }),
  setPaused: (paused) => set({ paused }),
  remove: (ids) =>
    set((s) => {
      if (ids.length === 0) return s
      const next = { ...s.jobs }
      for (const id of ids) delete next[id]
      return { jobs: next }
    }),
  active: () =>
    Object.values(get().jobs)
      .filter((j) => j.status === 'queued' || j.status === 'running')
      .sort(byCreatedDesc),
  running: () => Object.values(get().jobs).filter((j) => j.status === 'running').sort(byCreatedDesc),
  queued: () => Object.values(get().jobs).filter((j) => j.status === 'queued').sort(byCreatedDesc),
  completed: () => Object.values(get().jobs).filter((j) => j.status === 'completed').sort(byCreatedDesc),
  failed: () => Object.values(get().jobs).filter((j) => j.status === 'failed').sort(byCreatedDesc),
  byExternal: (source, externalId) =>
    Object.values(get().jobs).find((j) => j.source === source && j.externalId === externalId),
}))
