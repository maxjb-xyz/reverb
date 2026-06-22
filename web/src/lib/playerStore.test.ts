import { describe, expect, it } from 'vitest'
import { act } from '@testing-library/react'
import { usePlayer } from './playerStore'
import type { Track } from './types'

function track(id: string): Track {
  return {
    id, title: 'T' + id, albumId: 'al', album: 'Album', artistId: 'ar', artist: 'Artist',
    coverArtId: 'co', trackNumber: 1, discNumber: 1, durationMs: 1000, bitRate: 320,
    suffix: 'mp3', contentType: 'audio/mpeg',
  }
}

describe('playerStore', () => {
  it('mirrors engine state into the store after playTrackList', () => {
    act(() => {
      usePlayer.getState().playTrackList([track('1'), track('2')], 0)
    })
    expect(usePlayer.getState().current?.id).toBe('1')
    expect(usePlayer.getState().queue.length).toBe(2)
  })

  it('next updates the mirrored current', () => {
    act(() => {
      usePlayer.getState().playTrackList([track('1'), track('2')], 0)
      usePlayer.getState().cycleRepeat() // off -> all so next wraps within 2 items
      usePlayer.getState().next()
    })
    expect(usePlayer.getState().current?.id).toBe('2')
  })

  it('next() with no next track (single-track queue, repeat off) leaves playing state unchanged', () => {
    act(() => {
      usePlayer.getState().playTrackList([track('1')], 0)
    })
    // Manually set playing to true via the engine (playTrackList triggers autoplay)
    // After playTrackList, engine emits playing: true
    // Now call next() — with repeat=off and a single track, there is no next
    act(() => {
      usePlayer.getState().next()
    })
    // playing should still be true (no desync) and current track unchanged
    expect(usePlayer.getState().current?.id).toBe('1')
    expect(usePlayer.getState().playing).toBe(true)
  })
})
