import { describe, expect, it, beforeEach, vi, afterEach } from 'vitest'
import { render, screen, fireEvent, act } from '@testing-library/react'
import { PlayerBar } from './PlayerBar'
import { usePlayer, engine } from '../lib/playerStore'
import { useUI } from '../lib/uiStore'
import type { Track } from '../lib/types'

function track(id: string): Track {
  return {
    id, title: 'Song ' + id, albumId: 'al', album: 'Album', artistId: 'ar', artist: 'Artist',
    coverArtId: 'co', trackNumber: 1, discNumber: 1, durationMs: 200000, bitRate: 320,
    suffix: 'mp3', contentType: 'audio/mpeg',
  }
}

describe('PlayerBar', () => {
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

  it('Queue button toggles the right panel', () => {
    render(<PlayerBar />)
    fireEvent.click(screen.getByRole('button', { name: /queue/i }))
    expect(useUI.getState().rightPanel).toBe('queue')
  })

  it('Downloads button toggles the downloads panel', () => {
    render(<PlayerBar />)
    const btn = screen.getByRole('button', { name: /downloads/i })
    expect(btn).not.toBeDisabled()
    fireEvent.click(btn)
    expect(useUI.getState().rightPanel).toBe('downloads')
  })

  // --- transport button tests ---

  it('play/pause button click calls toggle on the engine', () => {
    const spy = vi.spyOn(engine, 'toggle')
    render(<PlayerBar />)
    fireEvent.click(screen.getByRole('button', { name: /play|pause/i }))
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

  // --- keyboard shortcut tests ---

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
      // currentTimeMs starts at 0; seek should be 0 + 5000 = 5000
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

  // --- seek bar click test ---

  it('seek bar click calls seekMs with ratio * durationMs', () => {
    // jsdom never fires durationchange so engine.durationMs stays 0.
    // Seed it directly via private field access so the SeekBar renders and
    // its onClick guard (durationMs <= 0) doesn't short-circuit.
    const eng = engine as unknown as { durationMs: number; emit(): void }
    act(() => {
      eng.durationMs = 200000
      eng.emit()
    })

    const seekSpy = vi.spyOn(engine, 'seekMs')
    render(<PlayerBar />)

    const seekBar = screen.getByRole('slider', { name: /seek/i })

    // jsdom returns zeros for getBoundingClientRect; stub it to a known rect
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

    // Click at clientX=300 → ratio = (300-100)/400 = 0.5; durationMs=200000
    act(() => {
      fireEvent.click(seekBar, { clientX: 300 })
    })

    expect(seekSpy).toHaveBeenCalledTimes(1)
    expect(seekSpy).toHaveBeenCalledWith(100000) // 0.5 * 200000
    seekSpy.mockRestore()

    // restore durationMs to avoid leaking into other tests
    act(() => {
      eng.durationMs = 0
      eng.emit()
    })
  })
})
