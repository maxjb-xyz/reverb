import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { useAlbumPalette } from './useAlbumPalette'
import { __setComputeFnForTests, __resetForTests } from './paletteService'
import type { RGB } from './palette'

// Mock useSettings so the gate is controllable.
vi.mock('./settingsApi', () => ({
  useSettings: vi.fn(),
}))
import { useSettings } from './settingsApi'

function setSettings(dynamicBackground: boolean | undefined) {
  vi.mocked(useSettings).mockReturnValue({
    data: dynamicBackground === undefined ? undefined : { accentColor: '#F0354B', dynamicBackground },
  } as ReturnType<typeof useSettings>)
}

describe('useAlbumPalette', () => {
  beforeEach(() => {
    __resetForTests()
    __setComputeFnForTests(async () => [200, 30, 40] as RGB)
  })

  it('returns null while settings are still loading', () => {
    setSettings(undefined)
    const { result } = renderHook(() => useAlbumPalette('/cover/a'))
    expect(result.current).toBeNull()
  })

  it('returns null when dynamic_background is off', () => {
    setSettings(false)
    const { result } = renderHook(() => useAlbumPalette('/cover/a'))
    expect(result.current).toBeNull()
  })

  it('returns null for an empty cover URL', () => {
    setSettings(true)
    const { result } = renderHook(() => useAlbumPalette(''))
    expect(result.current).toBeNull()
  })

  it('resolves the palette with contrast text when on', async () => {
    setSettings(true)
    const { result } = renderHook(() => useAlbumPalette('/cover/a'))
    await waitFor(() => expect(result.current).not.toBeNull())
    expect(result.current?.rgb).toEqual([200, 30, 40])
    // luminance of (200,30,40) < 0.5 → light text
    expect(result.current?.text).toBe('#FFFFFF')
    expect(typeof result.current?.scrim).toBe('boolean')
  })

  it('does not show the previous cover’s palette while a new cover is extracting', async () => {
    setSettings(true)
    // Gate the compute so /cover/b stays pending after /cover/a has resolved.
    let releaseB: (v: RGB) => void = () => {}
    __setComputeFnForTests((url) =>
      url === '/cover/a'
        ? Promise.resolve([200, 30, 40] as RGB)
        : new Promise<RGB>((res) => {
            releaseB = res
          }),
    )

    const { result, rerender } = renderHook(({ url }) => useAlbumPalette(url), {
      initialProps: { url: '/cover/a' },
    })
    await waitFor(() => expect(result.current?.rgb).toEqual([200, 30, 40]))

    // Switch to /cover/b: until B resolves the hook must return null, NOT A's color.
    rerender({ url: '/cover/b' })
    expect(result.current).toBeNull()

    // Once B resolves, its palette appears.
    releaseB([10, 120, 220])
    await waitFor(() => expect(result.current?.rgb).toEqual([10, 120, 220]))
  })
})
