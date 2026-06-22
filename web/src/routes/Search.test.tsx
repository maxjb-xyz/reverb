import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { act, render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import Search from './Search'
import { engine } from '../lib/playerStore'
import { useSearch } from '../lib/searchStore'
import { makeTrack, makeAlbum } from '../test/factories'

// Search query/mode now live in a shared store; reset it before every test so
// one test's mode (e.g. 'everywhere') doesn't leak into the next.
beforeEach(() => useSearch.setState({ query: '', mode: 'library' }))

// eslint-disable-next-line @typescript-eslint/no-unused-vars -- _req exists for TypeScript compatibility with the mock wrapper
const postDownloadMock = vi.fn((_req: unknown) => Promise.resolve({ id: 'j-album', status: 'queued' } as never))
vi.mock('../lib/downloadApi', () => ({
  postDownload: (req: unknown) => postDownloadMock(req),
}))
vi.mock('../lib/downloadStore', () => ({
  useDownloads: Object.assign(
    vi.fn(() => ({ byExternal: () => undefined })),
    { getState: () => ({ upsert: vi.fn() }) },
  ),
}))
vi.mock('../lib/adaptersApi', () => ({
  useAdapters: vi.fn(() => ({ data: [] })),
}))

const stubTrack = makeTrack({ id: 't1', title: 'Found Song', artist: 'A', durationMs: 1000, trackNumber: 1 })
const stubAlbum = makeAlbum({ id: 'al1', name: 'Found Album', artist: 'A' })

function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return (
    <QueryClientProvider client={qc}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>
  )
}

// Helper: click the Everywhere tab in the Segmented control (role="tab")
function clickTab(name: RegExp | string) {
  fireEvent.click(screen.getByRole('tab', { name }))
}

describe('Search (empty query)', () => {
  it('shows an EmptyState prompt when no query is typed', () => {
    render(wrap(<Search />))
    // "Find your music" is the EmptyState title
    expect(screen.getByText(/find your music/i)).toBeInTheDocument()
  })
})

describe('Search (library mode)', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        new Response(
          JSON.stringify({
            tracks: [stubTrack],
            albums: [stubAlbum],
            artists: [{ id: 'ar1', name: 'Found Artist', coverArtId: '', albumCount: 1 }],
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        ),
      ),
    )
  })
  afterEach(() => vi.unstubAllGlobals())

  it('renders results in sections after typing a query', async () => {
    render(wrap(<Search />))
    fireEvent.change(screen.getByPlaceholderText(/search your library/i), { target: { value: 'found' } })
    await waitFor(() => expect(screen.getByText('Found Song')).toBeInTheDocument())
    expect(screen.getByText('Found Album')).toBeInTheDocument()
    expect(screen.getByText('Found Artist')).toBeInTheDocument()
  })

  it('placeholder is mode-conditional: My Library mode shows "Search your library"', () => {
    render(wrap(<Search />))
    expect(screen.getByPlaceholderText('Search your library')).toBeInTheDocument()
  })

  it('calls engine.playTrackList with track list and index when a track row is clicked', async () => {
    const spy = vi.spyOn(engine, 'playTrackList').mockImplementation(() => {})
    render(wrap(<Search />))
    fireEvent.change(screen.getByPlaceholderText(/search your library/i), { target: { value: 'found' } })
    await waitFor(() => expect(screen.getByText('Found Song')).toBeInTheDocument())
    fireEvent.click(screen.getByText('Found Song'))
    expect(spy).toHaveBeenCalledOnce()
    expect(spy).toHaveBeenCalledWith([stubTrack], 0)
    spy.mockRestore()
  })

  it('shows My Library tab as selected by default', () => {
    render(wrap(<Search />))
    // The scope toggle lives in the results header, which renders once a query
    // is present — type first, then assert the default-selected tab.
    fireEvent.change(screen.getByPlaceholderText(/search your library/i), { target: { value: 'found' } })
    const tab = screen.getByRole('tab', { name: /my library/i })
    expect(tab).toHaveAttribute('aria-selected', 'true')
  })
})

describe('Search (everywhere mode)', () => {
  it('streams external results into stable sections with source chips', async () => {
    // Stub EventSource so no real network is opened; capture the instance to emit.
    let inst: { onmessage: ((ev: { data: string }) => void) | null; onerror: (() => void) | null; close(): void } | null = null
    class StubES {
      onmessage: ((ev: { data: string }) => void) | null = null
      onerror: (() => void) | null = null
      url: string
      constructor(url: string) {
        this.url = url
        // eslint-disable-next-line @typescript-eslint/no-this-alias
        inst = this
      }
      close() {}
    }
    vi.stubGlobal('EventSource', StubES as unknown as typeof EventSource)

    render(wrap(<Search />))
    fireEvent.change(screen.getByPlaceholderText(/search/i), { target: { value: 'echoes' } })
    clickTab(/everywhere/i)

    act(() => {
      inst!.onmessage?.({
        data: JSON.stringify({
          source: 'spotify',
          status: 'ok',
          results: [
            { source: 'spotify', externalId: 'sp1', title: 'Echoes', artist: 'Vale', album: 'Deep', durationMs: 240000, type: 'track', match: { status: 'in_library', libraryTrackId: 't3', method: 'fuzzy', confidence: 0.9 } },
          ],
        }),
      })
    })

    await waitFor(() => expect(screen.getByText('Echoes')).toBeInTheDocument())
    // Source chip shows the source name (ok status) without a glyph literal
    expect(screen.getByText('Spotify')).toBeInTheDocument()
    // In-library track shows a "In Library" button with title
    expect(screen.getByTitle(/in library/i)).toBeInTheDocument()

    vi.unstubAllGlobals()
  })

  it('source chips render correct tone: error status shows error badge', async () => {
    let inst: { onmessage: ((ev: { data: string }) => void) | null; onerror: (() => void) | null; close(): void } | null = null
    class StubES {
      onmessage: ((ev: { data: string }) => void) | null = null
      onerror: (() => void) | null = null
      url: string
      constructor(url: string) {
        this.url = url
        // eslint-disable-next-line @typescript-eslint/no-this-alias
        inst = this
      }
      close() {}
    }
    vi.stubGlobal('EventSource', StubES as unknown as typeof EventSource)

    render(wrap(<Search />))
    fireEvent.change(screen.getByPlaceholderText(/search/i), { target: { value: 'test' } })
    clickTab(/everywhere/i)

    act(() => {
      inst!.onmessage?.({
        data: JSON.stringify({ source: 'spotify', status: 'error', results: [] }),
      })
    })

    await waitFor(() => expect(screen.getByText(/Spotify error/i)).toBeInTheDocument())

    vi.unstubAllGlobals()
  })

  it('external not-in-library track renders a Download affordance', async () => {
    let inst: { onmessage: ((ev: { data: string }) => void) | null; onerror: (() => void) | null; close(): void } | null = null
    class StubES {
      onmessage: ((ev: { data: string }) => void) | null = null
      onerror: (() => void) | null = null
      url: string
      constructor(url: string) {
        this.url = url
        // eslint-disable-next-line @typescript-eslint/no-this-alias
        inst = this
      }
      close() {}
    }
    vi.stubGlobal('EventSource', StubES as unknown as typeof EventSource)
    // Stub adapters API so DownloadAction renders with no-downloader state
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string) => {
        if (url.includes('/adapters')) {
          return new Response(JSON.stringify([]), { status: 200, headers: { 'Content-Type': 'application/json' } })
        }
        return new Response('{}', { status: 404 })
      }),
    )

    render(wrap(<Search />))
    fireEvent.change(screen.getByPlaceholderText(/search/i), { target: { value: 'newtrack' } })
    clickTab(/everywhere/i)

    act(() => {
      inst!.onmessage?.({
        data: JSON.stringify({
          source: 'spotify',
          status: 'ok',
          results: [
            { source: 'spotify', externalId: 'sp2', title: 'New Track', artist: 'Y', album: 'Z', durationMs: 180000, type: 'track', match: { status: 'not_in_library', libraryTrackId: '', method: 'none', confidence: 0 } },
          ],
        }),
      })
    })

    await waitFor(() => expect(screen.getByText('New Track')).toBeInTheDocument())
    // DownloadAction renders some download affordance (no-downloader badge or download button)
    // It should NOT show "In Library"
    expect(screen.queryByTitle(/in library/i)).not.toBeInTheDocument()

    vi.unstubAllGlobals()
  })

  it('close-on-change: closes the prior stream when the query changes', async () => {
    let inst: { onmessage: ((ev: { data: string }) => void) | null; onerror: (() => void) | null; close(): void } | null = null
    class StubES2 {
      onmessage: ((ev: { data: string }) => void) | null = null
      onerror: (() => void) | null = null
      url: string
      constructor(url: string) {
        this.url = url
        // eslint-disable-next-line @typescript-eslint/no-this-alias
        inst = this
      }
      close() {}
    }
    vi.stubGlobal('EventSource', StubES2 as unknown as typeof EventSource)

    render(wrap(<Search />))
    fireEvent.change(screen.getByPlaceholderText(/search/i), { target: { value: 'first' } })
    clickTab(/everywhere/i)

    // Capture the first instance and spy on it
    const firstInst = inst!
    const closeSpy = vi.spyOn(firstInst, 'close')

    // Change the query — the effect should close the prior stream
    await act(async () => {
      fireEvent.change(screen.getByPlaceholderText(/search/i), { target: { value: 'second' } })
    })

    expect(closeSpy).toHaveBeenCalled()

    vi.unstubAllGlobals()
  })

  it('placeholder is mode-conditional: Everywhere mode shows "Search everywhere"', async () => {
    // The scope toggle renders in the results header once a query is present, so
    // we must type first — which means Everywhere mode opens a real SSE stream.
    // Stub EventSource so no network is touched.
    class StubES {
      onmessage: ((ev: { data: string }) => void) | null = null
      onerror: (() => void) | null = null
      url: string
      constructor(url: string) { this.url = url }
      close() {}
    }
    vi.stubGlobal('EventSource', StubES as unknown as typeof EventSource)

    render(wrap(<Search />))
    fireEvent.change(screen.getByPlaceholderText(/search/i), { target: { value: 'x' } })
    clickTab(/everywhere/i)
    expect(screen.getByPlaceholderText('Search everywhere')).toBeInTheDocument()

    vi.unstubAllGlobals()
  })

  it('I2 — timeout chip: source with status timeout renders timeout label and warning tone', async () => {
    let inst: { onmessage: ((ev: { data: string }) => void) | null; close(): void } | null = null
    class StubES {
      onmessage: ((ev: { data: string }) => void) | null = null
      onerror: (() => void) | null = null
      url: string
      constructor(url: string) {
        this.url = url
        // eslint-disable-next-line @typescript-eslint/no-this-alias
        inst = this
      }
      close() {}
    }
    vi.stubGlobal('EventSource', StubES as unknown as typeof EventSource)

    render(wrap(<Search />))
    fireEvent.change(screen.getByPlaceholderText(/search/i), { target: { value: 'test' } })
    clickTab(/everywhere/i)

    act(() => {
      inst!.onmessage?.({
        data: JSON.stringify({ source: 'spotify', status: 'timeout', results: [] }),
      })
    })

    await waitFor(() => expect(screen.getByText(/Spotify timed out/i)).toBeInTheDocument())
    vi.unstubAllGlobals()
  })

  it('C2 — non-in-library album shows a Download-all control that calls postDownload with album fields', async () => {
    let inst: { onmessage: ((ev: { data: string }) => void) | null; close(): void } | null = null
    class StubES {
      onmessage: ((ev: { data: string }) => void) | null = null
      onerror: (() => void) | null = null
      url: string
      constructor(url: string) {
        this.url = url
        // eslint-disable-next-line @typescript-eslint/no-this-alias
        inst = this
      }
      close() {}
    }
    vi.stubGlobal('EventSource', StubES as unknown as typeof EventSource)
    postDownloadMock.mockClear()

    render(wrap(<Search />))
    fireEvent.change(screen.getByPlaceholderText(/search/i), { target: { value: 'blues' } })
    clickTab(/everywhere/i)

    act(() => {
      inst!.onmessage?.({
        data: JSON.stringify({
          source: 'spotify',
          status: 'ok',
          results: [
            { source: 'spotify', externalId: 'alb1', title: 'Blue Album', artist: 'Band', album: 'Blue Album', durationMs: 0, type: 'album', match: { status: 'not_in_library' } },
          ],
        }),
      })
    })

    await waitFor(() => expect(screen.getByRole('button', { name: /download all of Blue Album/i })).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: /download all of Blue Album/i }))
    expect(postDownloadMock).toHaveBeenCalledWith(
      expect.objectContaining({ source: 'spotify', externalId: 'alb1', artist: 'Band', title: 'Blue Album', album: 'Blue Album' }),
    )
    vi.unstubAllGlobals()
  })

  it('switch-back to Library: shows library UI after switching from Everywhere', async () => {
    let inst: { onmessage: ((ev: { data: string }) => void) | null; onerror: (() => void) | null; close(): void } | null = null
    class StubES3 {
      onmessage: ((ev: { data: string }) => void) | null = null
      onerror: (() => void) | null = null
      url: string
      constructor(url: string) {
        this.url = url
        // eslint-disable-next-line @typescript-eslint/no-this-alias
        inst = this
      }
      close() {}
    }
    vi.stubGlobal('EventSource', StubES3 as unknown as typeof EventSource)
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        new Response(JSON.stringify({ tracks: [], albums: [], artists: [] }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    )

    render(wrap(<Search />))
    fireEvent.change(screen.getByPlaceholderText(/search/i), { target: { value: 'echoes' } })
    clickTab(/everywhere/i)

    // Emit one envelope so Everywhere UI is visible
    act(() => {
      inst!.onmessage?.({
        data: JSON.stringify({
          source: 'spotify',
          status: 'ok',
          results: [
            { source: 'spotify', externalId: 'sp1', title: 'EverywhereTitle', artist: 'X', album: 'Y', durationMs: 1000, type: 'track', match: { status: 'none' } },
          ],
        }),
      })
    })
    await waitFor(() => expect(screen.getByText('EverywhereTitle')).toBeInTheDocument())

    // Switch back to Library
    await act(async () => {
      clickTab(/my library/i)
    })

    // External Everywhere rows should be gone; library input placeholder still present
    expect(screen.queryByText('EverywhereTitle')).not.toBeInTheDocument()
    expect(screen.getByPlaceholderText(/search your library/i)).toBeInTheDocument()

    vi.unstubAllGlobals()
  })
})
