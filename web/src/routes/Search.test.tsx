import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { act } from '@testing-library/react'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import Search from './Search'
import { engine } from '../lib/playerStore'
import { makeTrack, makeAlbum } from '../test/factories'

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
        inst = this
      }
      close() {}
    }
    vi.stubGlobal('EventSource', StubES as unknown as typeof EventSource)

    render(wrap(<Search />))
    fireEvent.change(screen.getByPlaceholderText(/search/i), { target: { value: 'echoes' } })
    fireEvent.click(screen.getByRole('button', { name: /everywhere/i }))

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
    expect(screen.getByText(/Spotify ✓/)).toBeInTheDocument()
    expect(screen.getByTitle(/in library/i)).toBeInTheDocument()

    vi.unstubAllGlobals()
  })
})
