import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AddToPlaylistMenu } from './AddToPlaylistMenu'
import type { Track } from '../lib/types'

vi.mock('../lib/syncedPlaylistApi', () => ({
  useSyncedPlaylists: vi.fn(),
  addSyncedTrack: vi.fn(),
}))

vi.mock('../lib/libraryApi', () => ({
  createPlaylist: vi.fn(),
}))

const mockNavigate = vi.fn()
vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>()
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  }
})

import { useSyncedPlaylists, addSyncedTrack } from '../lib/syncedPlaylistApi'
import { createPlaylist } from '../lib/libraryApi'

const SYNCED_PLAYLISTS = [
  { id: 'p1', name: 'Chill Mix', coverUrl: '', source: 'library', externalId: 'p1', syncEnabled: false, syncIntervalSec: 0, autoDownload: false, lastSyncedAt: 0, trackCount: 10 },
  { id: 'p2', name: 'Road Trip', coverUrl: '', source: 'library', externalId: 'p2', syncEnabled: false, syncIntervalSec: 0, autoDownload: false, lastSyncedAt: 0, trackCount: 5 },
]

const TEST_TRACK: Track = {
  id: 't1',
  title: 'Karma Police',
  album: 'OK Computer',
  albumId: 'alb-1',
  artist: 'Radiohead',
  artistId: 'art-1',
  coverArtId: 'cov-1',
  trackNumber: 4,
  discNumber: 1,
  durationMs: 238000,
  bitRate: 320,
  suffix: 'mp3',
  contentType: 'audio/mpeg',
  isrc: 'GBAYE9400347',
}

function renderMenu(onClose = vi.fn()) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const utils = render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <AddToPlaylistMenu track={TEST_TRACK} onClose={onClose} />
      </MemoryRouter>
    </QueryClientProvider>,
  )
  return { ...utils, onClose }
}

describe('AddToPlaylistMenu', () => {
  beforeEach(() => {
    mockNavigate.mockClear()
    vi.mocked(useSyncedPlaylists).mockReturnValue({
      data: SYNCED_PLAYLISTS,
      isLoading: false,
    } as unknown as ReturnType<typeof useSyncedPlaylists>)
    vi.mocked(addSyncedTrack).mockResolvedValue({
      id: 'p1', name: 'Chill Mix', coverUrl: '', source: 'library', externalId: 'p1',
      syncEnabled: false, syncIntervalSec: 0, autoDownload: false, lastSyncedAt: 0,
      trackCount: 11, ownedCount: 11, totalCount: 11, tracks: [],
    })
    vi.mocked(createPlaylist).mockResolvedValue({
      id: 'p-new',
      name: 'New One',
      coverUrl: '',
      source: 'library',
      externalId: 'p-new',
      syncEnabled: false,
      syncIntervalSec: 0,
      autoDownload: false,
      lastSyncedAt: 0,
      trackCount: 0,
      ownedCount: 0,
      totalCount: 0,
      tracks: [],
    })
  })

  it('renders existing managed playlists', () => {
    renderMenu()
    expect(screen.getByText('Chill Mix')).toBeInTheDocument()
    expect(screen.getByText('Road Trip')).toBeInTheDocument()
  })

  it('clicking a playlist calls addSyncedTrack with the track entry', async () => {
    const { onClose } = renderMenu()
    fireEvent.click(screen.getByRole('button', { name: /add to chill mix/i }))
    await waitFor(() => {
      expect(addSyncedTrack).toHaveBeenCalledWith('p1', {
        source: 'library',
        externalId: TEST_TRACK.id,
        title: TEST_TRACK.title,
        artist: TEST_TRACK.artist,
        album: TEST_TRACK.album,
        isrc: TEST_TRACK.isrc,
        durationMs: TEST_TRACK.durationMs,
        coverArtId: TEST_TRACK.coverArtId,
      })
    })
    await waitFor(() => expect(onClose).toHaveBeenCalled())
  })

  it('typing a new name and pressing Enter creates the playlist then calls addSyncedTrack', async () => {
    renderMenu()
    const input = screen.getByLabelText(/new playlist name/i)
    fireEvent.change(input, { target: { value: 'New One' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    await waitFor(() => {
      expect(createPlaylist).toHaveBeenCalledWith('New One')
    })
    await waitFor(() => {
      expect(addSyncedTrack).toHaveBeenCalledWith('p-new', {
        source: 'library',
        externalId: TEST_TRACK.id,
        title: TEST_TRACK.title,
        artist: TEST_TRACK.artist,
        album: TEST_TRACK.album,
        isrc: TEST_TRACK.isrc,
        durationMs: TEST_TRACK.durationMs,
        coverArtId: TEST_TRACK.coverArtId,
      })
    })
  })

  it('create-and-add navigates to /playlist/:id', async () => {
    renderMenu()
    const input = screen.getByLabelText(/new playlist name/i)
    fireEvent.change(input, { target: { value: 'New One' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    await waitFor(() => {
      expect(createPlaylist).toHaveBeenCalledWith('New One')
    })
    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith('/playlist/p-new')
    })
  })

  it('Escape calls onClose', () => {
    const { onClose } = renderMenu()
    act(() => {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    })
    expect(onClose).toHaveBeenCalled()
  })

  it('shows an honest empty state when there are no playlists', () => {
    vi.mocked(useSyncedPlaylists).mockReturnValue({
      data: [],
      isLoading: false,
    } as unknown as ReturnType<typeof useSyncedPlaylists>)
    renderMenu()
    expect(screen.getByText(/no playlists yet/i)).toBeInTheDocument()
  })

  it('renders the dialog inside document.body (portaled)', () => {
    renderMenu()
    const dialog = screen.getByRole('dialog')
    expect(document.body.contains(dialog)).toBe(true)
  })
})
