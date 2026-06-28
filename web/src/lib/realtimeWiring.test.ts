import { describe, it, expect, beforeEach, vi } from 'vitest'
import { renderHook } from '@testing-library/react'
import { createElement, type ReactNode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useRealtime } from './realtimeWiring'
import { useDownloads } from './downloadStore'
import { useLibraryRevision } from './libraryRevisionStore'
import { useRequestStore } from './requestApi'
import { useToastStore } from './toastStore'
import type { WebSocketLike } from './realtime'

// Player spy: usePlayer((s) => s.playTrackList) must return our spy.
const playTrackList = vi.fn()
vi.mock('./playerStore', () => ({
  usePlayer: (sel: (s: { playTrackList: typeof playTrackList }) => unknown) => sel({ playTrackList }),
}))

// downloadApi resync is stubbed (no real network).
vi.mock('./downloadApi', () => ({
  getDownloads: vi.fn(() => Promise.resolve([])),
  getQueueState: vi.fn(() => Promise.resolve({ paused: false })),
}))

// A controllable stub socket the test drives.
const sockets: StubSocket[] = []
class StubSocket implements WebSocketLike {
  onopen: (() => void) | null = null
  onmessage: ((ev: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  closed = false
  url: string
  constructor(url: string) {
    this.url = url
    sockets.push(this)
  }
  close() {
    this.closed = true
    this.onclose?.()
  }
}

function frame(type: string, payload: unknown) {
  return { data: JSON.stringify({ type, payload }) }
}

describe('useRealtime', () => {
  let qc: QueryClient
  let invalidateSpy: ReturnType<typeof vi.spyOn>

  beforeEach(() => {
    sockets.length = 0
    playTrackList.mockClear()
    useDownloads.setState({ jobs: {} })
    useLibraryRevision.setState({ revision: 0 })
    useRequestStore.setState({ byId: {} })
    useToastStore.setState({ toasts: [] })
    qc = new QueryClient()
    invalidateSpy = vi.spyOn(qc, 'invalidateQueries')
  })

  function wrapper({ children }: { children: ReactNode }) {
    return createElement(QueryClientProvider, { client: qc }, children)
  }

  it('updates the store on progress, plays a play-when-ready completion, and invalidates', () => {
    // Seed a job started with playWhenReady so completion auto-plays.
    useDownloads.getState().upsert({
      id: 'j1', dedupKey: 'dk', status: 'running', progress: 0, downloaderName: 'spotdl',
      priority: 0, attempts: 0, source: 'spotify', externalId: 'sp1', playWhenReady: true,
      title: 'Song', artist: 'Artist', album: 'Album', createdAt: 1, startedAt: 0, finishedAt: 0,
    } as never)

    const { unmount } = renderHook(() => useRealtime((url) => new StubSocket(url)), { wrapper })
    const s = sockets[0]
    expect(s.url).toContain('/api/v1/ws')

    // A progress event patches the store.
    s.onmessage?.(frame('download.progress', { jobId: 'j1', dedupKey: 'dk', status: 'running', progress: 42, source: 'spotify', externalId: 'sp1' }))
    expect(useDownloads.getState().jobs['j1'].progress).toBe(42)

    // A completion event: store reflects completed + libraryTrackId, player auto-plays
    // (job had playWhenReady), and library + detail queries are invalidated.
    s.onmessage?.(frame('download.complete', { jobId: 'j1', dedupKey: 'dk', status: 'completed', progress: 100, source: 'spotify', externalId: 'sp1', libraryTrackId: 't9' }))
    expect(useDownloads.getState().jobs['j1'].status).toBe('completed')
    expect(useDownloads.getState().jobs['j1'].libraryTrackId).toBe('t9')
    expect(playTrackList).toHaveBeenCalledTimes(1)
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['library'] })
    // Detail-page query keys must also be invalidated so missing rows flip to playable.
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['album-detail'] })
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['artist-detail'] })
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['synced-playlist'] })

    // library.updated also invalidates (broad fallback even with empty IDs).
    invalidateSpy.mockClear()
    s.onmessage?.(frame('library.updated', { artistIds: [], albumIds: [] }))
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['library'] })
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['album-detail'] })
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['artist-detail'] })
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ['synced-playlist'] })

    // Unmount closes the socket.
    unmount()
    expect(s.closed).toBe(true)
  })

  it('bumps library revision on download.complete', () => {
    vi.useFakeTimers()
    try {
      useDownloads.getState().upsert({
        id: 'j3', dedupKey: 'dk3', status: 'running', progress: 0, downloaderName: 'spotdl',
        priority: 0, attempts: 0, source: 'spotify', externalId: 'sp3', playWhenReady: false,
        title: 'Song3', artist: 'Artist3', album: 'Album3', createdAt: 1, startedAt: 0, finishedAt: 0,
      } as never)

      renderHook(() => useRealtime((url) => new StubSocket(url)), { wrapper })
      const s = sockets[0]
      expect(useLibraryRevision.getState().revision).toBe(0)

      s.onmessage?.(frame('download.complete', { jobId: 'j3', dedupKey: 'dk3', status: 'completed', progress: 100, source: 'spotify', externalId: 'sp3', libraryTrackId: 't3' }))
      vi.advanceTimersByTime(300)
      expect(useLibraryRevision.getState().revision).toBe(1)
    } finally {
      vi.useRealTimers()
    }
  })

  it('bumps library revision on library.updated', () => {
    vi.useFakeTimers()
    try {
      renderHook(() => useRealtime((url) => new StubSocket(url)), { wrapper })
      const s = sockets[0]
      expect(useLibraryRevision.getState().revision).toBe(0)

      s.onmessage?.(frame('library.updated', { artistIds: [], albumIds: [] }))
      vi.advanceTimersByTime(300)
      expect(useLibraryRevision.getState().revision).toBe(1)
    } finally {
      vi.useRealTimers()
    }
  })

  it('does NOT auto-play a completion whose job had playWhenReady=false', () => {
    useDownloads.getState().upsert({
      id: 'j2', dedupKey: 'dk2', status: 'running', progress: 0, downloaderName: 'spotdl',
      priority: 0, attempts: 0, source: 'spotify', externalId: 'sp2', playWhenReady: false,
      title: 'Song2', artist: 'Artist2', album: 'Album2', createdAt: 1, startedAt: 0, finishedAt: 0,
    } as never)
    renderHook(() => useRealtime((url) => new StubSocket(url)), { wrapper })
    sockets[0].onmessage?.(frame('download.complete', { jobId: 'j2', dedupKey: 'dk2', status: 'completed', progress: 100, source: 'spotify', externalId: 'sp2', libraryTrackId: 't5' }))
    expect(playTrackList).not.toHaveBeenCalled()
  })

  it('handles download.queue (paused) and download.removed (drop jobs)', () => {
    useDownloads.setState({
      jobs: {
        x: { id: 'x', dedupKey: 'x', status: 'completed', progress: 100, downloaderName: 'spotdl', priority: 0, attempts: 0, source: 's', externalId: 'x', playWhenReady: false, createdAt: 1, startedAt: 0, finishedAt: 0 } as never,
      },
      paused: false,
    })
    renderHook(() => useRealtime((url) => new StubSocket(url)), { wrapper })
    const s = sockets[0]

    s.onmessage?.(frame('download.queue', { paused: true }))
    expect(useDownloads.getState().paused).toBe(true)

    s.onmessage?.(frame('download.removed', { jobIds: ['x'] }))
    expect(useDownloads.getState().jobs['x']).toBeUndefined()
  })

  it('request.created upserts into the request store and emits no toast', () => {
    renderHook(() => useRealtime((url) => new StubSocket(url)), { wrapper })
    const s = sockets[0]

    s.onmessage?.(frame('request.created', {
      request: { id: 'r1', requestedBy: 'u1', source: 'spotify', externalId: 'e1', title: 'Bones', artist: 'Imagine Dragons', status: 'pending', createdAt: 1 },
    }))

    expect(useRequestStore.getState().byId['r1']).toBeDefined()
    expect(useRequestStore.getState().byId['r1'].title).toBe('Bones')
    expect(useToastStore.getState().toasts).toHaveLength(0)
  })

  it('request.updated with status fulfilled upserts store and shows a success toast mentioning the title', () => {
    renderHook(() => useRealtime((url) => new StubSocket(url)), { wrapper })
    const s = sockets[0]

    s.onmessage?.(frame('request.updated', {
      request: { id: 'r2', requestedBy: 'u1', source: 'spotify', externalId: 'e2', title: 'Bones', artist: 'Imagine Dragons', status: 'fulfilled', createdAt: 1 },
    }))

    expect(useRequestStore.getState().byId['r2']).toBeDefined()
    const toasts = useToastStore.getState().toasts
    expect(toasts).toHaveLength(1)
    expect(toasts[0].kind).toBe('success')
    expect(toasts[0].message).toContain('Bones')
    expect(toasts[0].message).toContain('added')
  })

  it('request.updated with status denied shows an error toast', () => {
    renderHook(() => useRealtime((url) => new StubSocket(url)), { wrapper })
    const s = sockets[0]

    s.onmessage?.(frame('request.updated', {
      request: { id: 'r3', requestedBy: 'u1', source: 'spotify', externalId: 'e3', title: 'Bones', artist: 'Imagine Dragons', status: 'denied', createdAt: 1 },
    }))

    const toasts = useToastStore.getState().toasts
    expect(toasts).toHaveLength(1)
    expect(toasts[0].kind).toBe('error')
    expect(toasts[0].message).toContain('denied')
  })

  it('request.updated with status failed shows an error toast', () => {
    renderHook(() => useRealtime((url) => new StubSocket(url)), { wrapper })
    const s = sockets[0]

    s.onmessage?.(frame('request.updated', {
      request: { id: 'r4', requestedBy: 'u1', source: 'spotify', externalId: 'e4', title: 'Bones', artist: 'Imagine Dragons', status: 'failed', createdAt: 1 },
    }))

    const toasts = useToastStore.getState().toasts
    expect(toasts).toHaveLength(1)
    expect(toasts[0].kind).toBe('error')
    expect(toasts[0].message).toContain('failed')
  })

  it('request.updated with status pending or approved emits no toast', () => {
    renderHook(() => useRealtime((url) => new StubSocket(url)), { wrapper })
    const s = sockets[0]

    s.onmessage?.(frame('request.updated', {
      request: { id: 'r5', requestedBy: 'u1', source: 'spotify', externalId: 'e5', title: 'Bones', artist: 'Imagine Dragons', status: 'pending', createdAt: 1 },
    }))
    s.onmessage?.(frame('request.updated', {
      request: { id: 'r6', requestedBy: 'u1', source: 'spotify', externalId: 'e6', title: 'Bones', artist: 'Imagine Dragons', status: 'approved', createdAt: 1 },
    }))

    expect(useToastStore.getState().toasts).toHaveLength(0)
  })
})
