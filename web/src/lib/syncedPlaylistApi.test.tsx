import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  useSyncedPlaylists,
  useSyncedPlaylist,
  importPlaylist,
  syncNow,
  downloadMissingForPlaylist,
  updateSyncSettings,
  deleteSyncedPlaylist,
} from './syncedPlaylistApi'
import type { SyncedPlaylist, SyncedPlaylistDetail, DownloadJob } from './types'

const mockPlaylist: SyncedPlaylist = {
  id: 'sp-1',
  source: 'spotify',
  externalId: 'ext-123',
  name: 'My Synced Playlist',
  coverUrl: 'https://example.com/cover.jpg',
  syncEnabled: true,
  syncIntervalSec: 3600,
  autoDownload: false,
  lastSyncedAt: 1700000000,
  trackCount: 20,
}

const mockDetail: SyncedPlaylistDetail = {
  ...mockPlaylist,
  ownedCount: 15,
  totalCount: 20,
  tracks: [],
}

function wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>
}

// ── useSyncedPlaylists ────────────────────────────────────────────────────────

describe('useSyncedPlaylists', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        new Response(JSON.stringify([mockPlaylist]), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    )
  })
  afterEach(() => vi.unstubAllGlobals())

  it('fetches the synced-playlists list at the correct URL', async () => {
    const { result } = renderHook(() => useSyncedPlaylists(), { wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data).toHaveLength(1)
    expect(result.current.data![0].name).toBe('My Synced Playlist')
    const fetchMock = vi.mocked(fetch)
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/synced-playlists'),
      expect.anything(),
    )
  })
})

// ── useSyncedPlaylist ─────────────────────────────────────────────────────────

describe('useSyncedPlaylist', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string) => {
        if (url.includes('/synced-playlists/')) {
          return new Response(JSON.stringify(mockDetail), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          })
        }
        return new Response('not found', { status: 404 })
      }),
    )
  })
  afterEach(() => vi.unstubAllGlobals())

  it('fetches a single synced-playlist by id at the correct URL', async () => {
    const { result } = renderHook(() => useSyncedPlaylist('sp-1'), { wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data?.name).toBe('My Synced Playlist')
    expect(result.current.data?.ownedCount).toBe(15)
    const fetchMock = vi.mocked(fetch)
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/synced-playlists/sp-1'),
      expect.anything(),
    )
  })

  it('does not fetch when id is empty', () => {
    const { result } = renderHook(() => useSyncedPlaylist(''), { wrapper })
    expect(result.current.fetchStatus).toBe('idle')
  })
})

// ── importPlaylist ────────────────────────────────────────────────────────────

describe('importPlaylist', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        new Response(JSON.stringify(mockDetail), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    )
  })
  afterEach(() => vi.unstubAllGlobals())

  it('POSTs to /synced-playlists with the correct body', async () => {
    const result = await importPlaylist('https://open.spotify.com/playlist/abc', true)
    expect(result.name).toBe('My Synced Playlist')
    const fetchMock = vi.mocked(fetch)
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/synced-playlists'),
      expect.objectContaining({ method: 'POST' }),
    )
    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    const body = JSON.parse(init.body as string)
    expect(body.url).toBe('https://open.spotify.com/playlist/abc')
    expect(body.downloadMissing).toBe(true)
  })
})

// ── syncNow ───────────────────────────────────────────────────────────────────

describe('syncNow', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        new Response(JSON.stringify(mockDetail), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    )
  })
  afterEach(() => vi.unstubAllGlobals())

  it('POSTs to /synced-playlists/{id}/sync', async () => {
    await syncNow('sp-1')
    const fetchMock = vi.mocked(fetch)
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/synced-playlists/sp-1/sync'),
      expect.objectContaining({ method: 'POST' }),
    )
  })
})

// ── downloadMissingForPlaylist ────────────────────────────────────────────────

describe('downloadMissingForPlaylist', () => {
  const mockJobs: Partial<DownloadJob>[] = [{ id: 'job-1' }, { id: 'job-2' }]

  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        new Response(JSON.stringify(mockJobs), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    )
  })
  afterEach(() => vi.unstubAllGlobals())

  it('POSTs to /synced-playlists/{id}/download-missing', async () => {
    const jobs = await downloadMissingForPlaylist('sp-1')
    expect(jobs).toHaveLength(2)
    const fetchMock = vi.mocked(fetch)
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/synced-playlists/sp-1/download-missing'),
      expect.objectContaining({ method: 'POST' }),
    )
  })
})

// ── updateSyncSettings ────────────────────────────────────────────────────────

describe('updateSyncSettings', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response(null, { status: 200 })),
    )
  })
  afterEach(() => vi.unstubAllGlobals())

  it('PUTs settings to /synced-playlists/{id}/settings with the correct body', async () => {
    await updateSyncSettings('sp-1', { syncEnabled: true, intervalSec: 7200, autoDownload: true })
    const fetchMock = vi.mocked(fetch)
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/synced-playlists/sp-1/settings'),
      expect.objectContaining({ method: 'PUT' }),
    )
    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    const body = JSON.parse(init.body as string)
    expect(body.syncEnabled).toBe(true)
    expect(body.intervalSec).toBe(7200)
    expect(body.autoDownload).toBe(true)
  })
})

// ── deleteSyncedPlaylist ──────────────────────────────────────────────────────

describe('deleteSyncedPlaylist', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response(null, { status: 200 })),
    )
  })
  afterEach(() => vi.unstubAllGlobals())

  it('DELETEs /synced-playlists/{id}', async () => {
    await deleteSyncedPlaylist('sp-1')
    const fetchMock = vi.mocked(fetch)
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/synced-playlists/sp-1'),
      expect.objectContaining({ method: 'DELETE' }),
    )
  })
})
