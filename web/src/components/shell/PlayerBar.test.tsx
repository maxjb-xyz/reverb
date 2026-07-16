import { describe, expect, it, beforeEach, vi, afterEach } from 'vitest'
import { render, screen, fireEvent, act } from '@testing-library/react'
import { PlayerBar } from './PlayerBar'
import { usePlayer, engine } from '../../lib/playerStore'
import { useUI } from '../../lib/uiStore'
import type { Track } from '../../lib/types'
import { useAlbumPalette } from '../../lib/useAlbumPalette'
vi.mock('../../lib/useAlbumPalette', () => ({ useAlbumPalette: vi.fn(() => null) }))
vi.mock('../../lib/peaksApi', () => ({ usePeaks: vi.fn(() => ({ data: null })) }))

const mockNavigate = vi.fn()
vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>()
  return { ...actual, useNavigate: () => mockNavigate }
})

function track(id: string): Track {
  return {
    id,
    title: 'Song ' + id,
    albumId: 'al',
    album: 'Album',
    artistId: 'ar',
    artist: 'Artist',
    coverArtId: 'co',
    trackNumber: 1,
    discNumber: 1,
    durationMs: 200000,
    bitRate: 320,
    suffix: 'mp3',
    contentType: 'audio/mpeg',
  }
}

describe('PlayerBar (shell)', () => {
  beforeEach(() => {
    act(() => {
      usePlayer.getState().playTrackList([track('1'), track('2')], 0)
      useUI.getState().closePanel()
    })
  })

  it('shows the current track title and artist', () => {
    render(<PlayerBar />)
    expect(screen.getByText('Song 1')).toBeInTheDocument()
    expect(screen.getAllByText('Artist').length).toBeGreaterThan(0)
  })

  it('clicking the artist name navigates to the source-qualified artist route', () => {
    mockNavigate.mockClear()
    render(<PlayerBar />)
    fireEvent.click(screen.getByRole('button', { name: 'Artist' }))
    expect(mockNavigate).toHaveBeenCalledWith('/artist/library/ar')
  })

  it('artist button navigates to /artist/spotify/:id when artistExternalId is set', () => {
    const trackWithExtId: Track = { ...track('1'), artistExternalId: 'ext-99' }
    act(() => {
      usePlayer.getState().playTrackList([trackWithExtId], 0)
    })
    mockNavigate.mockClear()
    render(<PlayerBar />)
    fireEvent.click(screen.getByRole('button', { name: 'Artist' }))
    expect(mockNavigate).toHaveBeenCalledWith('/artist/spotify/ext-99')
  })

  it('artist button navigates to /artist/library/:id when no artistExternalId', () => {
    act(() => {
      usePlayer.getState().playTrackList([track('1')], 0)
    })
    mockNavigate.mockClear()
    render(<PlayerBar />)
    fireEvent.click(screen.getByRole('button', { name: 'Artist' }))
    expect(mockNavigate).toHaveBeenCalledWith('/artist/library/ar')
  })

  // --- transport button actions ---

  it('play/pause button click calls toggle on the engine', () => {
    const spy = vi.spyOn(engine, 'toggle')
    render(<PlayerBar />)
    // Match exactly "Play" or "Pause", not "Mini player"
    fireEvent.click(screen.getByRole('button', { name: /^(Play|Pause)$/i }))
    expect(spy).toHaveBeenCalledTimes(1)
    spy.mockRestore()
  })

  it('Previous button click calls prev on the engine', () => {
    const spy = vi.spyOn(engine, 'prev')
    render(<PlayerBar />)
    fireEvent.click(screen.getByRole('button', { name: /previous/i }))
    expect(spy).toHaveBeenCalledTimes(1)
    spy.mockRestore()
  })

  it('Next button click calls next on the engine', () => {
    const spy = vi.spyOn(engine, 'next')
    render(<PlayerBar />)
    fireEvent.click(screen.getByRole('button', { name: /next/i }))
    expect(spy).toHaveBeenCalledTimes(1)
    spy.mockRestore()
  })

  it('Shuffle button click calls toggleShuffle on the engine', () => {
    const spy = vi.spyOn(engine, 'toggleShuffle')
    render(<PlayerBar />)
    fireEvent.click(screen.getByRole('button', { name: /shuffle/i }))
    expect(spy).toHaveBeenCalledTimes(1)
    spy.mockRestore()
  })

  it('Repeat button click calls cycleRepeat on the engine', () => {
    const spy = vi.spyOn(engine, 'cycleRepeat')
    render(<PlayerBar />)
    fireEvent.click(screen.getByRole('button', { name: /repeat/i }))
    expect(spy).toHaveBeenCalledTimes(1)
    spy.mockRestore()
  })

  // --- add-to-playlist control ---

  it('shows the "Add to playlist" button when a track is current', () => {
    render(<PlayerBar />)
    expect(screen.getByRole('button', { name: /add to playlist/i })).toBeInTheDocument()
  })

  it('hides the "Add to playlist" button when nothing is playing', () => {
    act(() => {
      usePlayer.getState().playTrackList([], 0)
    })
    render(<PlayerBar />)
    expect(screen.queryByRole('button', { name: /add to playlist/i })).not.toBeInTheDocument()
  })

  // --- right-panel toggling ---

  it('Queue button toggles the nowplaying panel', () => {
    render(<PlayerBar />)
    fireEvent.click(screen.getByRole('button', { name: /queue/i }))
    expect(useUI.getState().rightPanel).toBe('nowplaying')
    fireEvent.click(screen.getByRole('button', { name: /queue/i }))
    expect(useUI.getState().rightPanel).toBe(null)
  })

  it('toggles the cinema view from the expand button', () => {
    render(<PlayerBar />)
    fireEvent.click(screen.getByLabelText('Full screen'))
    expect(useUI.getState().cinemaOpen).toBe(true)
  })

  it('volume button mutes and restores the previous volume', () => {
    act(() => usePlayer.getState().setVolume(0.42))
    render(<PlayerBar />)

    fireEvent.click(screen.getByRole('button', { name: 'Mute' }))
    expect(usePlayer.getState().volume).toBe(0)

    fireEvent.click(screen.getByRole('button', { name: 'Unmute' }))
    expect(usePlayer.getState().volume).toBe(0.42)
  })

  // --- seek bar ---

  it('seek bar click calls seekMs with ratio * durationMs', () => {
    const eng = engine as unknown as { durationMs: number; emit(): void }
    act(() => {
      eng.durationMs = 200000
      eng.emit()
    })

    const seekSpy = vi.spyOn(engine, 'seekMs')
    render(<PlayerBar />)

    const seekBar = screen.getByRole('slider', { name: /seek/i })

    vi.spyOn(seekBar, 'getBoundingClientRect').mockReturnValue({
      left: 100,
      width: 400,
      top: 0,
      bottom: 0,
      right: 500,
      height: 0,
      x: 100,
      y: 0,
      toJSON: () => ({}),
    } as DOMRect)

    act(() => {
      fireEvent.click(seekBar, { clientX: 300 })
    })

    expect(seekSpy).toHaveBeenCalledTimes(1)
    expect(seekSpy).toHaveBeenCalledWith(100000) // 0.5 * 200000
    seekSpy.mockRestore()

    act(() => {
      eng.durationMs = 0
      eng.emit()
    })
  })

  it('seek bar is keyboard focusable (tabIndex=0) and operable via arrows/Home/End', () => {
    const eng = engine as unknown as { durationMs: number; emit(): void }
    act(() => {
      eng.durationMs = 200000
      eng.emit()
    })

    const seekSpy = vi.spyOn(engine, 'seekMs')
    render(<PlayerBar />)

    const seekBar = screen.getByRole('slider', { name: /seek/i })
    expect(seekBar).toHaveAttribute('tabindex', '0')

    // currentTimeMs defaults to 0 in tests
    act(() => { fireEvent.keyDown(seekBar, { key: 'ArrowRight' }) })
    expect(seekSpy).toHaveBeenLastCalledWith(5000)

    act(() => { fireEvent.keyDown(seekBar, { key: 'ArrowLeft' }) })
    expect(seekSpy).toHaveBeenLastCalledWith(0)

    act(() => { fireEvent.keyDown(seekBar, { key: 'End' }) })
    expect(seekSpy).toHaveBeenLastCalledWith(200000)

    act(() => { fireEvent.keyDown(seekBar, { key: 'Home' }) })
    expect(seekSpy).toHaveBeenLastCalledWith(0)

    seekSpy.mockRestore()
    act(() => {
      eng.durationMs = 0
      eng.emit()
    })
  })

  // --- keyboard shortcuts ---

  describe('keyboard shortcuts', () => {
    afterEach(() => {
      vi.restoreAllMocks()
    })

    it('Space key calls toggle and prevents default', () => {
      const toggleSpy = vi.spyOn(engine, 'toggle')
      render(<PlayerBar />)

      const event = new KeyboardEvent('keydown', { code: 'Space', bubbles: true, cancelable: true })
      const preventDefaultSpy = vi.spyOn(event, 'preventDefault')

      act(() => {
        window.dispatchEvent(event)
      })

      expect(toggleSpy).toHaveBeenCalledTimes(1)
      expect(preventDefaultSpy).toHaveBeenCalledTimes(1)
    })

    it('Space key does NOT call toggle when an <input> is focused', () => {
      const toggleSpy = vi.spyOn(engine, 'toggle')
      render(
        <>
          <PlayerBar />
          <input data-testid="text-input" />
        </>,
      )

      const input = screen.getByTestId('text-input')
      input.focus()

      act(() => {
        input.dispatchEvent(new KeyboardEvent('keydown', { code: 'Space', bubbles: true, cancelable: true }))
      })

      expect(toggleSpy).not.toHaveBeenCalled()
    })

    it('ArrowRight seeks forward 5 seconds', () => {
      const seekSpy = vi.spyOn(engine, 'seekMs')
      render(<PlayerBar />)

      act(() => {
        window.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true, cancelable: true }))
      })

      expect(seekSpy).toHaveBeenCalledTimes(1)
      expect(seekSpy).toHaveBeenCalledWith(5000)
    })

    it('Shift+ArrowRight calls next and does NOT call seekMs', () => {
      const nextSpy = vi.spyOn(engine, 'next')
      const seekSpy = vi.spyOn(engine, 'seekMs')
      render(<PlayerBar />)

      act(() => {
        window.dispatchEvent(
          new KeyboardEvent('keydown', { key: 'ArrowRight', shiftKey: true, bubbles: true, cancelable: true }),
        )
      })

      expect(nextSpy).toHaveBeenCalledTimes(1)
      expect(seekSpy).not.toHaveBeenCalled()
    })

    it('Shift+ArrowLeft calls prev and does NOT call seekMs', () => {
      const prevSpy = vi.spyOn(engine, 'prev')
      const seekSpy = vi.spyOn(engine, 'seekMs')
      render(<PlayerBar />)

      act(() => {
        window.dispatchEvent(
          new KeyboardEvent('keydown', { key: 'ArrowLeft', shiftKey: true, bubbles: true, cancelable: true }),
        )
      })

      expect(prevSpy).toHaveBeenCalledTimes(1)
      expect(seekSpy).not.toHaveBeenCalled()
    })
  })

  // --- dynamic tint ---

  describe('dynamic tint', () => {
    beforeEach(() => {
      act(() => {
        usePlayer.getState().playTrackList([track('1')], 0)
        useUI.getState().closePanel()
      })
      vi.mocked(useAlbumPalette).mockReset()
    })

    it('applies the dominant-color fill + contrast text when a palette is present', () => {
      vi.mocked(useAlbumPalette).mockReturnValue({ rgb: [200, 30, 40], text: '#FFFFFF', scrim: false })
      render(<PlayerBar />)
      const bar = screen.getByTestId('player-bar')
      expect(bar.style.backgroundColor).toBe('rgb(200, 30, 40)')
      expect(bar.style.color).toBe('rgb(255, 255, 255)')
    })

    it('falls back to the static look when there is no palette', () => {
      vi.mocked(useAlbumPalette).mockReturnValue(null)
      render(<PlayerBar />)
      const bar = screen.getByTestId('player-bar')
      expect(bar.style.backgroundColor).toBe('')
    })

    it('title does NOT have text-text-primary when a light palette is active (inherits palette.text)', () => {
      vi.mocked(useAlbumPalette).mockReturnValue({ rgb: [240, 240, 240], text: '#1a1a1a', scrim: false })
      render(<PlayerBar />)
      const title = screen.getByText('Song 1')
      expect(title.className).not.toContain('text-text-primary')
    })

    it('title DOES have text-text-primary when there is no palette', () => {
      vi.mocked(useAlbumPalette).mockReturnValue(null)
      render(<PlayerBar />)
      const title = screen.getByText('Song 1')
      expect(title.className).toContain('text-text-primary')
    })
  })
})
