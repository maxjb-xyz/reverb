import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { AppShell } from './AppShell'
import { useUI } from '../lib/uiStore'
import { useDownloads } from '../lib/downloadStore'
import { usePlayer } from '../lib/playerStore'
import type { Track } from '../lib/types'

vi.mock('../lib/realtimeWiring', () => ({ useRealtime: () => {} }))
import { useAlbumPalette } from '../lib/useAlbumPalette'
vi.mock('../lib/useAlbumPalette', () => ({ useAlbumPalette: vi.fn(() => null) }))

// Suppress library API fetches in tests
vi.mock('../lib/libraryApi', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/libraryApi')>()
  return {
    ...actual,
    usePlaylists: () => ({ isLoading: false, data: [] }),
    useAlbums: () => ({ isLoading: false, data: [] }),
    useArtists: () => ({ isLoading: false, data: [] }),
  }
})

function track(id: string): Track {
  return {
    id, title: 'Song ' + id, albumId: 'al', album: 'Album', artistId: 'ar', artist: 'Artist',
    coverArtId: 'co', trackNumber: 1, discNumber: 1, durationMs: 200000, bitRate: 320,
    suffix: 'mp3', contentType: 'audio/mpeg',
  }
}

function renderShell() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <AppShell />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('AppShell', () => {
  beforeEach(() => {
    useDownloads.setState({ jobs: {} })
    useUI.setState({ rightPanel: null, nowPlayingOpen: false })
    vi.mocked(useAlbumPalette).mockReset()
    vi.mocked(useAlbumPalette).mockReturnValue(null)
  })

  it('TopBar is always present', () => {
    renderShell()
    // TopBar renders a header element
    expect(screen.getByRole('banner')).toBeInTheDocument()
  })

  it('LibraryRail is in the DOM (hidden on mobile via CSS)', () => {
    renderShell()
    // LibraryRail has "Your Library" label
    expect(screen.getByText('Your Library')).toBeInTheDocument()
  })

  it('PlayerBar is in the DOM (hidden on mobile via CSS)', () => {
    renderShell()
    expect(screen.getByTestId('player-bar')).toBeInTheDocument()
  })

  it('right column is ABSENT when rightPanel is null (default)', () => {
    useUI.setState({ rightPanel: null })
    renderShell()
    expect(screen.queryByTestId('right-panel-column')).not.toBeInTheDocument()
  })

  it('right column renders NowPlayingPanel when rightPanel is nowplaying', () => {
    act(() => { usePlayer.getState().playTrackList([track('1')], 0) })
    useUI.setState({ rightPanel: 'nowplaying' })
    renderShell()
    expect(screen.getByTestId('right-panel-column')).toBeInTheDocument()
    expect(screen.getByTestId('now-playing-panel')).toBeInTheDocument()
  })

  it('right column renders DownloadTray when rightPanel is downloads', () => {
    useUI.setState({ rightPanel: 'downloads' })
    renderShell()
    expect(screen.getByTestId('right-panel-column')).toBeInTheDocument()
    expect(screen.getByText('Download Tray')).toBeInTheDocument()
  })

  it('mobile chrome (MobileTabNav) is in the DOM', () => {
    renderShell()
    expect(screen.getByTestId('mobile-tab-nav')).toBeInTheDocument()
  })

  it('paints an ambient background when a palette is present', () => {
    vi.mocked(useAlbumPalette).mockReturnValue({ rgb: [200, 30, 40], text: '#FFFFFF', scrim: false })
    useUI.setState({ rightPanel: null })
    act(() => { usePlayer.getState().playTrackList([track('1')], 0) })
    renderShell()
    const root = screen.getByTestId('app-shell-root')
    expect(root.style.background).not.toBe('')
  })

  it('uses the static background when no palette (dynamic_background off)', () => {
    vi.mocked(useAlbumPalette).mockReturnValue(null)
    renderShell()
    const root = screen.getByTestId('app-shell-root')
    expect(root.style.background).toBe('')
  })
})
