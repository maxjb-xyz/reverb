import { describe, expect, it, beforeEach } from 'vitest'
import { render, screen, fireEvent, act } from '@testing-library/react'
import { PlayerBar } from './PlayerBar'
import { usePlayer } from '../lib/playerStore'
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

  it('Downloads button is disabled (M3 placeholder)', () => {
    render(<PlayerBar />)
    expect(screen.getByRole('button', { name: /downloads/i })).toBeDisabled()
  })
})
