import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, fireEvent, act } from '@testing-library/react'
import { NowPlayingOverlay } from './NowPlayingOverlay'
import { useAlbumPalette } from '../lib/useAlbumPalette'
import { usePlayer, engine } from '../lib/playerStore'
import { useUI } from '../lib/uiStore'
import type { Track } from '../lib/types'

vi.mock('../lib/useAlbumPalette', () => ({ useAlbumPalette: vi.fn(() => null) }))
const mockUseLyrics = vi.fn((): { data: unknown } => ({ data: null }))
vi.mock('../lib/lyricsApi', () => ({ useLyrics: () => mockUseLyrics() }))
vi.mock('./AddToPlaylistMenu', () => ({ AddToPlaylistMenu: ({ track }: { track: Track }) => <div>add-menu:{track.id}</div> }))

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

  it('paints the album wash over an opaque base so the page never shows through', () => {
    vi.mocked(useAlbumPalette).mockReturnValueOnce({ rgb: [200, 80, 20], text: '#fff', scrim: false })
    act(() => { useUI.getState().openNowPlaying() })
    render(<NowPlayingOverlay />)
    const style = screen.getByTestId('now-playing-overlay').getAttribute('style') ?? ''
    expect(style).toMatch(/var\(--bg-base\) 70%\), var\(--bg-base\)/)
  })

  it('has shuffle and repeat like the desktop bar', () => {
    act(() => useUI.getState().openNowPlaying())
    render(<NowPlayingOverlay />)
    fireEvent.click(screen.getByRole('button', { name: 'Shuffle' }))
    expect(usePlayer.getState().shuffle).toBe(true)
    fireEvent.click(screen.getByRole('button', { name: /^Repeat/ }))
    expect(usePlayer.getState().repeat).toBe('all')
    act(() => { usePlayer.getState().toggleShuffle(); usePlayer.getState().cycleRepeat(); usePlayer.getState().cycleRepeat() })
  })

  it('Queue swaps the artwork for what is up next, and a row jumps to that track', () => {
    act(() => useUI.getState().openNowPlaying())
    render(<NowPlayingOverlay />)
    fireEvent.click(screen.getByRole('button', { name: 'Queue' }))
    const queue = screen.getByTestId('now-playing-queue')
    expect(queue).toHaveTextContent('Song 2')
    fireEvent.click(screen.getByRole('button', { name: /Song 2/ }))
    expect(usePlayer.getState().current?.id).toBe('2')
  })

  it('tapping the seek bar seeks', () => {
    act(() => useUI.getState().openNowPlaying())
    const seek = vi.spyOn(engine, 'seekMs')
    act(() => usePlayer.setState({ durationMs: 200000 }))
    render(<NowPlayingOverlay />)
    const slider = screen.getByRole('slider', { name: 'Seek' })
    slider.getBoundingClientRect = () => ({ left: 0, width: 200, top: 0, height: 24, right: 200, bottom: 24, x: 0, y: 0, toJSON: () => ({}) })
    fireEvent.click(slider, { clientX: 50 })
    expect(seek).toHaveBeenCalledWith(50000)
    seek.mockRestore()
  })

  it('offers add-to-playlist for the current track', () => {
    act(() => useUI.getState().openNowPlaying())
    render(<NowPlayingOverlay />)
    fireEvent.click(screen.getByRole('button', { name: 'Add to playlist' }))
    expect(screen.getByText('add-menu:1')).toBeInTheDocument()
  })

  it('shows the Lyrics button only when the track has lyrics', () => {
    act(() => useUI.getState().openNowPlaying())
    const { unmount } = render(<NowPlayingOverlay />)
    expect(screen.queryByRole('button', { name: 'Lyrics' })).not.toBeInTheDocument()
    unmount()
    mockUseLyrics.mockReturnValue({ data: { synced: false, plain: 'la la la' } })
    render(<NowPlayingOverlay />)
    fireEvent.click(screen.getByRole('button', { name: 'Lyrics' }))
    expect(screen.getByTestId('now-playing-lyrics')).toHaveTextContent('la la la')
    mockUseLyrics.mockReturnValue({ data: null })
  })
})
