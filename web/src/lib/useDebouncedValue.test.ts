import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useDebouncedValue } from './useDebouncedValue'

describe('useDebouncedValue', () => {
  afterEach(() => vi.useRealTimers())
  it('trails the input by delayMs', () => {
    vi.useFakeTimers()
    const { result, rerender } = renderHook(({ value }) => useDebouncedValue(value, 400), { initialProps: { value: 'a' } })
    rerender({ value: 'ab' })
    expect(result.current).toBe('a')
    act(() => vi.advanceTimersByTime(400))
    expect(result.current).toBe('ab')
  })
})
