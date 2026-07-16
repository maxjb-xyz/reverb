import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'
import { usePlayer, engine } from '../lib/playerStore'
import type { Track } from '../lib/types'
import { useUI } from '../lib/uiStore'
import { CinemaView } from './CinemaView'

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
})
