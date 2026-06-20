import { describe, expect, it, beforeEach } from 'vitest'
import { render, screen, fireEvent, act } from '@testing-library/react'
import { PlayQueue } from './PlayQueue'
import { usePlayer } from '../lib/playerStore'
import { useUI } from '../lib/uiStore'
import type { Track } from '../lib/types'

function track(id: string): Track {
  return {
    id, title: 'Song ' + id, albumId: 'al', album: 'Album', artistId: 'ar', artist: 'Artist',
    coverArtId: 'co', trackNumber: 1, discNumber: 1, durationMs: 1000, bitRate: 320,
    suffix: 'mp3', contentType: 'audio/mpeg',
  }
}

describe('PlayQueue', () => {
  beforeEach(() => {
    act(() => {
      usePlayer.getState().playTrackList([track('1'), track('2'), track('3')], 0)
      useUI.getState().openPanel('queue')
    })
  })

  it('renders the now-playing header and up-next items', () => {
    render(<PlayQueue />)
    expect(screen.getByText('Now Playing')).toBeInTheDocument()
    expect(screen.getByText('Song 1')).toBeInTheDocument()
    expect(screen.getByText('Song 2')).toBeInTheDocument()
  })

  it('remove drops a track from the queue', () => {
    render(<PlayQueue />)
    const removeButtons = screen.getAllByRole('button', { name: /remove/i })
    fireEvent.click(removeButtons[removeButtons.length - 1])
    expect(usePlayer.getState().queue.length).toBe(2)
  })

  it('is hidden when the panel is closed', () => {
    act(() => useUI.getState().closePanel())
    const { container } = render(<PlayQueue />)
    expect(container.firstChild).toBeNull()
  })
})
