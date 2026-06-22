import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { useLibraryRevision } from './libraryRevisionStore'

describe('useLibraryRevision', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    useLibraryRevision.setState({ revision: 0 })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('starts at revision 0', () => {
    expect(useLibraryRevision.getState().revision).toBe(0)
  })

  it('increments revision by 1 each time bump() is called', () => {
    useLibraryRevision.getState().bump()
    vi.advanceTimersByTime(300)
    expect(useLibraryRevision.getState().revision).toBe(1)

    useLibraryRevision.getState().bump()
    vi.advanceTimersByTime(300)
    expect(useLibraryRevision.getState().revision).toBe(2)
  })

  describe('debounce', () => {
    it('collapses rapid bump() calls into a single increment', () => {
      const { bump } = useLibraryRevision.getState()
      bump()
      bump()
      bump()
      bump()
      bump()
      // debounce hasn't fired yet
      expect(useLibraryRevision.getState().revision).toBe(0)
      // advance past debounce window
      vi.advanceTimersByTime(300)
      // exactly one increment regardless of how many times bump() was called
      expect(useLibraryRevision.getState().revision).toBe(1)
    })

    it('a single bump() fires after 300ms', () => {
      useLibraryRevision.getState().bump()
      expect(useLibraryRevision.getState().revision).toBe(0)
      vi.advanceTimersByTime(300)
      expect(useLibraryRevision.getState().revision).toBe(1)
    })
  })
})
