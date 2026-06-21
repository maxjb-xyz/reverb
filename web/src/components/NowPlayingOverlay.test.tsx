import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent, act } from '@testing-library/react'
import { NowPlayingOverlay } from './NowPlayingOverlay'
import { usePlayer, engine } from '../lib/playerStore'
import { useUI } from '../lib/uiStore'
import type { Track } from '../lib/types'

vi.mock('../lib/useAlbumPalette', () => ({ useAlbumPalette: vi.fn(() => null) }))

function track(id: string): Track {
  return {
    id, title: 'Song ' + id, albumId: 'al', album: 'Album', artistId: 'ar', artist: 'Artist',
    coverArtId: 'co', trackNumber: 1, discNumber: 1, durationMs: 200000, bitRate: 320,
    suffix: 'mp3', contentType: 'audio/mpeg',
  }
}

describe('NowPlayingOverlay', () => {
  beforeEach(() => {
    act(() => { usePlayer.getState().playTrackList([track('1'), track('2')], 0) })
  })

  it('renders nothing when closed', () => {
    act(() => { useUI.getState().closeNowPlaying() })
    const { container } = render(<NowPlayingOverlay />)
    expect(container.firstChild).toBeNull()
  })

  it('renders the current track when open', () => {
    act(() => { useUI.getState().openNowPlaying() })
    render(<NowPlayingOverlay />)
    expect(screen.getByTestId('now-playing-overlay')).toBeInTheDocument()
    expect(screen.getByText('Song 1')).toBeInTheDocument()
  })

  it('the close button closes the overlay', () => {
    act(() => { useUI.getState().openNowPlaying() })
    render(<NowPlayingOverlay />)
    fireEvent.click(screen.getByRole('button', { name: /close now playing/i }))
    expect(useUI.getState().nowPlayingOpen).toBe(false)
  })

  it('transport buttons drive the engine', () => {
    act(() => { useUI.getState().openNowPlaying() })
    const nextSpy = vi.spyOn(engine, 'next')
    render(<NowPlayingOverlay />)
    fireEvent.click(screen.getByRole('button', { name: /^next$/i }))
    expect(nextSpy).toHaveBeenCalledTimes(1)
    nextSpy.mockRestore()
  })
})
