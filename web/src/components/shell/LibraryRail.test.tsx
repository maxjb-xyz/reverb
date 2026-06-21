import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { LibraryRail } from './LibraryRail'
import { usePlayer } from '../../lib/playerStore'
import type { Track } from '../../lib/types'

// Mock library API hooks
vi.mock('../../lib/libraryApi', () => ({
  usePlaylists: vi.fn(),
  useArtists: vi.fn(),
  useAlbums: vi.fn(),
  coverUrl: vi.fn((id: string) => `/covers/${id}`),
}))

import { usePlaylists, useArtists, useAlbums } from '../../lib/libraryApi'

const PLAYLISTS = [
  { id: 'p1', name: 'Chill Mix', coverArtId: 'c1', songCount: 10, durationMs: 3600000 },
  { id: 'p2', name: 'Road Trip', coverArtId: 'c2', songCount: 5, durationMs: 1800000 },
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

function renderRail() {
  const qc = new QueryClient()
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <LibraryRail />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('LibraryRail', () => {
  beforeEach(() => {
    vi.mocked(usePlaylists).mockReturnValue({ data: PLAYLISTS, isLoading: false } as unknown as ReturnType<typeof usePlaylists>)
    vi.mocked(useArtists).mockReturnValue({ data: ARTISTS, isLoading: false } as unknown as ReturnType<typeof useArtists>)
    vi.mocked(useAlbums).mockReturnValue({ data: ALBUMS, isLoading: false } as unknown as ReturnType<typeof useAlbums>)
    // Reset player
    usePlayer.setState({ current: null, queue: [], index: -1, playing: false, currentTimeMs: 0, durationMs: 0, bufferedMs: 0, volume: 1, shuffle: false, repeat: 'off' })
  })

  it('renders the Your Library heading', () => {
    renderRail()
    expect(screen.getByText('Your Library')).toBeInTheDocument()
  })

  it('shows Playlists chip selected by default and renders playlist names', () => {
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
    vi.mocked(usePlaylists).mockReturnValue({ data: undefined, isLoading: true } as unknown as ReturnType<typeof usePlaylists>)
    renderRail()
    expect(screen.getAllByTestId('lib-skeleton').length).toBeGreaterThan(0)
  })

  it('shows EmptyState when list is empty', () => {
    vi.mocked(usePlaylists).mockReturnValue({ data: [], isLoading: false } as unknown as ReturnType<typeof usePlaylists>)
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

  it('nav links for Search, Library, Settings are present', () => {
    renderRail()
    expect(screen.getByRole('link', { name: /search/i })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /library/i })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /settings/i })).toBeInTheDocument()
  })
})
