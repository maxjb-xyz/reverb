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
  renameSyncedPlaylist,
  uploadPlaylistCover,
  reorderSyncedTracks,
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

// ── renameSyncedPlaylist ──────────────────────────────────────────────────────

describe('renameSyncedPlaylist', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (_url: string, init?: RequestInit) => {
        const body = JSON.parse(init?.body as string) as { name: string }
        if (body.name !== 'New Name') return new Response(null, { status: 400 })
        return new Response(JSON.stringify({ ...mockDetail, name: 'New Name' }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      }),
    )
  })
  afterEach(() => vi.unstubAllGlobals())

  it('PUTs to /synced-playlists/:id with name', async () => {
    const result = await renameSyncedPlaylist('sp-1', 'New Name')
    expect(result).toMatchObject({ name: 'New Name' })
    const fetchMock = vi.mocked(fetch)
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/synced-playlists/sp-1'),
      expect.objectContaining({ method: 'PUT' }),
    )
    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    const body = JSON.parse(init.body as string)
    expect(body.name).toBe('New Name')
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

// ── uploadPlaylistCover ───────────────────────────────────────────────────────

describe('uploadPlaylistCover', () => {
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

  it('POSTs to /synced-playlists/{id}/cover with multipart form data containing the image field', async () => {
    const file = new File(['(image-data)'], 'cover.jpg', { type: 'image/jpeg' })
    const result = await uploadPlaylistCover('sp-1', file)
    expect(result.name).toBe('My Synced Playlist')
    const fetchMock = vi.mocked(fetch)
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/synced-playlists/sp-1/cover'),
      expect.objectContaining({ method: 'POST', credentials: 'include' }),
    )
    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    // Body must be FormData (not JSON) — no Content-Type header set manually
    expect(init.body).toBeInstanceOf(FormData)
    const form = init.body as FormData
    expect(form.get('image')).toBe(file)
    // Must NOT set Content-Type: application/json
    expect((init.headers as Record<string, string> | undefined)?.['Content-Type']).toBeUndefined()
  })

  it('throws ApiError when server returns non-ok status', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response('too large', { status: 413 })),
    )
    const file = new File(['(big)'], 'big.jpg', { type: 'image/jpeg' })
    await expect(uploadPlaylistCover('sp-1', file)).rejects.toThrow()
  })
})

// ── reorderSyncedTracks ───────────────────────────────────────────────────────

describe('reorderSyncedTracks', () => {
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

  it('PUTs to /synced-playlists/{id}/tracks/order with {order:[...]} body', async () => {
    const order = [
      { source: 'spotify', externalId: 'e2' },
      { source: 'spotify', externalId: 'e1' },
      { source: 'spotify', externalId: 'e3' },
    ]
    const result = await reorderSyncedTracks('sp-1', order)
    expect(result.name).toBe('My Synced Playlist')
    const fetchMock = vi.mocked(fetch)
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/synced-playlists/sp-1/tracks/order'),
      expect.objectContaining({ method: 'PUT' }),
    )
    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    const body = JSON.parse(init.body as string)
    expect(body.order).toEqual(order)
  })
})
