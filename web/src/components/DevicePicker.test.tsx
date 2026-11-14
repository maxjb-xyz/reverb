import { afterEach, describe, expect, it } from 'vitest'
import { act, fireEvent, render, screen } from '@testing-library/react'
import { DevicePicker } from './DevicePicker'
import { handlePlaybackFrame, resetPlaybackSession, setPlaybackTransport, useSession } from '../lib/playbackSession'

let sent: Array<{ type: string; payload: unknown }> = []

function connectWith(devices: Array<{ id: string; name: string; kind: string }>, activeDeviceId = '') {
  sent = []
  act(() => {
    setPlaybackTransport((f) => sent.push(f))
    handlePlaybackFrame('playback.session', { devices, activeDeviceId, state: null, serverTime: Date.now() })
  })
}

afterEach(() => act(() => resetPlaybackSession()))

describe('DevicePicker', () => {
  it('is hidden without a live session', () => {
    render(<DevicePicker />)
    expect(screen.queryByTestId('device-button')).toBeNull()
  })

  it('lists this browser and the other devices, and moves playback on pick', () => {
    const me = useSession.getState().deviceId
    connectWith([
      { id: me, name: 'Chrome on macOS', kind: 'web' },
      { id: 'p1', name: 'Pixel', kind: 'phone' },
    ])
    render(<DevicePicker />)
    fireEvent.click(screen.getByTestId('device-button'))
    expect(screen.getByText('This browser')).toBeTruthy()
    fireEvent.click(screen.getByText('Pixel'))
    expect(sent.find((f) => f.type === 'playback.transfer')?.payload).toEqual({ deviceId: 'p1', play: true })
  })

  it('shows where music is playing when it is elsewhere, and can bring it back', () => {
    const me = useSession.getState().deviceId
    connectWith(
      [
        { id: me, name: 'Chrome on macOS', kind: 'web' },
        { id: 'p1', name: 'Pixel', kind: 'phone' },
      ],
      'p1',
    )
    render(<DevicePicker />)
    expect(screen.getByTestId('remote-device-label').textContent).toBe('Pixel')
    fireEvent.click(screen.getByTestId('device-button'))
    fireEvent.click(screen.getByText('This browser'))
    expect(sent.find((f) => f.type === 'playback.transfer')?.payload).toMatchObject({ deviceId: me })
  })

  it('stays out of the phone bar when there is nowhere else to play', () => {
    const me = useSession.getState().deviceId
    connectWith([{ id: me, name: 'Safari on iOS', kind: 'phone' }])
    render(<DevicePicker look="mini" />)
    expect(screen.queryByTestId('device-button')).toBeNull()
  })
})
