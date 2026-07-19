import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { engine } from '../../lib/playerStore'
import type { Track } from '../../lib/types'
import { useUI } from '../../lib/uiStore'
import { useLyrics } from '../../lib/lyricsApi'
import { LyricsView } from './LyricsView'

vi.mock('../../lib/useAlbumPalette', () => ({ useAlbumPalette: vi.fn(() => null) }))
vi.mock('../../lib/lyricsApi', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../lib/lyricsApi')>()
  return { ...actual, useLyrics: vi.fn(() => ({ data: null })) }
})

const track: Track = { id: 't1', title: 'Karma Police', albumId: 'al1', album: 'OK Computer', artistId: 'ar1', artist: 'Radiohead', coverArtId: 'c1', trackNumber: 1, discNumber: 1, durationMs: 238000, bitRate: 0, suffix: '', contentType: '' }

describe('LyricsView', () => {
  beforeEach(() => {
    useUI.setState({ lyricsOpen: false })
    vi.mocked(useLyrics).mockReturnValue({ data: null } as ReturnType<typeof useLyrics>)
  })

  it('renders nothing when closed', () => {
    render(<LyricsView />)
    expect(screen.queryByTestId('lyrics-view')).toBeNull()
  })

  it('renders synced lines with the active line marked', () => {
    engine.playTrackList([track], 0)
    const eng = engine as unknown as { currentTimeMs: number; emit(): void }
    eng.currentTimeMs = 1000
    eng.emit()
    vi.mocked(useLyrics).mockReturnValue({
      data: {
        synced: true,
        lines: [
          { timeMs: 0, text: 'first line' },
          { timeMs: 1000, text: 'second line' },
          { timeMs: 5000, text: 'third line' },
        ],
      },
    } as ReturnType<typeof useLyrics>)
    useUI.setState({ lyricsOpen: true })
    render(<LyricsView />)

    expect(screen.getByText('first line')).toBeInTheDocument()
    expect(screen.getByText('second line')).toBeInTheDocument()
    expect(screen.getByText('third line')).toBeInTheDocument()

    const active = screen.getByText('second line')
    expect(active).toHaveAttribute('data-active', 'true')
    expect(active).toHaveAttribute('aria-current', 'true')

    eng.currentTimeMs = 0
    eng.emit()
  })

  it('clicking a line calls seekMs with the line timeMs', () => {
    engine.playTrackList([track], 0)
    vi.mocked(useLyrics).mockReturnValue({
      data: {
        synced: true,
        lines: [
          { timeMs: 0, text: 'first line' },
          { timeMs: 4200, text: 'second line' },
        ],
      },
    } as ReturnType<typeof useLyrics>)
    useUI.setState({ lyricsOpen: true })

    const seekSpy = vi.spyOn(engine, 'seekMs')
    render(<LyricsView />)
    fireEvent.click(screen.getByText('second line'))
    expect(seekSpy).toHaveBeenCalledWith(4200)
    seekSpy.mockRestore()
  })

  it('shows the empty state and stays open when there is no lyrics payload', () => {
    engine.playTrackList([track], 0)
    vi.mocked(useLyrics).mockReturnValue({ data: null } as ReturnType<typeof useLyrics>)
    useUI.setState({ lyricsOpen: true })
    render(<LyricsView />)

    expect(screen.getByText('No lyrics for this track')).toBeInTheDocument()
    expect(screen.getByTestId('lyrics-view')).toBeInTheDocument()
  })

  it('renders plain text as a single block with no line buttons', () => {
    engine.playTrackList([track], 0)
    vi.mocked(useLyrics).mockReturnValue({
      data: { synced: false, plain: 'la la la\nla la la' },
    } as ReturnType<typeof useLyrics>)
    useUI.setState({ lyricsOpen: true })
    render(<LyricsView />)

    expect(screen.getByText(/la la la/)).toBeInTheDocument()
    expect(screen.queryByTestId('lyrics-lines')).toBeNull()
  })

  it('Escape key calls closeLyrics', () => {
    engine.playTrackList([track], 0)
    useUI.setState({ lyricsOpen: true })
    render(<LyricsView />)
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(useUI.getState().lyricsOpen).toBe(false)
  })
})
