import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest'
import { useToastStore } from './toastStore'

describe('useToastStore', () => {
  beforeEach(() => {
    useToastStore.setState({ toasts: [] })
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('push adds a toast with a unique id, message, and kind', () => {
    useToastStore.getState().push('Hello world', 'success')

    const { toasts } = useToastStore.getState()
    expect(toasts).toHaveLength(1)
    expect(toasts[0].message).toBe('Hello world')
    expect(toasts[0].kind).toBe('success')
    expect(toasts[0].id).toBeTruthy()
  })

  it('push generates unique ids for multiple toasts', () => {
    useToastStore.getState().push('First', 'info')
    useToastStore.getState().push('Second', 'error')

    const { toasts } = useToastStore.getState()
    expect(toasts).toHaveLength(2)
    expect(toasts[0].id).not.toBe(toasts[1].id)
  })

  it('dismiss removes the toast with the given id', () => {
    useToastStore.getState().push('Removable', 'info')
    const id = useToastStore.getState().toasts[0].id
    useToastStore.getState().dismiss(id)

    expect(useToastStore.getState().toasts).toHaveLength(0)
  })

  it('dismiss leaves other toasts intact', () => {
    useToastStore.getState().push('Keep me', 'success')
    useToastStore.getState().push('Remove me', 'error')
    const toasts = useToastStore.getState().toasts
    const removeId = toasts[1].id

    useToastStore.getState().dismiss(removeId)

    const remaining = useToastStore.getState().toasts
    expect(remaining).toHaveLength(1)
    expect(remaining[0].message).toBe('Keep me')
  })

  it('push auto-dismisses the toast after 5 seconds', () => {
    useToastStore.getState().push('Auto-gone', 'info')
    expect(useToastStore.getState().toasts).toHaveLength(1)

    vi.advanceTimersByTime(5000)

    expect(useToastStore.getState().toasts).toHaveLength(0)
  })
})
