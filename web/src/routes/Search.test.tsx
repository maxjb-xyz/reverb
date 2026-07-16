import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { act, render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import Search from './Search'
import { engine } from '../lib/playerStore'
import { useSearch } from '../lib/searchStore'
import { makeTrack, makeAlbum } from '../test/factories'

beforeEach(() => useSearch.setState({ query: '' }))


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

describe('Search (empty query)', () => {
  it('shows an EmptyState prompt when no query is typed', () => {
    render(wrap(<Search />))
    // "Find your music" is the EmptyState title
    expect(screen.getByText(/find your music/i)).toBeInTheDocument()
  })
})

describe('Search (blended results)', () => {
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

  it('uses the blended-search placeholder', () => {
    render(wrap(<Search />))
    expect(screen.getByPlaceholderText('Search your library — or everywhere')).toBeInTheDocument()
  })

  it('waits for external search to finish before showing the empty state', async () => {
    let stream: { onerror: (() => void) | null } | null = null
    class StubEventSource {
      onmessage: ((event: MessageEvent) => void) | null = null
      onerror: (() => void) | null = null
      constructor() { stream = this }
      close() {}
    }
    vi.stubGlobal('EventSource', StubEventSource as unknown as typeof EventSource)
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ tracks: [], albums: [], artists: [] }), { status: 200, headers: { 'Content-Type': 'application/json' } })))
    render(wrap(<Search />))
    fireEvent.change(screen.getByPlaceholderText(/search your library/i), { target: { value: 'missing' } })
    await new Promise((resolve) => setTimeout(resolve, 450))
    await waitFor(() => expect(stream).not.toBeNull())
    expect(screen.queryByText('No results')).toBeNull()
    act(() => stream!.onerror?.())
    await waitFor(() => expect(screen.getByText('No results')).toBeInTheDocument())
    vi.unstubAllGlobals()
  })

  it('calls engine.playTrackList with track list and index when a track row is double-clicked', async () => {
    const spy = vi.spyOn(engine, 'playTrackList').mockImplementation(() => {})
    render(wrap(<Search />))
    fireEvent.change(screen.getByPlaceholderText(/search your library/i), { target: { value: 'found' } })
    await waitFor(() => expect(screen.getByText('Found Song')).toBeInTheDocument())
    fireEvent.doubleClick(screen.getByText('Found Song'))
    expect(spy).toHaveBeenCalledOnce()
    expect(spy).toHaveBeenCalledWith([stubTrack], 0)
    spy.mockRestore()
  })

  it('merges library and external tracks into a single Songs section, library first', async () => {
    let inst: { onmessage: ((ev: { data: string }) => void) | null; onerror: (() => void) | null; close(): void } | null = null
    class StubES {
      onmessage: ((ev: { data: string }) => void) | null = null
      onerror: (() => void) | null = null
      constructor() { inst = this }
      close() {}
    }
    vi.stubGlobal('EventSource', StubES as unknown as typeof EventSource)

    render(wrap(<Search />))
    fireEvent.change(screen.getByPlaceholderText(/search your library/i), { target: { value: 'found' } })
    await waitFor(() => expect(screen.getByText('Found Song')).toBeInTheDocument())
    await waitFor(() => expect(inst).not.toBeNull())

    act(() => {
      inst!.onmessage?.({
        data: JSON.stringify({
          source: 'spotify',
          status: 'ok',
          results: [
            {
              source: 'spotify',
              externalId: 'sp-ext',
              title: 'External Only Song',
              artist: 'B',
              album: 'B',
              durationMs: 200000,
              type: 'track',
              match: { status: 'not_in_library', libraryTrackId: '', method: 'none', confidence: 0 },
            },
          ],
        }),
      })
    })

    await waitFor(() => expect(screen.getByText('External Only Song')).toBeInTheDocument())

    // Exactly one "Songs" heading — the two old blocks are merged. (The
    // Segmented filter also has a "Songs" tab label, so scope to the heading role.)
    expect(screen.getAllByRole('heading', { name: 'Songs' })).toHaveLength(1)

    // Library row precedes the external row in DOM order.
    const libNode = screen.getByText('Found Song')
    const extNode = screen.getByText('External Only Song')
    // eslint-disable-next-line no-bitwise
    expect(libNode.compareDocumentPosition(extNode) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()

    vi.unstubAllGlobals()
  })

  it('does not show "No tracks found." when the library has songs and external has none', async () => {
    let inst: { onmessage: ((ev: { data: string }) => void) | null; onerror: (() => void) | null; close(): void } | null = null
    class StubES {
      onmessage: ((ev: { data: string }) => void) | null = null
      onerror: (() => void) | null = null
      constructor() { inst = this }
      close() {}
    }
    vi.stubGlobal('EventSource', StubES as unknown as typeof EventSource)

    render(wrap(<Search />))
    fireEvent.change(screen.getByPlaceholderText(/search your library/i), { target: { value: 'found' } })
    await waitFor(() => expect(screen.getByText('Found Song')).toBeInTheDocument())
    await waitFor(() => expect(inst).not.toBeNull())

    act(() => inst!.onerror?.())

    await waitFor(() => expect(screen.queryByText('No results')).not.toBeInTheDocument())
    expect(screen.queryByText('No tracks found.')).not.toBeInTheDocument()

    vi.unstubAllGlobals()
  })

  it('selecting the Albums filter hides both library and external song rows', async () => {
    let inst: { onmessage: ((ev: { data: string }) => void) | null; onerror: (() => void) | null; close(): void } | null = null
    class StubES {
      onmessage: ((ev: { data: string }) => void) | null = null
      onerror: (() => void) | null = null
      constructor() { inst = this }
      close() {}
    }
    vi.stubGlobal('EventSource', StubES as unknown as typeof EventSource)

    render(wrap(<Search />))
    fireEvent.change(screen.getByPlaceholderText(/search your library/i), { target: { value: 'found' } })
    await waitFor(() => expect(screen.getByText('Found Song')).toBeInTheDocument())
    await waitFor(() => expect(inst).not.toBeNull())

    act(() => {
      inst!.onmessage?.({
        data: JSON.stringify({
          source: 'spotify',
          status: 'ok',
          results: [
            {
              source: 'spotify',
              externalId: 'sp-ext2',
              title: 'External Filter Song',
              artist: 'B',
              album: 'B',
              durationMs: 200000,
              type: 'track',
              match: { status: 'not_in_library', libraryTrackId: '', method: 'none', confidence: 0 },
            },
          ],
        }),
      })
    })
    await waitFor(() => expect(screen.getByText('External Filter Song')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('tab', { name: /^Albums$/i }))

    expect(screen.queryByText('Found Song')).not.toBeInTheDocument()
    expect(screen.queryByText('External Filter Song')).not.toBeInTheDocument()

    vi.unstubAllGlobals()
  })

})

