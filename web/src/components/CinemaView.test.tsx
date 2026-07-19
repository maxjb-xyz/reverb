import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { engine } from '../lib/playerStore'
import type { Track } from '../lib/types'
import { useUI } from '../lib/uiStore'
import { usePeaks } from '../lib/peaksApi'
import { useLyrics } from '../lib/lyricsApi'
import { CinemaView } from './CinemaView'

vi.mock('../lib/useAlbumPalette', () => ({ useAlbumPalette: vi.fn(() => null) }))
vi.mock('../lib/peaksApi', () => ({ usePeaks: vi.fn(() => ({ data: null })) }))
vi.mock('../lib/lyricsApi', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/lyricsApi')>()
  return { ...actual, useLyrics: vi.fn(() => ({ data: null })) }
})

const track: Track = { id: 't1', title: 'Karma Police', albumId: 'al1', album: 'OK Computer', artistId: 'ar1', artist: 'Radiohead', coverArtId: 'c1', trackNumber: 1, discNumber: 1, durationMs: 238000, bitRate: 0, suffix: '', contentType: '' }

describe('CinemaView', () => {
  beforeEach(() => {
    useUI.setState({ cinemaOpen: false })
    vi.mocked(useLyrics).mockReturnValue({ data: null } as ReturnType<typeof useLyrics>)
  })
  it('renders nothing when closed', () => { render(<CinemaView />); expect(screen.queryByTestId('cinema-view')).toBeNull() })
  it('shows the current track and closes on Escape', () => {
    engine.playTrackList([track], 0)
    useUI.setState({ cinemaOpen: true })
    render(<CinemaView />)
    expect(screen.getByTestId('cinema-view')).toBeInTheDocument()
    expect(screen.getByText('Karma Police')).toBeInTheDocument()
    fireEvent.keyDown(window, { key: 'Escape' })
    expect(useUI.getState().cinemaOpen).toBe(false)
  })

  it('seek bar is keyboard operable via ArrowRight (+5s)', () => {
    const eng = engine as unknown as { durationMs: number; currentTimeMs: number; emit(): void }
    engine.playTrackList([track], 0)
    eng.durationMs = 200000
    eng.currentTimeMs = 10000
    eng.emit()
    useUI.setState({ cinemaOpen: true })

    const seekSpy = vi.spyOn(engine, 'seekMs')
    render(<CinemaView />)

    const seekBar = screen.getByRole('slider', { name: /seek/i })
    seekBar.focus()
    fireEvent.keyDown(seekBar, { key: 'ArrowRight' })

    expect(seekSpy).toHaveBeenCalledWith(10000 + 5000)

    seekSpy.mockRestore()
    eng.durationMs = 0
    eng.currentTimeMs = 0
    eng.emit()
  })

  it('renders the waveform when peaks are available', () => {
    vi.mocked(usePeaks).mockReturnValueOnce({ data: [0.2, 0.5, 0.8, 0.3] } as ReturnType<typeof usePeaks>)
    engine.playTrackList([track], 0)
    useUI.setState({ cinemaOpen: true })
    render(<CinemaView />)
    expect(screen.getByTestId('waveform')).toBeInTheDocument()
  })

  it('toggles between the queue and lyrics in the side column', () => {
    vi.mocked(useLyrics).mockReturnValue({
      data: {
        synced: true,
        lines: [
          { timeMs: 0, text: 'line one' },
          { timeMs: 1000, text: 'line two' },
        ],
      },
    } as ReturnType<typeof useLyrics>)
    engine.playTrackList([track], 0)
    useUI.setState({ cinemaOpen: true })
    render(<CinemaView />)

    const button = screen.getByRole('button', { name: 'Show lyrics' })
    expect(screen.getByText('Up next')).toBeInTheDocument()

    fireEvent.click(button)
    expect(screen.getByTestId('lyrics-lines')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Show queue' })).toBeInTheDocument()
    expect(screen.queryByText('Up next')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Show queue' }))
    expect(screen.queryByTestId('lyrics-lines')).not.toBeInTheDocument()
    expect(screen.getByText('Up next')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Show lyrics' })).toBeInTheDocument()
  })

  it('does not show a lyrics toggle when there are no lyrics', () => {
    vi.mocked(useLyrics).mockReturnValue({ data: null } as ReturnType<typeof useLyrics>)
    engine.playTrackList([track], 0)
    useUI.setState({ cinemaOpen: true })
    render(<CinemaView />)
    expect(screen.queryByRole('button', { name: 'Show lyrics' })).not.toBeInTheDocument()
  })
})
