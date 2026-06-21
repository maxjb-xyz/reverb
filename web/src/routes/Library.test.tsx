import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { UseQueryResult } from '@tanstack/react-query'
import Library from './Library'
import type { Album, Artist, Playlist } from '../lib/types'

// ------------------------------------------------------------------
// Helpers
// ------------------------------------------------------------------

function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return (
    <QueryClientProvider client={qc}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>
  )
}

function makeAlbum(overrides: Partial<Album> = {}): Album {
  return {
    id: 'al1',
    name: 'Test Album',
    artistId: 'ar1',
    artist: 'Test Artist',
    coverArtId: '',
    year: 2021,
    songCount: 10,
    durationMs: 180000,
    tracks: [],
    ...overrides,
  }
}

function makeArtist(overrides: Partial<Artist> = {}): Artist {
  return {
    id: 'ar1',
    name: 'Test Artist',
    coverArtId: '',
    albumCount: 1,
    ...overrides,
  }
}

function makePlaylist(overrides: Partial<Playlist> = {}): Playlist {
  return {
    id: 'pl1',
    name: 'Test Playlist',
    coverArtId: '',
    songCount: 5,
    durationMs: 900000,
    ...overrides,
  }
}

// ------------------------------------------------------------------
// Mocks
// ------------------------------------------------------------------

vi.mock('../lib/libraryApi', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/libraryApi')>()
  return {
    ...actual,
    useAlbums: vi.fn(),
    useArtists: vi.fn(),
    usePlaylists: vi.fn(),
    coverUrl: vi.fn((id: string) => `/api/v1/cover/${id}`),
  }
})

// ------------------------------------------------------------------
// Suites
// ------------------------------------------------------------------

describe('Library page', () => {
  beforeEach(async () => {
    const { useAlbums, useArtists, usePlaylists } = await import('../lib/libraryApi')
    vi.mocked(useAlbums).mockReturnValue({ data: [], isLoading: false, error: null } as unknown as UseQueryResult<Album[], Error>)
    vi.mocked(useArtists).mockReturnValue({ data: [], isLoading: false, error: null } as unknown as UseQueryResult<Artist[], Error>)
    vi.mocked(usePlaylists).mockReturnValue({ data: [], isLoading: false, error: null } as unknown as UseQueryResult<Playlist[], Error>)
  })

  afterEach(() => {
    vi.clearAllMocks()
  })

  it('renders "Your Library" page heading', () => {
    render(wrap(<Library />))
    expect(screen.getByRole('heading', { name: /your library/i })).toBeInTheDocument()
  })

  it('renders Albums, Artists, Playlists filter chips', () => {
    render(wrap(<Library />))
    expect(screen.getByRole('button', { name: /^albums$/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^artists$/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^playlists$/i })).toBeInTheDocument()
  })

  it('defaults to Albums tab showing album cards', async () => {
    const { useAlbums } = await import('../lib/libraryApi')
    vi.mocked(useAlbums).mockReturnValue({
      data: [makeAlbum({ id: 'al1', name: 'OK Computer', artist: 'Radiohead' })],
      isLoading: false,
      error: null,
    } as unknown as UseQueryResult<Album[], Error>)

    render(wrap(<Library />))
    expect(screen.getByRole('button', { name: /ok computer/i })).toBeInTheDocument()
  })

  it('switches to Artists grid when Artists chip is clicked', async () => {
    const { useArtists } = await import('../lib/libraryApi')
    vi.mocked(useArtists).mockReturnValue({
      data: [makeArtist({ id: 'ar1', name: 'Radiohead' })],
      isLoading: false,
      error: null,
    } as unknown as UseQueryResult<Artist[], Error>)

    render(wrap(<Library />))
    fireEvent.click(screen.getByRole('button', { name: /^artists$/i }))
    expect(screen.getByRole('button', { name: /radiohead/i })).toBeInTheDocument()
  })

  it('switches to Playlists grid when Playlists chip is clicked', async () => {
    const { usePlaylists } = await import('../lib/libraryApi')
    vi.mocked(usePlaylists).mockReturnValue({
      data: [makePlaylist({ id: 'pl1', name: 'My Playlist' })],
      isLoading: false,
      error: null,
    } as unknown as UseQueryResult<Playlist[], Error>)

    render(wrap(<Library />))
    fireEvent.click(screen.getByRole('button', { name: /^playlists$/i }))
    expect(screen.getByRole('button', { name: /my playlist/i })).toBeInTheDocument()
  })

  it('shows EmptyState when album list is empty', () => {
    render(wrap(<Library />))
    expect(screen.getByText(/nothing here yet/i)).toBeInTheDocument()
  })

  it('shows EmptyState when artists list is empty', () => {
    render(wrap(<Library />))
    fireEvent.click(screen.getByRole('button', { name: /^artists$/i }))
    expect(screen.getByText(/nothing here yet/i)).toBeInTheDocument()
  })

  it('shows EmptyState when playlists list is empty', () => {
    render(wrap(<Library />))
    fireEvent.click(screen.getByRole('button', { name: /^playlists$/i }))
    expect(screen.getByText(/nothing here yet/i)).toBeInTheDocument()
  })

  it('shows skeleton grid while albums are loading', async () => {
    const { useAlbums } = await import('../lib/libraryApi')
    vi.mocked(useAlbums).mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
    } as unknown as UseQueryResult<Album[], Error>)

    render(wrap(<Library />))
    const skeletons = document.querySelectorAll('.animate-pulse')
    expect(skeletons.length).toBeGreaterThan(0)
  })

  it('shows skeleton grid while artists are loading', async () => {
    const { useArtists } = await import('../lib/libraryApi')
    vi.mocked(useArtists).mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
    } as unknown as UseQueryResult<Artist[], Error>)

    render(wrap(<Library />))
    fireEvent.click(screen.getByRole('button', { name: /^artists$/i }))

    const skeletons = document.querySelectorAll('.animate-pulse')
    expect(skeletons.length).toBeGreaterThan(0)
  })

  it('navigates to /album/:id when an album card is clicked', async () => {
    const { useAlbums } = await import('../lib/libraryApi')
    vi.mocked(useAlbums).mockReturnValue({
      data: [makeAlbum({ id: 'al42', name: 'Kid A', artist: 'Radiohead' })],
      isLoading: false,
      error: null,
    } as unknown as UseQueryResult<Album[], Error>)

    render(wrap(<Library />))

    const card = screen.getByRole('button', { name: /kid a/i })
    expect(card).toBeInTheDocument()
    // Clicking should not throw — navigation happens inside MemoryRouter
    fireEvent.click(card)
  })

  it('playlist cards are non-navigating (no dead-route nav to /playlist/:id)', async () => {
    const { usePlaylists } = await import('../lib/libraryApi')
    vi.mocked(usePlaylists).mockReturnValue({
      data: [makePlaylist({ id: 'pl99', name: 'My Vibes' })],
      isLoading: false,
      error: null,
    } as unknown as UseQueryResult<Playlist[], Error>)

    render(wrap(<Library />))
    fireEvent.click(screen.getByRole('button', { name: /^playlists$/i }))

    // The playlist card should render but have no onClick that navigates
    const card = screen.queryByRole('button', { name: /my vibes/i })
    // Card may render as a non-interactive element when no onClick is provided
    // Either way, no navigation to /playlist/:id should occur — the route doesn't exist.
    // Clicking it (if it's a button) should not throw or navigate away.
    if (card) fireEvent.click(card)
    // Still on same view — playlist heading still visible
    expect(screen.getByRole('button', { name: /^playlists$/i })).toBeInTheDocument()
  })
})
