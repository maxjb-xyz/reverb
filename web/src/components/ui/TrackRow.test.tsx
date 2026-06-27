import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { TrackRow } from './TrackRow'
import type { Track } from '../../lib/types'

vi.mock('../../lib/libraryApi', () => ({
  createPlaylist: vi.fn(),
  coverUrl: vi.fn((id: string) => (id ? `cover:${id}` : '')),
  trackCoverUrl: vi.fn((track: { albumId?: string; coverArtId?: string }) => {
    const id = track.albumId || track.coverArtId || ''
    return id ? `cover:${id}` : ''
  }),
}))
vi.mock('../../lib/syncedPlaylistApi', () => ({
  useSyncedPlaylists: vi.fn(),
  addSyncedTrack: vi.fn(),
}))
vi.mock('@tanstack/react-query', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-query')>()
  return {
    ...actual,
    useQueryClient: () => ({ invalidateQueries: vi.fn() }),
  }
})

import { useSyncedPlaylists } from '../../lib/syncedPlaylistApi'

const track: Track = {
  id: 'trk-1',
  title: 'Karma Police',
  album: 'OK Computer',
  albumId: 'alb-1',
  artist: 'Radiohead',
  artistId: 'art-1',
  coverArtId: 'cov-1',
  trackNumber: 4,
  discNumber: 1,
  durationMs: 238000, // 3:58
  bitRate: 320,
  suffix: 'mp3',
  contentType: 'audio/mpeg',
}

/** Track with no artistId — simulates missing/external rows */
const trackNoArtist: Track = {
  ...track,
  artistId: '',
}

function renderRow(props: Partial<Parameters<typeof TrackRow>[0]> & Pick<Parameters<typeof TrackRow>[0], 'onPlay'>) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <TrackRow track={track} {...props} />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.mocked(useSyncedPlaylists).mockReturnValue({
    data: [],
    isLoading: false,
  } as unknown as ReturnType<typeof useSyncedPlaylists>)
})

describe('TrackRow', () => {
  it('renders the track title', () => {
    renderRow({ onPlay: vi.fn() })
    expect(screen.getByText('Karma Police')).toBeInTheDocument()
  })

  it('renders the album cover directly (album-primary, no per-song request)', () => {
    // trackCoverUrl prefers albumId — so the img src is the album cover, not the song's mf- id
    renderRow({ onPlay: vi.fn() })
    const img = document.querySelector('img') as HTMLImageElement
    // track.albumId = 'alb-1', track.coverArtId = 'cov-1'; album wins
    expect(img.getAttribute('src')).toBe('cover:alb-1')
  })

  it('uses coverSrc prop as override (external images still work)', () => {
    renderRow({ onPlay: vi.fn(), coverSrc: 'https://cdn.example.com/art.jpg' })
    const img = document.querySelector('img') as HTMLImageElement
    expect(img.getAttribute('src')).toBe('https://cdn.example.com/art.jpg')
  })

  it('renders the artist name', () => {
    renderRow({ onPlay: vi.fn() })
    expect(screen.getByText('Radiohead')).toBeInTheDocument()
  })

  it('renders the album name', () => {
    renderRow({ onPlay: vi.fn() })
    expect(screen.getByText('OK Computer')).toBeInTheDocument()
  })

  it('formats duration as m:ss', () => {
    renderRow({ onPlay: vi.fn() })
    expect(screen.getByText('3:58')).toBeInTheDocument()
  })

  it('renders the 1-based index when provided', () => {
    renderRow({ index: 3, onPlay: vi.fn() })
    expect(screen.getByText('4')).toBeInTheDocument()
  })

  // ── Play interaction (Spotify semantics) ─────────────────────────────────

  it('does NOT call onPlay on single click of the row', () => {
    const onPlay = vi.fn()
    const { container } = renderRow({ onPlay })
    const row = container.firstChild as HTMLElement
    fireEvent.click(row)
    expect(onPlay).not.toHaveBeenCalled()
  })

  it('calls onPlay on double-click of the row', () => {
    const onPlay = vi.fn()
    const { container } = renderRow({ onPlay })
    const row = container.firstChild as HTMLElement
    fireEvent.doubleClick(row)
    expect(onPlay).toHaveBeenCalledTimes(1)
  })

  it('calls onPlay when the hover play button is clicked', () => {
    const onPlay = vi.fn()
    renderRow({ onPlay })
    const playBtn = screen.getByRole('button', { name: `Play ${track.title}` })
    fireEvent.click(playBtn)
    expect(onPlay).toHaveBeenCalledTimes(1)
  })

  it('calls onPlay on keyboard Enter', () => {
    const onPlay = vi.fn()
    const { container } = renderRow({ onPlay })
    const row = container.firstChild as HTMLElement
    fireEvent.keyDown(row, { key: 'Enter' })
    expect(onPlay).toHaveBeenCalledTimes(1)
  })

  it('calls onPlay on keyboard Space', () => {
    const onPlay = vi.fn()
    const { container } = renderRow({ onPlay })
    const row = container.firstChild as HTMLElement
    fireEvent.keyDown(row, { key: ' ' })
    expect(onPlay).toHaveBeenCalledTimes(1)
  })

  // ── Artist link ───────────────────────────────────────────────────────────

  it('renders artist as a link when artistId is present', () => {
    renderRow({ onPlay: vi.fn() })
    const link = screen.getByRole('link', { name: 'Radiohead' })
    expect(link).toBeInTheDocument()
    expect(link).toHaveAttribute('href', '/artist/library/art-1')
  })

  it('clicking the artist link does NOT call onPlay', () => {
    const onPlay = vi.fn()
    renderRow({ onPlay })
    const link = screen.getByRole('link', { name: 'Radiohead' })
    fireEvent.click(link)
    expect(onPlay).not.toHaveBeenCalled()
  })

  it('renders artist as plain text when artistId is empty', () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={qc}>
        <MemoryRouter>
          <TrackRow track={trackNoArtist} onPlay={vi.fn()} />
        </MemoryRouter>
      </QueryClientProvider>,
    )
    expect(screen.getByText('Radiohead')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Radiohead' })).toBeNull()
  })

  // ── Album link ──────────────────────────────────────────────────────────────

  it('renders album as a link to /album/library/:albumId when albumId is present', () => {
    renderRow({ onPlay: vi.fn() })
    const link = screen.getByRole('link', { name: 'OK Computer' })
    expect(link).toBeInTheDocument()
    expect(link).toHaveAttribute('href', '/album/library/alb-1')
  })

  it('clicking the album link does NOT call onPlay', () => {
    const onPlay = vi.fn()
    renderRow({ onPlay })
    const link = screen.getByRole('link', { name: 'OK Computer' })
    fireEvent.click(link)
    expect(onPlay).not.toHaveBeenCalled()
  })

  it('renders album as plain text when albumId is empty', () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    render(
      <QueryClientProvider client={qc}>
        <MemoryRouter>
          <TrackRow track={{ ...track, albumId: '' }} onPlay={vi.fn()} />
        </MemoryRouter>
      </QueryClientProvider>,
    )
    expect(screen.getByText('OK Computer')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'OK Computer' })).toBeNull()
  })

  // ── Active / now-playing treatment ───────────────────────────────────────

  it('applies text-accent when active', () => {
    const { container } = renderRow({ active: true, onPlay: vi.fn() })
    const row = container.firstChild as HTMLElement
    expect(row.className).toMatch(/text-accent/)
  })

  it('does not apply text-accent when not active', () => {
    const { container } = renderRow({ onPlay: vi.fn() })
    const row = container.firstChild as HTMLElement
    expect(row.className).not.toMatch(/text-accent/)
  })

  it('renders Equalizer (eq-bar) when active', () => {
    const { container } = renderRow({ active: true, onPlay: vi.fn() })
    const bars = container.querySelectorAll('[data-testid="eq-bar"]')
    expect(bars.length).toBeGreaterThan(0)
  })

  it('does not render Equalizer when not active', () => {
    const { container } = renderRow({ onPlay: vi.fn() })
    const bars = container.querySelectorAll('[data-testid="eq-bar"]')
    expect(bars.length).toBe(0)
  })

  it('active+playing=true → eq bars have animate-eq', () => {
    const { container } = renderRow({ active: true, playing: true, onPlay: vi.fn() })
    const bars = container.querySelectorAll('[data-testid="eq-bar"]')
    expect(bars.length).toBeGreaterThan(0)
    bars.forEach((bar) => {
      expect(bar.className).toMatch(/animate-eq/)
    })
  })

  it('active+playing=false → eq bars keep animate-eq and add paused state', () => {
    const { container } = renderRow({ active: true, playing: false, onPlay: vi.fn() })
    const bars = container.querySelectorAll('[data-testid="eq-bar"]')
    expect(bars.length).toBeGreaterThan(0)
    bars.forEach((bar) => {
      expect(bar.className).toMatch(/animate-eq/)
      expect(bar.className).toMatch(/animation-play-state:paused/)
      expect(bar.className).toMatch(/h-1/)
    })
  })

  // ── Right slot ────────────────────────────────────────────────────────────

  it('renders the right slot when provided', () => {
    renderRow({ onPlay: vi.fn(), right: <span data-testid="dl-badge">DL</span> })
    expect(screen.getByTestId('dl-badge')).toBeInTheDocument()
  })

  // ── artistTo / albumTo overrides ──────────────────────────────────────────

  it('artistTo overrides the artist link destination', () => {
    renderRow({ onPlay: vi.fn(), artistTo: '/artist/spotify/sp-artist-1' })
    const link = screen.getByRole('link', { name: 'Radiohead' })
    expect(link).toHaveAttribute('href', '/artist/spotify/sp-artist-1')
  })

  it('albumTo overrides the album link destination', () => {
    renderRow({ onPlay: vi.fn(), albumTo: '/album/spotify/sp-album-1' })
    const link = screen.getByRole('link', { name: 'OK Computer' })
    expect(link).toHaveAttribute('href', '/album/spotify/sp-album-1')
  })

  it('falls back to /artist/library/:artistId when artistTo is not provided', () => {
    renderRow({ onPlay: vi.fn() })
    const link = screen.getByRole('link', { name: 'Radiohead' })
    expect(link).toHaveAttribute('href', '/artist/library/art-1')
  })

  it('falls back to /album/library/:albumId when albumTo is not provided', () => {
    renderRow({ onPlay: vi.fn() })
    const link = screen.getByRole('link', { name: 'OK Computer' })
    expect(link).toHaveAttribute('href', '/album/library/alb-1')
  })

  // ── A11y ──────────────────────────────────────────────────────────────────

  it('has role="button" on the container', () => {
    const { container } = renderRow({ onPlay: vi.fn() })
    const row = container.firstChild as HTMLElement
    expect(row.getAttribute('role')).toBe('button')
  })

  it('has focus-visible ring class on the row container', () => {
    const { container } = renderRow({ onPlay: vi.fn() })
    const row = container.firstChild as HTMLElement
    expect(row.className).toMatch(/focus-visible:ring/)
  })
})

describe('Add-to-playlist button', () => {
  it('shows an "Add to playlist" button on a row with a truthy track.id', () => {
    renderRow({ onPlay: vi.fn() })
    expect(screen.getByRole('button', { name: /add to playlist/i })).toBeInTheDocument()
  })

  it('does NOT show an "Add to playlist" button when track.id is empty', () => {
    renderRow({ onPlay: vi.fn(), track: { ...track, id: '' } })
    expect(screen.queryByRole('button', { name: /add to playlist/i })).toBeNull()
  })

  it('clicking "Add to playlist" opens AddToPlaylistMenu and does NOT call onPlay', () => {
    const onPlay = vi.fn()
    renderRow({ onPlay })
    fireEvent.click(screen.getByRole('button', { name: /add to playlist/i }))
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(onPlay).not.toHaveBeenCalled()
  })
})
