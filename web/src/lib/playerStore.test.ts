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

describe('playerStore as a remote control', () => {
  it('shows the active device and sends actions to it instead of playing here', async () => {
    const { handlePlaybackFrame, setPlaybackTransport, useSession, resetPlaybackSession } = await import('./playbackSession')
    const { engine } = await import('./playerStore')
    const sent: Array<{ type: string; payload: unknown }> = []
    act(() => {
      usePlayer.getState().pause()
      setPlaybackTransport((f) => sent.push(f))
      handlePlaybackFrame('playback.session', {
        devices: [{ id: 'phone', name: 'Pixel', kind: 'phone' }],
        activeDeviceId: 'phone',
        state: { queue: [track('r1'), track('r2')], index: 1, positionMs: 0, playing: false, shuffle: false, repeat: 'off', volume: 1, updatedAt: Date.now() },
        serverTime: Date.now(),
      })
    })
    expect(usePlayer.getState().current?.id).toBe('r2')
    const before = engine.getState().current?.id
    act(() => {
      usePlayer.getState().next()
      usePlayer.getState().playTrackList([track('z')], 0, { kind: 'album', id: 'a1' })
      usePlayer.getState().seekMs(1234.4)
    })
    expect(engine.getState().current?.id).toBe(before)
    const cmds = sent.filter((f) => f.type === 'playback.command').map((f) => f.payload)
    expect(cmds).toEqual([
      { action: 'next' },
      { action: 'playTracks', tracks: [track('z')], index: 0, context: { kind: 'album', id: 'a1' } },
      { action: 'seek', positionMs: 1234 },
    ])
    act(() => {
      expect(useSession.getState().connected).toBe(true)
      setPlaybackTransport(null)
    })
    // Back to the local engine.
    expect(usePlayer.getState().current?.id).toBe(before)
    resetPlaybackSession()
  })
})
