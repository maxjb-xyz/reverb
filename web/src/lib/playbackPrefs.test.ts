import { beforeEach, describe, expect, it } from 'vitest'
import { loadPlaybackPrefs, savePlaybackPrefs } from './playbackPrefs'

beforeEach(() => localStorage.clear())

describe('playbackPrefs', () => {
  it('defaults to no crossfade', () => {
    expect(loadPlaybackPrefs()).toEqual({ crossfadeMs: 0 })
  })

  it('round-trips through localStorage', () => {
    savePlaybackPrefs({ crossfadeMs: 6000 })
    expect(loadPlaybackPrefs()).toEqual({ crossfadeMs: 6000 })
  })

  it('ignores garbage and out-of-range values', () => {
    localStorage.setItem('reverb.playback.v1', '{not json')
    expect(loadPlaybackPrefs()).toEqual({ crossfadeMs: 0 })
    localStorage.setItem('reverb.playback.v1', JSON.stringify({ crossfadeMs: 99_000 }))
    expect(loadPlaybackPrefs()).toEqual({ crossfadeMs: 12_000 })
    localStorage.setItem('reverb.playback.v1', JSON.stringify({ crossfadeMs: 'x' }))
    expect(loadPlaybackPrefs()).toEqual({ crossfadeMs: 0 })
  })
})
