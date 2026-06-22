import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { SearchSuggest } from './SearchSuggest'

// Mock useNavigate so we can assert album/artist navigation.
const mockNavigate = vi.fn()
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return { ...actual, useNavigate: () => mockNavigate }
})

// Mock the library search hook so we control the preview results.
const useLibrarySearchMock = vi.fn()
vi.mock('../../lib/libraryApi', async () => {
  const actual = await vi.importActual<typeof import('../../lib/libraryApi')>('../../lib/libraryApi')
  return { ...actual, useLibrarySearch: (q: string) => useLibrarySearchMock(q) }
})

// Spy on the player so a track click plays it.
const playTrackList = vi.fn()
vi.mock('../../lib/playerStore', () => ({
  usePlayer: { getState: () => ({ playTrackList }) },
}))

const data = {
  tracks: [{ id: 't1', title: 'Found Song', artist: 'A', coverArtId: '' }],
  albums: [{ id: 'al1', name: 'Found Album', artist: 'A', coverArtId: '' }],
  artists: [{ id: 'ar1', name: 'Found Artist', coverArtId: '' }],
}

function wrap(ui: React.ReactNode) {
  return <MemoryRouter>{ui}</MemoryRouter>
}

describe('SearchSuggest', () => {
  beforeEach(() => {
    mockNavigate.mockReset()
    playTrackList.mockReset()
    useLibrarySearchMock.mockReset()
    useLibrarySearchMock.mockReturnValue({ data, isFetching: false })
  })

  it('renders track, album and artist preview rows after debounce', async () => {
    render(wrap(<SearchSuggest query="found" onNavigateAll={vi.fn()} onClose={vi.fn()} />))
    await waitFor(() => expect(screen.getByText('Found Song')).toBeInTheDocument())
    expect(screen.getByText('Found Album')).toBeInTheDocument()
    expect(screen.getByText('Found Artist')).toBeInTheDocument()
  })

  it('clicking a track row plays it and closes', async () => {
    const onClose = vi.fn()
    render(wrap(<SearchSuggest query="found" onNavigateAll={vi.fn()} onClose={onClose} />))
    await waitFor(() => expect(screen.getByText('Found Song')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('option', { name: /play found song/i }))
    expect(playTrackList).toHaveBeenCalledWith([data.tracks[0]], 0)
    expect(onClose).toHaveBeenCalled()
  })

  it('clicking an album row navigates to the album and closes', async () => {
    const onClose = vi.fn()
    render(wrap(<SearchSuggest query="found" onNavigateAll={vi.fn()} onClose={onClose} />))
    await waitFor(() => expect(screen.getByText('Found Album')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('option', { name: /open album found album/i }))
    expect(mockNavigate).toHaveBeenCalledWith('/album/library/al1')
    expect(onClose).toHaveBeenCalled()
  })

  it('clicking an artist row navigates to the artist and closes', async () => {
    const onClose = vi.fn()
    render(wrap(<SearchSuggest query="found" onNavigateAll={vi.fn()} onClose={onClose} />))
    await waitFor(() => expect(screen.getByText('Found Artist')).toBeInTheDocument())
    fireEvent.click(screen.getByRole('option', { name: /open artist found artist/i }))
    expect(mockNavigate).toHaveBeenCalledWith('/artist/library/ar1')
    expect(onClose).toHaveBeenCalled()
  })

  it('"See all results" calls onNavigateAll', async () => {
    const onNavigateAll = vi.fn()
    render(wrap(<SearchSuggest query="found" onNavigateAll={onNavigateAll} onClose={vi.fn()} />))
    fireEvent.click(screen.getByRole('button', { name: /see all results for/i }))
    expect(onNavigateAll).toHaveBeenCalled()
  })

  it('Escape calls onClose', async () => {
    const onClose = vi.fn()
    render(wrap(<SearchSuggest query="found" onNavigateAll={vi.fn()} onClose={onClose} />))
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).toHaveBeenCalled()
  })

  it('shows the empty-library hint when there are no matches', async () => {
    useLibrarySearchMock.mockReturnValue({ data: { tracks: [], albums: [], artists: [] }, isFetching: false })
    render(wrap(<SearchSuggest query="zzz" onNavigateAll={vi.fn()} onClose={vi.fn()} />))
    await waitFor(() =>
      expect(screen.getByText(/no matches in your library/i)).toBeInTheDocument(),
    )
  })
})
