import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { NowPlayingPanel } from './NowPlayingPanel'
import { usePlayer } from '../../lib/playerStore'
import { useUI } from '../../lib/uiStore'
import type { Track, Artist } from '../../lib/types'
import { coverUrl } from '../../lib/libraryApi'

const mockNavigate = vi.fn()
vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>()
  return { ...actual, useNavigate: () => mockNavigate }
})

vi.mock('../../lib/libraryApi', () => ({
  streamUrl: vi.fn((id: string) => `/stream/${id}`),
  coverUrl: vi.fn((id: string) => `/covers/${id}`),
  useArtist: vi.fn(() => ({ data: undefined })),
}))
import { useArtist } from '../../lib/libraryApi'

function track(id: string, extra: Partial<Track> = {}): Track {
  return {
    id,
    title: 'Song ' + id,
    albumId: 'al1',
    album: 'Test Album',
    artistId: 'ar1',
    artist: 'Test Artist',
    coverArtId: 'cover-' + id,
    trackNumber: Number(id),
    discNumber: 1,
    durationMs: 200000,
    bitRate: 320,
    suffix: 'mp3',
    contentType: 'audio/mpeg',
    ...extra,
  }
}

const artist: Artist = {
  id: 'ar1',
  name: 'Test Artist',
  coverArtId: 'artist-cover',
  albumCount: 3,
}

function renderPanel() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <NowPlayingPanel />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('NowPlayingPanel', () => {
  beforeEach(() => {
    mockNavigate.mockClear()
    vi.mocked(useArtist).mockReturnValue({ data: artist } as ReturnType<typeof useArtist>)
    act(() => {
      usePlayer.getState().playTrackList([track('1'), track('2'), track('3')], 0)
      useUI.getState().openPanel('nowplaying')
    })
  })

  it('renders nothing when panel is not open', () => {
    act(() => { useUI.getState().closePanel() })
    renderPanel()
    expect(screen.queryByTestId('now-playing-panel')).not.toBeInTheDocument()
  })

  it('renders the current track title and artist', () => {
    renderPanel()
    expect(screen.getByText('Song 1')).toBeInTheDocument()
    expect(screen.getAllByText('Test Artist').length).toBeGreaterThan(0)
  })

  it('lists up-next tracks from the queue', () => {
    renderPanel()
    expect(screen.getByText('Song 2')).toBeInTheDocument()
    expect(screen.getByText('Song 3')).toBeInTheDocument()
    // Current track (Song 1) should not appear in the up-next list
    // (it may appear in the header meta, but not in the queue buttons)
    const queueButtons = screen.getAllByRole('button', { name: /play song/i })
    expect(queueButtons.length).toBe(2)
  })

  it('clicking a queue item calls jumpTo with the correct index', () => {
    renderPanel()
    const playBtn = screen.getByRole('button', { name: /play song 2/i })
    fireEvent.click(playBtn)
    // Song 2 is at queue index 1
    expect(usePlayer.getState().index).toBe(1)
  })

  it('close button calls closePanel', () => {
    renderPanel()
    fireEvent.click(screen.getByRole('button', { name: /close panel/i }))
    expect(useUI.getState().rightPanel).toBe(null)
  })

  it('shows artist info from useArtist', () => {
    renderPanel()
    // "Test Artist" appears in the meta (artist name) and in the About card
    expect(screen.getAllByText('Test Artist').length).toBeGreaterThan(0)
    expect(screen.getByText(/In your library · 3 albums/i)).toBeInTheDocument()
  })

  it('clicking the artist name navigates to the source-qualified artist route', () => {
    renderPanel()
    const artistBtn = screen.getByRole('button', { name: 'Test Artist' })
    fireEvent.click(artistBtn)
    expect(mockNavigate).toHaveBeenCalledWith('/artist/library/ar1')
  })

  it('clicking the album name navigates to the source-qualified album route', () => {
    renderPanel()
    const albumBtn = screen.getByRole('button', { name: 'Test Album' })
    fireEvent.click(albumBtn)
    expect(mockNavigate).toHaveBeenCalledWith('/album/library/al1')
  })

  it('shows cover-placeholder (no broken img) when artist has no coverArtId', () => {
    const artistWithoutCover: Artist = { id: 'ar1', name: 'Test Artist', albumCount: 3 }
    vi.mocked(useArtist).mockReturnValue({ data: artistWithoutCover } as ReturnType<typeof useArtist>)
    renderPanel()
    expect(screen.getAllByTestId('cover-placeholder').length).toBeGreaterThan(0)
    // No img rendered for the artist card header (no src means no <img> from Cover)
    const imgs = screen.queryAllByRole('img')
    const artistCardImgs = imgs.filter(
      (img) => (img as HTMLImageElement).src?.includes('artist-cover'),
    )
    expect(artistCardImgs).toHaveLength(0)
  })

  it('renders artist cover img with correct src when artist has coverArtId', () => {
    // beforeEach sets artist with coverArtId: 'artist-cover'; coverUrl mock returns /covers/<id>
    renderPanel()
    const imgs = screen.queryAllByRole('img')
    const artistImg = imgs.find((img) =>
      (img as HTMLImageElement).src?.includes('artist-cover'),
    )
    expect(artistImg).toBeDefined()
  })
})
