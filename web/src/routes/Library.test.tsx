import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { UseQueryResult } from '@tanstack/react-query'
import Library from './Library'
import type { Album, Artist } from '../lib/types'

// ------------------------------------------------------------------
// Helpers
// ------------------------------------------------------------------

function wrap(ui: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return (
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        {ui}
      </MemoryRouter>
    </QueryClientProvider>
  )
}

function wrapWithRoutes(ui: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return (
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <Routes>
          <Route path="/" element={<>{ui}</>} />
          <Route path="/album/:source/:id" element={<div data-testid="album-page" />} />
          <Route path="/artist/:source/:id" element={<div data-testid="artist-page" />} />
          <Route path="/synced-playlist/:id" element={<div data-testid="synced-playlist-page" />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  )
}

function makeSyncedPlaylist(overrides: Partial<SyncedPlaylist> = {}): SyncedPlaylist {
  return {
    id: 'sp1',
    name: 'Synced One',
    coverUrl: 'http://img/sp1.jpg',
    source: 'spotify',
    externalId: 'ext1',
    syncEnabled: true,
    syncIntervalSec: 3600,
    autoDownload: false,
    lastSyncedAt: 0,
    trackCount: 12,
    ...overrides,
  }
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

// ------------------------------------------------------------------
// Mocks
// ------------------------------------------------------------------

vi.mock('../lib/libraryApi', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/libraryApi')>()
  return {
    ...actual,
    useAlbums: vi.fn(),
    useArtists: vi.fn(),
    coverUrl: vi.fn((id: string) => `/api/v1/cover/${id}`),
  }
})

vi.mock('../lib/syncedPlaylistApi', () => ({
  useSyncedPlaylists: vi.fn(),
}))

import { useSyncedPlaylists } from '../lib/syncedPlaylistApi'
import type { SyncedPlaylist } from '../lib/types'

// ------------------------------------------------------------------
// Suites
// ------------------------------------------------------------------

describe('Library page', () => {
  beforeEach(async () => {
    const { useAlbums, useArtists } = await import('../lib/libraryApi')
    vi.mocked(useAlbums).mockReturnValue({ data: [], isLoading: false, error: null } as unknown as UseQueryResult<Album[], Error>)
    vi.mocked(useArtists).mockReturnValue({ data: [], isLoading: false, error: null } as unknown as UseQueryResult<Artist[], Error>)
    vi.mocked(useSyncedPlaylists).mockReturnValue({ data: [], isLoading: false, error: null } as unknown as ReturnType<typeof useSyncedPlaylists>)
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
    vi.mocked(useSyncedPlaylists).mockReturnValue({
      data: [makeSyncedPlaylist({ id: 'sp1', name: 'My Playlist' })],
      isLoading: false,
      error: null,
    } as unknown as ReturnType<typeof useSyncedPlaylists>)

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

  it('shows skeleton grid while playlists are loading', () => {
    vi.mocked(useSyncedPlaylists).mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
    } as unknown as ReturnType<typeof useSyncedPlaylists>)

    render(wrap(<Library />))
    fireEvent.click(screen.getByRole('button', { name: /^playlists$/i }))

    const skeletons = document.querySelectorAll('.animate-pulse')
    expect(skeletons.length).toBeGreaterThan(0)
  })

  it('navigates to /album/library/:id when an album card is clicked', async () => {
    const { useAlbums } = await import('../lib/libraryApi')
    vi.mocked(useAlbums).mockReturnValue({
      data: [makeAlbum({ id: 'al42', name: 'Kid A', artist: 'Radiohead' })],
      isLoading: false,
      error: null,
    } as unknown as UseQueryResult<Album[], Error>)

    render(wrapWithRoutes(<Library />))

    const card = screen.getByRole('button', { name: /kid a/i })
    expect(card).toBeInTheDocument()
    fireEvent.click(card)
    expect(screen.getByTestId('album-page')).toBeInTheDocument()
  })

  it('navigates to /artist/library/:id when an artist card is clicked', async () => {
    const { useArtists } = await import('../lib/libraryApi')
    vi.mocked(useArtists).mockReturnValue({
      data: [makeArtist({ id: 'ar42', name: 'Radiohead' })],
      isLoading: false,
      error: null,
    } as unknown as UseQueryResult<Artist[], Error>)

    render(wrapWithRoutes(<Library />))
    fireEvent.click(screen.getByRole('button', { name: /^artists$/i }))
    fireEvent.click(screen.getByRole('button', { name: /radiohead/i }))
    expect(screen.getByTestId('artist-page')).toBeInTheDocument()
  })

  it('navigates to /synced-playlist/:id when a managed playlist card is clicked', () => {
    vi.mocked(useSyncedPlaylists).mockReturnValue({
      data: [makeSyncedPlaylist({ id: 'sp99', name: 'My Vibes' })],
      isLoading: false,
      error: null,
    } as unknown as ReturnType<typeof useSyncedPlaylists>)

    render(wrapWithRoutes(<Library />))
    fireEvent.click(screen.getByRole('button', { name: /^playlists$/i }))

    const card = screen.getByRole('button', { name: /my vibes/i })
    expect(card).toBeInTheDocument()
    fireEvent.click(card)
    expect(screen.getByTestId('synced-playlist-page')).toBeInTheDocument()
  })

  it('shows "Import from Spotify" button on the default Albums filter (always visible)', () => {
    render(wrap(<Library />))
    // Default filter is albums — button must already be present without switching tabs
    const importBtn = screen.getByRole('button', { name: /import from spotify/i })
    expect(importBtn).toBeInTheDocument()
  })

  it('"Import from Spotify" button opens the dialog regardless of active filter', () => {
    render(wrap(<Library />))
    const importBtn = screen.getByRole('button', { name: /import from spotify/i })

    // Dialog is closed initially
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    // Clicking opens it
    fireEvent.click(importBtn)
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: /import from spotify/i })).toBeInTheDocument()
  })

  it('renders managed playlists in the Playlists grid', () => {
    vi.mocked(useSyncedPlaylists).mockReturnValue({
      data: [makeSyncedPlaylist({ id: 'sp1', name: 'Synced One' })],
      isLoading: false,
      error: null,
    } as unknown as ReturnType<typeof useSyncedPlaylists>)

    render(wrap(<Library />))
    fireEvent.click(screen.getByRole('button', { name: /^playlists$/i }))

    expect(screen.getByRole('button', { name: /synced one/i })).toBeInTheDocument()
  })
})
