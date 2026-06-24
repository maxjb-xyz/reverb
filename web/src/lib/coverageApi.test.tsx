import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useArtistDetail, useAlbumDetail } from './coverageApi'
import { postBatchDownload } from './downloadApi'
import type { ArtistDetail, AlbumDetail, ExternalTrackRef } from './types'

const mockArtistDetail: ArtistDetail = {
  source: 'spotify',
  id: 'artist-1',
  name: 'Test Artist',
  resolved: true,
  albums: [
    {
      source: 'spotify',
      externalId: 'album-1',
      name: 'Test Album',
      year: 2020,
      kind: 'album',
      totalTracks: 10,
    },
  ],
}

const mockAlbumDetail: AlbumDetail = {
  source: 'spotify',
  id: 'album-1',
  name: 'Test Album',
  artist: 'Test Artist',
  year: 2020,
  ownedCount: 5,
  totalCount: 10,
  tracks: [],
}

function wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>
}

describe('useArtistDetail', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string) => {
        if (url.includes('/artist/')) {
          return new Response(JSON.stringify(mockArtistDetail), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          })
        }
        return new Response('not found', { status: 404 })
      }),
    )
  })
  afterEach(() => vi.unstubAllGlobals())

  it('fetches artist detail at the correct URL', async () => {
    const { result } = renderHook(() => useArtistDetail('spotify', 'artist-1'), { wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data?.name).toBe('Test Artist')
    expect(result.current.data?.albums).toHaveLength(1)
    const fetchMock = vi.mocked(fetch)
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/artist/spotify/artist-1'),
      expect.anything(),
    )
  })

  it('does not fetch when source or id is empty', () => {
    const { result } = renderHook(() => useArtistDetail('', 'artist-1'), { wrapper })
    expect(result.current.fetchStatus).toBe('idle')
  })
})

describe('useAlbumDetail', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string) => {
        if (url.includes('/album/')) {
          return new Response(JSON.stringify(mockAlbumDetail), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          })
        }
        return new Response('not found', { status: 404 })
      }),
    )
  })
  afterEach(() => vi.unstubAllGlobals())

  it('fetches album detail at the correct URL', async () => {
    const { result } = renderHook(() => useAlbumDetail('spotify', 'album-1'), { wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data?.name).toBe('Test Album')
    expect(result.current.data?.ownedCount).toBe(5)
    const fetchMock = vi.mocked(fetch)
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/album/spotify/album-1'),
      expect.anything(),
    )
  })

  it('does not fetch when source or id is empty', () => {
    const { result } = renderHook(() => useAlbumDetail('spotify', ''), { wrapper })
    expect(result.current.fetchStatus).toBe('idle')
  })
})

describe('postBatchDownload', () => {
  const tracks: ExternalTrackRef[] = [
    { source: 'spotify', externalId: 'tr-1', title: 'Song A', artist: 'Artist A', album: 'Album A', durationMs: 200000 },
    { source: 'spotify', externalId: 'tr-2', title: 'Song B', durationMs: 180000 },
  ]

  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        new Response(JSON.stringify([{ id: 'job-1' }, { id: 'job-2' }]), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    )
  })
  afterEach(() => vi.unstubAllGlobals())

  it('posts to /downloads/batch with the track list', async () => {
    const jobs = await postBatchDownload(tracks)
    expect(jobs).toHaveLength(2)
    const fetchMock = vi.mocked(fetch)
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/downloads/batch'),
      expect.objectContaining({ method: 'POST' }),
    )
    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    const body = JSON.parse(init.body as string)
    expect(body.tracks).toHaveLength(2)
    expect(body.tracks[0].externalId).toBe('tr-1')
  })
})
