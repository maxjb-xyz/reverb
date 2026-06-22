import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { TrackRow } from './TrackRow'
import type { Track } from '../../lib/types'

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
  return render(
    <MemoryRouter>
      <TrackRow track={track} {...props} />
    </MemoryRouter>,
  )
}

describe('TrackRow', () => {
  it('renders the track title', () => {
    renderRow({ onPlay: vi.fn() })
    expect(screen.getByText('Karma Police')).toBeInTheDocument()
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
    render(
      <MemoryRouter>
        <TrackRow track={trackNoArtist} onPlay={vi.fn()} />
      </MemoryRouter>,
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
    render(
      <MemoryRouter>
        <TrackRow track={{ ...track, albumId: '' }} onPlay={vi.fn()} />
      </MemoryRouter>,
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

  // ── Right slot ────────────────────────────────────────────────────────────

  it('renders the right slot when provided', () => {
    renderRow({ onPlay: vi.fn(), right: <span data-testid="dl-badge">DL</span> })
    expect(screen.getByTestId('dl-badge')).toBeInTheDocument()
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
