import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { engine } from '../../lib/playerStore'
import type { Track } from '../../lib/types'
import { useUI } from '../../lib/uiStore'
import { useLyrics } from '../../lib/lyricsApi'
import { LyricsCard } from './LyricsCard'

vi.mock('../../lib/lyricsApi', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../lib/lyricsApi')>()
  return { ...actual, useLyrics: vi.fn(() => ({ data: null })) }
})

const track: Track = { id: 't1', title: 'Karma Police', albumId: 'al1', album: 'OK Computer', artistId: 'ar1', artist: 'Radiohead', coverArtId: 'c1', trackNumber: 1, discNumber: 1, durationMs: 238000, bitRate: 0, suffix: '', contentType: '' }

describe('LyricsCard', () => {
  beforeEach(() => {
    useUI.setState({ lyricsOpen: false })
    vi.mocked(useLyrics).mockReturnValue({ data: null } as ReturnType<typeof useLyrics>)
    engine.playTrackList([track], 0)
  })

  it('renders nothing when there is no lyrics payload', () => {
    render(<LyricsCard />)
    expect(screen.queryByTestId('lyrics-card')).toBeNull()
  })

  it('shows a ~3 line window around the active line for synced lyrics', () => {
    const eng = engine as unknown as { currentTimeMs: number; emit(): void }
    eng.currentTimeMs = 1000
    eng.emit()
    vi.mocked(useLyrics).mockReturnValue({
      data: {
        synced: true,
        lines: [
          { timeMs: 0, text: 'line one' },
          { timeMs: 1000, text: 'line two' },
          { timeMs: 2000, text: 'line three' },
          { timeMs: 3000, text: 'line four' },
          { timeMs: 4000, text: 'line five' },
        ],
      },
    } as ReturnType<typeof useLyrics>)

    render(<LyricsCard />)

    expect(screen.getByText('line one')).toBeInTheDocument()
    expect(screen.getByText('line two')).toBeInTheDocument()
    expect(screen.getByText('line three')).toBeInTheDocument()
    expect(screen.queryByText('line four')).not.toBeInTheDocument()
    expect(screen.queryByText('line five')).not.toBeInTheDocument()

    const active = screen.getByText('line two')
    expect(active).toHaveAttribute('data-active', 'true')

    eng.currentTimeMs = 0
    eng.emit()
  })

  it('clicking the card calls openLyrics', () => {
    vi.mocked(useLyrics).mockReturnValue({
      data: {
        synced: true,
        lines: [
          { timeMs: 0, text: 'line one' },
          { timeMs: 1000, text: 'line two' },
        ],
      },
    } as ReturnType<typeof useLyrics>)

    render(<LyricsCard />)
    fireEvent.click(screen.getByTestId('lyrics-card'))
    expect(useUI.getState().lyricsOpen).toBe(true)
  })

  it('shows the first few lines of plain lyrics with no active marker', () => {
    vi.mocked(useLyrics).mockReturnValue({
      data: { synced: false, plain: 'la la la\nla la la 2\nla la la 3\nla la la 4' },
    } as ReturnType<typeof useLyrics>)

    render(<LyricsCard />)

    expect(screen.getByText('la la la')).toBeInTheDocument()
    expect(screen.getByText('la la la 2')).toBeInTheDocument()
    expect(screen.getByText('la la la 3')).toBeInTheDocument()
    expect(screen.queryByText('la la la 4')).not.toBeInTheDocument()
    expect(screen.queryByTestId('lyrics-card')?.querySelector('[data-active]')).toBeNull()
  })
})
