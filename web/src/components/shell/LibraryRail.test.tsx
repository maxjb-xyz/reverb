import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { LibraryRail } from './LibraryRail'
import { usePlayer } from '../../lib/playerStore'
import type { Track } from '../../lib/types'

// Mock library API hooks
vi.mock('../../lib/libraryApi', () => ({
  useArtists: vi.fn(),
  useAlbums: vi.fn(),
  coverUrl: vi.fn((id: string) => `/covers/${id}`),
  createPlaylist: vi.fn(),
}))

// Mock synced playlist API — also mock importPlaylist used by ImportPlaylistDialog
vi.mock('../../lib/syncedPlaylistApi', () => ({
  useSyncedPlaylists: vi.fn(),
  importPlaylist: vi.fn(),
}))

import { useArtists, useAlbums, createPlaylist } from '../../lib/libraryApi'
import { useSyncedPlaylists } from '../../lib/syncedPlaylistApi'

const SYNCED_PLAYLISTS = [
  { id: 'sp1', name: 'Chill Mix', coverUrl: 'http://img/sp1.jpg', source: 'spotify', externalId: 'ext1', syncEnabled: true, syncIntervalSec: 3600, autoDownload: false, lastSyncedAt: 0, trackCount: 10 },
  { id: 'sp2', name: 'Road Trip', coverUrl: 'http://img/sp2.jpg', source: 'library', externalId: 'ext2', syncEnabled: false, syncIntervalSec: 0, autoDownload: false, lastSyncedAt: 0, trackCount: 5 },
]
const ALBUMS = [
  { id: 'al1', name: 'Dark Side', artistId: 'ar1', artist: 'Pink Floyd', coverArtId: 'c3', year: 1973, songCount: 10, durationMs: 2400000 },
]
const ARTISTS = [
  { id: 'ar1', name: 'Pink Floyd', coverArtId: 'c4', albumCount: 3 },
]

function track(id: string): Track {
  return {
    id, title: 'Song', albumId: 'al1', album: 'Dark Side',
    artistId: 'ar1', artist: 'Pink Floyd', coverArtId: 'c3',
    trackNumber: 1, discNumber: 1, durationMs: 200000, bitRate: 320,
    suffix: 'mp3', contentType: 'audio/mpeg',
  }
}

function renderRail(initialPath = '/') {
  const qc = new QueryClient()
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[initialPath]}>
        <Routes>
          <Route path="*" element={<LibraryRail />} />
          <Route path="/library" element={<div data-testid="library-page" />} />
          <Route path="/album/:source/:id" element={<div data-testid="album-page" />} />
          <Route path="/artist/:source/:id" element={<div data-testid="artist-page" />} />
          <Route path="/playlist/:id" element={<div data-testid="synced-playlist-page" />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('LibraryRail', () => {
  beforeEach(() => {
    vi.mocked(useArtists).mockReturnValue({ data: ARTISTS, isLoading: false } as unknown as ReturnType<typeof useArtists>)
    vi.mocked(useAlbums).mockReturnValue({ data: ALBUMS, isLoading: false } as unknown as ReturnType<typeof useAlbums>)
    vi.mocked(useSyncedPlaylists).mockReturnValue({ data: SYNCED_PLAYLISTS, isLoading: false } as unknown as ReturnType<typeof useSyncedPlaylists>)
    vi.mocked(createPlaylist).mockResolvedValue({
      id: 'new-pl',
      name: 'New Playlist',
      coverUrl: '',
      source: 'library',
      externalId: 'new-pl',
      syncEnabled: false,
      syncIntervalSec: 0,
      autoDownload: false,
      lastSyncedAt: 0,
      trackCount: 0,
      ownedCount: 0,
      totalCount: 0,
      tracks: [],
    })
    // Reset player
    usePlayer.setState({ current: null, queue: [], index: -1, playing: false, currentTimeMs: 0, durationMs: 0, bufferedMs: 0, volume: 1, shuffle: false, repeat: 'off' })
  })

  it('renders the Your Library heading', () => {
    renderRail()
    expect(screen.getByText('Your Library')).toBeInTheDocument()
  })

  it('shows Playlists chip selected by default and renders managed playlist names', () => {
    renderRail()
    expect(screen.getByText('Chill Mix')).toBeInTheDocument()
    expect(screen.getByText('Road Trip')).toBeInTheDocument()
  })

  it('switching to Albums chip shows albums', () => {
    renderRail()
    fireEvent.click(screen.getByRole('button', { name: /albums/i }))
    expect(screen.getByText('Dark Side')).toBeInTheDocument()
    // playlists no longer visible
    expect(screen.queryByText('Chill Mix')).not.toBeInTheDocument()
  })

  it('switching to Artists chip shows artists', () => {
    renderRail()
    fireEvent.click(screen.getByRole('button', { name: /artists/i }))
    expect(screen.getByText('Pink Floyd')).toBeInTheDocument()
    expect(screen.queryByText('Chill Mix')).not.toBeInTheDocument()
  })

  it('shows skeleton rows while loading', () => {
    vi.mocked(useSyncedPlaylists).mockReturnValue({ data: undefined, isLoading: true } as unknown as ReturnType<typeof useSyncedPlaylists>)
    renderRail()
    expect(screen.getAllByTestId('lib-skeleton').length).toBeGreaterThan(0)
  })

  it('shows EmptyState when synced playlists list is empty', () => {
    vi.mocked(useSyncedPlaylists).mockReturnValue({ data: [], isLoading: false } as unknown as ReturnType<typeof useSyncedPlaylists>)
    renderRail()
    expect(screen.getByText(/no playlists/i)).toBeInTheDocument()
  })

  it('highlights the currently-playing album row with text-accent and Equalizer', () => {
    // Playing a track whose albumId matches al1
    usePlayer.setState({ current: track('t1'), queue: [track('t1')], index: 0, playing: true, currentTimeMs: 0, durationMs: 200000, bufferedMs: 0, volume: 1, shuffle: false, repeat: 'off' })
    vi.mocked(useAlbums).mockReturnValue({ data: ALBUMS, isLoading: false } as unknown as ReturnType<typeof useAlbums>)
    renderRail()
    // Switch to albums
    fireEvent.click(screen.getByRole('button', { name: /albums/i }))
    // The equalizer should be visible (eq-bar testids)
    expect(screen.getAllByTestId('eq-bar').length).toBeGreaterThan(0)
  })

  it('clicking the "Your Library" header navigates to /library', () => {
    renderRail()
    fireEvent.click(screen.getByRole('button', { name: /open your library/i }))
    expect(screen.getByTestId('library-page')).toBeInTheDocument()
  })

  it('clicking an album row navigates to /album/:id', () => {
    renderRail()
    fireEvent.click(screen.getByRole('button', { name: /albums/i }))
    const albumBtn = screen.getByRole('button', { name: 'Dark Side' })
    expect(albumBtn).toBeInTheDocument()
    fireEvent.click(albumBtn)
    expect(screen.getByTestId('album-page')).toBeInTheDocument()
  })

  it('clicking an artist row navigates to /artist/:id', () => {
    renderRail()
    fireEvent.click(screen.getByRole('button', { name: /artists/i }))
    const artistBtn = screen.getByRole('button', { name: 'Pink Floyd' })
    expect(artistBtn).toBeInTheDocument()
    fireEvent.click(artistBtn)
    expect(screen.getByTestId('artist-page')).toBeInTheDocument()
  })

  it('clicking a managed playlist row navigates to /playlist/:id', () => {
    renderRail()
    // Playlists are shown by default
    const playlistBtn = screen.getByRole('button', { name: 'Chill Mix' })
    expect(playlistBtn).toBeInTheDocument()
    fireEvent.click(playlistBtn)
    expect(screen.getByTestId('synced-playlist-page')).toBeInTheDocument()
  })

  it('renders an "Import from Spotify" icon button in the rail header', () => {
    renderRail()
    expect(screen.getByRole('button', { name: /import from spotify/i })).toBeInTheDocument()
  })

  it('clicking the "Import from Spotify" rail button opens the import dialog', () => {
    renderRail()
    // Dialog should not be open initially
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /import from spotify/i }))

    // Dialog should now be open
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: /import from spotify/i })).toBeInTheDocument()
    // URL input inside the dialog is visible
    expect(screen.getByLabelText(/playlist url/i)).toBeInTheDocument()
  })

  it('creating a playlist navigates to /playlist/:id', async () => {
    renderRail()
    fireEvent.click(screen.getByRole('button', { name: /create playlist/i }))
    await waitFor(() => {
      expect(screen.getByTestId('synced-playlist-page')).toBeInTheDocument()
    })
  })
})
