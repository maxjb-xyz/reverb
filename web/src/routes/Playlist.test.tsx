import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import Playlist from './Playlist'
import { makeTrack } from '../test/factories'
import type { Playlist as PlaylistType } from '../lib/types'

// ── Player mock ────────────────────────────────────────────────────────────────
const mockPlayTrackList = vi.fn()
const mockToggleShuffle = vi.fn()

vi.mock('../lib/playerStore', () => ({
  usePlayer: (selector: (s: { playTrackList: typeof mockPlayTrackList; toggleShuffle: typeof mockToggleShuffle; shuffle: boolean; current: null }) => unknown) =>
    selector({ playTrackList: mockPlayTrackList, toggleShuffle: mockToggleShuffle, shuffle: false, current: null }),
}))

// ── coverageApi mock ───────────────────────────────────────────────────────────
vi.mock('../lib/coverageApi', () => ({
  usePlaylistDetail: vi.fn(),
}))

// ── react-router params ───────────────────────────────────────────────────────
vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>()
  return {
    ...actual,
    useParams: () => ({ id: 'p1' }),
  }
})

// ── Fixtures ──────────────────────────────────────────────────────────────────
const track1 = makeTrack({ id: 't1', title: 'Song One', artist: 'Artist A', album: 'Album A', durationMs: 200000 })
const track2 = makeTrack({ id: 't2', title: 'Song Two', artist: 'Artist B', album: 'Album B', durationMs: 180000 })
const track3 = makeTrack({ id: 't3', title: 'Song Three', artist: 'Artist C', album: 'Album C', durationMs: 220000 })

const chillPlaylist: PlaylistType = {
  id: 'p1',
  name: 'Chill',
  coverArtId: 'c1',
  songCount: 3,
  durationMs: 600000,
  tracks: [track1, track2, track3],
}

// ── Helpers ───────────────────────────────────────────────────────────────────
function wrapper(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/playlist/p1']}>
        <Routes>
          <Route path="/playlist/:id" element={ui} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

async function renderLoaded() {
  const { usePlaylistDetail } = await import('../lib/coverageApi')
  vi.mocked(usePlaylistDetail).mockReturnValue({
    data: chillPlaylist,
    isLoading: false,
    isError: false,
  } as ReturnType<typeof usePlaylistDetail>)
  wrapper(<Playlist />)
  await waitFor(() => expect(screen.getByRole('heading', { name: 'Chill' })).toBeInTheDocument())
}

// ── Tests ─────────────────────────────────────────────────────────────────────
describe('Playlist page', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders loading skeleton while fetching', async () => {
    const { usePlaylistDetail } = await import('../lib/coverageApi')
    vi.mocked(usePlaylistDetail).mockReturnValue({
      data: undefined,
      isLoading: true,
      isError: false,
    } as ReturnType<typeof usePlaylistDetail>)
    wrapper(<Playlist />)
    expect(screen.getByTestId('playlist-skeleton')).toBeInTheDocument()
  })

  it('renders the playlist name in the header', async () => {
    await renderLoaded()
    expect(screen.getByRole('heading', { name: 'Chill' })).toBeInTheDocument()
  })

  it('renders "3 songs" in the meta line', async () => {
    await renderLoaded()
    expect(screen.getByText(/3 songs/)).toBeInTheDocument()
  })

  it('renders "Playlist" eyebrow label', async () => {
    await renderLoaded()
    expect(screen.getByText('Playlist')).toBeInTheDocument()
  })

  it('renders all 3 track rows', async () => {
    await renderLoaded()
    expect(screen.getByText('Song One')).toBeInTheDocument()
    expect(screen.getByText('Song Two')).toBeInTheDocument()
    expect(screen.getByText('Song Three')).toBeInTheDocument()
  })

  it('clicking Play calls playTrackList with all tracks at index 0', async () => {
    await renderLoaded()
    fireEvent.click(screen.getByRole('button', { name: /play chill/i }))
    expect(mockPlayTrackList).toHaveBeenCalledWith(
      [track1, track2, track3],
      0,
    )
  })

  it('Play button is disabled when playlist has no tracks', async () => {
    const { usePlaylistDetail } = await import('../lib/coverageApi')
    vi.mocked(usePlaylistDetail).mockReturnValue({
      data: { ...chillPlaylist, tracks: [], songCount: 0 },
      isLoading: false,
      isError: false,
    } as unknown as ReturnType<typeof usePlaylistDetail>)
    wrapper(<Playlist />)
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Chill' })).toBeInTheDocument())
    expect(screen.getByRole('button', { name: /play chill/i })).toBeDisabled()
  })

  it('clicking a track row calls playTrackList at that track index', async () => {
    await renderLoaded()
    fireEvent.click(screen.getByText('Song Two'))
    expect(mockPlayTrackList).toHaveBeenCalledWith([track1, track2, track3], 1)
  })

  it('shows EmptyState when playlist not found', async () => {
    const { usePlaylistDetail } = await import('../lib/coverageApi')
    vi.mocked(usePlaylistDetail).mockReturnValue({
      data: undefined,
      isLoading: false,
      isError: true,
    } as ReturnType<typeof usePlaylistDetail>)
    wrapper(<Playlist />)
    await waitFor(() => expect(screen.getByText(/playlist not found/i)).toBeInTheDocument())
  })

  it('shows formatted duration in meta line', async () => {
    await renderLoaded()
    // 600000ms = 10:00
    expect(screen.getByText(/10:00/)).toBeInTheDocument()
  })
})
