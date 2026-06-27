import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { coverUrl, streamUrl, trackCoverUrl, useLibrarySearch } from './libraryApi'

describe('url builders', () => {
  it('builds stream and cover urls', () => {
    expect(streamUrl('t 1')).toBe('/api/v1/stream/t%201')
    expect(coverUrl('al-1', 200)).toBe('/api/v1/cover/al-1?size=200')
    expect(coverUrl('')).toBe('')
  })
})

describe('trackCoverUrl', () => {
  it('prefers albumId over coverArtId', () => {
    expect(trackCoverUrl({ albumId: 'al-1', coverArtId: 'mf-9' }, 80)).toBe('/api/v1/cover/al-1?size=80')
  })

  it('falls back to coverArtId when albumId is absent', () => {
    expect(trackCoverUrl({ coverArtId: 'mf-9' }, 80)).toBe('/api/v1/cover/mf-9?size=80')
  })

  it('returns empty string when neither albumId nor coverArtId is present', () => {
    expect(trackCoverUrl({}, 80)).toBe('')
  })

  it('uses default size 300 when size is omitted', () => {
    expect(trackCoverUrl({ albumId: 'al-2' })).toBe('/api/v1/cover/al-2?size=300')
  })
})

describe('useLibrarySearch', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        new Response(JSON.stringify({ tracks: [{ id: 't1', title: 'Song' }], albums: [], artists: [] }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    )
  })
  afterEach(() => vi.unstubAllGlobals())

  it('fetches when query is non-empty', async () => {
    const qc = new QueryClient()
    const wrapper = ({ children }: { children: React.ReactNode }) => (
      <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    )
    const { result } = renderHook(() => useLibrarySearch('hello'), { wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data?.tracks[0].title).toBe('Song')
  })
})
