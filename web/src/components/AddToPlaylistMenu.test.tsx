import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AddToPlaylistMenu } from './AddToPlaylistMenu'

vi.mock('../lib/libraryApi', () => ({
  usePlaylists: vi.fn(),
  createPlaylist: vi.fn(),
  addTracksToPlaylist: vi.fn(),
}))

import { usePlaylists, createPlaylist, addTracksToPlaylist } from '../lib/libraryApi'

const PLAYLISTS = [
  { id: 'p1', name: 'Chill Mix', coverArtId: 'c1', songCount: 10, durationMs: 3600000 },
  { id: 'p2', name: 'Road Trip', coverArtId: 'c2', songCount: 5, durationMs: 1800000 },
]

function renderMenu(onClose = vi.fn()) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const utils = render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <AddToPlaylistMenu trackId="t1" onClose={onClose} />
      </MemoryRouter>
    </QueryClientProvider>,
  )
  return { ...utils, onClose }
}

describe('AddToPlaylistMenu', () => {
  beforeEach(() => {
    vi.mocked(usePlaylists).mockReturnValue({
      data: PLAYLISTS,
      isLoading: false,
    } as unknown as ReturnType<typeof usePlaylists>)
    vi.mocked(addTracksToPlaylist).mockResolvedValue({ ok: true })
    vi.mocked(createPlaylist).mockResolvedValue({
      id: 'p-new',
      name: 'New One',
      coverArtId: '',
      songCount: 0,
      durationMs: 0,
    })
  })

  it('renders the existing playlists', () => {
    renderMenu()
    expect(screen.getByText('Chill Mix')).toBeInTheDocument()
    expect(screen.getByText('Road Trip')).toBeInTheDocument()
  })

  it('clicking a playlist calls addTracksToPlaylist with (id, [trackId])', async () => {
    const { onClose } = renderMenu()
    fireEvent.click(screen.getByRole('button', { name: /add to chill mix/i }))
    await waitFor(() => {
      expect(addTracksToPlaylist).toHaveBeenCalledWith('p1', ['t1'])
    })
    await waitFor(() => expect(onClose).toHaveBeenCalled())
  })

  it('typing a new name and pressing Enter creates the playlist then adds the track', async () => {
    renderMenu()
    const input = screen.getByLabelText(/new playlist name/i)
    fireEvent.change(input, { target: { value: 'New One' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    await waitFor(() => {
      expect(createPlaylist).toHaveBeenCalledWith('New One')
    })
    await waitFor(() => {
      expect(addTracksToPlaylist).toHaveBeenCalledWith('p-new', ['t1'])
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
    vi.mocked(usePlaylists).mockReturnValue({
      data: [],
      isLoading: false,
    } as unknown as ReturnType<typeof usePlaylists>)
    renderMenu()
    expect(screen.getByText(/no playlists yet/i)).toBeInTheDocument()
  })
})
