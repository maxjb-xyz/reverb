import { describe, expect, it, vi, afterEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useLyrics } from './lyricsApi'
import type { Track } from './types'

const track: Track = {
  id: 't1',
  title: 'Song Title',
  albumId: 'al-1',
  album: 'Album Name',
  artistId: 'ar-1',
  artist: 'Artist Name',
  coverArtId: 'mf-1',
  trackNumber: 1,
  discNumber: 1,
  durationMs: 210000,
  bitRate: 320,
  suffix: 'mp3',
  contentType: 'audio/mpeg',
}

function wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient()
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>
}

describe('useLyrics', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('returns synced payload on 200', async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL) =>
        new Response(
          JSON.stringify({ synced: true, lines: [{ timeMs: 1000, text: 'a' }] }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        ),
    )
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useLyrics(track), { wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(result.current.data).toEqual({ synced: true, lines: [{ timeMs: 1000, text: 'a' }] })

    const url = String(fetchMock.mock.calls[0][0])
    expect(url).toContain('/library/track/t1/lyrics')
    const query = new URLSearchParams(url.split('?')[1])
    expect(query.get('artist')).toBe('Artist Name')
    expect(query.get('title')).toBe('Song Title')
    expect(query.get('album')).toBe('Album Name')
    expect(query.get('durationMs')).toBe('210000')
  })

  it('returns null on 204', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response(null, { status: 204 })),
    )

    const { result } = renderHook(() => useLyrics(track), { wrapper })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(result.current.data).toBeNull()
    expect(result.current.isError).toBe(false)
  })

  it('does not fetch when track is null', () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useLyrics(null), { wrapper })

    expect(result.current.fetchStatus).toBe('idle')
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
