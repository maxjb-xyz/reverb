import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { engine } from '../lib/playerStore'
import type { Track } from '../lib/types'
import { useUI } from '../lib/uiStore'
import { CinemaView } from './CinemaView'

vi.mock('../lib/useAlbumPalette', () => ({ useAlbumPalette: vi.fn(() => null) }))

const track: Track = { id: 't1', title: 'Karma Police', albumId: 'al1', album: 'OK Computer', artistId: 'ar1', artist: 'Radiohead', coverArtId: 'c1', trackNumber: 1, discNumber: 1, durationMs: 238000, bitRate: 0, suffix: '', contentType: '' }

describe('CinemaView', () => {
  beforeEach(() => useUI.setState({ cinemaOpen: false }))
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
})
