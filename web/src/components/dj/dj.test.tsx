import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, render, screen, fireEvent, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'

const get = vi.fn()
const post = vi.fn()
vi.mock('../../lib/api', () => ({
  api: { get: (p: string) => get(p), post: (p: string, b?: unknown) => post(p, b) },
}))
const playTrackList = vi.fn()
vi.mock('../../lib/playerStore', () => ({
  usePlayer: (sel: (s: { playTrackList: typeof playTrackList }) => unknown) => sel({ playTrackList }),
}))
const push = vi.fn()
vi.mock('../../lib/toastStore', () => ({
  useToastStore: (sel: (s: { push: typeof push }) => unknown) => sel({ push }),
}))

import { DJTile } from './DJTile'
import { DJBar } from './DJBar'
import { _resetDJ, useDJ } from '../../lib/dj'

const track = (id: string) => ({ id, title: `Song ${id}`, albumId: 'al', album: 'Album', artistId: 'ar', artist: 'Bonobo', coverArtId: 'c', trackNumber: 1, discNumber: 1, durationMs: 200000, bitRate: 320, suffix: 'mp3', contentType: 'audio/mpeg' })
const lane = (name: string, ids: string[]) => ({
  station: { id: `lane:${name}:taste`, kind: 'taste', lane: name, label: name === 'on-repeat' ? 'On repeat' : 'Sounds like Bonobo' },
  items: ids.map((id) => ({ owned: true, track: track(id), score: 1, reason: { kind: 'on_repeat', playCount: 9 } })),
})

function wrap(ui: ReactNode) {
  return render(<>{ui}</>)
}

beforeEach(() => {
  vi.clearAllMocks()
  get.mockImplementation(async (p: string) =>
    p === '/dj/status' ? { voice: { available: false, name: '' }, writer: { available: false, name: '' } } : null,
  )
  post.mockImplementation(async (p: string, b: { lane?: string }) => {
    if (p === '/engine/next') return b.lane === 'on-repeat' ? lane('on-repeat', ['a1', 'a2', 'a3', 'a4']) : lane(b.lane ?? '', ['b1', 'b2', 'b3', 'b4'])
    if (p === '/dj/line') return { text: 'You have had this on repeat.', source: 'template' }
    throw new Error(`unexpected ${p}`)
  })
})

afterEach(() => {
  act(() => _resetDJ())
})

describe('DJTile', () => {
  it('starts the DJ on the first lane', async () => {
    wrap(<DJTile />)
    fireEvent.click(screen.getByRole('button', { name: 'Start the DJ' }))
    await waitFor(() =>
      expect(playTrackList).toHaveBeenCalledWith([track('a1'), track('a2'), track('a3'), track('a4')], 0, { kind: 'dj', id: 'dj' }),
    )
    expect(post).toHaveBeenCalledWith('/engine/next', { lane: 'on-repeat', n: 4, exclude: [] })
    expect(useDJ.getState().active).toBe(true)
  })

  it('explains when there is nothing to play yet', async () => {
    post.mockImplementation(async () => ({ station: { id: 'x', kind: 'taste', label: 'x' }, items: [] }))
    wrap(<DJTile />)
    fireEvent.click(screen.getByRole('button', { name: 'Start the DJ' }))
    await waitFor(() => expect(push).toHaveBeenCalledWith(expect.stringMatching(/Not enough in your library for the DJ/), 'error'))
    expect(playTrackList).not.toHaveBeenCalled()
  })
})

describe('DJBar', () => {
  it('is hidden while the DJ is off', () => {
    wrap(<DJBar />)
    expect(screen.queryByTestId('dj-bar')).toBeNull()
  })

  it('shows the lane and the latest line, and turns the DJ off', () => {
    act(() => useDJ.setState({ active: true, label: 'On repeat', card: { key: 1, label: 'On repeat', text: 'Nine plays this week.' } }))
    wrap(<DJBar />)
    expect(screen.getByTestId('dj-lane')).toHaveTextContent('On repeat')
    expect(screen.getByTestId('dj-line')).toHaveTextContent('Nine plays this week.')
    fireEvent.click(screen.getByRole('button', { name: 'Turn off the DJ' }))
    expect(screen.queryByTestId('dj-bar')).toBeNull()
  })

  it('changes it up to the next lane', async () => {
    wrap(<><DJTile /><DJBar /></>)
    fireEvent.click(screen.getByRole('button', { name: 'Start the DJ' }))
    await screen.findByTestId('dj-bar')
    fireEvent.click(screen.getByRole('button', { name: 'Change it up' }))
    await waitFor(() => expect(screen.getByTestId('dj-lane')).toHaveTextContent('Sounds like Bonobo'))
    expect(playTrackList).toHaveBeenLastCalledWith([track('b1'), track('b2'), track('b3'), track('b4')], 0, { kind: 'dj', id: 'dj' })
  })
})
