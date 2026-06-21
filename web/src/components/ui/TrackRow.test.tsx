import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
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

describe('TrackRow', () => {
  it('renders the track title', () => {
    render(<TrackRow track={track} onPlay={vi.fn()} />)
    expect(screen.getByText('Karma Police')).toBeInTheDocument()
  })

  it('renders the artist name', () => {
    render(<TrackRow track={track} onPlay={vi.fn()} />)
    expect(screen.getByText('Radiohead')).toBeInTheDocument()
  })

  it('renders the album name', () => {
    render(<TrackRow track={track} onPlay={vi.fn()} />)
    expect(screen.getByText('OK Computer')).toBeInTheDocument()
  })

  it('formats duration as m:ss', () => {
    render(<TrackRow track={track} onPlay={vi.fn()} />)
    expect(screen.getByText('3:58')).toBeInTheDocument()
  })

  it('renders the 1-based index when provided', () => {
    render(<TrackRow track={track} index={3} onPlay={vi.fn()} />)
    expect(screen.getByText('4')).toBeInTheDocument()
  })

  it('calls onPlay when clicked', () => {
    const onPlay = vi.fn()
    render(<TrackRow track={track} onPlay={onPlay} />)
    fireEvent.click(screen.getByRole('button'))
    expect(onPlay).toHaveBeenCalledTimes(1)
  })

  it('applies text-accent when active', () => {
    const { container } = render(<TrackRow track={track} active onPlay={vi.fn()} />)
    const root = container.firstChild as HTMLElement
    expect(root.className).toMatch(/text-accent/)
  })

  it('does not apply text-accent when not active', () => {
    const { container } = render(<TrackRow track={track} onPlay={vi.fn()} />)
    const root = container.firstChild as HTMLElement
    expect(root.className).not.toMatch(/text-accent/)
  })

  it('renders Equalizer (eq-bar) when active', () => {
    const { container } = render(<TrackRow track={track} active onPlay={vi.fn()} />)
    const bars = container.querySelectorAll('[data-testid="eq-bar"]')
    expect(bars.length).toBeGreaterThan(0)
  })

  it('does not render Equalizer when not active', () => {
    const { container } = render(<TrackRow track={track} onPlay={vi.fn()} />)
    const bars = container.querySelectorAll('[data-testid="eq-bar"]')
    expect(bars.length).toBe(0)
  })

  it('renders the right slot when provided', () => {
    render(<TrackRow track={track} onPlay={vi.fn()} right={<span data-testid="dl-badge">DL</span>} />)
    expect(screen.getByTestId('dl-badge')).toBeInTheDocument()
  })

  it('has focus-visible ring on the button', () => {
    render(<TrackRow track={track} onPlay={vi.fn()} />)
    expect(screen.getByRole('button').className).toMatch(/focus-visible:ring/)
  })
})
