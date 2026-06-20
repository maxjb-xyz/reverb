import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { coverUrl, streamUrl, useLibrarySearch } from './libraryApi'

describe('url builders', () => {
  it('builds stream and cover urls', () => {
    expect(streamUrl('t 1')).toBe('/api/v1/stream/t%201')
    expect(coverUrl('al-1', 200)).toBe('/api/v1/cover/al-1?size=200')
    expect(coverUrl('')).toBe('')
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
