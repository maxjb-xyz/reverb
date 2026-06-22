import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import Playlist from './Playlist'
import { makeTrack } from '../test/factories'
import type { Playlist as PlaylistType } from '../lib/types'
import { useAlbumPalette } from '../lib/useAlbumPalette'

// ── useAlbumPalette mock ───────────────────────────────────────────────────────
vi.mock('../lib/useAlbumPalette', () => ({ useAlbumPalette: vi.fn(() => null) }))

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

// ── libraryApi mock ────────────────────────────────────────────────────────────
const mockRenamePlaylist = vi.fn().mockResolvedValue({ ok: true })
const mockDeletePlaylist = vi.fn().mockResolvedValue({ ok: true })
const mockRemovePlaylistTrack = vi.fn().mockResolvedValue({ ok: true })

vi.mock('../lib/libraryApi', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/libraryApi')>()
  return {
    ...actual,
    renamePlaylist: (...args: Parameters<typeof mockRenamePlaylist>) => mockRenamePlaylist(...args),
    deletePlaylist: (...args: Parameters<typeof mockDeletePlaylist>) => mockDeletePlaylist(...args),
    removePlaylistTrack: (...args: Parameters<typeof mockRemovePlaylistTrack>) => mockRemovePlaylistTrack(...args),
  }
})

// ── react-router mocks ────────────────────────────────────────────────────────
const mockNavigate = vi.fn()

vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>()
  return {
    ...actual,
    useParams: () => ({ id: 'p1' }),
    useNavigate: () => mockNavigate,
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
          <Route path="/library" element={<div>Library</div>} />
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

  it('double-clicking a track row calls playTrackList at that track index', async () => {
    await renderLoaded()
    fireEvent.doubleClick(screen.getByText('Song Two'))
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

  // ── Playlist management ────────────────────────────────────────────────────

  it('opens the "…" menu with Rename and Delete playlist options', async () => {
    await renderLoaded()
    fireEvent.click(screen.getByRole('button', { name: /more options/i }))
    expect(screen.getByRole('menuitem', { name: 'Rename' })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: 'Delete playlist' })).toBeInTheDocument()
  })

  it('Rename turns the title into an input pre-filled with the name', async () => {
    await renderLoaded()
    fireEvent.click(screen.getByRole('button', { name: /more options/i }))
    fireEvent.click(screen.getByRole('menuitem', { name: 'Rename' }))
    const input = screen.getByRole('textbox', { name: /rename playlist/i })
    expect(input).toBeInTheDocument()
    expect((input as HTMLInputElement).value).toBe('Chill')
  })

  it('committing rename (Enter) calls renamePlaylist with new name', async () => {
    await renderLoaded()
    fireEvent.click(screen.getByRole('button', { name: /more options/i }))
    fireEvent.click(screen.getByRole('menuitem', { name: 'Rename' }))
    const input = screen.getByRole('textbox', { name: /rename playlist/i })
    fireEvent.change(input, { target: { value: 'Late Night' } })
    await act(async () => {
      fireEvent.keyDown(input, { key: 'Enter' })
    })
    expect(mockRenamePlaylist).toHaveBeenCalledWith('p1', 'Late Night')
  })

  it('committing rename (blur) calls renamePlaylist with new name', async () => {
    await renderLoaded()
    fireEvent.click(screen.getByRole('button', { name: /more options/i }))
    fireEvent.click(screen.getByRole('menuitem', { name: 'Rename' }))
    const input = screen.getByRole('textbox', { name: /rename playlist/i })
    fireEvent.change(input, { target: { value: 'Morning Coffee' } })
    await act(async () => {
      fireEvent.blur(input)
    })
    expect(mockRenamePlaylist).toHaveBeenCalledWith('p1', 'Morning Coffee')
  })

  it('Escape cancels rename without calling renamePlaylist', async () => {
    await renderLoaded()
    fireEvent.click(screen.getByRole('button', { name: /more options/i }))
    fireEvent.click(screen.getByRole('menuitem', { name: 'Rename' }))
    const input = screen.getByRole('textbox', { name: /rename playlist/i })
    fireEvent.change(input, { target: { value: 'Changed' } })
    fireEvent.keyDown(input, { key: 'Escape' })
    expect(mockRenamePlaylist).not.toHaveBeenCalled()
    // heading should be restored
    expect(screen.getByRole('heading', { name: 'Chill' })).toBeInTheDocument()
  })

  it('Delete (confirm=true) calls deletePlaylist and navigates to /library', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    await renderLoaded()
    fireEvent.click(screen.getByRole('button', { name: /more options/i }))
    await act(async () => {
      fireEvent.click(screen.getByRole('menuitem', { name: 'Delete playlist' }))
    })
    expect(mockDeletePlaylist).toHaveBeenCalledWith('p1')
    await waitFor(() => expect(mockNavigate).toHaveBeenCalledWith('/library'))
  })

  it('Delete (confirm=false) does NOT call deletePlaylist', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(false)
    await renderLoaded()
    fireEvent.click(screen.getByRole('button', { name: /more options/i }))
    fireEvent.click(screen.getByRole('menuitem', { name: 'Delete playlist' }))
    expect(mockDeletePlaylist).not.toHaveBeenCalled()
    expect(mockNavigate).not.toHaveBeenCalled()
  })

  it('clicking a track remove button calls removePlaylistTrack with the right index', async () => {
    await renderLoaded()
    // track2 is at index 1
    const removeBtn = screen.getByRole('button', { name: /remove song two from playlist/i })
    await act(async () => {
      fireEvent.click(removeBtn)
    })
    expect(mockRemovePlaylistTrack).toHaveBeenCalledWith('p1', 1)
  })

  it('clicking the remove button does NOT trigger row play', async () => {
    await renderLoaded()
    const removeBtn = screen.getByRole('button', { name: /remove song one from playlist/i })
    await act(async () => {
      fireEvent.click(removeBtn)
    })
    expect(mockPlayTrackList).not.toHaveBeenCalled()
  })

  it('header wrapper has dynamic style.background when palette is present', async () => {
    vi.mocked(useAlbumPalette).mockReturnValue({ rgb: [120, 80, 200], text: '#FFFFFF', scrim: false })
    await renderLoaded()
    const heading = screen.getByRole('heading', { name: 'Chill' })
    const gradientWrapper = heading.closest('[class*="from-raised"]')
    expect(gradientWrapper).toBeTruthy()
    const bg = (gradientWrapper as HTMLElement).style.background
    expect(bg).toContain('linear-gradient')
    // Browser normalizes rgb(120 80 200 / 0.55) → rgba(120, 80, 200, 0.55)
    expect(bg).toMatch(/120/)
    expect(bg).toMatch(/80/)
    expect(bg).toMatch(/200/)
  })

  it('header wrapper has no inline style when palette is null', async () => {
    vi.mocked(useAlbumPalette).mockReturnValue(null)
    await renderLoaded()
    const heading = screen.getByRole('heading', { name: 'Chill' })
    const gradientWrapper = heading.closest('[class*="from-raised"]')
    expect(gradientWrapper).toBeTruthy()
    expect((gradientWrapper as HTMLElement).style.background).toBe('')
  })
})
