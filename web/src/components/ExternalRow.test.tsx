import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { ExternalRow } from './ExternalRow'
import type { ExternalResult } from '../lib/types'

const play = vi.fn()
vi.mock('../lib/playerStore', () => ({
  usePlayer: (sel: (s: { playTrackList: typeof play }) => unknown) => sel({ playTrackList: play }),
}))

function res(p: Partial<ExternalResult>): ExternalResult {
  return {
    source: 'spotify', externalId: 'sp1', title: 'Song', artist: 'Artist', album: 'Album',
    durationMs: 200000, type: 'track', ...p,
  }
}

describe('ExternalRow', () => {
  beforeEach(() => play.mockClear())

  it('shows in-library check and plays the matched track on click', () => {
    render(<ExternalRow result={res({ match: { status: 'in_library', libraryTrackId: 't1', method: 'isrc', confidence: 1 } })} />)
    expect(screen.getByTitle(/in library/i)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button'))
    expect(play).toHaveBeenCalledTimes(1)
    const arg = play.mock.calls[0][0] as Array<{ id: string }>
    expect(arg[0].id).toBe('t1')
  })

  it('renders a plain non-button row when not in library', () => {
    render(<ExternalRow result={res({ match: { status: 'not_in_library', libraryTrackId: '', method: 'none', confidence: 0 } })} />)
    expect(screen.queryByRole('button')).toBeNull()
    expect(screen.getByText('Song')).toBeInTheDocument()
  })
})
