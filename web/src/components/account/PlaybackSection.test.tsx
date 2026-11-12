import { beforeEach, describe, expect, it } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { PlaybackSection } from './PlaybackSection'
import { usePlayer } from '../../lib/playerStore'
import { loadPlaybackPrefs } from '../../lib/playbackPrefs'

beforeEach(() => {
  localStorage.clear()
  usePlayer.getState().setCrossfade(0)
})

describe('PlaybackSection', () => {
  it('shows crossfade off by default', () => {
    render(<PlaybackSection />)
    expect(screen.getByRole('tab', { name: 'Off' })).toHaveAttribute('aria-selected', 'true')
  })

  it('sets the crossfade on the player and remembers it on this device', () => {
    render(<PlaybackSection />)
    fireEvent.click(screen.getByRole('tab', { name: '6 s' }))
    expect(usePlayer.getState().crossfadeMs).toBe(6000)
    expect(loadPlaybackPrefs().crossfadeMs).toBe(6000)
    expect(screen.getByRole('tab', { name: '6 s' })).toHaveAttribute('aria-selected', 'true')
  })
})
