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

function track(id: string): Track {
  return {
    id, title: 'Song ' + id, albumId: 'al', album: 'Album', artistId: 'ar', artist: 'Artist',
    coverArtId: 'co', trackNumber: 1, discNumber: 1, durationMs: 200000, bitRate: 320,
    suffix: 'mp3', contentType: 'audio/mpeg',
  }
}

function renderShell() {
  const qc = new QueryClient()
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
    useUI.setState({ rightPanel: 'downloads', nowPlayingOpen: false })
    vi.mocked(useAlbumPalette).mockReset()
    vi.mocked(useAlbumPalette).mockReturnValue(null)
  })

  it('mounts the Download Tray when the right panel is downloads', () => {
    renderShell()
    expect(screen.getByText('Download Tray')).toBeInTheDocument()
  })

  it('renders the desktop sidebar and the mobile tab nav (chrome swaps via CSS)', () => {
    renderShell()
    // Both chromes are in the DOM; Tailwind hidden/md: classes decide visibility.
    expect(screen.getByTestId('app-shell-root')).toBeInTheDocument()
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

  it('uses the static background when dynamic_background is off (no palette)', () => {
    vi.mocked(useAlbumPalette).mockReturnValue(null)
    renderShell()
    const root = screen.getByTestId('app-shell-root')
    expect(root.style.background).toBe('')
  })
})
