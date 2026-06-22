import { describe, it, expect, beforeEach } from 'vitest'
import { useLibraryRevision } from './libraryRevisionStore'

describe('useLibraryRevision', () => {
  beforeEach(() => {
    useLibraryRevision.setState({ revision: 0 })
  })

  it('starts at revision 0', () => {
    expect(useLibraryRevision.getState().revision).toBe(0)
  })

  it('increments revision by 1 each time bump() is called', () => {
    useLibraryRevision.getState().bump()
    expect(useLibraryRevision.getState().revision).toBe(1)

    useLibraryRevision.getState().bump()
    expect(useLibraryRevision.getState().revision).toBe(2)
  })
})
