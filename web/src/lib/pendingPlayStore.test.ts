import { describe, it, expect, beforeEach } from 'vitest'
import { usePendingPlay } from './pendingPlayStore'

describe('usePendingPlay store', () => {
  beforeEach(() => {
    // Reset store state before each test
    usePendingPlay.setState({ pending: null })
  })

  it('begin sets pending with jobId, progress=-1, failed=false', () => {
    usePendingPlay.getState().begin({
      jobId: 'job-123',
      title: 'Test Track',
      artist: 'Test Artist',
    })

    const state = usePendingPlay.getState()
    expect(state.pending).toBeDefined()
    expect(state.pending?.jobId).toBe('job-123')
    expect(state.pending?.progress).toBe(-1)
    expect(state.pending?.failed).toBe(false)
    expect(state.pending?.title).toBe('Test Track')
    expect(state.pending?.artist).toBe('Test Artist')
  })

  it('begin accepts optional coverArtId', () => {
    usePendingPlay.getState().begin({
      jobId: 'job-456',
      title: 'Album Track',
      artist: 'Album Artist',
      coverArtId: 'art-789',
    })

    const state = usePendingPlay.getState()
    expect(state.pending?.coverArtId).toBe('art-789')
  })

  it('update with matching jobId sets progress', () => {
    usePendingPlay.getState().begin({
      jobId: 'job-123',
      title: 'Test',
      artist: 'Artist',
    })

    usePendingPlay.getState().update('job-123', 50)

    const state = usePendingPlay.getState()
    expect(state.pending?.progress).toBe(50)
    expect(state.pending?.failed).toBe(false)
  })

  it('update with wrong jobId is a no-op', () => {
    usePendingPlay.getState().begin({
      jobId: 'job-123',
      title: 'Test',
      artist: 'Artist',
    })

    const originalProgress = usePendingPlay.getState().pending?.progress

    usePendingPlay.getState().update('job-wrong', 50)

    const state = usePendingPlay.getState()
    expect(state.pending?.progress).toBe(originalProgress)
    expect(state.pending?.jobId).toBe('job-123')
  })

  it('fail with matching jobId flips failed to true', () => {
    usePendingPlay.getState().begin({
      jobId: 'job-123',
      title: 'Test',
      artist: 'Artist',
    })

    expect(usePendingPlay.getState().pending?.failed).toBe(false)

    usePendingPlay.getState().fail('job-123')

    expect(usePendingPlay.getState().pending?.failed).toBe(true)
  })

  it('fail with wrong jobId is a no-op', () => {
    usePendingPlay.getState().begin({
      jobId: 'job-123',
      title: 'Test',
      artist: 'Artist',
    })

    usePendingPlay.getState().fail('job-wrong')

    const state = usePendingPlay.getState()
    expect(state.pending?.failed).toBe(false)
    expect(state.pending?.jobId).toBe('job-123')
  })

  it('clear with matching jobId nulls pending', () => {
    usePendingPlay.getState().begin({
      jobId: 'job-123',
      title: 'Test',
      artist: 'Artist',
    })

    expect(usePendingPlay.getState().pending).not.toBeNull()

    usePendingPlay.getState().clear('job-123')

    expect(usePendingPlay.getState().pending).toBeNull()
  })

  it('clear with wrong jobId keeps pending', () => {
    usePendingPlay.getState().begin({
      jobId: 'job-123',
      title: 'Test',
      artist: 'Artist',
    })

    usePendingPlay.getState().clear('job-wrong')

    const state = usePendingPlay.getState()
    expect(state.pending).not.toBeNull()
    expect(state.pending?.jobId).toBe('job-123')
  })

  it('full lifecycle: begin → update → fail → clear', () => {
    usePendingPlay.getState().begin({
      jobId: 'job-full',
      title: 'Full Test',
      artist: 'Full Artist',
    })

    let state = usePendingPlay.getState()
    expect(state.pending?.progress).toBe(-1)
    expect(state.pending?.failed).toBe(false)

    usePendingPlay.getState().update('job-full', 75)
    state = usePendingPlay.getState()
    expect(state.pending?.progress).toBe(75)

    usePendingPlay.getState().fail('job-full')
    state = usePendingPlay.getState()
    expect(state.pending?.failed).toBe(true)

    usePendingPlay.getState().clear('job-full')
    state = usePendingPlay.getState()
    expect(state.pending).toBeNull()
  })
})
