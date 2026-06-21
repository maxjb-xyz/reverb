import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import Album from './Album'
import { makeTrack, makeAlbum } from '../test/factories'

const mockPlayTrackList = vi.fn()
const mockToggleShuffle = vi.fn()

vi.mock('../lib/playerStore', () => ({
  usePlayer: (selector: (s: { playTrackList: typeof mockPlayTrackList; toggleShuffle: typeof mockToggleShuffle; shuffle: boolean; current: null }) => unknown) =>
    selector({ playTrackList: mockPlayTrackList, toggleShuffle: mockToggleShuffle, shuffle: false, current: null }),
}))

const stubTrack1 = makeTrack({ id: 't1', title: 'Track One', artist: 'Artist A', durationMs: 60000, trackNumber: 1 })
const stubTrack2 = makeTrack({ id: 't2', title: 'Track Two', artist: 'Artist A', durationMs: 90000, trackNumber: 2 })
const stubAlbum = makeAlbum({
  id: 'al1',
  name: 'Great Album',
  artistId: 'ar1',
  artist: 'Artist A',
  year: 2021,
  songCount: 2,
  durationMs: 150000,
  tracks: [stubTrack1, stubTrack2],
})

function wrapper(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/album/al1']}>
        <Routes>
          <Route path="/album/:id" element={ui} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('Album page', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        new Response(JSON.stringify(stubAlbum), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    )
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    mockPlayTrackList.mockReset()
    mockToggleShuffle.mockReset()
  })

  it('renders loading skeleton while fetching', () => {
    // Stall fetch so data never resolves
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})))
    wrapper(<Album />)
    expect(screen.getByTestId('album-skeleton')).toBeInTheDocument()
  })

  it('renders album title and meta after load', async () => {
    wrapper(<Album />)
    await waitFor(() => expect(screen.getByText('Great Album')).toBeInTheDocument())
    // Artist A appears in header link and in track rows; confirm at least once
    expect(screen.getAllByText('Artist A').length).toBeGreaterThan(0)
    expect(screen.getByText(/2021/)).toBeInTheDocument()
    expect(screen.getByText(/2 songs/)).toBeInTheDocument()
  })

  it('renders track rows', async () => {
    wrapper(<Album />)
    await waitFor(() => expect(screen.getByText('Track One')).toBeInTheDocument())
    expect(screen.getByText('Track Two')).toBeInTheDocument()
  })

  it('big play button calls playTrackList with all tracks from index 0', async () => {
    wrapper(<Album />)
    await waitFor(() => expect(screen.getByText('Great Album')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: /play great album/i }))
    expect(mockPlayTrackList).toHaveBeenCalledWith(stubAlbum.tracks, 0)
  })

  it('clicking a track row plays from its index', async () => {
    wrapper(<Album />)
    await waitFor(() => expect(screen.getByText('Track Two')).toBeInTheDocument())
    // Click the text "Track Two" which lives inside the TrackRow button
    fireEvent.click(screen.getByText('Track Two'))
    expect(mockPlayTrackList).toHaveBeenCalledWith(stubAlbum.tracks, 1)
  })

  it('shuffle button enables shuffle then plays from index 0', async () => {
    wrapper(<Album />)
    await waitFor(() => expect(screen.getByText('Great Album')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: /shuffle great album/i }))
    expect(mockToggleShuffle).toHaveBeenCalledTimes(1)
    expect(mockPlayTrackList).toHaveBeenCalledWith(stubAlbum.tracks, 0)
  })

  it('shows EmptyState when album not found', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response('null', { status: 404, headers: { 'Content-Type': 'application/json' } })),
    )
    wrapper(<Album />)
    await waitFor(() => expect(screen.getByText(/album not found/i)).toBeInTheDocument())
  })
})
